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

// This file contains a miner stress test for the mbl1/2 transition
package main

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/mbali/go-mbali/accounts/keystore"
	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/common/fdlimit"
	"github.com/mbali/go-mbali/consensus/mblash"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/beacon"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/crypto"
	"github.com/mbali/go-mbali/mbl"
	mblcatalyst "github.com/mbali/go-mbali/mbl/catalyst"
	"github.com/mbali/go-mbali/mbl/downloader"
	"github.com/mbali/go-mbali/mbl/mblconfig"
	"github.com/mbali/go-mbali/les"
	lescatalyst "github.com/mbali/go-mbali/les/catalyst"
	"github.com/mbali/go-mbali/log"
	"github.com/mbali/go-mbali/miner"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/enode"
	"github.com/mbali/go-mbali/params"
)

type nodetype int

const (
	legacyMiningNode nodetype = iota
	legacyNormalNode
	mbl2MiningNode
	mbl2NormalNode
	mbl2LightClient
)

func (typ nodetype) String() string {
	switch typ {
	case legacyMiningNode:
		return "legacyMiningNode"
	case legacyNormalNode:
		return "legacyNormalNode"
	case mbl2MiningNode:
		return "mbl2MiningNode"
	case mbl2NormalNode:
		return "mbl2NormalNode"
	case mbl2LightClient:
		return "mbl2LightClient"
	default:
		return "undefined"
	}
}

var (
	// transitionDifficulty is the target total difficulty for transition
	transitionDifficulty = new(big.Int).Mul(big.NewInt(20), params.MinimumDifficulty)

	// blockInterval is the time interval for creating a new mbl2 block
	blockInterval    = time.Second * 3
	blockIntervalInt = 3

	// finalizationDist is the block distance for finalizing block
	finalizationDist = 10
)

type mblNode struct {
	typ        nodetype
	stack      *node.Node
	enode      *enode.Node
	api        *mblcatalyst.ConsensusAPI
	mblBackend *mbl.mbali
	lapi       *lescatalyst.ConsensusAPI
	lesBackend *les.Lightmbali
}

func newNode(typ nodetype, genesis *core.Genesis, enodes []*enode.Node) *mblNode {
	var (
		err        error
		api        *mblcatalyst.ConsensusAPI
		lapi       *lescatalyst.ConsensusAPI
		stack      *node.Node
		mblBackend *mbl.mbali
		lesBackend *les.Lightmbali
	)
	// Start the node and wait until it's up
	if typ == mbl2LightClient {
		stack, lesBackend, lapi, err = makeLightNode(genesis)
	} else {
		stack, mblBackend, api, err = makeFullNode(genesis)
	}
	if err != nil {
		panic(err)
	}
	for stack.Server().NodeInfo().Ports.Listener == 0 {
		time.Sleep(250 * time.Millisecond)
	}
	// Connect the node to all the previous ones
	for _, n := range enodes {
		stack.Server().AddPeer(n)
	}
	enode := stack.Server().Self()

	// Inject the signer key and start sealing with it
	stack.AccountManager().AddBackend(keystore.NewPlaintextKeyStore("beacon-stress"))
	store := stack.AccountManager().Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
	if _, err := store.NewAccount(""); err != nil {
		panic(err)
	}
	return &mblNode{
		typ:        typ,
		api:        api,
		mblBackend: mblBackend,
		lapi:       lapi,
		lesBackend: lesBackend,
		stack:      stack,
		enode:      enode,
	}
}

func (n *mblNode) assembleBlock(parentHash common.Hash, parentTimestamp uint64) (*beacon.ExecutableDataV1, error) {
	if n.typ != mbl2MiningNode {
		return nil, errors.New("invalid node type")
	}
	timestamp := uint64(time.Now().Unix())
	if timestamp <= parentTimestamp {
		timestamp = parentTimestamp + 1
	}
	payloadAttribute := beacon.PayloadAttributesV1{
		Timestamp:             timestamp,
		Random:                common.Hash{},
		SuggestedFeeRecipient: common.HexToAddress("0xdeadbeef"),
	}
	fcState := beacon.ForkchoiceStateV1{
		HeadBlockHash:      parentHash,
		SafeBlockHash:      common.Hash{},
		FinalizedBlockHash: common.Hash{},
	}
	payload, err := n.api.ForkchoiceUpdatedV1(fcState, &payloadAttribute)
	if err != nil {
		return nil, err
	}
	return n.api.GetPayloadV1(*payload.PayloadID)
}

func (n *mblNode) insertBlock(eb beacon.ExecutableDataV1) error {
	if !mbl2types(n.typ) {
		return errors.New("invalid node type")
	}
	switch n.typ {
	case mbl2NormalNode, mbl2MiningNode:
		newResp, err := n.api.NewPayloadV1(eb)
		if err != nil {
			return err
		} else if newResp.Status != "VALID" {
			return errors.New("failed to insert block")
		}
		return nil
	case mbl2LightClient:
		newResp, err := n.lapi.ExecutePayloadV1(eb)
		if err != nil {
			return err
		} else if newResp.Status != "VALID" {
			return errors.New("failed to insert block")
		}
		return nil
	default:
		return errors.New("undefined node")
	}
}

func (n *mblNode) insertBlockAndSmblead(parent *types.Header, ed beacon.ExecutableDataV1) error {
	if !mbl2types(n.typ) {
		return errors.New("invalid node type")
	}
	if err := n.insertBlock(ed); err != nil {
		return err
	}
	block, err := beacon.ExecutableDataToBlock(ed)
	if err != nil {
		return err
	}
	fcState := beacon.ForkchoiceStateV1{
		HeadBlockHash:      block.ParentHash(),
		SafeBlockHash:      common.Hash{},
		FinalizedBlockHash: common.Hash{},
	}
	switch n.typ {
	case mbl2NormalNode, mbl2MiningNode:
		if _, err := n.api.ForkchoiceUpdatedV1(fcState, nil); err != nil {
			return err
		}
		return nil
	case mbl2LightClient:
		if _, err := n.lapi.ForkchoiceUpdatedV1(fcState, nil); err != nil {
			return err
		}
		return nil
	default:
		return errors.New("undefined node")
	}
}

type nodeManager struct {
	genesis      *core.Genesis
	genesisBlock *types.Block
	nodes        []*mblNode
	enodes       []*enode.Node
	close        chan struct{}
}

func newNodeManager(genesis *core.Genesis) *nodeManager {
	return &nodeManager{
		close:        make(chan struct{}),
		genesis:      genesis,
		genesisBlock: genesis.ToBlock(nil),
	}
}

func (mgr *nodeManager) createNode(typ nodetype) {
	node := newNode(typ, mgr.genesis, mgr.enodes)
	mgr.nodes = append(mgr.nodes, node)
	mgr.enodes = append(mgr.enodes, node.enode)
}

func (mgr *nodeManager) getNodes(typ nodetype) []*mblNode {
	var ret []*mblNode
	for _, node := range mgr.nodes {
		if node.typ == typ {
			ret = append(ret, node)
		}
	}
	return ret
}

func (mgr *nodeManager) startMining() {
	for _, node := range append(mgr.getNodes(mbl2MiningNode), mgr.getNodes(legacyMiningNode)...) {
		if err := node.mblBackend.StartMining(1); err != nil {
			panic(err)
		}
	}
}

func (mgr *nodeManager) shutdown() {
	close(mgr.close)
	for _, node := range mgr.nodes {
		node.stack.Close()
	}
}

func (mgr *nodeManager) run() {
	if len(mgr.nodes) == 0 {
		return
	}
	chain := mgr.nodes[0].mblBackend.BlockChain()
	sink := make(chan core.ChainHeadEvent, 1024)
	sub := chain.SubscribeChainHeadEvent(sink)
	defer sub.Unsubscribe()

	var (
		transitioned bool
		parentBlock  *types.Block
		waitFinalise []*types.Block
	)
	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	// Handle the by default transition.
	if transitionDifficulty.Sign() == 0 {
		transitioned = true
		parentBlock = mgr.genesisBlock
		timer.Reset(blockInterval)
		log.Info("Enable the transition by default")
	}

	// Handle the block finalization.
	checkFinalise := func() {
		if parentBlock == nil {
			return
		}
		if len(waitFinalise) == 0 {
			return
		}
		oldest := waitFinalise[0]
		if oldest.NumberU64() > parentBlock.NumberU64() {
			return
		}
		distance := parentBlock.NumberU64() - oldest.NumberU64()
		if int(distance) < finalizationDist {
			return
		}
		nodes := mgr.getNodes(mbl2MiningNode)
		nodes = append(nodes, mgr.getNodes(mbl2NormalNode)...)
		nodes = append(nodes, mgr.getNodes(mbl2LightClient)...)
		for _, node := range append(nodes) {
			fcState := beacon.ForkchoiceStateV1{
				HeadBlockHash:      oldest.Hash(),
				SafeBlockHash:      common.Hash{},
				FinalizedBlockHash: oldest.Hash(),
			}
			// TODO(rjl493456442) finalization doesn't work properly, FIX IT
			_ = fcState
			_ = node
			//node.api.ForkchoiceUpdatedV1(fcState, nil)
		}
		log.Info("Finalised mbl2 block", "number", oldest.NumberU64(), "hash", oldest.Hash())
		waitFinalise = waitFinalise[1:]
	}

	for {
		checkFinalise()
		select {
		case <-mgr.close:
			return

		case ev := <-sink:
			if transitioned {
				continue
			}
			td := chain.GetTd(ev.Block.Hash(), ev.Block.NumberU64())
			if td.Cmp(transitionDifficulty) < 0 {
				continue
			}
			transitioned, parentBlock = true, ev.Block
			timer.Reset(blockInterval)
			log.Info("Transition difficulty reached", "td", td, "target", transitionDifficulty, "number", ev.Block.NumberU64(), "hash", ev.Block.Hash())

		case <-timer.C:
			producers := mgr.getNodes(mbl2MiningNode)
			if len(producers) == 0 {
				continue
			}
			hash, timestamp := parentBlock.Hash(), parentBlock.Time()
			if parentBlock.NumberU64() == 0 {
				timestamp = uint64(time.Now().Unix()) - uint64(blockIntervalInt)
			}
			ed, err := producers[0].assembleBlock(hash, timestamp)
			if err != nil {
				log.Error("Failed to assemble the block", "err", err)
				continue
			}
			block, _ := beacon.ExecutableDataToBlock(*ed)

			nodes := mgr.getNodes(mbl2MiningNode)
			nodes = append(nodes, mgr.getNodes(mbl2NormalNode)...)
			nodes = append(nodes, mgr.getNodes(mbl2LightClient)...)
			for _, node := range nodes {
				if err := node.insertBlockAndSmblead(parentBlock.Header(), *ed); err != nil {
					log.Error("Failed to insert block", "type", node.typ, "err", err)
				}
			}
			log.Info("Create and insert mbl2 block", "number", ed.Number)
			parentBlock = block
			waitFinalise = append(waitFinalise, block)
			timer.Reset(blockInterval)
		}
	}
}

func main() {
	log.Root().Smblandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	fdlimit.Raise(2048)

	// Generate a batch of accounts to seal and fund with
	faucets := make([]*ecdsa.PrivateKey, 16)
	for i := 0; i < len(faucets); i++ {
		faucets[i], _ = crypto.GenerateKey()
	}
	// Pre-generate the mblash mining DAG so we don't race
	mblash.MakeDataset(1, filepath.Join(os.Getenv("HOME"), ".mblash"))

	// Create an mblash network based off of the Ropsten config
	genesis := makeGenesis(faucets)
	manager := newNodeManager(genesis)
	defer manager.shutdown()

	manager.createNode(mbl2NormalNode)
	manager.createNode(mbl2MiningNode)
	manager.createNode(legacyMiningNode)
	manager.createNode(legacyNormalNode)
	manager.createNode(mbl2LightClient)

	// Iterate over all the nodes and start mining
	time.Sleep(3 * time.Second)
	if transitionDifficulty.Sign() != 0 {
		manager.startMining()
	}
	go manager.run()

	// Start injecting transactions from the faucets like crazy
	time.Sleep(3 * time.Second)
	nonces := make([]uint64, len(faucets))
	for {
		// Pick a random mining node
		nodes := manager.getNodes(mbl2MiningNode)

		index := rand.Intn(len(faucets))
		node := nodes[index%len(nodes)]

		// Create a self transaction and inject into the pool
		tx, err := types.SignTx(types.NewTransaction(nonces[index], crypto.PubkeyToAddress(faucets[index].PublicKey), new(big.Int), 21000, big.NewInt(100000000000+rand.Int63n(65536)), nil), types.HomesteadSigner{}, faucets[index])
		if err != nil {
			panic(err)
		}
		if err := node.mblBackend.TxPool().AddLocal(tx); err != nil {
			panic(err)
		}
		nonces[index]++

		// Wait if we're too saturated
		if pend, _ := node.mblBackend.TxPool().Stats(); pend > 2048 {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// makeGenesis creates a custom mblash genesis block based on some pre-defined
// faucet accounts.
func makeGenesis(faucets []*ecdsa.PrivateKey) *core.Genesis {
	genesis := core.DefaultRopstenGenesisBlock()
	genesis.Difficulty = params.MinimumDifficulty
	genesis.GasLimit = 25000000

	genesis.BaseFee = big.NewInt(params.InitialBaseFee)
	genesis.Config = params.AllmblashProtocolChanges
	genesis.Config.TerminalTotalDifficulty = transitionDifficulty

	genesis.Alloc = core.GenesisAlloc{}
	for _, faucet := range faucets {
		genesis.Alloc[crypto.PubkeyToAddress(faucet.PublicKey)] = core.GenesisAccount{
			Balance: new(big.Int).Exp(big.NewInt(2), big.NewInt(128), nil),
		}
	}
	return genesis
}

func makeFullNode(genesis *core.Genesis) (*node.Node, *mbl.mbali, *mblcatalyst.ConsensusAPI, error) {
	// Define the basic configurations for the mbali node
	datadir, _ := os.MkdirTemp("", "")

	config := &node.Config{
		Name:    "gombl",
		Version: params.Version,
		DataDir: datadir,
		P2P: p2p.Config{
			ListenAddr:  "0.0.0.0:0",
			NoDiscovery: true,
			MaxPeers:    25,
		},
		UseLightweightKDF: true,
	}
	// Create the node and configure a full mbali node on it
	stack, err := node.New(config)
	if err != nil {
		return nil, nil, nil, err
	}
	econfig := &mblconfig.Config{
		Genesis:         genesis,
		NetworkId:       genesis.Config.ChainID.Uint64(),
		SyncMode:        downloader.FullSync,
		DatabaseCache:   256,
		DatabaseHandles: 256,
		TxPool:          core.DefaultTxPoolConfig,
		GPO:             mblconfig.Defaults.GPO,
		mblash:          mblconfig.Defaults.mblash,
		Miner: miner.Config{
			GasFloor: genesis.GasLimit * 9 / 10,
			GasCeil:  genesis.GasLimit * 11 / 10,
			GasPrice: big.NewInt(1),
			Recommit: 10 * time.Second, // Disable the recommit
		},
		LightServ:        100,
		LightPeers:       10,
		LightNoSyncServe: true,
	}
	mblBackend, err := mbl.New(stack, econfig)
	if err != nil {
		return nil, nil, nil, err
	}
	_, err = les.NewLesServer(stack, mblBackend, econfig)
	if err != nil {
		log.Crit("Failed to create the LES server", "err", err)
	}
	err = stack.Start()
	return stack, mblBackend, mblcatalyst.NewConsensusAPI(mblBackend), err
}

func makeLightNode(genesis *core.Genesis) (*node.Node, *les.Lightmbali, *lescatalyst.ConsensusAPI, error) {
	// Define the basic configurations for the mbali node
	datadir, _ := os.MkdirTemp("", "")

	config := &node.Config{
		Name:    "gombl",
		Version: params.Version,
		DataDir: datadir,
		P2P: p2p.Config{
			ListenAddr:  "0.0.0.0:0",
			NoDiscovery: true,
			MaxPeers:    25,
		},
		UseLightweightKDF: true,
	}
	// Create the node and configure a full mbali node on it
	stack, err := node.New(config)
	if err != nil {
		return nil, nil, nil, err
	}
	lesBackend, err := les.New(stack, &mblconfig.Config{
		Genesis:         genesis,
		NetworkId:       genesis.Config.ChainID.Uint64(),
		SyncMode:        downloader.LightSync,
		DatabaseCache:   256,
		DatabaseHandles: 256,
		TxPool:          core.DefaultTxPoolConfig,
		GPO:             mblconfig.Defaults.GPO,
		mblash:          mblconfig.Defaults.mblash,
		LightPeers:      10,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	err = stack.Start()
	return stack, lesBackend, lescatalyst.NewConsensusAPI(lesBackend), err
}

func mbl2types(typ nodetype) bool {
	if typ == mbl2LightClient || typ == mbl2NormalNode || typ == mbl2MiningNode {
		return true
	}
	return false
}
