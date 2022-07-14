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
	"math/big"

	"github.com/mbali/go-mbali/mbl/protocols/mbl"
	"github.com/mbali/go-mbali/mbl/protocols/snap"
)

// mblPeerInfo represents a short summary of the `mbl` sub-protocol metadata known
// about a connected peer.
type mblPeerInfo struct {
	Version    uint     `json:"version"`    // mbali protocol version negotiated
	Difficulty *big.Int `json:"difficulty"` // Total difficulty of the peer's blockchain
	Head       string   `json:"head"`       // Hex hash of the peer's best owned block
}

// mblPeer is a wrapper around mbl.Peer to maintain a few extra metadata.
type mblPeer struct {
	*mbl.Peer
	snapExt  *snapPeer     // Satellite `snap` connection
	snapWait chan struct{} // Notification channel for snap connections
}

// info gathers and returns some `mbl` protocol metadata known about a peer.
func (p *mblPeer) info() *mblPeerInfo {
	hash, td := p.Head()

	return &mblPeerInfo{
		Version:    p.Version(),
		Difficulty: td,
		Head:       hash.Hex(),
	}
}

// snapPeerInfo represents a short summary of the `snap` sub-protocol metadata known
// about a connected peer.
type snapPeerInfo struct {
	Version uint `json:"version"` // Snapshot protocol version negotiated
}

// snapPeer is a wrapper around snap.Peer to maintain a few extra metadata.
type snapPeer struct {
	*snap.Peer
}

// info gathers and returns some `snap` protocol metadata known about a peer.
func (p *snapPeer) info() *snapPeerInfo {
	return &snapPeerInfo{
		Version: p.Version(),
	}
}
