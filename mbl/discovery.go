// Copyright 2020 The go-mbali Authors
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
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/forkid"
	"github.com/mbali/go-mbali/p2p/enode"
	"github.com/mbali/go-mbali/rlp"
)

// mblEntry is the "mbl" ENR entry which advertises mbl protocol
// on the discovery network.
type mblEntry struct {
	ForkID forkid.ID // Fork identifier per EIP-2124

	// Ignore additional fields (for forward compatibility).
	Rest []rlp.RawValue `rlp:"tail"`
}

// ENRKey implements enr.Entry.
func (e mblEntry) ENRKey() string {
	return "mbl"
}

// startmblEntryUpdate starts the ENR updater loop.
func (mbl *mbali) startmblEntryUpdate(ln *enode.LocalNode) {
	var newHead = make(chan core.ChainHeadEvent, 10)
	sub := mbl.blockchain.SubscribeChainHeadEvent(newHead)

	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case <-newHead:
				ln.Set(mbl.currentmblEntry())
			case <-sub.Err():
				// Would be nice to sync with mbl.Stop, but there is no
				// good way to do that.
				return
			}
		}
	}()
}

func (mbl *mbali) currentmblEntry() *mblEntry {
	return &mblEntry{ForkID: forkid.NewID(mbl.blockchain.Config(), mbl.blockchain.Genesis().Hash(),
		mbl.blockchain.CurrentHeader().Number.Uint64())}
}
