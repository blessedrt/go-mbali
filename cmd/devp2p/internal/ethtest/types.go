// Copyright 2020 The go-mbali Authors
// This file is part of go-mbali.
//
// go-mbali is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-mbali is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-mbali. If not, see <http://www.gnu.org/licenses/>.

package mbltest

import (
	"crypto/ecdsa"
	"fmt"
	"time"

	"github.com/mbali/go-mbali/mbl/protocols/mbl"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/rlpx"
	"github.com/mbali/go-mbali/rlp"
)

type Message interface {
	Code() int
}

type Error struct {
	err error
}

func (e *Error) Unwrap() error  { return e.err }
func (e *Error) Error() string  { return e.err.Error() }
func (e *Error) Code() int      { return -1 }
func (e *Error) String() string { return e.Error() }

func errorf(format string, args ...interface{}) *Error {
	return &Error{fmt.Errorf(format, args...)}
}

// Hello is the RLP structure of the protocol handshake.
type Hello struct {
	Version    uint64
	Name       string
	Caps       []p2p.Cap
	ListenPort uint64
	ID         []byte // secp256k1 public key

	// Ignore additional fields (for forward compatibility).
	Rest []rlp.RawValue `rlp:"tail"`
}

func (h Hello) Code() int { return 0x00 }

// Disconnect is the RLP structure for a disconnect message.
type Disconnect struct {
	Reason p2p.DiscReason
}

func (d Disconnect) Code() int { return 0x01 }

type Ping struct{}

func (p Ping) Code() int { return 0x02 }

type Pong struct{}

func (p Pong) Code() int { return 0x03 }

// Status is the network packet for the status message for mbl/64 and later.
type Status mbl.StatusPacket

func (s Status) Code() int { return 16 }

// NewBlockHashes is the network packet for the block announcements.
type NewBlockHashes mbl.NewBlockHashesPacket

func (nbh NewBlockHashes) Code() int { return 17 }

type Transactions mbl.TransactionsPacket

func (t Transactions) Code() int { return 18 }

// GetBlockHeaders represents a block header query.
type GetBlockHeaders mbl.GetBlockHeadersPacket

func (g GetBlockHeaders) Code() int { return 19 }

type BlockHeaders mbl.BlockHeadersPacket

func (bh BlockHeaders) Code() int { return 20 }

// GetBlockBodies represents a GetBlockBodies request
type GetBlockBodies mbl.GetBlockBodiesPacket

func (gbb GetBlockBodies) Code() int { return 21 }

// BlockBodies is the network packet for block content distribution.
type BlockBodies mbl.BlockBodiesPacket

func (bb BlockBodies) Code() int { return 22 }

// NewBlock is the network packet for the block propagation message.
type NewBlock mbl.NewBlockPacket

func (nb NewBlock) Code() int { return 23 }

// NewPooledTransactionHashes is the network packet for the tx hash propagation message.
type NewPooledTransactionHashes mbl.NewPooledTransactionHashesPacket

func (nb NewPooledTransactionHashes) Code() int { return 24 }

type GetPooledTransactions mbl.GetPooledTransactionsPacket

func (gpt GetPooledTransactions) Code() int { return 25 }

type PooledTransactions mbl.PooledTransactionsPacket

func (pt PooledTransactions) Code() int { return 26 }

// Conn represents an individual connection with a peer
type Conn struct {
	*rlpx.Conn
	ourKey                     *ecdsa.PrivateKey
	negotiatedProtoVersion     uint
	negotiatedSnapProtoVersion uint
	ourHighestProtoVersion     uint
	ourHighestSnapProtoVersion uint
	caps                       []p2p.Cap
}

// Read reads an mbl packet from the connection.
func (c *Conn) Read() Message {
	code, rawData, _, err := c.Conn.Read()
	if err != nil {
		return errorf("could not read from connection: %v", err)
	}

	var msg Message
	switch int(code) {
	case (Hello{}).Code():
		msg = new(Hello)
	case (Ping{}).Code():
		msg = new(Ping)
	case (Pong{}).Code():
		msg = new(Pong)
	case (Disconnect{}).Code():
		msg = new(Disconnect)
	case (Status{}).Code():
		msg = new(Status)
	case (GetBlockHeaders{}).Code():
		msg = new(GetBlockHeaders)
	case (BlockHeaders{}).Code():
		msg = new(BlockHeaders)
	case (GetBlockBodies{}).Code():
		msg = new(GetBlockBodies)
	case (BlockBodies{}).Code():
		msg = new(BlockBodies)
	case (NewBlock{}).Code():
		msg = new(NewBlock)
	case (NewBlockHashes{}).Code():
		msg = new(NewBlockHashes)
	case (Transactions{}).Code():
		msg = new(Transactions)
	case (NewPooledTransactionHashes{}).Code():
		msg = new(NewPooledTransactionHashes)
	case (GetPooledTransactions{}.Code()):
		msg = new(GetPooledTransactions)
	case (PooledTransactions{}.Code()):
		msg = new(PooledTransactions)
	default:
		return errorf("invalid message code: %d", code)
	}
	// if message is devp2p, decode here
	if err := rlp.DecodeBytes(rawData, msg); err != nil {
		return errorf("could not rlp decode message: %v", err)
	}
	return msg
}

// Read66 reads an mbl66 packet from the connection.
func (c *Conn) Read66() (uint64, Message) {
	code, rawData, _, err := c.Conn.Read()
	if err != nil {
		return 0, errorf("could not read from connection: %v", err)
	}

	var msg Message
	switch int(code) {
	case (Hello{}).Code():
		msg = new(Hello)
	case (Ping{}).Code():
		msg = new(Ping)
	case (Pong{}).Code():
		msg = new(Pong)
	case (Disconnect{}).Code():
		msg = new(Disconnect)
	case (Status{}).Code():
		msg = new(Status)
	case (GetBlockHeaders{}).Code():
		mblMsg := new(mbl.GetBlockHeadersPacket66)
		if err := rlp.DecodeBytes(rawData, mblMsg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return mblMsg.RequestId, GetBlockHeaders(*mblMsg.GetBlockHeadersPacket)
	case (BlockHeaders{}).Code():
		mblMsg := new(mbl.BlockHeadersPacket66)
		if err := rlp.DecodeBytes(rawData, mblMsg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return mblMsg.RequestId, BlockHeaders(mblMsg.BlockHeadersPacket)
	case (GetBlockBodies{}).Code():
		mblMsg := new(mbl.GetBlockBodiesPacket66)
		if err := rlp.DecodeBytes(rawData, mblMsg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return mblMsg.RequestId, GetBlockBodies(mblMsg.GetBlockBodiesPacket)
	case (BlockBodies{}).Code():
		mblMsg := new(mbl.BlockBodiesPacket66)
		if err := rlp.DecodeBytes(rawData, mblMsg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return mblMsg.RequestId, BlockBodies(mblMsg.BlockBodiesPacket)
	case (NewBlock{}).Code():
		msg = new(NewBlock)
	case (NewBlockHashes{}).Code():
		msg = new(NewBlockHashes)
	case (Transactions{}).Code():
		msg = new(Transactions)
	case (NewPooledTransactionHashes{}).Code():
		msg = new(NewPooledTransactionHashes)
	case (GetPooledTransactions{}.Code()):
		mblMsg := new(mbl.GetPooledTransactionsPacket66)
		if err := rlp.DecodeBytes(rawData, mblMsg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return mblMsg.RequestId, GetPooledTransactions(mblMsg.GetPooledTransactionsPacket)
	case (PooledTransactions{}.Code()):
		mblMsg := new(mbl.PooledTransactionsPacket66)
		if err := rlp.DecodeBytes(rawData, mblMsg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return mblMsg.RequestId, PooledTransactions(mblMsg.PooledTransactionsPacket)
	default:
		msg = errorf("invalid message code: %d", code)
	}

	if msg != nil {
		if err := rlp.DecodeBytes(rawData, msg); err != nil {
			return 0, errorf("could not rlp decode message: %v", err)
		}
		return 0, msg
	}
	return 0, errorf("invalid message: %s", string(rawData))
}

// Write writes a mbl packet to the connection.
func (c *Conn) Write(msg Message) error {
	payload, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return err
	}
	_, err = c.Conn.Write(uint64(msg.Code()), payload)
	return err
}

// Write66 writes an mbl66 packet to the connection.
func (c *Conn) Write66(req mbl.Packet, code int) error {
	payload, err := rlp.EncodeToBytes(req)
	if err != nil {
		return err
	}
	_, err = c.Conn.Write(uint64(code), payload)
	return err
}

// ReadSnap reads a snap/1 response with the given id from the connection.
func (c *Conn) ReadSnap(id uint64) (Message, error) {
	respId := id + 1
	start := time.Now()
	for respId != id && time.Since(start) < timeout {
		code, rawData, _, err := c.Conn.Read()
		if err != nil {
			return nil, fmt.Errorf("could not read from connection: %v", err)
		}
		var snpMsg interface{}
		switch int(code) {
		case (GetAccountRange{}).Code():
			snpMsg = new(GetAccountRange)
		case (AccountRange{}).Code():
			snpMsg = new(AccountRange)
		case (GetStorageRanges{}).Code():
			snpMsg = new(GetStorageRanges)
		case (StorageRanges{}).Code():
			snpMsg = new(StorageRanges)
		case (GetByteCodes{}).Code():
			snpMsg = new(GetByteCodes)
		case (ByteCodes{}).Code():
			snpMsg = new(ByteCodes)
		case (GetTrieNodes{}).Code():
			snpMsg = new(GetTrieNodes)
		case (TrieNodes{}).Code():
			snpMsg = new(TrieNodes)
		default:
			//return nil, fmt.Errorf("invalid message code: %d", code)
			continue
		}
		if err := rlp.DecodeBytes(rawData, snpMsg); err != nil {
			return nil, fmt.Errorf("could not rlp decode message: %v", err)
		}
		return snpMsg.(Message), nil

	}
	return nil, fmt.Errorf("request timed out")
}
