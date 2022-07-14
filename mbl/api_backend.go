// Copyright 2015 The go-mbali Authors
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

package mbl

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/mbali/go-mbali"
	"github.com/mbali/go-mbali/accounts"
	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/consensus"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/bloombits"
	"github.com/mbali/go-mbali/core/rawdb"
	"github.com/mbali/go-mbali/core/state"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/core/vm"
	"github.com/mbali/go-mbali/mbl/gasprice"
	"github.com/mbali/go-mbali/mbldb"
	"github.com/mbali/go-mbali/event"
	"github.com/mbali/go-mbali/miner"
	"github.com/mbali/go-mbali/params"
	"github.com/mbali/go-mbali/rpc"
)

// mblAPIBackend implements mblapi.Backend for full nodes
type mblAPIBackend struct {
	extRPCEnabled       bool
	allowUnprotectedTxs bool
	mbl                 *mbali
	gpo                 *gasprice.Oracle
}

// ChainConfig returns the active chain configuration.
func (b *mblAPIBackend) ChainConfig() *params.ChainConfig {
	return b.mbl.blockchain.Config()
}

func (b *mblAPIBackend) CurrentBlock() *types.Block {
	return b.mbl.blockchain.CurrentBlock()
}

func (b *mblAPIBackend) Smblead(number uint64) {
	b.mbl.handler.downloader.Cancel()
	b.mbl.blockchain.Smblead(number)
}

func (b *mblAPIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.mbl.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.mbl.blockchain.CurrentBlock().Header(), nil
	}
	if number == rpc.FinalizedBlockNumber {
		return b.mbl.blockchain.CurrentFinalizedBlock().Header(), nil
	}
	return b.mbl.blockchain.gombleaderByNumber(uint64(number)), nil
}

func (b *mblAPIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.mbl.blockchain.gombleaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.mbl.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		return header, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *mblAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.mbl.blockchain.gombleaderByHash(hash), nil
}

func (b *mblAPIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.mbl.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.mbl.blockchain.CurrentBlock(), nil
	}
	if number == rpc.FinalizedBlockNumber {
		return b.mbl.blockchain.CurrentFinalizedBlock(), nil
	}
	return b.mbl.blockchain.GetBlockByNumber(uint64(number)), nil
}

func (b *mblAPIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.mbl.blockchain.GetBlockByHash(hash), nil
}

func (b *mblAPIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.mbl.blockchain.gombleaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.mbl.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		block := b.mbl.blockchain.GetBlock(hash, header.Number.Uint64())
		if block == nil {
			return nil, errors.New("header found, but block body is missing")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *mblAPIBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return b.mbl.miner.PendingBlockAndReceipts()
}

func (b *mblAPIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if number == rpc.PendingBlockNumber {
		block, state := b.mbl.miner.Pending()
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	stateDb, err := b.mbl.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *mblAPIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.mbl.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, nil, errors.New("hash is not currently canonical")
		}
		stateDb, err := b.mbl.BlockChain().StateAt(header.Root)
		return stateDb, header, err
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *mblAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return b.mbl.blockchain.GetReceiptsByHash(hash), nil
}

func (b *mblAPIBackend) GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error) {
	db := b.mbl.ChainDb()
	number := rawdb.ReadHeaderNumber(db, hash)
	if number == nil {
		return nil, fmt.Errorf("failed to get block number for hash %#x", hash)
	}
	logs := rawdb.ReadLogs(db, hash, *number, b.mbl.blockchain.Config())
	if logs == nil {
		return nil, fmt.Errorf("failed to get logs for block #%d (0x%s)", *number, hash.TerminalString())
	}
	return logs, nil
}

func (b *mblAPIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	if header := b.mbl.blockchain.gombleaderByHash(hash); header != nil {
		return b.mbl.blockchain.GetTd(hash, header.Number.Uint64())
	}
	return nil
}

func (b *mblAPIBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config) (*vm.EVM, func() error, error) {
	vmError := func() error { return nil }
	if vmConfig == nil {
		vmConfig = b.mbl.blockchain.GetVMConfig()
	}
	txContext := core.NewEVMTxContext(msg)
	context := core.NewEVMBlockContext(header, b.mbl.BlockChain(), nil)
	return vm.NewEVM(context, txContext, state, b.mbl.blockchain.Config(), *vmConfig), vmError, nil
}

func (b *mblAPIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.mbl.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *mblAPIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.mbl.miner.SubscribePendingLogs(ch)
}

func (b *mblAPIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.mbl.BlockChain().SubscribeChainEvent(ch)
}

func (b *mblAPIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.mbl.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *mblAPIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.mbl.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *mblAPIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.mbl.BlockChain().SubscribeLogsEvent(ch)
}

func (b *mblAPIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.mbl.txPool.AddLocal(signedTx)
}

func (b *mblAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending := b.mbl.txPool.Pending(false)
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *mblAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.mbl.txPool.Get(hash)
}

func (b *mblAPIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(b.mbl.ChainDb(), txHash)
	return tx, blockHash, blockNumber, index, nil
}

func (b *mblAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.mbl.txPool.Nonce(addr), nil
}

func (b *mblAPIBackend) Stats() (pending int, queued int) {
	return b.mbl.txPool.Stats()
}

func (b *mblAPIBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.mbl.TxPool().Content()
}

func (b *mblAPIBackend) TxPoolContentFrom(addr common.Address) (types.Transactions, types.Transactions) {
	return b.mbl.TxPool().ContentFrom(addr)
}

func (b *mblAPIBackend) TxPool() *core.TxPool {
	return b.mbl.TxPool()
}

func (b *mblAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.mbl.TxPool().SubscribeNewTxsEvent(ch)
}

func (b *mblAPIBackend) SyncProgress() mbali.SyncProgress {
	return b.mbl.Downloader().Progress()
}

func (b *mblAPIBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestTipCap(ctx)
}

func (b *mblAPIBackend) FeeHistory(ctx context.Context, blockCount int, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (firstBlock *big.Int, reward [][]*big.Int, baseFee []*big.Int, gasUsedRatio []float64, err error) {
	return b.gpo.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
}

func (b *mblAPIBackend) ChainDb() mbldb.Database {
	return b.mbl.ChainDb()
}

func (b *mblAPIBackend) EventMux() *event.TypeMux {
	return b.mbl.EventMux()
}

func (b *mblAPIBackend) AccountManager() *accounts.Manager {
	return b.mbl.AccountManager()
}

func (b *mblAPIBackend) ExtRPCEnabled() bool {
	return b.extRPCEnabled
}

func (b *mblAPIBackend) UnprotectedAllowed() bool {
	return b.allowUnprotectedTxs
}

func (b *mblAPIBackend) RPCGasCap() uint64 {
	return b.mbl.config.RPCGasCap
}

func (b *mblAPIBackend) RPCEVMTimeout() time.Duration {
	return b.mbl.config.RPCEVMTimeout
}

func (b *mblAPIBackend) RPCTxFeeCap() float64 {
	return b.mbl.config.RPCTxFeeCap
}

func (b *mblAPIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.mbl.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *mblAPIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.mbl.bloomRequests)
	}
}

func (b *mblAPIBackend) Engine() consensus.Engine {
	return b.mbl.engine
}

func (b *mblAPIBackend) CurrentHeader() *types.Header {
	return b.mbl.blockchain.CurrentHeader()
}

func (b *mblAPIBackend) Miner() *miner.Miner {
	return b.mbl.Miner()
}

func (b *mblAPIBackend) StartMining(threads int) error {
	return b.mbl.StartMining(threads)
}

func (b *mblAPIBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, checkLive, preferDisk bool) (*state.StateDB, error) {
	return b.mbl.StateAtBlock(block, reexec, base, checkLive, preferDisk)
}

func (b *mblAPIBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (core.Message, vm.BlockContext, *state.StateDB, error) {
	return b.mbl.stateAtTransaction(block, txIndex, reexec)
}
