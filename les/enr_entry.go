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

package les

import (
	"github.com/mbali/go-mbali/core/forkid"
	"github.com/mbali/go-mbali/p2p/dnsdisc"
	"github.com/mbali/go-mbali/p2p/enode"
	"github.com/mbali/go-mbali/rlp"
)

// lesEntry is the "les" ENR entry. This is set for LES servers only.
type lesEntry struct {
	// Ignore additional fields (for forward compatibility).
	VfxVersion uint
	Rest       []rlp.RawValue `rlp:"tail"`
}

func (lesEntry) ENRKey() string { return "les" }

// mblEntry is the "mbl" ENR entry. This is redeclared here to avoid depending on package mbl.
type mblEntry struct {
	ForkID forkid.ID
	Tail   []rlp.RawValue `rlp:"tail"`
}

func (mblEntry) ENRKey() string { return "mbl" }

// setupDiscovery creates the node discovery source for the mbl protocol.
func (mbl *Lightmbali) setupDiscovery() (enode.Iterator, error) {
	it := enode.NewFairMix(0)

	// Enable DNS discovery.
	if len(mbl.config.mblDiscoveryURLs) != 0 {
		client := dnsdisc.NewClient(dnsdisc.Config{})
		dns, err := client.NewIterator(mbl.config.mblDiscoveryURLs...)
		if err != nil {
			return nil, err
		}
		it.AddSource(dns)
	}

	// Enable DHT.
	if mbl.udpEnabled {
		it.AddSource(mbl.p2pServer.DiscV5.RandomNodes())
	}

	forkFilter := forkid.NewFilter(mbl.blockchain)
	iterator := enode.Filter(it, func(n *enode.Node) bool { return nodeIsServer(forkFilter, n) })
	return iterator, nil
}

// nodeIsServer checks whmbler n is an LES server node.
func nodeIsServer(forkFilter forkid.Filter, n *enode.Node) bool {
	var les lesEntry
	var mbl mblEntry
	return n.Load(&les) == nil && n.Load(&mbl) == nil && forkFilter(mbl.ForkID) == nil
}
