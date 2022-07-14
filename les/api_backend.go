// Copyright 2016 The go-mbali Authors
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

package les

import (
	"context"
	"errors"
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
	"github.com/mbali/go-mbali/light"
	"github.com/mbali/go-mbali/params"
	"github.com/mbali/go-mbali/rpc"
)

type LesApiBackend struct {
	extRPCEnabled       bool
	allowUnprotectedTxs bool
	mbl                 *Lightmbali
	gpo                 *gasprice.Oracle
}

func (b *LesApiBackend) ChainConfig() *params.ChainConfig {
	return b.mbl.chainConfig
}

func (b *LesApiBackend) CurrentBlock() *types.Block {
	return types.NewBlockWithHeader(b.mbl.BlockChain().CurrentHeader())
}

func (b *LesApiBackend) Smblead(number uint64) {
	b.mbl.handler.downloader.Cancel()
	b.mbl.blockchain.Smblead(number)
}

func (b *LesApiBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	// Return the latest current as the pending one since there
	// is no pending notion in the light client. TODO(rjl493456442)
	// unify the behavior of `HeaderByNumber` and `PendingBlockAndReceipts`.
	if number == rpc.PendingBlockNumber {
		return b.mbl.blockchain.CurrentHeader(), nil
	}
	if number == rpc.LatestBlockNumber {
		return b.mbl.blockchain.CurrentHeader(), nil
	}
	return b.mbl.blockchain.gombleaderByNumberOdr(ctx, uint64(number))
}

func (b *LesApiBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, err
		}
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

func (b *LesApiBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.mbl.blockchain.gombleaderByHash(hash), nil
}

func (b *LesApiBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	header, err := b.HeaderByNumber(ctx, number)
	if header == nil || err != nil {
		return nil, err
	}
	return b.BlockByHash(ctx, header.Hash())
}

func (b *LesApiBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.mbl.blockchain.GetBlockByHash(ctx, hash)
}

func (b *LesApiBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		block, err := b.BlockByHash(ctx, hash)
		if err != nil {
			return nil, err
		}
		if block == nil {
			return nil, errors.New("header found, but block body is missing")
		}
		if blockNrOrHash.RequireCanonical && b.mbl.blockchain.GetCanonicalHash(block.NumberU64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *LesApiBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return nil, nil
}

func (b *LesApiBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	return light.NewState(ctx, header, b.mbl.odr), header, nil
}

func (b *LesApiBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.mbl.blockchain.gombleaderByHash(hash)
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.mbl.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, nil, errors.New("hash is not currently canonical")
		}
		return light.NewState(ctx, header, b.mbl.odr), header, nil
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *LesApiBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	if number := rawdb.ReadHeaderNumber(b.mbl.chainDb, hash); number != nil {
		return light.GetBlockReceipts(ctx, b.mbl.odr, hash, *number)
	}
	return nil, nil
}

func (b *LesApiBackend) GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error) {
	if number := rawdb.ReadHeaderNumber(b.mbl.chainDb, hash); number != nil {
		return light.GetBlockLogs(ctx, b.mbl.odr, hash, *number)
	}
	return nil, nil
}

func (b *LesApiBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	if number := rawdb.ReadHeaderNumber(b.mbl.chainDb, hash); number != nil {
		return b.mbl.blockchain.GetTdOdr(ctx, hash, *number)
	}
	return nil
}

func (b *LesApiBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config) (*vm.EVM, func() error, error) {
	if vmConfig == nil {
		vmConfig = new(vm.Config)
	}
	txContext := core.NewEVMTxContext(msg)
	context := core.NewEVMBlockContext(header, b.mbl.blockchain, nil)
	return vm.NewEVM(context, txContext, state, b.mbl.chainConfig, *vmConfig), state.Error, nil
}

func (b *LesApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.mbl.txPool.Add(ctx, signedTx)
}

func (b *LesApiBackend) RemoveTx(txHash common.Hash) {
	b.mbl.txPool.RemoveTx(txHash)
}

func (b *LesApiBackend) GetPoolTransactions() (types.Transactions, error) {
	return b.mbl.txPool.GetTransactions()
}

func (b *LesApiBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	return b.mbl.txPool.GetTransaction(txHash)
}

func (b *LesApiBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	return light.GetTransaction(ctx, b.mbl.odr, txHash)
}

func (b *LesApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.mbl.txPool.GetNonce(ctx, addr)
}

func (b *LesApiBackend) Stats() (pending int, queued int) {
	return b.mbl.txPool.Stats(), 0
}

func (b *LesApiBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.mbl.txPool.Content()
}

func (b *LesApiBackend) TxPoolContentFrom(addr common.Address) (types.Transactions, types.Transactions) {
	return b.mbl.txPool.ContentFrom(addr)
}

func (b *LesApiBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.mbl.txPool.SubscribeNewTxsEvent(ch)
}

func (b *LesApiBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.mbl.blockchain.SubscribeChainEvent(ch)
}

func (b *LesApiBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.mbl.blockchain.SubscribeChainHeadEvent(ch)
}

func (b *LesApiBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.mbl.blockchain.SubscribeChainSideEvent(ch)
}

func (b *LesApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.mbl.blockchain.SubscribeLogsEvent(ch)
}

func (b *LesApiBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return event.NewSubscription(func(quit <-chan struct{}) error {
		<-quit
		return nil
	})
}

func (b *LesApiBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.mbl.blockchain.SubscribeRemovedLogsEvent(ch)
}

func (b *LesApiBackend) SyncProgress() mbali.SyncProgress {
	return b.mbl.Downloader().Progress()
}

func (b *LesApiBackend) ProtocolVersion() int {
	return b.mbl.LesVersion() + 10000
}

func (b *LesApiBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestTipCap(ctx)
}

func (b *LesApiBackend) FeeHistory(ctx context.Context, blockCount int, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (firstBlock *big.Int, reward [][]*big.Int, baseFee []*big.Int, gasUsedRatio []float64, err error) {
	return b.gpo.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
}

func (b *LesApiBackend) ChainDb() mbldb.Database {
	return b.mbl.chainDb
}

func (b *LesApiBackend) AccountManager() *accounts.Manager {
	return b.mbl.accountManager
}

func (b *LesApiBackend) ExtRPCEnabled() bool {
	return b.extRPCEnabled
}

func (b *LesApiBackend) UnprotectedAllowed() bool {
	return b.allowUnprotectedTxs
}

func (b *LesApiBackend) RPCGasCap() uint64 {
	return b.mbl.config.RPCGasCap
}

func (b *LesApiBackend) RPCEVMTimeout() time.Duration {
	return b.mbl.config.RPCEVMTimeout
}

func (b *LesApiBackend) RPCTxFeeCap() float64 {
	return b.mbl.config.RPCTxFeeCap
}

func (b *LesApiBackend) BloomStatus() (uint64, uint64) {
	if b.mbl.bloomIndexer == nil {
		return 0, 0
	}
	sections, _, _ := b.mbl.bloomIndexer.Sections()
	return params.BloomBitsBlocksClient, sections
}

func (b *LesApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.mbl.bloomRequests)
	}
}

func (b *LesApiBackend) Engine() consensus.Engine {
	return b.mbl.engine
}

func (b *LesApiBackend) CurrentHeader() *types.Header {
	return b.mbl.blockchain.CurrentHeader()
}

func (b *LesApiBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, checkLive bool, preferDisk bool) (*state.StateDB, error) {
	return b.mbl.stateAtBlock(ctx, block, reexec)
}

func (b *LesApiBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (core.Message, vm.BlockContext, *state.StateDB, error) {
	return b.mbl.stateAtTransaction(ctx, block, txIndex, reexec)
}
