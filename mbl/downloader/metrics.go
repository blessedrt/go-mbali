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

// Contains the metrics collected by the downloader.

package downloader

import (
	"github.com/mbali/go-mbali/metrics"
)

var (
	headerInMeter      = metrics.NewRegisteredMeter("mbl/downloader/headers/in", nil)
	headerReqTimer     = metrics.NewRegisteredTimer("mbl/downloader/headers/req", nil)
	headerDropMeter    = metrics.NewRegisteredMeter("mbl/downloader/headers/drop", nil)
	headerTimeoutMeter = metrics.NewRegisteredMeter("mbl/downloader/headers/timeout", nil)

	bodyInMeter      = metrics.NewRegisteredMeter("mbl/downloader/bodies/in", nil)
	bodyReqTimer     = metrics.NewRegisteredTimer("mbl/downloader/bodies/req", nil)
	bodyDropMeter    = metrics.NewRegisteredMeter("mbl/downloader/bodies/drop", nil)
	bodyTimeoutMeter = metrics.NewRegisteredMeter("mbl/downloader/bodies/timeout", nil)

	receiptInMeter      = metrics.NewRegisteredMeter("mbl/downloader/receipts/in", nil)
	receiptReqTimer     = metrics.NewRegisteredTimer("mbl/downloader/receipts/req", nil)
	receiptDropMeter    = metrics.NewRegisteredMeter("mbl/downloader/receipts/drop", nil)
	receiptTimeoutMeter = metrics.NewRegisteredMeter("mbl/downloader/receipts/timeout", nil)

	throttleCounter = metrics.NewRegisteredCounter("mbl/downloader/throttle", nil)
)
