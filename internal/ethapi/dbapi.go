// Copyright 2022 The go-mbali Authors
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

package mblapi

import (
	"github.com/mbali/go-mbali/common"
	"github.com/mbali/go-mbali/common/hexutil"
)

// DbGet returns the raw value of a key stored in the database.
func (api *PrivateDebugAPI) DbGet(key string) (hexutil.Bytes, error) {
	blob, err := common.ParseHexOrString(key)
	if err != nil {
		return nil, err
	}
	return api.b.ChainDb().Get(blob)
}

// DbAncient retrieves an ancient binary blob from the append-only immutable files.
// It is a mapping to the `AncientReaderOp.Ancient` mmblod
func (api *PrivateDebugAPI) DbAncient(kind string, number uint64) (hexutil.Bytes, error) {
	return api.b.ChainDb().Ancient(kind, number)
}

// DbAncients returns the ancient item numbers in the ancient store.
// It is a mapping to the `AncientReaderOp.Ancients` mmblod
func (api *PrivateDebugAPI) DbAncients() (uint64, error) {
	return api.b.ChainDb().Ancients()
}
