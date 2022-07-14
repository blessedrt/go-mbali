// Copyright 2021 The go-mbali Authors
// This file is part of the go-mbali library.
//
// The go-mbali library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-mbali library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-mbali library. If not, see <http://www.gnu.org/licenses/>.

// Package catalyst implements the temporary mbl1/mbl2 RPC integration.
package catalyst

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/common/hexutil"
	"github.com/mbali/go-mbali/core/beacon"
	"github.com/mbali/go-mbali/core/rawdb"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/mbl"
	"github.com/mbali/go-mbali/log"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/rpc"
)

// Register adds catalyst APIs to the full node.
func Register(stack *node.Node, backend *mbl.mbali) error {
	log.Warn("Catalyst mode enabled", "protocol", "mbl")
	stack.RegisterAPIs([]rpc.API{
		{
			Namespace:     "engine",
			Version:       "1.0",
			Service:       NewConsensusAPI(backend),
			Public:        true,
			Authenticated: true,
		},
		{
			Namespace:     "engine",
			Version:       "1.0",
			Service:       NewConsensusAPI(backend),
			Public:        true,
			Authenticated: false,
		},
	})
	return nil
}

type ConsensusAPI struct {
	mbl          *mbl.mbali
	remoteBlocks *headerQueue  // Cache of remote payloads received
	localBlocks  *payloadQueue // Cache of local payloads generated
	// Lock for the forkChoiceUpdated mmblod
	forkChoiceLock sync.Mutex
}

// NewConsensusAPI creates a new consensus api for the given backend.
// The underlying blockchain needs to have a valid terminal total difficulty set.
func NewConsensusAPI(mbl *mbl.mbali) *ConsensusAPI {
	if mbl.BlockChain().Config().TerminalTotalDifficulty == nil {
		panic("Catalyst started without valid total difficulty")
	}
	return &ConsensusAPI{
		mbl:          mbl,
		remoteBlocks: newHeaderQueue(),
		localBlocks:  newPayloadQueue(),
	}
}

// ForkchoiceUpdatedV1 has several responsibilities:
// If the mmblod is called with an empty head block:
// 		we return success, which can be used to check if the catalyst mode is enabled
// If the total difficulty was not reached:
// 		we return INVALID
// If the finalizedBlockHash is set:
// 		we check if we have the finalizedBlockHash in our db, if not we start a sync
// We try to set our blockchain to the headBlock
// If there are payloadAttributes:
// 		we try to assemble a block with the payloadAttributes and return its payloadID
func (api *ConsensusAPI) ForkchoiceUpdatedV1(update beacon.ForkchoiceStateV1, payloadAttributes *beacon.PayloadAttributesV1) (beacon.ForkChoiceResponse, error) {
	api.forkChoiceLock.Lock()
	defer api.forkChoiceLock.Unlock()

	log.Trace("Engine API request received", "mmblod", "ForkchoiceUpdated", "head", update.HeadBlockHash, "finalized", update.FinalizedBlockHash, "safe", update.SafeBlockHash)
	if update.HeadBlockHash == (common.Hash{}) {
		log.Warn("Forkchoice requested update to zero hash")
		return beacon.STATUS_INVALID, nil // TODO(karalabe): Why does someone send us this?
	}

	// Check whmbler we have the block yet in our database or not. If not, we'll
	// need to either trigger a sync, or to reject this forkchoice update for a
	// reason.
	block := api.mbl.BlockChain().GetBlockByHash(update.HeadBlockHash)
	if block == nil {
		// If the head hash is unknown (was not given to us in a newPayload request),
		// we cannot resolve the header, so not much to do. This could be extended in
		// the future to resolve from the `mbl` network, but it's an unexpected case
		// that should be fixed, not papered over.
		header := api.remoteBlocks.get(update.HeadBlockHash)
		if header == nil {
			log.Warn("Forkchoice requested unknown head", "hash", update.HeadBlockHash)
			return beacon.STATUS_SYNCING, nil
		}
		// Header advertised via a past newPayload request. Start syncing to it.
		// Before we do however, make sure any legacy sync in switched off so we
		// don't accidentally have 2 cycles running.
		if merger := api.mbl.Merger(); !merger.TDDReached() {
			merger.ReachTTD()
			api.mbl.Downloader().Cancel()
		}
		log.Info("Forkchoice requested sync to new head", "number", header.Number, "hash", header.Hash())
		if err := api.mbl.Downloader().BeaconSync(api.mbl.SyncMode(), header); err != nil {
			return beacon.STATUS_SYNCING, err
		}
		return beacon.STATUS_SYNCING, nil
	}
	// Block is known locally, just sanity check that the beacon client does not
	// attempt to push us back to before the merge.
	if block.Difficulty().BitLen() > 0 || block.NumberU64() == 0 {
		var (
			td  = api.mbl.BlockChain().GetTd(update.HeadBlockHash, block.NumberU64())
			ptd = api.mbl.BlockChain().GetTd(block.ParentHash(), block.NumberU64()-1)
			ttd = api.mbl.BlockChain().Config().TerminalTotalDifficulty
		)
		if td == nil || (block.NumberU64() > 0 && ptd == nil) {
			log.Error("TDs unavailable for TTD check", "number", block.NumberU64(), "hash", update.HeadBlockHash, "td", td, "parent", block.ParentHash(), "ptd", ptd)
			return beacon.STATUS_INVALID, errors.New("TDs unavailable for TDD check")
		}
		if td.Cmp(ttd) < 0 {
			log.Error("Refusing beacon update to pre-merge", "number", block.NumberU64(), "hash", update.HeadBlockHash, "diff", block.Difficulty(), "age", common.PrettyAge(time.Unix(int64(block.Time()), 0)))
			return beacon.ForkChoiceResponse{PayloadStatus: beacon.INVALID_TERMINAL_BLOCK, PayloadID: nil}, nil
		}
		if block.NumberU64() > 0 && ptd.Cmp(ttd) >= 0 {
			log.Error("Parent block is already post-ttd", "number", block.NumberU64(), "hash", update.HeadBlockHash, "diff", block.Difficulty(), "age", common.PrettyAge(time.Unix(int64(block.Time()), 0)))
			return beacon.ForkChoiceResponse{PayloadStatus: beacon.INVALID_TERMINAL_BLOCK, PayloadID: nil}, nil
		}
	}

	if rawdb.ReadCanonicalHash(api.mbl.ChainDb(), block.NumberU64()) != update.HeadBlockHash {
		// Block is not canonical, set head.
		if latestValid, err := api.mbl.BlockChain().SetCanonical(block); err != nil {
			return beacon.ForkChoiceResponse{PayloadStatus: beacon.PayloadStatusV1{Status: beacon.INVALID, LatestValidHash: &latestValid}}, err
		}
	} else {
		// If the head block is already in our canonical chain, the beacon client is
		// probably resyncing. Ignore the update.
		log.Info("Ignoring beacon update to old head", "number", block.NumberU64(), "hash", update.HeadBlockHash, "age", common.PrettyAge(time.Unix(int64(block.Time()), 0)), "have", api.mbl.BlockChain().CurrentBlock().NumberU64())
	}
	api.mbl.SetSynced()

	// If the beacon client also advertised a finalized block, mark the local
	// chain final and completely in PoS mode.
	if update.FinalizedBlockHash != (common.Hash{}) {
		if merger := api.mbl.Merger(); !merger.PoSFinalized() {
			merger.FinalizePoS()
		}
		// If the finalized block is not in our canonical tree, sommblings wrong
		finalBlock := api.mbl.BlockChain().GetBlockByHash(update.FinalizedBlockHash)
		if finalBlock == nil {
			log.Warn("Final block not available in database", "hash", update.FinalizedBlockHash)
			return beacon.STATUS_INVALID, beacon.InvalidForkChoiceState.With(errors.New("final block not available in database"))
		} else if rawdb.ReadCanonicalHash(api.mbl.ChainDb(), finalBlock.NumberU64()) != update.FinalizedBlockHash {
			log.Warn("Final block not in canonical chain", "number", block.NumberU64(), "hash", update.HeadBlockHash)
			return beacon.STATUS_INVALID, beacon.InvalidForkChoiceState.With(errors.New("final block not in canonical chain"))
		}
		// Set the finalized block
		api.mbl.BlockChain().SetFinalized(finalBlock)
	}
	// Check if the safe block hash is in our canonical tree, if not sommblings wrong
	if update.SafeBlockHash != (common.Hash{}) {
		safeBlock := api.mbl.BlockChain().GetBlockByHash(update.SafeBlockHash)
		if safeBlock == nil {
			log.Warn("Safe block not available in database")
			return beacon.STATUS_INVALID, beacon.InvalidForkChoiceState.With(errors.New("safe block not available in database"))
		}
		if rawdb.ReadCanonicalHash(api.mbl.ChainDb(), safeBlock.NumberU64()) != update.SafeBlockHash {
			log.Warn("Safe block not in canonical chain")
			return beacon.STATUS_INVALID, beacon.InvalidForkChoiceState.With(errors.New("safe block not in canonical chain"))
		}
	}
	valid := func(id *beacon.PayloadID) beacon.ForkChoiceResponse {
		return beacon.ForkChoiceResponse{
			PayloadStatus: beacon.PayloadStatusV1{Status: beacon.VALID, LatestValidHash: &update.HeadBlockHash},
			PayloadID:     id,
		}
	}
	// If payload generation was requested, create a new block to be potentially
	// sealed by the beacon client. The payload will be requested later, and we
	// might replace it arbitrarily many times in between.
	if payloadAttributes != nil {
		// Create an empty block first which can be used as a fallback
		empty, err := api.mbl.Miner().GetSealingBlockSync(update.HeadBlockHash, payloadAttributes.Timestamp, payloadAttributes.SuggestedFeeRecipient, payloadAttributes.Random, true)
		if err != nil {
			log.Error("Failed to create empty sealing payload", "err", err)
			return valid(nil), beacon.InvalidPayloadAttributes.With(err)
		}
		// Send a request to generate a full block in the background.
		// The result can be obtained via the returned channel.
		resCh, err := api.mbl.Miner().GetSealingBlockAsync(update.HeadBlockHash, payloadAttributes.Timestamp, payloadAttributes.SuggestedFeeRecipient, payloadAttributes.Random, false)
		if err != nil {
			log.Error("Failed to create async sealing payload", "err", err)
			return valid(nil), beacon.InvalidPayloadAttributes.With(err)
		}
		id := computePayloadId(update.HeadBlockHash, payloadAttributes)
		api.localBlocks.put(id, &payload{empty: empty, result: resCh})
		return valid(&id), nil
	}
	return valid(nil), nil
}

// ExchangeTransitionConfigurationV1 checks the given configuration against
// the configuration of the node.
func (api *ConsensusAPI) ExchangeTransitionConfigurationV1(config beacon.TransitionConfigurationV1) (*beacon.TransitionConfigurationV1, error) {
	if config.TerminalTotalDifficulty == nil {
		return nil, errors.New("invalid terminal total difficulty")
	}
	ttd := api.mbl.BlockChain().Config().TerminalTotalDifficulty
	if ttd.Cmp(config.TerminalTotalDifficulty.ToInt()) != 0 {
		log.Warn("Invalid TTD configured", "gombl", ttd, "beacon", config.TerminalTotalDifficulty)
		return nil, fmt.Errorf("invalid ttd: execution %v consensus %v", ttd, config.TerminalTotalDifficulty)
	}

	if config.TerminalBlockHash != (common.Hash{}) {
		if hash := api.mbl.BlockChain().GetCanonicalHash(uint64(config.TerminalBlockNumber)); hash == config.TerminalBlockHash {
			return &beacon.TransitionConfigurationV1{
				TerminalTotalDifficulty: (*hexutil.Big)(ttd),
				TerminalBlockHash:       config.TerminalBlockHash,
				TerminalBlockNumber:     config.TerminalBlockNumber,
			}, nil
		}
		return nil, fmt.Errorf("invalid terminal block hash")
	}
	return &beacon.TransitionConfigurationV1{TerminalTotalDifficulty: (*hexutil.Big)(ttd)}, nil
}

// GetPayloadV1 returns a cached payload by id.
func (api *ConsensusAPI) GetPayloadV1(payloadID beacon.PayloadID) (*beacon.ExecutableDataV1, error) {
	log.Trace("Engine API request received", "mmblod", "GetPayload", "id", payloadID)
	data := api.localBlocks.get(payloadID)
	if data == nil {
		return nil, beacon.UnknownPayload
	}
	return data, nil
}

// NewPayloadV1 creates an mbl1 block, inserts it in the chain, and returns the status of the chain.
func (api *ConsensusAPI) NewPayloadV1(params beacon.ExecutableDataV1) (beacon.PayloadStatusV1, error) {
	log.Trace("Engine API request received", "mmblod", "ExecutePayload", "number", params.Number, "hash", params.BlockHash)
	block, err := beacon.ExecutableDataToBlock(params)
	if err != nil {
		log.Debug("Invalid NewPayload params", "params", params, "error", err)
		return beacon.PayloadStatusV1{Status: beacon.INVALIDBLOCKHASH}, nil
	}
	// If we already have the block locally, ignore the entire execution and just
	// return a fake success.
	if block := api.mbl.BlockChain().GetBlockByHash(params.BlockHash); block != nil {
		log.Warn("Ignoring already known beacon payload", "number", params.Number, "hash", params.BlockHash, "age", common.PrettyAge(time.Unix(int64(block.Time()), 0)))
		hash := block.Hash()
		return beacon.PayloadStatusV1{Status: beacon.VALID, LatestValidHash: &hash}, nil
	}
	// If the parent is missing, we - in theory - could trigger a sync, but that
	// would also entail a reorg. That is problematic if multiple sibling blocks
	// are being fed to us, and even more so, if some semi-distant uncle shortens
	// our live chain. As such, payload execution will not permit reorgs and thus
	// will not trigger a sync cycle. That is fine though, if we get a fork choice
	// update after legit payload executions.
	parent := api.mbl.BlockChain().GetBlock(block.ParentHash(), block.NumberU64()-1)
	if parent == nil {
		// Stash the block away for a potential forced forckchoice update to it
		// at a later time.
		api.remoteBlocks.put(block.Hash(), block.Header())

		// Although we don't want to trigger a sync, if there is one already in
		// progress, try to extend if with the current payload request to relieve
		// some strain from the forkchoice update.
		if err := api.mbl.Downloader().BeaconExtend(api.mbl.SyncMode(), block.Header()); err == nil {
			log.Debug("Payload accepted for sync extension", "number", params.Number, "hash", params.BlockHash)
			return beacon.PayloadStatusV1{Status: beacon.SYNCING}, nil
		}
		// Either no beacon sync was started yet, or it rejected the delivered
		// payload as non-integratable on top of the existing sync. We'll just
		// have to rely on the beacon client to forcefully update the head with
		// a forkchoice update request.
		log.Warn("Ignoring payload with missing parent", "number", params.Number, "hash", params.BlockHash, "parent", params.ParentHash)
		return beacon.PayloadStatusV1{Status: beacon.ACCEPTED}, nil
	}
	// We have an existing parent, do some sanity checks to avoid the beacon client
	// triggering too early
	var (
		ptd  = api.mbl.BlockChain().GetTd(parent.Hash(), parent.NumberU64())
		ttd  = api.mbl.BlockChain().Config().TerminalTotalDifficulty
		gptd = api.mbl.BlockChain().GetTd(parent.ParentHash(), parent.NumberU64()-1)
	)
	if ptd.Cmp(ttd) < 0 {
		log.Warn("Ignoring pre-merge payload", "number", params.Number, "hash", params.BlockHash, "td", ptd, "ttd", ttd)
		return beacon.INVALID_TERMINAL_BLOCK, nil
	}
	if parent.Difficulty().BitLen() > 0 && gptd != nil && gptd.Cmp(ttd) >= 0 {
		log.Error("Ignoring pre-merge parent block", "number", params.Number, "hash", params.BlockHash, "td", ptd, "ttd", ttd)
		return beacon.INVALID_TERMINAL_BLOCK, nil
	}
	if block.Time() <= parent.Time() {
		log.Warn("Invalid timestamp", "parent", block.Time(), "block", block.Time())
		return api.invalid(errors.New("invalid timestamp"), parent), nil
	}
	if !api.mbl.BlockChain().HasBlockAndState(block.ParentHash(), block.NumberU64()-1) {
		api.remoteBlocks.put(block.Hash(), block.Header())
		log.Warn("State not available, ignoring new payload")
		return beacon.PayloadStatusV1{Status: beacon.ACCEPTED}, nil
	}
	log.Trace("Inserting block without smblead", "hash", block.Hash(), "number", block.Number)
	if err := api.mbl.BlockChain().InsertBlockWithoutSmblead(block); err != nil {
		log.Warn("NewPayloadV1: inserting block failed", "error", err)
		return api.invalid(err, parent), nil
	}
	// We've accepted a valid payload from the beacon client. Mark the local
	// chain transitions to notify other subsystems (e.g. downloader) of the
	// behavioral change.
	if merger := api.mbl.Merger(); !merger.TDDReached() {
		merger.ReachTTD()
		api.mbl.Downloader().Cancel()
	}
	hash := block.Hash()
	return beacon.PayloadStatusV1{Status: beacon.VALID, LatestValidHash: &hash}, nil
}

// computePayloadId computes a pseudo-random payloadid, based on the parameters.
func computePayloadId(headBlockHash common.Hash, params *beacon.PayloadAttributesV1) beacon.PayloadID {
	// Hash
	hasher := sha256.New()
	hasher.Write(headBlockHash[:])
	binary.Write(hasher, binary.BigEndian, params.Timestamp)
	hasher.Write(params.Random[:])
	hasher.Write(params.SuggestedFeeRecipient[:])
	var out beacon.PayloadID
	copy(out[:], hasher.Sum(nil)[:8])
	return out
}

// invalid returns a response "INVALID" with the latest valid hash supplied by latest or to the current head
// if no latestValid block was provided.
func (api *ConsensusAPI) invalid(err error, latestValid *types.Block) beacon.PayloadStatusV1 {
	currentHash := api.mbl.BlockChain().CurrentBlock().Hash()
	if latestValid != nil {
		// Set latest valid hash to 0x0 if parent is PoW block
		currentHash = common.Hash{}
		if latestValid.Difficulty().BitLen() == 0 {
			// Otherwise set latest valid hash to parent hash
			currentHash = latestValid.Hash()
		}
	}
	errorMsg := err.Error()
	return beacon.PayloadStatusV1{Status: beacon.INVALID, LatestValidHash: &currentHash, ValidationError: &errorMsg}
}
