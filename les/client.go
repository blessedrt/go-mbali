// Copyright 2019 The go-mbali Authors
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

// Package les implements the Light mbali Subprotocol.
package les

import (
	"fmt"
	"strings"
	"time"

	"github.com/mbali/go-mbali/accounts"
	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/common/hexutil"
	"github.com/mbali/go-mbali/common/mclock"
	"github.com/mbali/go-mbali/consensus"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/bloombits"
	"github.com/mbali/go-mbali/core/rawdb"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/mbl/mblconfig"
	"github.com/mbali/go-mbali/mbl/filters"
	"github.com/mbali/go-mbali/mbl/gasprice"
	"github.com/mbali/go-mbali/event"
	"github.com/mbali/go-mbali/internal/mblapi"
	"github.com/mbali/go-mbali/internal/shutdowncheck"
	"github.com/mbali/go-mbali/les/downloader"
	"github.com/mbali/go-mbali/les/vflux"
	vfc "github.com/mbali/go-mbali/les/vflux/client"
	"github.com/mbali/go-mbali/light"
	"github.com/mbali/go-mbali/log"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/enode"
	"github.com/mbali/go-mbali/p2p/enr"
	"github.com/mbali/go-mbali/params"
	"github.com/mbali/go-mbali/rlp"
	"github.com/mbali/go-mbali/rpc"
)

type Lightmbali struct {
	lesCommons

	peers              *serverPeerSet
	reqDist            *requestDistributor
	retriever          *retrieveManager
	odr                *LesOdr
	relay              *lesTxRelay
	handler            *clientHandler
	txPool             *light.TxPool
	blockchain         *light.LightChain
	serverPool         *vfc.ServerPool
	serverPoolIterator enode.Iterator
	pruner             *pruner
	merger             *consensus.Merger

	bloomRequests chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer  *core.ChainIndexer             // Bloom indexer operating during block imports

	ApiBackend     *LesApiBackend
	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager
	netRPCService  *mblapi.PublicNetAPI

	p2pServer  *p2p.Server
	p2pConfig  *p2p.Config
	udpEnabled bool

	shutdownTracker *shutdowncheck.ShutdownTracker // Tracks if and when the node has shutdown ungracefully
}

// New creates an instance of the light client.
func New(stack *node.Node, config *mblconfig.Config) (*Lightmbali, error) {
	chainDb, err := stack.OpenDatabase("lightchaindata", config.DatabaseCache, config.DatabaseHandles, "mbl/db/chaindata/", false)
	if err != nil {
		return nil, err
	}
	lesDb, err := stack.OpenDatabase("les.client", 0, 0, "mbl/db/lesclient/", false)
	if err != nil {
		return nil, err
	}
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlockWithOverride(chainDb, config.Genesis, config.OverrideArrowGlacier, config.OverrideTerminalTotalDifficulty)
	if _, isCompat := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !isCompat {
		return nil, genesisErr
	}
	log.Info("")
	log.Info(strings.Repeat("-", 153))
	for _, line := range strings.Split(chainConfig.String(), "\n") {
		log.Info(line)
	}
	log.Info(strings.Repeat("-", 153))
	log.Info("")

	peers := newServerPeerSet()
	merger := consensus.NewMerger(chainDb)
	lmbl := &Lightmbali{
		lesCommons: lesCommons{
			genesis:     genesisHash,
			config:      config,
			chainConfig: chainConfig,
			iConfig:     light.DefaultClientIndexerConfig,
			chainDb:     chainDb,
			lesDb:       lesDb,
			closeCh:     make(chan struct{}),
		},
		peers:           peers,
		eventMux:        stack.EventMux(),
		reqDist:         newRequestDistributor(peers, &mclock.System{}),
		accountManager:  stack.AccountManager(),
		merger:          merger,
		engine:          mblconfig.CreateConsensusEngine(stack, chainConfig, &config.mblash, nil, false, chainDb),
		bloomRequests:   make(chan chan *bloombits.Retrieval),
		bloomIndexer:    core.NewBloomIndexer(chainDb, params.BloomBitsBlocksClient, params.HelperTrieConfirmations),
		p2pServer:       stack.Server(),
		p2pConfig:       &stack.Config().P2P,
		udpEnabled:      stack.Config().P2P.DiscoveryV5,
		shutdownTracker: shutdowncheck.NewShutdownTracker(chainDb),
	}

	var prenegQuery vfc.QueryFunc
	if lmbl.udpEnabled {
		prenegQuery = lmbl.prenegQuery
	}
	lmbl.serverPool, lmbl.serverPoolIterator = vfc.NewServerPool(lesDb, []byte("serverpool:"), time.Second, prenegQuery, &mclock.System{}, config.UltraLightServers, requestList)
	lmbl.serverPool.AddMetrics(suggestedTimeoutGauge, totalValueGauge, serverSelectableGauge, serverConnectedGauge, sessionValueMeter, serverDialedMeter)

	lmbl.retriever = newRetrieveManager(peers, lmbl.reqDist, lmbl.serverPool.GetTimeout)
	lmbl.relay = newLesTxRelay(peers, lmbl.retriever)

	lmbl.odr = NewLesOdr(chainDb, light.DefaultClientIndexerConfig, lmbl.peers, lmbl.retriever)
	lmbl.chtIndexer = light.NewChtIndexer(chainDb, lmbl.odr, params.CHTFrequency, params.HelperTrieConfirmations, config.LightNoPrune)
	lmbl.bloomTrieIndexer = light.NewBloomTrieIndexer(chainDb, lmbl.odr, params.BloomBitsBlocksClient, params.BloomTrieFrequency, config.LightNoPrune)
	lmbl.odr.SetIndexers(lmbl.chtIndexer, lmbl.bloomTrieIndexer, lmbl.bloomIndexer)

	checkpoint := config.Checkpoint
	if checkpoint == nil {
		checkpoint = params.TrustedCheckpoints[genesisHash]
	}
	// Note: NewLightChain adds the trusted checkpoint so it needs an ODR with
	// indexers already set but not started yet
	if lmbl.blockchain, err = light.NewLightChain(lmbl.odr, lmbl.chainConfig, lmbl.engine, checkpoint); err != nil {
		return nil, err
	}
	lmbl.chainReader = lmbl.blockchain
	lmbl.txPool = light.NewTxPool(lmbl.chainConfig, lmbl.blockchain, lmbl.relay)

	// Set up checkpoint oracle.
	lmbl.oracle = lmbl.setupOracle(stack, genesisHash, config)

	// Note: AddChildIndexer starts the update process for the child
	lmbl.bloomIndexer.AddChildIndexer(lmbl.bloomTrieIndexer)
	lmbl.chtIndexer.Start(lmbl.blockchain)
	lmbl.bloomIndexer.Start(lmbl.blockchain)

	// Start a light chain pruner to delete useless historical data.
	lmbl.pruner = newPruner(chainDb, lmbl.chtIndexer, lmbl.bloomTrieIndexer)

	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		lmbl.blockchain.Smblead(compat.RewindTo)
		rawdb.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}

	lmbl.ApiBackend = &LesApiBackend{stack.Config().ExtRPCEnabled(), stack.Config().AllowUnprotectedTxs, lmbl, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.Miner.GasPrice
	}
	lmbl.ApiBackend.gpo = gasprice.NewOracle(lmbl.ApiBackend, gpoParams)

	lmbl.handler = newClientHandler(config.UltraLightServers, config.UltraLightFraction, checkpoint, lmbl)
	if lmbl.handler.ulc != nil {
		log.Warn("Ultra light client is enabled", "trustedNodes", len(lmbl.handler.ulc.keys), "minTrustedFraction", lmbl.handler.ulc.fraction)
		lmbl.blockchain.DisableCheckFreq()
	}

	lmbl.netRPCService = mblapi.NewPublicNetAPI(lmbl.p2pServer, lmbl.config.NetworkId)

	// Register the backend on the node
	stack.RegisterAPIs(lmbl.APIs())
	stack.RegisterProtocols(lmbl.Protocols())
	stack.RegisterLifecycle(lmbl)

	// Successful startup; push a marker and check previous unclean shutdowns.
	lmbl.shutdownTracker.MarkStartup()

	return lmbl, nil
}

// VfluxRequest sends a batch of requests to the given node through discv5 UDP TalkRequest and returns the responses
func (s *Lightmbali) VfluxRequest(n *enode.Node, reqs vflux.Requests) vflux.Replies {
	if !s.udpEnabled {
		return nil
	}
	reqsEnc, _ := rlp.EncodeToBytes(&reqs)
	repliesEnc, _ := s.p2pServer.DiscV5.TalkRequest(s.serverPool.DialNode(n), "vfx", reqsEnc)
	var replies vflux.Replies
	if len(repliesEnc) == 0 || rlp.DecodeBytes(repliesEnc, &replies) != nil {
		return nil
	}
	return replies
}

// vfxVersion returns the version number of the "les" service subdomain of the vflux UDP
// service, as advertised in the ENR record
func (s *Lightmbali) vfxVersion(n *enode.Node) uint {
	if n.Seq() == 0 {
		var err error
		if !s.udpEnabled {
			return 0
		}
		if n, err = s.p2pServer.DiscV5.RequestENR(n); n != nil && err == nil && n.Seq() != 0 {
			s.serverPool.Persist(n)
		} else {
			return 0
		}
	}

	var les []rlp.RawValue
	if err := n.Load(enr.WithEntry("les", &les)); err != nil || len(les) < 1 {
		return 0
	}
	var version uint
	rlp.DecodeBytes(les[0], &version) // Ignore additional fields (for forward compatibility).
	return version
}

// prenegQuery sends a capacity query to the given server node to determine whmbler
// a connection slot is immediately available
func (s *Lightmbali) prenegQuery(n *enode.Node) int {
	if s.vfxVersion(n) < 1 {
		// UDP query not supported, always try TCP connection
		return 1
	}

	var requests vflux.Requests
	requests.Add("les", vflux.CapacityQueryName, vflux.CapacityQueryReq{
		Bias:      180,
		AddTokens: []vflux.IntOrInf{{}},
	})
	replies := s.VfluxRequest(n, requests)
	var cqr vflux.CapacityQueryReply
	if replies.Get(0, &cqr) != nil || len(cqr) != 1 { // Note: Get returns an error if replies is nil
		return -1
	}
	if cqr[0] > 0 {
		return 1
	}
	return 0
}

type LightDummyAPI struct{}

// mblerbase is the address that mining rewards will be send to
func (s *LightDummyAPI) mblerbase() (common.Address, error) {
	return common.Address{}, fmt.Errorf("mining is not supported in light mode")
}

// Coinbase is the address that mining rewards will be send to (alias for mblerbase)
func (s *LightDummyAPI) Coinbase() (common.Address, error) {
	return common.Address{}, fmt.Errorf("mining is not supported in light mode")
}

// Hashrate returns the POW hashrate
func (s *LightDummyAPI) Hashrate() hexutil.Uint {
	return 0
}

// Mining returns an indication if this node is currently mining.
func (s *LightDummyAPI) Mining() bool {
	return false
}

// APIs returns the collection of RPC services the mbali package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Lightmbali) APIs() []rpc.API {
	apis := mblapi.GetAPIs(s.ApiBackend)
	apis = append(apis, s.engine.APIs(s.BlockChain().HeaderChain())...)
	return append(apis, []rpc.API{
		{
			Namespace: "mbl",
			Version:   "1.0",
			Service:   &LightDummyAPI{},
			Public:    true,
		}, {
			Namespace: "mbl",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.handler.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "mbl",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.ApiBackend, true, 5*time.Minute),
			Public:    true,
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		}, {
			Namespace: "les",
			Version:   "1.0",
			Service:   NewPrivateLightAPI(&s.lesCommons),
			Public:    false,
		}, {
			Namespace: "vflux",
			Version:   "1.0",
			Service:   s.serverPool.API(),
			Public:    false,
		},
	}...)
}

func (s *Lightmbali) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *Lightmbali) BlockChain() *light.LightChain      { return s.blockchain }
func (s *Lightmbali) TxPool() *light.TxPool              { return s.txPool }
func (s *Lightmbali) Engine() consensus.Engine           { return s.engine }
func (s *Lightmbali) LesVersion() int                    { return int(ClientProtocolVersions[0]) }
func (s *Lightmbali) Downloader() *downloader.Downloader { return s.handler.downloader }
func (s *Lightmbali) EventMux() *event.TypeMux           { return s.eventMux }
func (s *Lightmbali) Merger() *consensus.Merger          { return s.merger }

// Protocols returns all the currently configured network protocols to start.
func (s *Lightmbali) Protocols() []p2p.Protocol {
	return s.makeProtocols(ClientProtocolVersions, s.handler.runPeer, func(id enode.ID) interface{} {
		if p := s.peers.peer(id.String()); p != nil {
			return p.Info()
		}
		return nil
	}, s.serverPoolIterator)
}

// Start implements node.Lifecycle, starting all internal goroutines needed by the
// light mbali protocol implementation.
func (s *Lightmbali) Start() error {
	log.Warn("Light client mode is an experimental feature")

	// Regularly update shutdown marker
	s.shutdownTracker.Start()

	if s.udpEnabled && s.p2pServer.DiscV5 == nil {
		s.udpEnabled = false
		log.Error("Discovery v5 is not initialized")
	}
	discovery, err := s.setupDiscovery()
	if err != nil {
		return err
	}
	s.serverPool.AddSource(discovery)
	s.serverPool.Start()
	// Start bloom request workers.
	s.wg.Add(bloomServicmblreads)
	s.startBloomHandlers(params.BloomBitsBlocksClient)
	s.handler.start()

	return nil
}

// Stop implements node.Lifecycle, terminating all internal goroutines used by the
// mbali protocol.
func (s *Lightmbali) Stop() error {
	close(s.closeCh)
	s.serverPool.Stop()
	s.peers.close()
	s.reqDist.close()
	s.odr.Stop()
	s.relay.Stop()
	s.bloomIndexer.Close()
	s.chtIndexer.Close()
	s.blockchain.Stop()
	s.handler.stop()
	s.txPool.Stop()
	s.engine.Close()
	s.pruner.close()
	s.eventMux.Stop()
	// Clean shutdown marker as the last thing before closing db
	s.shutdownTracker.Stop()

	s.chainDb.Close()
	s.lesDb.Close()
	s.wg.Wait()
	log.Info("Light mbali stopped")
	return nil
}
