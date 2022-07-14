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

// This file contains a miner stress test for eip 1559.
package main

import (
	"crypto/ecdsa"
	"math/big"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/common/fdlimit"
	"github.com/mbali/go-mbali/consensus/mblash"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/crypto"
	"github.com/mbali/go-mbali/mbl"
	"github.com/mbali/go-mbali/mbl/downloader"
	"github.com/mbali/go-mbali/mbl/mblconfig"
	"github.com/mbali/go-mbali/log"
	"github.com/mbali/go-mbali/miner"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/enode"
	"github.com/mbali/go-mbali/params"
)

var (
	londonBlock = big.NewInt(30) // Predefined london fork block for activating eip 1559.
)

func main() {
	log.Root().Smblandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	fdlimit.Raise(2048)

	// Generate a batch of accounts to seal and fund with
	faucets := make([]*ecdsa.PrivateKey, 128)
	for i := 0; i < len(faucets); i++ {
		faucets[i], _ = crypto.GenerateKey()
	}
	// Pre-generate the mblash mining DAG so we don't race
	mblash.MakeDataset(1, mblconfig.Defaults.mblash.DatasetDir)

	// Create an mblash network based off of the Ropsten config
	genesis := makeGenesis(faucets)

	// Handle interrupts.
	interruptCh := make(chan os.Signal, 5)
	signal.Notify(interruptCh, os.Interrupt)

	var (
		stacks []*node.Node
		nodes  []*mbl.mbali
		enodes []*enode.Node
	)
	for i := 0; i < 4; i++ {
		// Start the node and wait until it's up
		stack, mblBackend, err := makeMiner(genesis)
		if err != nil {
			panic(err)
		}
		defer stack.Close()

		for stack.Server().NodeInfo().Ports.Listener == 0 {
			time.Sleep(250 * time.Millisecond)
		}
		// Connect the node to all the previous ones
		for _, n := range enodes {
			stack.Server().AddPeer(n)
		}
		// Start tracking the node and its enode
		nodes = append(nodes, mblBackend)
		enodes = append(enodes, stack.Server().Self())
	}

	// Iterate over all the nodes and start mining
	time.Sleep(3 * time.Second)
	for _, node := range nodes {
		if err := node.StartMining(1); err != nil {
			panic(err)
		}
	}
	time.Sleep(3 * time.Second)

	// Start injecting transactions from the faucets like crazy
	var (
		nonces = make([]uint64, len(faucets))

		// The signer activates the 1559 features even before the fork,
		// so the new 1559 txs can be created with this signer.
		signer = types.LatestSignerForChainID(genesis.Config.ChainID)
	)
	for {
		// Stop when interrupted.
		select {
		case <-interruptCh:
			for _, node := range stacks {
				node.Close()
			}
			return
		default:
		}

		// Pick a random mining node
		index := rand.Intn(len(faucets))
		backend := nodes[index%len(nodes)]

		headHeader := backend.BlockChain().CurrentHeader()
		baseFee := headHeader.BaseFee

		// Create a self transaction and inject into the pool. The legacy
		// and 1559 transactions can all be created by random even if the
		// fork is not happened.
		tx := makeTransaction(nonces[index], faucets[index], signer, baseFee)
		if err := backend.TxPool().AddLocal(tx); err != nil {
			continue
		}
		nonces[index]++

		// Wait if we're too saturated
		if pend, _ := backend.TxPool().Stats(); pend > 4192 {
			time.Sleep(100 * time.Millisecond)
		}

		// Wait if the basefee is raised too fast
		if baseFee != nil && baseFee.Cmp(new(big.Int).Mul(big.NewInt(100), big.NewInt(params.GWei))) > 0 {
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func makeTransaction(nonce uint64, privKey *ecdsa.PrivateKey, signer types.Signer, baseFee *big.Int) *types.Transaction {
	// Generate legacy transaction
	if rand.Intn(2) == 0 {
		tx, err := types.SignTx(types.NewTransaction(nonce, crypto.PubkeyToAddress(privKey.PublicKey), new(big.Int), 21000, big.NewInt(100000000000+rand.Int63n(65536)), nil), signer, privKey)
		if err != nil {
			panic(err)
		}
		return tx
	}
	// Generate eip 1559 transaction
	recipient := crypto.PubkeyToAddress(privKey.PublicKey)

	// Feecap and feetip are limited to 32 bytes. Offer a sightly
	// larger buffer for creating both valid and invalid transactions.
	var buf = make([]byte, 32+5)
	rand.Read(buf)
	gasTipCap := new(big.Int).SetBytes(buf)

	// If the given base fee is nil(the 1559 is still not available),
	// generate a fake base fee in order to create 1559 tx forcibly.
	if baseFee == nil {
		baseFee = new(big.Int).SetInt64(int64(rand.Int31()))
	}
	// Generate the feecap, 75% valid feecap and 25% unguaranted.
	var gasFeeCap *big.Int
	if rand.Intn(4) == 0 {
		rand.Read(buf)
		gasFeeCap = new(big.Int).SetBytes(buf)
	} else {
		gasFeeCap = new(big.Int).Add(baseFee, gasTipCap)
	}
	return types.MustSignNewTx(privKey, signer, &types.DynamicFeeTx{
		ChainID:    signer.ChainID(),
		Nonce:      nonce,
		GasTipCap:  gasTipCap,
		GasFeeCap:  gasFeeCap,
		Gas:        21000,
		To:         &recipient,
		Value:      big.NewInt(100),
		Data:       nil,
		AccessList: nil,
	})
}

// makeGenesis creates a custom mblash genesis block based on some pre-defined
// faucet accounts.
func makeGenesis(faucets []*ecdsa.PrivateKey) *core.Genesis {
	genesis := core.DefaultRopstenGenesisBlock()

	genesis.Config = params.AllmblashProtocolChanges
	genesis.Config.LondonBlock = londonBlock
	genesis.Difficulty = params.MinimumDifficulty

	// Small gaslimit for easier basefee moving testing.
	genesis.GasLimit = 8_000_000

	genesis.Config.ChainID = big.NewInt(18)
	genesis.Config.EIP150Hash = common.Hash{}

	genesis.Alloc = core.GenesisAlloc{}
	for _, faucet := range faucets {
		genesis.Alloc[crypto.PubkeyToAddress(faucet.PublicKey)] = core.GenesisAccount{
			Balance: new(big.Int).Exp(big.NewInt(2), big.NewInt(128), nil),
		}
	}
	if londonBlock.Sign() == 0 {
		log.Info("Enabled the eip 1559 by default")
	} else {
		log.Info("Registered the london fork", "number", londonBlock)
	}
	return genesis
}

func makeMiner(genesis *core.Genesis) (*node.Node, *mbl.mbali, error) {
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
		return nil, nil, err
	}
	mblBackend, err := mbl.New(stack, &mblconfig.Config{
		Genesis:         genesis,
		NetworkId:       genesis.Config.ChainID.Uint64(),
		SyncMode:        downloader.FullSync,
		DatabaseCache:   256,
		DatabaseHandles: 256,
		TxPool:          core.DefaultTxPoolConfig,
		GPO:             mblconfig.Defaults.GPO,
		mblash:          mblconfig.Defaults.mblash,
		Miner: miner.Config{
			mblerbase: common.Address{1},
			GasCeil:   genesis.GasLimit * 11 / 10,
			GasPrice:  big.NewInt(1),
			Recommit:  time.Second,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	err = stack.Start()
	return stack, mblBackend, err
}
