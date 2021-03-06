// Copyright 2014 The go-mbali Authors
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

// Package mbl implements the mbali protocol.
package mbl

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mbali/go-mbali/accounts"
	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/common/hexutil"
	"github.com/mbali/go-mbali/consensus"
	"github.com/mbali/go-mbali/consensus/beacon"
	"github.com/mbali/go-mbali/consensus/clique"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/bloombits"
	"github.com/mbali/go-mbali/core/rawdb"
	"github.com/mbali/go-mbali/core/state/pruner"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/core/vm"
	"github.com/mbali/go-mbali/mbl/downloader"
	"github.com/mbali/go-mbali/mbl/mblconfig"
	"github.com/mbali/go-mbali/mbl/filters"
	"github.com/mbali/go-mbali/mbl/gasprice"
	"github.com/mbali/go-mbali/mbl/protocols/mbl"
	"github.com/mbali/go-mbali/mbl/protocols/snap"
	"github.com/mbali/go-mbali/mbldb"
	"github.com/mbali/go-mbali/event"
	"github.com/mbali/go-mbali/internal/mblapi"
	"github.com/mbali/go-mbali/internal/shutdowncheck"
	"github.com/mbali/go-mbali/log"
	"github.com/mbali/go-mbali/miner"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/dnsdisc"
	"github.com/mbali/go-mbali/p2p/enode"
	"github.com/mbali/go-mbali/params"
	"github.com/mbali/go-mbali/rlp"
	"github.com/mbali/go-mbali/rpc"
)

// Config contains the configuration options of the mbl protocol.
// Deprecated: use mblconfig.Config instead.
type Config = mblconfig.Config

// mbali implements the mbali full node service.
type mbali struct {
	config *mblconfig.Config

	// Handlers
	txPool             *core.TxPool
	blockchain         *core.BlockChain
	handler            *handler
	mblDialCandidates  enode.Iterator
	snapDialCandidates enode.Iterator
	merger             *consensus.Merger

	// DB interfaces
	chainDb mbldb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests     chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer      *core.ChainIndexer             // Bloom indexer operating during block imports
	closeBloomHandler chan struct{}

	APIBackend *mblAPIBackend

	miner     *miner.Miner
	gasPrice  *big.Int
	mblerbase common.Address

	networkID     uint64
	netRPCService *mblapi.PublicNetAPI

	p2pServer *p2p.Server

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and mblerbase)

	shutdownTracker *shutdowncheck.ShutdownTracker // Tracks if and when the node has shutdown ungracefully
}

// New creates a new mbali object (including the
// initialisation of the common mbali object)
func New(stack *node.Node, config *mblconfig.Config) (*mbali, error) {
	// Ensure configuration values are compatible and sane
	if config.SyncMode == downloader.LightSync {
		return nil, errors.New("can't run mbl.mbali in light sync mode, use les.Lightmbali")
	}
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	if config.Miner.GasPrice == nil || config.Miner.GasPrice.Cmp(common.Big0) <= 0 {
		log.Warn("Sanitizing invalid miner gas price", "provided", config.Miner.GasPrice, "updated", mblconfig.Defaults.Miner.GasPrice)
		config.Miner.GasPrice = new(big.Int).Set(mblconfig.Defaults.Miner.GasPrice)
	}
	if config.NoPruning && config.TrieDirtyCache > 0 {
		if config.SnapshotCache > 0 {
			config.TrieCleanCache += config.TrieDirtyCache * 3 / 5
			config.SnapshotCache += config.TrieDirtyCache * 2 / 5
		} else {
			config.TrieCleanCache += config.TrieDirtyCache
		}
		config.TrieDirtyCache = 0
	}
	log.Info("Allocated trie memory caches", "clean", common.StorageSize(config.TrieCleanCache)*1024*1024, "dirty", common.StorageSize(config.TrieDirtyCache)*1024*1024)

	// Transfer mining-related config to the mblash config.
	mblashConfig := config.mblash
	mblashConfig.NotifyFull = config.Miner.NotifyFull

	// Assemble the mbali object
	chainDb, err := stack.OpenDatabaseWithFreezer("chaindata", config.DatabaseCache, config.DatabaseHandles, config.DatabaseFreezer, "mbl/db/chaindata/", false)
	if err != nil {
		return nil, err
	}
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlockWithOverride(chainDb, config.Genesis, config.OverrideArrowGlacier, config.OverrideTerminalTotalDifficulty)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("")
	log.Info(strings.Repeat("-", 153))
	for _, line := range strings.Split(chainConfig.String(), "\n") {
		log.Info(line)
	}
	log.Info(strings.Repeat("-", 153))
	log.Info("")

	if err := pruner.RecoverPruning(stack.ResolvePath(""), chainDb, stack.ResolvePath(config.TrieCleanCacheJournal)); err != nil {
		log.Error("Failed to recover state", "error", err)
	}
	merger := consensus.NewMerger(chainDb)
	mbl := &mbali{
		config:            config,
		merger:            merger,
		chainDb:           chainDb,
		eventMux:          stack.EventMux(),
		accountManager:    stack.AccountManager(),
		engine:            mblconfig.CreateConsensusEngine(stack, chainConfig, &mblashConfig, config.Miner.Notify, config.Miner.Noverify, chainDb),
		closeBloomHandler: make(chan struct{}),
		networkID:         config.NetworkId,
		gasPrice:          config.Miner.GasPrice,
		mblerbase:         config.Miner.mblerbase,
		bloomRequests:     make(chan chan *bloombits.Retrieval),
		bloomIndexer:      core.NewBloomIndexer(chainDb, params.BloomBitsBlocks, params.BloomConfirms),
		p2pServer:         stack.Server(),
		shutdownTracker:   shutdowncheck.NewShutdownTracker(chainDb),
	}

	bcVersion := rawdb.ReadDatabaseVersion(chainDb)
	var dbVer = "<nil>"
	if bcVersion != nil {
		dbVer = fmt.Sprintf("%d", *bcVersion)
	}
	log.Info("Initialising mbali protocol", "network", config.NetworkId, "dbversion", dbVer)

	if !config.SkipBcVersionCheck {
		if bcVersion != nil && *bcVersion > core.BlockChainVersion {
			return nil, fmt.Errorf("database version is v%d, gombl %s only supports v%d", *bcVersion, params.VersionWithMeta, core.BlockChainVersion)
		} else if bcVersion == nil || *bcVersion < core.BlockChainVersion {
			if bcVersion != nil { // only print warning on upgrade, not on init
				log.Warn("Upgrade blockchain database version", "from", dbVer, "to", core.BlockChainVersion)
			}
			rawdb.WriteDatabaseVersion(chainDb, core.BlockChainVersion)
		}
	}
	var (
		vmConfig = vm.Config{
			EnablePreimageRecording: config.EnablePreimageRecording,
		}
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit:      config.TrieCleanCache,
			TrieCleanJournal:    stack.ResolvePath(config.TrieCleanCacheJournal),
			TrieCleanRejournal:  config.TrieCleanCacheRejournal,
			TrieCleanNoPrefetch: config.NoPrefetch,
			TrieDirtyLimit:      config.TrieDirtyCache,
			TrieDirtyDisabled:   config.NoPruning,
			TrieTimeLimit:       config.TrieTimeout,
			SnapshotLimit:       config.SnapshotCache,
			Preimages:           config.Preimages,
		}
	)
	mbl.blockchain, err = core.NewBlockChain(chainDb, cacheConfig, chainConfig, mbl.engine, vmConfig, mbl.shouldPreserve, &config.TxLookupLimit)
	if err != nil {
		return nil, err
	}
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		mbl.blockchain.Smblead(compat.RewindTo)
		rawdb.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}
	mbl.bloomIndexer.Start(mbl.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = stack.ResolvePath(config.TxPool.Journal)
	}
	mbl.txPool = core.NewTxPool(config.TxPool, chainConfig, mbl.blockchain)

	// Permit the downloader to use the trie cache allowance during fast sync
	cacheLimit := cacheConfig.TrieCleanLimit + cacheConfig.TrieDirtyLimit + cacheConfig.SnapshotLimit
	checkpoint := config.Checkpoint
	if checkpoint == nil {
		checkpoint = params.TrustedCheckpoints[genesisHash]
	}
	if mbl.handler, err = newHandler(&handlerConfig{
		Database:       chainDb,
		Chain:          mbl.blockchain,
		TxPool:         mbl.txPool,
		Merger:         merger,
		Network:        config.NetworkId,
		Sync:           config.SyncMode,
		BloomCache:     uint64(cacheLimit),
		EventMux:       mbl.eventMux,
		Checkpoint:     checkpoint,
		RequiredBlocks: config.RequiredBlocks,
	}); err != nil {
		return nil, err
	}

	mbl.miner = miner.New(mbl, &config.Miner, chainConfig, mbl.EventMux(), mbl.engine, mbl.isLocalBlock)
	mbl.miner.SetExtra(makeExtraData(config.Miner.ExtraData))

	mbl.APIBackend = &mblAPIBackend{stack.Config().ExtRPCEnabled(), stack.Config().AllowUnprotectedTxs, mbl, nil}
	if mbl.APIBackend.allowUnprotectedTxs {
		log.Info("Unprotected transactions allowed")
	}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.Miner.GasPrice
	}
	mbl.APIBackend.gpo = gasprice.NewOracle(mbl.APIBackend, gpoParams)

	// Setup DNS discovery iterators.
	dnsclient := dnsdisc.NewClient(dnsdisc.Config{})
	mbl.mblDialCandidates, err = dnsclient.NewIterator(mbl.config.mblDiscoveryURLs...)
	if err != nil {
		return nil, err
	}
	mbl.snapDialCandidates, err = dnsclient.NewIterator(mbl.config.SnapDiscoveryURLs...)
	if err != nil {
		return nil, err
	}

	// Start the RPC service
	mbl.netRPCService = mblapi.NewPublicNetAPI(mbl.p2pServer, config.NetworkId)

	// Register the backend on the node
	stack.RegisterAPIs(mbl.APIs())
	stack.RegisterProtocols(mbl.Protocols())
	stack.RegisterLifecycle(mbl)

	// Successful startup; push a marker and check previous unclean shutdowns.
	mbl.shutdownTracker.MarkStartup()

	return mbl, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(params.VersionMajor<<16 | params.VersionMinor<<8 | params.VersionPatch),
			"gombl",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize)
		extra = nil
	}
	return extra
}

// APIs return the collection of RPC services the mbali package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *mbali) APIs() []rpc.API {
	apis := mblapi.GetAPIs(s.APIBackend)

	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, s.engine.APIs(s.BlockChain())...)

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "mbl",
			Version:   "1.0",
			Service:   NewPublicmbaliAPI(s),
			Public:    true,
		}, {
			Namespace: "mbl",
			Version:   "1.0",
			Service:   NewPublicMinerAPI(s),
			Public:    true,
		}, {
			Namespace: "mbl",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.handler.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "miner",
			Version:   "1.0",
			Service:   NewPrivateMinerAPI(s),
			Public:    false,
		}, {
			Namespace: "mbl",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.APIBackend, false, 5*time.Minute),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(s),
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *mbali) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *mbali) mblerbase() (eb common.Address, err error) {
	s.lock.RLock()
	mblerbase := s.mblerbase
	s.lock.RUnlock()

	if mblerbase != (common.Address{}) {
		return mblerbase, nil
	}
	if wallets := s.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			mblerbase := accounts[0].Address

			s.lock.Lock()
			s.mblerbase = mblerbase
			s.lock.Unlock()

			log.Info("mblerbase automatically configured", "address", mblerbase)
			return mblerbase, nil
		}
	}
	return common.Address{}, fmt.Errorf("mblerbase must be explicitly specified")
}

// isLocalBlock checks whmbler the specified block is mined
// by local miner accounts.
//
// We regard two types of accounts as local miner account: mblerbase
// and accounts specified via `txpool.locals` flag.
func (s *mbali) isLocalBlock(header *types.Header) bool {
	author, err := s.engine.Author(header)
	if err != nil {
		log.Warn("Failed to retrieve block author", "number", header.Number.Uint64(), "hash", header.Hash(), "err", err)
		return false
	}
	// Check whmbler the given address is mblerbase.
	s.lock.RLock()
	mblerbase := s.mblerbase
	s.lock.RUnlock()
	if author == mblerbase {
		return true
	}
	// Check whmbler the given address is specified by `txpool.local`
	// CLI flag.
	for _, account := range s.config.TxPool.Locals {
		if account == author {
			return true
		}
	}
	return false
}

// shouldPreserve checks whmbler we should preserve the given block
// during the chain reorg depending on whmbler the author of block
// is a local account.
func (s *mbali) shouldPreserve(header *types.Header) bool {
	// The reason we need to disable the self-reorg preserving for clique
	// is it can be probable to introduce a deadlock.
	//
	// e.g. If there are 7 available signers
	//
	// r1   A
	// r2     B
	// r3       C
	// r4         D
	// r5   A      [X] F G
	// r6    [X]
	//
	// In the round5, the inturn signer E is offline, so the worst case
	// is A, F and G sign the block of round5 and reject the block of opponents
	// and in the round6, the last available signer B is offline, the whole
	// network is stuck.
	if _, ok := s.engine.(*clique.Clique); ok {
		return false
	}
	return s.isLocalBlock(header)
}

// Setmblerbase sets the mining reward address.
func (s *mbali) Setmblerbase(mblerbase common.Address) {
	s.lock.Lock()
	s.mblerbase = mblerbase
	s.lock.Unlock()

	s.miner.Setmblerbase(mblerbase)
}

// StartMining starts the miner with the given number of CPU threads. If mining
// is already running, this mmblod adjust the number of threads allowed to use
// and updates the minimum price required by the transaction pool.
func (s *mbali) StartMining(threads int) error {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		log.Info("Updated mining threads", "threads", threads)
		if threads == 0 {
			threads = -1 // Disable the miner from within
		}
		th.SetThreads(threads)
	}
	// If the miner was not running, initialize it
	if !s.IsMining() {
		// Propagate the initial price point to the transaction pool
		s.lock.RLock()
		price := s.gasPrice
		s.lock.RUnlock()
		s.txPool.SetGasPrice(price)

		// Configure the local mining address
		eb, err := s.mblerbase()
		if err != nil {
			log.Error("Cannot start mining without mblerbase", "err", err)
			return fmt.Errorf("mblerbase missing: %v", err)
		}
		var cli *clique.Clique
		if c, ok := s.engine.(*clique.Clique); ok {
			cli = c
		} else if cl, ok := s.engine.(*beacon.Beacon); ok {
			if c, ok := cl.InnerEngine().(*clique.Clique); ok {
				cli = c
			}
		}
		if cli != nil {
			wallet, err := s.accountManager.Find(accounts.Account{Address: eb})
			if wallet == nil || err != nil {
				log.Error("mblerbase account unavailable locally", "err", err)
				return fmt.Errorf("signer missing: %v", err)
			}
			cli.Authorize(eb, wallet.SignData)
		}
		// If mining is started, we can disable the transaction rejection mechanism
		// introduced to speed sync times.
		atomic.StoreUint32(&s.handler.acceptTxs, 1)

		go s.miner.Start(eb)
	}
	return nil
}

// StopMining terminates the miner, both at the consensus engine level as well as
// at the block creation level.
func (s *mbali) StopMining() {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		th.SetThreads(-1)
	}
	// Stop the block creating itself
	s.miner.Stop()
}

func (s *mbali) IsMining() bool      { return s.miner.Mining() }
func (s *mbali) Miner() *miner.Miner { return s.miner }

func (s *mbali) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *mbali) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *mbali) TxPool() *core.TxPool               { return s.txPool }
func (s *mbali) EventMux() *event.TypeMux           { return s.eventMux }
func (s *mbali) Engine() consensus.Engine           { return s.engine }
func (s *mbali) ChainDb() mbldb.Database            { return s.chainDb }
func (s *mbali) IsListening() bool                  { return true } // Always listening
func (s *mbali) Downloader() *downloader.Downloader { return s.handler.downloader }
func (s *mbali) Synced() bool                       { return atomic.LoadUint32(&s.handler.acceptTxs) == 1 }
func (s *mbali) SetSynced()                         { atomic.StoreUint32(&s.handler.acceptTxs, 1) }
func (s *mbali) ArchiveMode() bool                  { return s.config.NoPruning }
func (s *mbali) BloomIndexer() *core.ChainIndexer   { return s.bloomIndexer }
func (s *mbali) Merger() *consensus.Merger          { return s.merger }
func (s *mbali) SyncMode() downloader.SyncMode {
	mode, _ := s.handler.chainSync.modeAndLocalHead()
	return mode
}

// Protocols returns all the currently configured
// network protocols to start.
func (s *mbali) Protocols() []p2p.Protocol {
	protos := mbl.MakeProtocols((*mblHandler)(s.handler), s.networkID, s.mblDialCandidates)
	if s.config.SnapshotCache > 0 {
		protos = append(protos, snap.MakeProtocols((*snapHandler)(s.handler), s.snapDialCandidates)...)
	}
	return protos
}

// Start implements node.Lifecycle, starting all internal goroutines needed by the
// mbali protocol implementation.
func (s *mbali) Start() error {
	mbl.StartENRUpdater(s.blockchain, s.p2pServer.LocalNode())

	// Start the bloom bits servicing goroutines
	s.startBloomHandlers(params.BloomBitsBlocks)

	// Regularly update shutdown marker
	s.shutdownTracker.Start()

	// Figure out a max peers count based on the server limits
	maxPeers := s.p2pServer.MaxPeers
	if s.config.LightServ > 0 {
		if s.config.LightPeers >= s.p2pServer.MaxPeers {
			return fmt.Errorf("invalid peer config: light peer count (%d) >= total peer count (%d)", s.config.LightPeers, s.p2pServer.MaxPeers)
		}
		maxPeers -= s.config.LightPeers
	}
	// Start the networking layer and the light server if requested
	s.handler.Start(maxPeers)
	return nil
}

// Stop implements node.Lifecycle, terminating all internal goroutines used by the
// mbali protocol.
func (s *mbali) Stop() error {
	// Stop all the peer-related stuff first.
	s.mblDialCandidates.Close()
	s.snapDialCandidates.Close()
	s.handler.Stop()

	// Then stop everything else.
	s.bloomIndexer.Close()
	close(s.closeBloomHandler)
	s.txPool.Stop()
	s.miner.Close()
	s.blockchain.Stop()
	s.engine.Close()

	// Clean shutdown marker as the last thing before closing db
	s.shutdownTracker.Stop()

	s.chainDb.Close()
	s.eventMux.Stop()

	return nil
}
