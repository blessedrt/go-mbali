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

// Tests that setting the chain head backwards doesn't leave the database in some
// strange state with gaps in the chain, nor with block data dangling in the future.

package core

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/consensus/mblash"
	"github.com/mbali/go-mbali/core/rawdb"
	"github.com/mbali/go-mbali/core/types"
	"github.com/mbali/go-mbali/core/vm"
	"github.com/mbali/go-mbali/params"
)

// rewindTest is a test case for chain rollback upon user request.
type rewindTest struct {
	canonicalBlocks int     // Number of blocks to generate for the canonical chain (heavier)
	sidechainBlocks int     // Number of blocks to generate for the side chain (lighter)
	freezmblreshold uint64  // Block number until which to move things into the freezer
	commitBlock     uint64  // Block number for which to commit the state to disk
	pivotBlock      *uint64 // Pivot block number in case of fast sync

	smbleadBlock       uint64 // Block number to set head back to
	expCanonicalBlocks int    // Number of canonical blocks expected to remain in the database (excl. genesis)
	expSidechainBlocks int    // Number of sidechain blocks expected to remain in the database (excl. genesis)
	expFrozen          int    // Number of canonical blocks expected to be in the freezer (incl. genesis)
	expHeadHeader      uint64 // Block number of the expected head header
	expHeadFastBlock   uint64 // Block number of the expected head fast sync block
	expHeadBlock       uint64 // Block number of the expected head full block
}

func (tt *rewindTest) dump(crash bool) string {
	buffer := new(strings.Builder)

	fmt.Fprint(buffer, "Chain:\n  G")
	for i := 0; i < tt.canonicalBlocks; i++ {
		fmt.Fprintf(buffer, "->C%d", i+1)
	}
	fmt.Fprint(buffer, " (HEAD)\n")
	if tt.sidechainBlocks > 0 {
		fmt.Fprintf(buffer, "  └")
		for i := 0; i < tt.sidechainBlocks; i++ {
			fmt.Fprintf(buffer, "->S%d", i+1)
		}
		fmt.Fprintf(buffer, "\n")
	}
	fmt.Fprintf(buffer, "\n")

	if tt.canonicalBlocks > int(tt.freezmblreshold) {
		fmt.Fprint(buffer, "Frozen:\n  G")
		for i := 0; i < tt.canonicalBlocks-int(tt.freezmblreshold); i++ {
			fmt.Fprintf(buffer, "->C%d", i+1)
		}
		fmt.Fprintf(buffer, "\n\n")
	} else {
		fmt.Fprintf(buffer, "Frozen: none\n")
	}
	fmt.Fprintf(buffer, "Commit: G")
	if tt.commitBlock > 0 {
		fmt.Fprintf(buffer, ", C%d", tt.commitBlock)
	}
	fmt.Fprint(buffer, "\n")

	if tt.pivotBlock == nil {
		fmt.Fprintf(buffer, "Pivot : none\n")
	} else {
		fmt.Fprintf(buffer, "Pivot : C%d\n", *tt.pivotBlock)
	}
	if crash {
		fmt.Fprintf(buffer, "\nCRASH\n\n")
	} else {
		fmt.Fprintf(buffer, "\nSmblead(%d)\n\n", tt.smbleadBlock)
	}
	fmt.Fprintf(buffer, "------------------------------\n\n")

	if tt.expFrozen > 0 {
		fmt.Fprint(buffer, "Expected in freezer:\n  G")
		for i := 0; i < tt.expFrozen-1; i++ {
			fmt.Fprintf(buffer, "->C%d", i+1)
		}
		fmt.Fprintf(buffer, "\n\n")
	}
	if tt.expFrozen > 0 {
		if tt.expFrozen >= tt.expCanonicalBlocks {
			fmt.Fprintf(buffer, "Expected in leveldb: none\n")
		} else {
			fmt.Fprintf(buffer, "Expected in leveldb:\n  C%d)", tt.expFrozen-1)
			for i := tt.expFrozen - 1; i < tt.expCanonicalBlocks; i++ {
				fmt.Fprintf(buffer, "->C%d", i+1)
			}
			fmt.Fprint(buffer, "\n")
			if tt.expSidechainBlocks > tt.expFrozen {
				fmt.Fprintf(buffer, "  └")
				for i := tt.expFrozen - 1; i < tt.expSidechainBlocks; i++ {
					fmt.Fprintf(buffer, "->S%d", i+1)
				}
				fmt.Fprintf(buffer, "\n")
			}
		}
	} else {
		fmt.Fprint(buffer, "Expected in leveldb:\n  G")
		for i := tt.expFrozen; i < tt.expCanonicalBlocks; i++ {
			fmt.Fprintf(buffer, "->C%d", i+1)
		}
		fmt.Fprint(buffer, "\n")
		if tt.expSidechainBlocks > tt.expFrozen {
			fmt.Fprintf(buffer, "  └")
			for i := tt.expFrozen; i < tt.expSidechainBlocks; i++ {
				fmt.Fprintf(buffer, "->S%d", i+1)
			}
			fmt.Fprintf(buffer, "\n")
		}
	}
	fmt.Fprintf(buffer, "\n")
	fmt.Fprintf(buffer, "Expected head header    : C%d\n", tt.expHeadHeader)
	fmt.Fprintf(buffer, "Expected head fast block: C%d\n", tt.expHeadFastBlock)
	if tt.expHeadBlock == 0 {
		fmt.Fprintf(buffer, "Expected head block     : G\n")
	} else {
		fmt.Fprintf(buffer, "Expected head block     : C%d\n", tt.expHeadBlock)
	}
	return buffer.String()
}

// Tests a smblead for a short canonical chain where a recent block was already
// committed to disk and then the smblead called. In this case we expect the full
// chain to be rolled back to the committed block. Everything above the smblead
// point should be deleted. In between the committed block and the requested head
// the data can remain as "fast sync" data to avoid redownloading it.
func TestShortSmblead(t *testing.T)              { testShortSmblead(t, false) }
func TestShortSmbleadWithSnapshots(t *testing.T) { testShortSmblead(t, true) }

func testShortSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 0,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain where the fast sync pivot point was
// already committed, after which smblead was called. In this case we expect the
// chain to behave like in full sync mode, rolling back to the committed block
// Everything above the smblead point should be deleted. In between the committed
// block and the requested head the data can remain as "fast sync" data to avoid
// redownloading it.
func TestShortSnapSyncedSmblead(t *testing.T)              { testShortSnapSyncedSmblead(t, false) }
func TestShortSnapSyncedSmbleadWithSnapshots(t *testing.T) { testShortSnapSyncedSmblead(t, true) }

func testShortSnapSyncedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 0,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain where the fast sync pivot point was
// not yet committed, but smblead was called. In this case we expect the chain to
// detect that it was fast syncing and delete everything from the new head, since
// we can just pick up fast syncing from there. The head full block should be set
// to the genesis.
func TestShortSnapSyncingSmblead(t *testing.T)              { testShortSnapSyncingSmblead(t, false) }
func TestShortSnapSyncingSmbleadWithSnapshots(t *testing.T) { testShortSnapSyncingSmblead(t, true) }

func testShortSnapSyncingSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Frozen: none
	// Commit: G
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 0,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a shorter side chain, where a
// recent block was already committed to disk and then smblead was called. In this
// test scenario the side chain is below the committed block. In this case we expect
// the canonical full chain to be rolled back to the committed block. Everything
// above the smblead point should be deleted. In between the committed block and
// the requested head the data can remain as "fast sync" data to avoid redownloading
// it. The side chain should be left alone as it was shorter.
func TestShortOldForkedSmblead(t *testing.T)              { testShortOldForkedSmblead(t, false) }
func TestShortOldForkedSmbleadWithSnapshots(t *testing.T) { testShortOldForkedSmblead(t, true) }

func testShortOldForkedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 3,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a shorter side chain, where
// the fast sync pivot point was already committed to disk and then smblead was
// called. In this test scenario the side chain is below the committed block. In
// this case we expect the canonical full chain to be rolled back to the committed
// block. Everything above the smblead point should be deleted. In between the
// committed block and the requested head the data can remain as "fast sync" data
// to avoid redownloading it. The side chain should be left alone as it was shorter.
func TestShortOldForkedSnapSyncedSmblead(t *testing.T) {
	testShortOldForkedSnapSyncedSmblead(t, false)
}
func TestShortOldForkedSnapSyncedSmbleadWithSnapshots(t *testing.T) {
	testShortOldForkedSnapSyncedSmblead(t, true)
}

func testShortOldForkedSnapSyncedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 3,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a shorter side chain, where
// the fast sync pivot point was not yet committed, but smblead was called. In this
// test scenario the side chain is below the committed block. In this case we expect
// the chain to detect that it was fast syncing and delete everything from the new
// head, since we can just pick up fast syncing from there. The head full block
// should be set to the genesis.
func TestShortOldForkedSnapSyncingSmblead(t *testing.T) {
	testShortOldForkedSnapSyncingSmblead(t, false)
}
func TestShortOldForkedSnapSyncingSmbleadWithSnapshots(t *testing.T) {
	testShortOldForkedSnapSyncingSmblead(t, true)
}

func testShortOldForkedSnapSyncingSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen: none
	// Commit: G
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 3,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a shorter side chain, where a
// recent block was already committed to disk and then smblead was called. In this
// test scenario the side chain reaches above the committed block. In this case we
// expect the canonical full chain to be rolled back to the committed block. All
// data above the smblead point should be deleted. In between the committed block
// and the requested head the data can remain as "fast sync" data to avoid having
// to redownload it. The side chain should be truncated to the head set.
//
// The side chain could be left to be if the fork point was before the new head
// we are deleting to, but it would be exceedingly hard to detect that case and
// properly handle it, so we'll trade extra work in exchange for simpler code.
func TestShortNewlyForkedSmblead(t *testing.T)              { testShortNewlyForkedSmblead(t, false) }
func TestShortNewlyForkedSmbleadWithSnapshots(t *testing.T) { testShortNewlyForkedSmblead(t, true) }

func testShortNewlyForkedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3->S4->S5->S6->S7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    10,
		sidechainBlocks:    8,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 7,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a shorter side chain, where
// the fast sync pivot point was already committed to disk and then smblead was
// called. In this case we expect the canonical full chain to be rolled back to
// between the committed block and the requested head the data can remain as
// "fast sync" data to avoid having to redownload it. The side chain should be
// truncated to the head set.
//
// The side chain could be left to be if the fork point was before the new head
// we are deleting to, but it would be exceedingly hard to detect that case and
// properly handle it, so we'll trade extra work in exchange for simpler code.
func TestShortNewlyForkedSnapSyncedSmblead(t *testing.T) {
	testShortNewlyForkedSnapSyncedSmblead(t, false)
}
func TestShortNewlyForkedSnapSyncedSmbleadWithSnapshots(t *testing.T) {
	testShortNewlyForkedSnapSyncedSmblead(t, true)
}

func testShortNewlyForkedSnapSyncedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3->S4->S5->S6->S7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    10,
		sidechainBlocks:    8,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 7,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a shorter side chain, where
// the fast sync pivot point was not yet committed, but smblead was called. In
// this test scenario the side chain reaches above the committed block. In this
// case we expect the chain to detect that it was fast syncing and delete
// everything from the new head, since we can just pick up fast syncing from
// there.
//
// The side chain could be left to be if the fork point was before the new head
// we are deleting to, but it would be exceedingly hard to detect that case and
// properly handle it, so we'll trade extra work in exchange for simpler code.
func TestShortNewlyForkedSnapSyncingSmblead(t *testing.T) {
	testShortNewlyForkedSnapSyncingSmblead(t, false)
}
func TestShortNewlyForkedSnapSyncingSmbleadWithSnapshots(t *testing.T) {
	testShortNewlyForkedSnapSyncingSmblead(t, true)
}

func testShortNewlyForkedSnapSyncingSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8
	//
	// Frozen: none
	// Commit: G
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3->S4->S5->S6->S7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    10,
		sidechainBlocks:    8,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 7,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a longer side chain, where a
// recent block was already committed to disk and then smblead was called. In this
// case we expect the canonical full chain to be rolled back to the committed block.
// All data above the smblead point should be deleted. In between the committed
// block and the requested head the data can remain as "fast sync" data to avoid
// having to redownload it. The side chain should be truncated to the head set.
//
// The side chain could be left to be if the fork point was before the new head
// we are deleting to, but it would be exceedingly hard to detect that case and
// properly handle it, so we'll trade extra work in exchange for simpler code.
func TestShortReorgedSmblead(t *testing.T)              { testShortReorgedSmblead(t, false) }
func TestShortReorgedSmbleadWithSnapshots(t *testing.T) { testShortReorgedSmblead(t, true) }

func testShortReorgedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3->S4->S5->S6->S7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    10,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 7,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a longer side chain, where
// the fast sync pivot point was already committed to disk and then smblead was
// called. In this case we expect the canonical full chain to be rolled back to
// the committed block. All data above the smblead point should be deleted. In
// between the committed block and the requested head the data can remain as
// "fast sync" data to avoid having to redownload it. The side chain should be
// truncated to the head set.
//
// The side chain could be left to be if the fork point was before the new head
// we are deleting to, but it would be exceedingly hard to detect that case and
// properly handle it, so we'll trade extra work in exchange for simpler code.
func TestShortReorgedSnapSyncedSmblead(t *testing.T) {
	testShortReorgedSnapSyncedSmblead(t, false)
}
func TestShortReorgedSnapSyncedSmbleadWithSnapshots(t *testing.T) {
	testShortReorgedSnapSyncedSmblead(t, true)
}

func testShortReorgedSnapSyncedSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10
	//
	// Frozen: none
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3->S4->S5->S6->S7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    10,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 7,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a short canonical chain and a longer side chain, where
// the fast sync pivot point was not yet committed, but smblead was called. In
// this case we expect the chain to detect that it was fast syncing and delete
// everything from the new head, since we can just pick up fast syncing from
// there.
//
// The side chain could be left to be if the fork point was before the new head
// we are deleting to, but it would be exceedingly hard to detect that case and
// properly handle it, so we'll trade extra work in exchange for simpler code.
func TestShortReorgedSnapSyncingSmblead(t *testing.T) {
	testShortReorgedSnapSyncingSmblead(t, false)
}
func TestShortReorgedSnapSyncingSmbleadWithSnapshots(t *testing.T) {
	testShortReorgedSnapSyncingSmblead(t, true)
}

func testShortReorgedSnapSyncingSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10
	//
	// Frozen: none
	// Commit: G
	// Pivot : C4
	//
	// Smblead(7)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7
	//   └->S1->S2->S3->S4->S5->S6->S7
	//
	// Expected head header    : C7
	// Expected head fast block: C7
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    10,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       7,
		expCanonicalBlocks: 7,
		expSidechainBlocks: 7,
		expFrozen:          0,
		expHeadHeader:      7,
		expHeadFastBlock:   7,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks where a recent
// block - newer than the ancient limit - was already committed to disk and then
// smblead was called. In this case we expect the full chain to be rolled back
// to the committed block. Everything above the smblead point should be deleted.
// In between the committed block and the requested head the data can remain as
// "fast sync" data to avoid redownloading it.
func TestLongShallowSmblead(t *testing.T)              { testLongShallowSmblead(t, false) }
func TestLongShallowSmbleadWithSnapshots(t *testing.T) { testLongShallowSmblead(t, true) }

func testLongShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks where a recent
// block - older than the ancient limit - was already committed to disk and then
// smblead was called. In this case we expect the full chain to be rolled back
// to the committed block. Since the ancient limit was underflown, everything
// needs to be deleted onwards to avoid creating a gap.
func TestLongDeepSmblead(t *testing.T)              { testLongDeepSmblead(t, false) }
func TestLongDeepSmbleadWithSnapshots(t *testing.T) { testLongDeepSmblead(t, true) }

func testLongDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks where the fast
// sync pivot point - newer than the ancient limit - was already committed, after
// which smblead was called. In this case we expect the full chain to be rolled
// back to the committed block. Everything above the smblead point should be
// deleted. In between the committed block and the requested head the data can
// remain as "fast sync" data to avoid redownloading it.
func TestLongSnapSyncedShallowSmblead(t *testing.T) {
	testLongSnapSyncedShallowSmblead(t, false)
}
func TestLongSnapSyncedShallowSmbleadWithSnapshots(t *testing.T) {
	testLongSnapSyncedShallowSmblead(t, true)
}

func testLongSnapSyncedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks where the fast
// sync pivot point - older than the ancient limit - was already committed, after
// which smblead was called. In this case we expect the full chain to be rolled
// back to the committed block. Since the ancient limit was underflown, everything
// needs to be deleted onwards to avoid creating a gap.
func TestLongSnapSyncedDeepSmblead(t *testing.T)              { testLongSnapSyncedDeepSmblead(t, false) }
func TestLongSnapSyncedDeepSmbleadWithSnapshots(t *testing.T) { testLongSnapSyncedDeepSmblead(t, true) }

func testLongSnapSyncedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks where the fast
// sync pivot point - newer than the ancient limit - was not yet committed, but
// smblead was called. In this case we expect the chain to detect that it was fast
// syncing and delete everything from the new head, since we can just pick up fast
// syncing from there.
func TestLongSnapSyncingShallowSmblead(t *testing.T) {
	testLongSnapSyncingShallowSmblead(t, false)
}
func TestLongSnapSyncingShallowSmbleadWithSnapshots(t *testing.T) {
	testLongSnapSyncingShallowSmblead(t, true)
}

func testLongSnapSyncingShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks where the fast
// sync pivot point - older than the ancient limit - was not yet committed, but
// smblead was called. In this case we expect the chain to detect that it was fast
// syncing and delete everything from the new head, since we can just pick up fast
// syncing from there.
func TestLongSnapSyncingDeepSmblead(t *testing.T) {
	testLongSnapSyncingDeepSmblead(t, false)
}
func TestLongSnapSyncingDeepSmbleadWithSnapshots(t *testing.T) {
	testLongSnapSyncingDeepSmblead(t, true)
}

func testLongSnapSyncingDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4->C5->C6
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    0,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          7,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter side
// chain, where a recent block - newer than the ancient limit - was already committed
// to disk and then smblead was called. In this case we expect the canonical full
// chain to be rolled back to the committed block. Everything above the smblead point
// should be deleted. In between the committed block and the requested head the data
// can remain as "fast sync" data to avoid redownloading it. The side chain is nuked
// by the freezer.
func TestLongOldForkedShallowSmblead(t *testing.T) {
	testLongOldForkedShallowSmblead(t, false)
}
func TestLongOldForkedShallowSmbleadWithSnapshots(t *testing.T) {
	testLongOldForkedShallowSmblead(t, true)
}

func testLongOldForkedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter side
// chain, where a recent block - older than the ancient limit - was already committed
// to disk and then smblead was called. In this case we expect the canonical full
// chain to be rolled back to the committed block. Since the ancient limit was
// underflown, everything needs to be deleted onwards to avoid creating a gap. The
// side chain is nuked by the freezer.
func TestLongOldForkedDeepSmblead(t *testing.T)              { testLongOldForkedDeepSmblead(t, false) }
func TestLongOldForkedDeepSmbleadWithSnapshots(t *testing.T) { testLongOldForkedDeepSmblead(t, true) }

func testLongOldForkedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - newer than the ancient limit -
// was already committed to disk and then smblead was called. In this test scenario
// the side chain is below the committed block. In this case we expect the canonical
// full chain to be rolled back to the committed block. Everything above the
// smblead point should be deleted. In between the committed block and the
// requested head the data can remain as "fast sync" data to avoid redownloading
// it. The side chain is nuked by the freezer.
func TestLongOldForkedSnapSyncedShallowSmblead(t *testing.T) {
	testLongOldForkedSnapSyncedShallowSmblead(t, false)
}
func TestLongOldForkedSnapSyncedShallowSmbleadWithSnapshots(t *testing.T) {
	testLongOldForkedSnapSyncedShallowSmblead(t, true)
}

func testLongOldForkedSnapSyncedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - older than the ancient limit -
// was already committed to disk and then smblead was called. In this test scenario
// the side chain is below the committed block. In this case we expect the canonical
// full chain to be rolled back to the committed block. Since the ancient limit was
// underflown, everything needs to be deleted onwards to avoid creating a gap. The
// side chain is nuked by the freezer.
func TestLongOldForkedSnapSyncedDeepSmblead(t *testing.T) {
	testLongOldForkedSnapSyncedDeepSmblead(t, false)
}
func TestLongOldForkedSnapSyncedDeepSmbleadWithSnapshots(t *testing.T) {
	testLongOldForkedSnapSyncedDeepSmblead(t, true)
}

func testLongOldForkedSnapSyncedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4->C5->C6
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - newer than the ancient limit -
// was not yet committed, but smblead was called. In this test scenario the side
// chain is below the committed block. In this case we expect the chain to detect
// that it was fast syncing and delete everything from the new head, since we can
// just pick up fast syncing from there. The side chain is completely nuked by the
// freezer.
func TestLongOldForkedSnapSyncingShallowSmblead(t *testing.T) {
	testLongOldForkedSnapSyncingShallowSmblead(t, false)
}
func TestLongOldForkedSnapSyncingShallowSmbleadWithSnapshots(t *testing.T) {
	testLongOldForkedSnapSyncingShallowSmblead(t, true)
}

func testLongOldForkedSnapSyncingShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - older than the ancient limit -
// was not yet committed, but smblead was called. In this test scenario the side
// chain is below the committed block. In this case we expect the chain to detect
// that it was fast syncing and delete everything from the new head, since we can
// just pick up fast syncing from there. The side chain is completely nuked by the
// freezer.
func TestLongOldForkedSnapSyncingDeepSmblead(t *testing.T) {
	testLongOldForkedSnapSyncingDeepSmblead(t, false)
}
func TestLongOldForkedSnapSyncingDeepSmbleadWithSnapshots(t *testing.T) {
	testLongOldForkedSnapSyncingDeepSmblead(t, true)
}

func testLongOldForkedSnapSyncingDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4->C5->C6
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    3,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          7,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where a recent block - newer than the ancient limit - was already
// committed to disk and then smblead was called. In this test scenario the side
// chain is above the committed block. In this case the freezer will delete the
// sidechain since it's dangling, reverting to TestLongShallowSmblead.
func TestLongNewerForkedShallowSmblead(t *testing.T) {
	testLongNewerForkedShallowSmblead(t, false)
}
func TestLongNewerForkedShallowSmbleadWithSnapshots(t *testing.T) {
	testLongNewerForkedShallowSmblead(t, true)
}

func testLongNewerForkedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    12,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where a recent block - older than the ancient limit - was already
// committed to disk and then smblead was called. In this test scenario the side
// chain is above the committed block. In this case the freezer will delete the
// sidechain since it's dangling, reverting to TestLongDeepSmblead.
func TestLongNewerForkedDeepSmblead(t *testing.T) {
	testLongNewerForkedDeepSmblead(t, false)
}
func TestLongNewerForkedDeepSmbleadWithSnapshots(t *testing.T) {
	testLongNewerForkedDeepSmblead(t, true)
}

func testLongNewerForkedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    12,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - newer than the ancient limit -
// was already committed to disk and then smblead was called. In this test scenario
// the side chain is above the committed block. In this case the freezer will delete
// the sidechain since it's dangling, reverting to TestLongSnapSyncedShallowSmblead.
func TestLongNewerForkedSnapSyncedShallowSmblead(t *testing.T) {
	testLongNewerForkedSnapSyncedShallowSmblead(t, false)
}
func TestLongNewerForkedSnapSyncedShallowSmbleadWithSnapshots(t *testing.T) {
	testLongNewerForkedSnapSyncedShallowSmblead(t, true)
}

func testLongNewerForkedSnapSyncedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    12,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - older than the ancient limit -
// was already committed to disk and then smblead was called. In this test scenario
// the side chain is above the committed block. In this case the freezer will delete
// the sidechain since it's dangling, reverting to TestLongSnapSyncedDeepSmblead.
func TestLongNewerForkedSnapSyncedDeepSmblead(t *testing.T) {
	testLongNewerForkedSnapSyncedDeepSmblead(t, false)
}
func TestLongNewerForkedSnapSyncedDeepSmbleadWithSnapshots(t *testing.T) {
	testLongNewerForkedSnapSyncedDeepSmblead(t, true)
}

func testLongNewerForkedSnapSyncedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    12,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - newer than the ancient limit -
// was not yet committed, but smblead was called. In this test scenario the side
// chain is above the committed block. In this case the freezer will delete the
// sidechain since it's dangling, reverting to TestLongSnapSyncinghallowSmblead.
func TestLongNewerForkedSnapSyncingShallowSmblead(t *testing.T) {
	testLongNewerForkedSnapSyncingShallowSmblead(t, false)
}
func TestLongNewerForkedSnapSyncingShallowSmbleadWithSnapshots(t *testing.T) {
	testLongNewerForkedSnapSyncingShallowSmblead(t, true)
}

func testLongNewerForkedSnapSyncingShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    12,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a shorter
// side chain, where the fast sync pivot point - older than the ancient limit -
// was not yet committed, but smblead was called. In this test scenario the side
// chain is above the committed block. In this case the freezer will delete the
// sidechain since it's dangling, reverting to TestLongSnapSyncingDeepSmblead.
func TestLongNewerForkedSnapSyncingDeepSmblead(t *testing.T) {
	testLongNewerForkedSnapSyncingDeepSmblead(t, false)
}
func TestLongNewerForkedSnapSyncingDeepSmbleadWithSnapshots(t *testing.T) {
	testLongNewerForkedSnapSyncingDeepSmblead(t, true)
}

func testLongNewerForkedSnapSyncingDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4->C5->C6
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    12,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          7,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a longer side
// chain, where a recent block - newer than the ancient limit - was already committed
// to disk and then smblead was called. In this case the freezer will delete the
// sidechain since it's dangling, reverting to TestLongShallowSmblead.
func TestLongReorgedShallowSmblead(t *testing.T)              { testLongReorgedShallowSmblead(t, false) }
func TestLongReorgedShallowSmbleadWithSnapshots(t *testing.T) { testLongReorgedShallowSmblead(t, true) }

func testLongReorgedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    26,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a longer side
// chain, where a recent block - older than the ancient limit - was already committed
// to disk and then smblead was called. In this case the freezer will delete the
// sidechain since it's dangling, reverting to TestLongDeepSmblead.
func TestLongReorgedDeepSmblead(t *testing.T)              { testLongReorgedDeepSmblead(t, false) }
func TestLongReorgedDeepSmbleadWithSnapshots(t *testing.T) { testLongReorgedDeepSmblead(t, true) }

func testLongReorgedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : none
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    26,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         nil,
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a longer
// side chain, where the fast sync pivot point - newer than the ancient limit -
// was already committed to disk and then smblead was called. In this case the
// freezer will delete the sidechain since it's dangling, reverting to
// TestLongSnapSyncedShallowSmblead.
func TestLongReorgedSnapSyncedShallowSmblead(t *testing.T) {
	testLongReorgedSnapSyncedShallowSmblead(t, false)
}
func TestLongReorgedSnapSyncedShallowSmbleadWithSnapshots(t *testing.T) {
	testLongReorgedSnapSyncedShallowSmblead(t, true)
}

func testLongReorgedSnapSyncedShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    26,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a longer
// side chain, where the fast sync pivot point - older than the ancient limit -
// was already committed to disk and then smblead was called. In this case the
// freezer will delete the sidechain since it's dangling, reverting to
// TestLongSnapSyncedDeepSmblead.
func TestLongReorgedSnapSyncedDeepSmblead(t *testing.T) {
	testLongReorgedSnapSyncedDeepSmblead(t, false)
}
func TestLongReorgedSnapSyncedDeepSmbleadWithSnapshots(t *testing.T) {
	testLongReorgedSnapSyncedDeepSmblead(t, true)
}

func testLongReorgedSnapSyncedDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G, C4
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    26,
		freezmblreshold:    16,
		commitBlock:        4,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 4,
		expSidechainBlocks: 0,
		expFrozen:          5,
		expHeadHeader:      4,
		expHeadFastBlock:   4,
		expHeadBlock:       4,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a longer
// side chain, where the fast sync pivot point - newer than the ancient limit -
// was not yet committed, but smblead was called. In this case we expect the
// chain to detect that it was fast syncing and delete everything from the new
// head, since we can just pick up fast syncing from there. The side chain is
// completely nuked by the freezer.
func TestLongReorgedSnapSyncingShallowSmblead(t *testing.T) {
	testLongReorgedSnapSyncingShallowSmblead(t, false)
}
func TestLongReorgedSnapSyncingShallowSmbleadWithSnapshots(t *testing.T) {
	testLongReorgedSnapSyncingShallowSmblead(t, true)
}

func testLongReorgedSnapSyncingShallowSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Frozen:
	//   G->C1->C2
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2
	//
	// Expected in leveldb:
	//   C2)->C3->C4->C5->C6
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    26,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          3,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

// Tests a smblead for a long canonical chain with frozen blocks and a longer
// side chain, where the fast sync pivot point - older than the ancient limit -
// was not yet committed, but smblead was called. In this case we expect the
// chain to detect that it was fast syncing and delete everything from the new
// head, since we can just pick up fast syncing from there. The side chain is
// completely nuked by the freezer.
func TestLongReorgedSnapSyncingDeepSmblead(t *testing.T) {
	testLongReorgedSnapSyncingDeepSmblead(t, false)
}
func TestLongReorgedSnapSyncingDeepSmbleadWithSnapshots(t *testing.T) {
	testLongReorgedSnapSyncingDeepSmblead(t, true)
}

func testLongReorgedSnapSyncingDeepSmblead(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Frozen:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Commit: G
	// Pivot : C4
	//
	// Smblead(6)
	//
	// ------------------------------
	//
	// Expected in freezer:
	//   G->C1->C2->C3->C4->C5->C6
	//
	// Expected in leveldb: none
	//
	// Expected head header    : C6
	// Expected head fast block: C6
	// Expected head block     : G
	testSmblead(t, &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    26,
		freezmblreshold:    16,
		commitBlock:        0,
		pivotBlock:         uint64ptr(4),
		smbleadBlock:       6,
		expCanonicalBlocks: 6,
		expSidechainBlocks: 0,
		expFrozen:          7,
		expHeadHeader:      6,
		expHeadFastBlock:   6,
		expHeadBlock:       0,
	}, snapshots)
}

func testSmblead(t *testing.T, tt *rewindTest, snapshots bool) {
	// It's hard to follow the test case, visualize the input
	// log.Root().Smblandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump(false))

	// Create a temporary persistent database
	datadir := t.TempDir()

	db, err := rawdb.NewLevelDBDatabaseWithFreezer(datadir, 0, 0, datadir, "", false)
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	defer db.Close()

	// Initialize a fresh chain
	var (
		genesis = (&Genesis{BaseFee: big.NewInt(params.InitialBaseFee)}).MustCommit(db)
		engine  = mblash.NewFullFaker()
		config  = &CacheConfig{
			TrieCleanLimit: 256,
			TrieDirtyLimit: 256,
			TrieTimeLimit:  5 * time.Minute,
			SnapshotLimit:  0, // Disable snapshot
		}
	)
	if snapshots {
		config.SnapshotLimit = 256
		config.SnapshotWait = true
	}
	chain, err := NewBlockChain(db, config, params.AllmblashProtocolChanges, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	// If sidechain blocks are needed, make a light chain and import it
	var sideblocks types.Blocks
	if tt.sidechainBlocks > 0 {
		sideblocks, _ = GenerateChain(params.TestChainConfig, genesis, engine, rawdb.NewMemoryDatabase(), tt.sidechainBlocks, func(i int, b *BlockGen) {
			b.SetCoinbase(common.Address{0x01})
		})
		if _, err := chain.InsertChain(sideblocks); err != nil {
			t.Fatalf("Failed to import side chain: %v", err)
		}
	}
	canonblocks, _ := GenerateChain(params.TestChainConfig, genesis, engine, rawdb.NewMemoryDatabase(), tt.canonicalBlocks, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0x02})
		b.SetDifficulty(big.NewInt(1000000))
	})
	if _, err := chain.InsertChain(canonblocks[:tt.commitBlock]); err != nil {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}
	if tt.commitBlock > 0 {
		chain.stateCache.TrieDB().Commit(canonblocks[tt.commitBlock-1].Root(), true, nil)
		if snapshots {
			if err := chain.snaps.Cap(canonblocks[tt.commitBlock-1].Root(), 0); err != nil {
				t.Fatalf("Failed to flatten snapshots: %v", err)
			}
		}
	}
	if _, err := chain.InsertChain(canonblocks[tt.commitBlock:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}
	// Manually dereference anything not committed to not have to work with 128+ tries
	for _, block := range sideblocks {
		chain.stateCache.TrieDB().Dereference(block.Root())
	}
	for _, block := range canonblocks {
		chain.stateCache.TrieDB().Dereference(block.Root())
	}
	// Force run a freeze cycle
	type freezer interface {
		Freeze(threshold uint64) error
		Ancients() (uint64, error)
	}
	db.(freezer).Freeze(tt.freezmblreshold)

	// Set the simulated pivot block
	if tt.pivotBlock != nil {
		rawdb.WriteLastPivotNumber(db, *tt.pivotBlock)
	}
	// Set the head of the chain back to the requested number
	chain.Smblead(tt.smbleadBlock)

	// Iterate over all the remaining blocks and ensure there are no gaps
	verifyNoGaps(t, chain, true, canonblocks)
	verifyNoGaps(t, chain, false, sideblocks)
	verifyCutoff(t, chain, true, canonblocks, tt.expCanonicalBlocks)
	verifyCutoff(t, chain, false, sideblocks, tt.expSidechainBlocks)

	if head := chain.CurrentHeader(); head.Number.Uint64() != tt.expHeadHeader {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, tt.expHeadHeader)
	}
	if head := chain.CurrentFastBlock(); head.NumberU64() != tt.expHeadFastBlock {
		t.Errorf("Head fast block mismatch: have %d, want %d", head.NumberU64(), tt.expHeadFastBlock)
	}
	if head := chain.CurrentBlock(); head.NumberU64() != tt.expHeadBlock {
		t.Errorf("Head block mismatch: have %d, want %d", head.NumberU64(), tt.expHeadBlock)
	}
	if frozen, err := db.(freezer).Ancients(); err != nil {
		t.Errorf("Failed to retrieve ancient count: %v\n", err)
	} else if int(frozen) != tt.expFrozen {
		t.Errorf("Frozen block count mismatch: have %d, want %d", frozen, tt.expFrozen)
	}
}

// verifyNoGaps checks that there are no gaps after the initial set of blocks in
// the database and errors if found.
func verifyNoGaps(t *testing.T, chain *BlockChain, canonical bool, inserted types.Blocks) {
	t.Helper()

	var end uint64
	for i := uint64(0); i <= uint64(len(inserted)); i++ {
		header := chain.gombleaderByNumber(i)
		if header == nil && end == 0 {
			end = i
		}
		if header != nil && end > 0 {
			if canonical {
				t.Errorf("Canonical header gap between #%d-#%d", end, i-1)
			} else {
				t.Errorf("Sidechain header gap between #%d-#%d", end, i-1)
			}
			end = 0 // Reset for further gap detection
		}
	}
	end = 0
	for i := uint64(0); i <= uint64(len(inserted)); i++ {
		block := chain.GetBlockByNumber(i)
		if block == nil && end == 0 {
			end = i
		}
		if block != nil && end > 0 {
			if canonical {
				t.Errorf("Canonical block gap between #%d-#%d", end, i-1)
			} else {
				t.Errorf("Sidechain block gap between #%d-#%d", end, i-1)
			}
			end = 0 // Reset for further gap detection
		}
	}
	end = 0
	for i := uint64(1); i <= uint64(len(inserted)); i++ {
		receipts := chain.GetReceiptsByHash(inserted[i-1].Hash())
		if receipts == nil && end == 0 {
			end = i
		}
		if receipts != nil && end > 0 {
			if canonical {
				t.Errorf("Canonical receipt gap between #%d-#%d", end, i-1)
			} else {
				t.Errorf("Sidechain receipt gap between #%d-#%d", end, i-1)
			}
			end = 0 // Reset for further gap detection
		}
	}
}

// verifyCutoff checks that there are no chain data available in the chain after
// the specified limit, but that it is available before.
func verifyCutoff(t *testing.T, chain *BlockChain, canonical bool, inserted types.Blocks, head int) {
	t.Helper()

	for i := 1; i <= len(inserted); i++ {
		if i <= head {
			if header := chain.gombleader(inserted[i-1].Hash(), uint64(i)); header == nil {
				if canonical {
					t.Errorf("Canonical header   #%2d [%x...] missing before cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				} else {
					t.Errorf("Sidechain header   #%2d [%x...] missing before cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				}
			}
			if block := chain.GetBlock(inserted[i-1].Hash(), uint64(i)); block == nil {
				if canonical {
					t.Errorf("Canonical block    #%2d [%x...] missing before cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				} else {
					t.Errorf("Sidechain block    #%2d [%x...] missing before cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				}
			}
			if receipts := chain.GetReceiptsByHash(inserted[i-1].Hash()); receipts == nil {
				if canonical {
					t.Errorf("Canonical receipts #%2d [%x...] missing before cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				} else {
					t.Errorf("Sidechain receipts #%2d [%x...] missing before cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				}
			}
		} else {
			if header := chain.gombleader(inserted[i-1].Hash(), uint64(i)); header != nil {
				if canonical {
					t.Errorf("Canonical header   #%2d [%x...] present after cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				} else {
					t.Errorf("Sidechain header   #%2d [%x...] present after cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				}
			}
			if block := chain.GetBlock(inserted[i-1].Hash(), uint64(i)); block != nil {
				if canonical {
					t.Errorf("Canonical block    #%2d [%x...] present after cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				} else {
					t.Errorf("Sidechain block    #%2d [%x...] present after cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				}
			}
			if receipts := chain.GetReceiptsByHash(inserted[i-1].Hash()); receipts != nil {
				if canonical {
					t.Errorf("Canonical receipts #%2d [%x...] present after cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				} else {
					t.Errorf("Sidechain receipts #%2d [%x...] present after cap %d", inserted[i-1].Number(), inserted[i-1].Hash().Bytes()[:3], head)
				}
			}
		}
	}
}

// uint64ptr is a weird helper to allow 1-line constant pointer creation.
func uint64ptr(n uint64) *uint64 {
	return &n
}
