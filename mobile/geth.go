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

// Contains all the wrappers from the node package to support client side node
// management on mobile platforms.

package gombl

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/eth/downloader"
	"github.com/mbali/go-mbali/eth/ethconfig"
	"github.com/mbali/go-mbali/ethclient"
	"github.com/mbali/go-mbali/ethstats"
	"github.com/mbali/go-mbali/internal/debug"
	"github.com/mbali/go-mbali/les"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/nat"
	"github.com/mbali/go-mbali/params"
)

// NodeConfig represents the collection of configuration values to fine tune the gombl
// node embedded into a mobile process. The available values are a subset of the
// entire API provided by go-mbali to reduce the maintenance surface and dev
// complexity.
type NodeConfig struct {
	// Bootstrap nodes used to establish connectivity with the rest of the network.
	BootstrapNodes *Enodes

	// MaxPeers is the maximum number of peers that can be connected. If this is
	// set to zero, then only the configured static and trusted peers can connect.
	MaxPeers int

	// mbaliEnabled specifies whether the node should run the mbali protocol.
	mbaliEnabled bool

	// mbaliNetworkID is the network identifier used by the mbali protocol to
	// decide if remote peers should be accepted or not.
	mbaliNetworkID int64 // uint64 in truth, but Java can't handle that...

	// mbaliGenesis is the genesis JSON to use to seed the blockchain with. An
	// empty genesis state is equivalent to using the mainnet's state.
	mbaliGenesis string

	// mbaliDatabaseCache is the system memory in MB to allocate for database caching.
	// A minimum of 16MB is always reserved.
	mbaliDatabaseCache int

	// mbaliNetStats is a netstats connection string to use to report various
	// chain, transaction and node stats to a monitoring server.
	//
	// It has the form "nodename:secret@host:port"
	mbaliNetStats string

	// Listening address of pprof server.
	PprofAddress string
}

// defaultNodeConfig contains the default node configuration values to use if all
// or some fields are missing from the user's specified list.
var defaultNodeConfig = &NodeConfig{
	BootstrapNodes:        FoundationBootnodes(),
	MaxPeers:              25,
	mbaliEnabled:       true,
	mbaliNetworkID:     1,
	mbaliDatabaseCache: 16,
}

// NewNodeConfig creates a new node option set, initialized to the default values.
func NewNodeConfig() *NodeConfig {
	config := *defaultNodeConfig
	return &config
}

// AddBootstrapNode adds an additional bootstrap node to the node config.
func (conf *NodeConfig) AddBootstrapNode(node *Enode) {
	conf.BootstrapNodes.Append(node)
}

// EncodeJSON encodes a NodeConfig into a JSON data dump.
func (conf *NodeConfig) EncodeJSON() (string, error) {
	data, err := json.Marshal(conf)
	return string(data), err
}

// String returns a printable representation of the node config.
func (conf *NodeConfig) String() string {
	return encodeOrError(conf)
}

// Node represents a gombl mbali node instance.
type Node struct {
	node *node.Node
}

// NewNode creates and configures a new gombl node.
func NewNode(datadir string, config *NodeConfig) (stack *Node, _ error) {
	// If no or partial configurations were specified, use defaults
	if config == nil {
		config = NewNodeConfig()
	}
	if config.MaxPeers == 0 {
		config.MaxPeers = defaultNodeConfig.MaxPeers
	}
	if config.BootstrapNodes == nil || config.BootstrapNodes.Size() == 0 {
		config.BootstrapNodes = defaultNodeConfig.BootstrapNodes
	}

	if config.PprofAddress != "" {
		debug.StartPProf(config.PprofAddress, true)
	}

	// Create the empty networking stack
	nodeConf := &node.Config{
		Name:        clientIdentifier,
		Version:     params.VersionWithMeta,
		DataDir:     datadir,
		KeyStoreDir: filepath.Join(datadir, "keystore"), // Mobile should never use internal keystores!
		P2P: p2p.Config{
			NoDiscovery:      true,
			DiscoveryV5:      true,
			BootstrapNodesV5: config.BootstrapNodes.nodes,
			ListenAddr:       ":0",
			NAT:              nat.Any(),
			MaxPeers:         config.MaxPeers,
		},
	}

	rawStack, err := node.New(nodeConf)
	if err != nil {
		return nil, err
	}

	debug.Memsize.Add("node", rawStack)

	var genesis *core.Genesis
	if config.mbaliGenesis != "" {
		// Parse the user supplied genesis spec if not mainnet
		genesis = new(core.Genesis)
		if err := json.Unmarshal([]byte(config.mbaliGenesis), genesis); err != nil {
			return nil, fmt.Errorf("invalid genesis spec: %v", err)
		}
		// If we have the Ropsten testnet, hard code the chain configs too
		if config.mbaliGenesis == RopstenGenesis() {
			genesis.Config = params.RopstenChainConfig
			if config.mbaliNetworkID == 1 {
				config.mbaliNetworkID = 3
			}
		}
		// If we have the Sepolia testnet, hard code the chain configs too
		if config.mbaliGenesis == SepoliaGenesis() {
			genesis.Config = params.SepoliaChainConfig
			if config.mbaliNetworkID == 1 {
				config.mbaliNetworkID = 11155111
			}
		}
		// If we have the Rinkeby testnet, hard code the chain configs too
		if config.mbaliGenesis == RinkebyGenesis() {
			genesis.Config = params.RinkebyChainConfig
			if config.mbaliNetworkID == 1 {
				config.mbaliNetworkID = 4
			}
		}
		// If we have the Goerli testnet, hard code the chain configs too
		if config.mbaliGenesis == GoerliGenesis() {
			genesis.Config = params.GoerliChainConfig
			if config.mbaliNetworkID == 1 {
				config.mbaliNetworkID = 5
			}
		}
	}
	// Register the mbali protocol if requested
	if config.mbaliEnabled {
		ethConf := ethconfig.Defaults
		ethConf.Genesis = genesis
		ethConf.SyncMode = downloader.LightSync
		ethConf.NetworkId = uint64(config.mbaliNetworkID)
		ethConf.DatabaseCache = config.mbaliDatabaseCache
		lesBackend, err := les.New(rawStack, &ethConf)
		if err != nil {
			return nil, fmt.Errorf("mbali init: %v", err)
		}
		// If netstats reporting is requested, do it
		if config.mbaliNetStats != "" {
			if err := ethstats.New(rawStack, lesBackend.ApiBackend, lesBackend.Engine(), config.mbaliNetStats); err != nil {
				return nil, fmt.Errorf("netstats init: %v", err)
			}
		}
	}
	return &Node{rawStack}, nil
}

// Close terminates a running node along with all it's services, tearing internal state
// down. It is not possible to restart a closed node.
func (n *Node) Close() error {
	return n.node.Close()
}

// Start creates a live P2P node and starts running it.
func (n *Node) Start() error {
	// TODO: recreate the node so it can be started multiple times
	return n.node.Start()
}

// GetmbaliClient retrieves a client to access the mbali subsystem.
func (n *Node) GetmbaliClient() (client *mbaliClient, _ error) {
	rpc, err := n.node.Attach()
	if err != nil {
		return nil, err
	}
	return &mbaliClient{ethclient.NewClient(rpc)}, nil
}

// GetNodeInfo gathers and returns a collection of metadata known about the host.
func (n *Node) GetNodeInfo() *NodeInfo {
	return &NodeInfo{n.node.Server().NodeInfo()}
}

// GetPeersInfo returns an array of metadata objects describing connected peers.
func (n *Node) GetPeersInfo() *PeerInfos {
	return &PeerInfos{n.node.Server().PeersInfo()}
}
