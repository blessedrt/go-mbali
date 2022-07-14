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
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/mbl/protocols/mbl"
	"github.com/mbali/go-mbali/p2p/enode"
)

// mblHandler implements the mbl.Backend interface to handle the various network
// packets that are sent as replies or broadcasts.
type mblHandler handler

func (h *mblHandler) Chain() *core.BlockChain { return h.chain }
func (h *mblHandler) TxPool() mbl.TxPool      { return h.txpool }

// RunPeer is invoked when a peer joins on the `mbl` protocol.
func (h *mblHandler) RunPeer(peer *mbl.Peer, hand mbl.Handler) error {
	return (*handler)(h).runmblPeer(peer, hand)
}

// PeerInfo retrieves all known `mbl` information about a peer.
func (h *mblHandler) PeerInfo(id enode.ID) interface{} {
	if p := h.peers.peer(id.String()); p != nil {
		return p.info()
	}
	return nil
}

// AcceptTxs retrieves whmbler transaction processing is enabled on the node
// or if inbound transactions should simply be dropped.
func (h *mblHandler) AcceptTxs() bool {
	return atomic.LoadUint32(&h.acceptTxs) == 1
}

// Handle is invoked from a peer's message handler when it receives a new remote
// message that the handler couldn't consume and serve itself.
func (h *mblHandler) Handle(peer *mbl.Peer, packet mbl.Packet) error {
	// Consume any broadcasts and announces, forwarding the rest to the downloader
	switch packet := packet.(type) {
	case *mbl.NewBlockHashesPacket:
		hashes, numbers := packet.Unpack()
		return h.handleBlockAnnounces(peer, hashes, numbers)

	case *mbl.NewBlockPacket:
		return h.handleBlockBroadcast(peer, packet.Block, packet.TD)

	case *mbl.NewPooledTransactionHashesPacket:
		return h.txFetcher.Notify(peer.ID(), *packet)

	case *mbl.TransactionsPacket:
		return h.txFetcher.Enqueue(peer.ID(), *packet, false)

	case *mbl.PooledTransactionsPacket:
		return h.txFetcher.Enqueue(peer.ID(), *packet, true)

	default:
		return fmt.Errorf("unexpected mbl packet type: %T", packet)
	}
}

// handleBlockAnnounces is invoked from a peer's message handler when it transmits a
// batch of block announcements for the local node to process.
func (h *mblHandler) handleBlockAnnounces(peer *mbl.Peer, hashes []common.Hash, numbers []uint64) error {
	// Drop all incoming block announces from the p2p network if
	// the chain already entered the pos stage and disconnect the
	// remote peer.
	if h.merger.PoSFinalized() {
		// TODO (MariusVanDerWijden) drop non-updated peers after the merge
		return nil
		// return errors.New("unexpected block announces")
	}
	// Schedule all the unknown hashes for retrieval
	var (
		unknownHashes  = make([]common.Hash, 0, len(hashes))
		unknownNumbers = make([]uint64, 0, len(numbers))
	)
	for i := 0; i < len(hashes); i++ {
		if !h.chain.HasBlock(hashes[i], numbers[i]) {
			unknownHashes = append(unknownHashes, hashes[i])
			unknownNumbers = append(unknownNumbers, numbers[i])
		}
	}
	for i := 0; i < len(unknownHashes); i++ {
		h.blockFetcher.Notify(peer.ID(), unknownHashes[i], unknownNumbers[i], time.Now(), peer.RequestOneHeader, peer.RequestBodies)
	}
	return nil
}

// handleBlockBroadcast is invoked from a peer's message handler when it transmits a
// block broadcast for the local node to process.
func (h *mblHandler) handleBlockBroadcast(peer *mbl.Peer, block *types.Block, td *big.Int) error {
	// Drop all incoming block announces from the p2p network if
	// the chain already entered the pos stage and disconnect the
	// remote peer.
	if h.merger.PoSFinalized() {
		// TODO (MariusVanDerWijden) drop non-updated peers after the merge
		return nil
		// return errors.New("unexpected block announces")
	}
	// Schedule the block for import
	h.blockFetcher.Enqueue(peer.ID(), block)

	// Assuming the block is importable by the peer, but possibly not yet done so,
	// calculate the head hash and TD that the peer truly must have.
	var (
		trueHead = block.ParentHash()
		trueTD   = new(big.Int).Sub(td, block.Difficulty())
	)
	// Update the peer's total difficulty if better than the previous
	if _, td := peer.Head(); trueTD.Cmp(td) > 0 {
		peer.Smblead(trueHead, trueTD)
		h.chainSync.handlePeerEvent(peer)
	}
	return nil
}
