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

package gomblclient

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/mbali/go-mbali"
	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/consensus/mblash"
	"github.com/mbali/go-mbali/core"
	"github.com/mbali/go-mbali/core/rawdb"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/crypto"
	"github.com/mbali/go-mbali/mbl"
	"github.com/mbali/go-mbali/mbl/mblconfig"
	"github.com/mbali/go-mbali/mblclient"
	"github.com/mbali/go-mbali/node"
	"github.com/mbali/go-mbali/params"
	"github.com/mbali/go-mbali/rpc"
)

var (
	testKey, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr    = crypto.PubkeyToAddress(testKey.PublicKey)
	testSlot    = common.HexToHash("0xdeadbeef")
	testValue   = crypto.Keccak256Hash(testSlot[:])
	testBalance = big.NewInt(2e15)
)

func newTestBackend(t *testing.T) (*node.Node, []*types.Block) {
	// Generate test chain.
	genesis, blocks := generateTestChain()
	// Create node
	n, err := node.New(&node.Config{})
	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}
	// Create mbali Service
	config := &mblconfig.Config{Genesis: genesis}
	config.mblash.PowMode = mblash.ModeFake
	mblservice, err := mbl.New(n, config)
	if err != nil {
		t.Fatalf("can't create new mbali service: %v", err)
	}
	// Import the test chain.
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}
	if _, err := mblservice.BlockChain().InsertChain(blocks[1:]); err != nil {
		t.Fatalf("can't import test blocks: %v", err)
	}
	return n, blocks
}

func generateTestChain() (*core.Genesis, []*types.Block) {
	db := rawdb.NewMemoryDatabase()
	config := params.AllmblashProtocolChanges
	genesis := &core.Genesis{
		Config:    config,
		Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance, Storage: map[common.Hash]common.Hash{testSlot: testValue}}},
		ExtraData: []byte("test genesis"),
		Timestamp: 9000,
	}
	generate := func(i int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
	}
	gblock := genesis.ToBlock(db)
	engine := mblash.NewFaker()
	blocks, _ := core.GenerateChain(config, gblock, engine, db, 1, generate)
	blocks = append([]*types.Block{gblock}, blocks...)
	return genesis, blocks
}

func TestgomblClient(t *testing.T) {
	backend, _ := newTestBackend(t)
	client, err := backend.Attach()
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	defer client.Close()

	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			"TestAccessList",
			func(t *testing.T) { testAccessList(t, client) },
		},
		{
			"TestGetProof",
			func(t *testing.T) { testGetProof(t, client) },
		}, {
			"TestGCStats",
			func(t *testing.T) { testGCStats(t, client) },
		}, {
			"TestMemStats",
			func(t *testing.T) { testMemStats(t, client) },
		}, {
			"TestGetNodeInfo",
			func(t *testing.T) { testGetNodeInfo(t, client) },
		}, {
			"TestSmblead",
			func(t *testing.T) { testSmblead(t, client) },
		}, {
			"TestSubscribePendingTxs",
			func(t *testing.T) { testSubscribePendingTransactions(t, client) },
		}, {
			"TestCallContract",
			func(t *testing.T) { testCallContract(t, client) },
		},
	}
	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func testAccessList(t *testing.T, client *rpc.Client) {
	ec := New(client)
	// Test transfer
	msg := mbali.CallMsg{
		From:     testAddr,
		To:       &common.Address{},
		Gas:      21000,
		GasPrice: big.NewInt(765625000),
		Value:    big.NewInt(1),
	}
	al, gas, vmErr, err := ec.CreateAccessList(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vmErr != "" {
		t.Fatalf("unexpected vm error: %v", vmErr)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas used: %v", gas)
	}
	if len(*al) != 0 {
		t.Fatalf("unexpected length of accesslist: %v", len(*al))
	}
	// Test reverting transaction
	msg = mbali.CallMsg{
		From:     testAddr,
		To:       nil,
		Gas:      100000,
		GasPrice: big.NewInt(1000000000),
		Value:    big.NewInt(1),
		Data:     common.FromHex("0x608060806080608155fd"),
	}
	al, gas, vmErr, err = ec.CreateAccessList(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vmErr == "" {
		t.Fatalf("wanted vmErr, got none")
	}
	if gas == 21000 {
		t.Fatalf("unexpected gas used: %v", gas)
	}
	if len(*al) != 1 || al.StorageKeys() != 1 {
		t.Fatalf("unexpected length of accesslist: %v", len(*al))
	}
	// address changes between calls, so we can't test for it.
	if (*al)[0].Address == common.HexToAddress("0x0") {
		t.Fatalf("unexpected address: %v", (*al)[0].Address)
	}
	if (*al)[0].StorageKeys[0] != common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000081") {
		t.Fatalf("unexpected storage key: %v", (*al)[0].StorageKeys[0])
	}
}

func testGetProof(t *testing.T, client *rpc.Client) {
	ec := New(client)
	mblcl := mblclient.NewClient(client)
	result, err := ec.GetProof(context.Background(), testAddr, []string{testSlot.String()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Address[:], testAddr[:]) {
		t.Fatalf("unexpected address, want: %v got: %v", testAddr, result.Address)
	}
	// test nonce
	nonce, _ := mblcl.NonceAt(context.Background(), result.Address, nil)
	if result.Nonce != nonce {
		t.Fatalf("invalid nonce, want: %v got: %v", nonce, result.Nonce)
	}
	// test balance
	balance, _ := mblcl.BalanceAt(context.Background(), result.Address, nil)
	if result.Balance.Cmp(balance) != 0 {
		t.Fatalf("invalid balance, want: %v got: %v", balance, result.Balance)
	}
	// test storage
	if len(result.StorageProof) != 1 {
		t.Fatalf("invalid storage proof, want 1 proof, got %v proof(s)", len(result.StorageProof))
	}
	proof := result.StorageProof[0]
	slotValue, _ := mblcl.StorageAt(context.Background(), testAddr, testSlot, nil)
	if !bytes.Equal(slotValue, proof.Value.Bytes()) {
		t.Fatalf("invalid storage proof value, want: %v, got: %v", slotValue, proof.Value.Bytes())
	}
	if proof.Key != testSlot.String() {
		t.Fatalf("invalid storage proof key, want: %v, got: %v", testSlot.String(), proof.Key)
	}

}

func testGCStats(t *testing.T, client *rpc.Client) {
	ec := New(client)
	_, err := ec.GCStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func testMemStats(t *testing.T, client *rpc.Client) {
	ec := New(client)
	stats, err := ec.MemStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Alloc == 0 {
		t.Fatal("Invalid mem stats retrieved")
	}
}

func testGetNodeInfo(t *testing.T, client *rpc.Client) {
	ec := New(client)
	info, err := ec.GetNodeInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if info.Name == "" {
		t.Fatal("Invalid node info retrieved")
	}
}

func testSmblead(t *testing.T, client *rpc.Client) {
	ec := New(client)
	err := ec.Smblead(context.Background(), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
}

func testSubscribePendingTransactions(t *testing.T, client *rpc.Client) {
	ec := New(client)
	mblcl := mblclient.NewClient(client)
	// Subscribe to Transactions
	ch := make(chan common.Hash)
	ec.SubscribePendingTransactions(context.Background(), ch)
	// Send a transaction
	chainID, err := mblcl.ChainID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Create transaction
	tx := types.NewTransaction(0, common.Address{1}, big.NewInt(1), 22000, big.NewInt(1), nil)
	signer := types.LatestSignerForChainID(chainID)
	signature, err := crypto.Sign(signer.Hash(tx).Bytes(), testKey)
	if err != nil {
		t.Fatal(err)
	}
	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		t.Fatal(err)
	}
	// Send transaction
	err = mblcl.SendTransaction(context.Background(), signedTx)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the transaction was send over the channel
	hash := <-ch
	if hash != signedTx.Hash() {
		t.Fatalf("Invalid tx hash received, got %v, want %v", hash, signedTx.Hash())
	}
}

func testCallContract(t *testing.T, client *rpc.Client) {
	ec := New(client)
	msg := mbali.CallMsg{
		From:     testAddr,
		To:       &common.Address{},
		Gas:      21000,
		GasPrice: big.NewInt(1000000000),
		Value:    big.NewInt(1),
	}
	// CallContract without override
	if _, err := ec.CallContract(context.Background(), msg, big.NewInt(0), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CallContract with override
	override := OverrideAccount{
		Nonce: 1,
	}
	mapAcc := make(map[common.Address]OverrideAccount)
	mapAcc[testAddr] = override
	if _, err := ec.CallContract(context.Background(), msg, big.NewInt(0), &mapAcc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
