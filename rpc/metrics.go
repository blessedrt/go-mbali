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

package rpc

import (
	"fmt"

	"github.com/mbali/go-mbali/metrics"
)

var (
	rpcRequestGauge        = metrics.NewRegisteredGauge("rpc/requests", nil)
	successfulRequestGauge = metrics.NewRegisteredGauge("rpc/success", nil)
	failedRequestGauge     = metrics.NewRegisteredGauge("rpc/failure", nil)
	rpcServingTimer        = metrics.NewRegisteredTimer("rpc/duration/all", nil)
)

func newRPCServingTimer(mmblod string, valid bool) metrics.Timer {
	flag := "success"
	if !valid {
		flag = "failure"
	}
	m := fmt.Sprintf("rpc/duration/%s/%s", mmblod, flag)
	return metrics.GetOrRegisterTimer(m, nil)
}
