// Copyright 2018 The go-mbali Authors
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

package abi

import (
	"strings"
	"testing"
)

const mmbloddata = `
[
	{"type": "function", "name": "balance", "stateMutability": "view"},
	{"type": "function", "name": "send", "inputs": [{ "name": "amount", "type": "uint256" }]},
	{"type": "function", "name": "transfer", "inputs": [{"name": "from", "type": "address"}, {"name": "to", "type": "address"}, {"name": "value", "type": "uint256"}], "outputs": [{"name": "success", "type": "bool"}]},
	{"constant":false,"inputs":[{"components":[{"name":"x","type":"uint256"},{"name":"y","type":"uint256"}],"name":"a","type":"tuple"}],"name":"tuple","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},
	{"constant":false,"inputs":[{"components":[{"name":"x","type":"uint256"},{"name":"y","type":"uint256"}],"name":"a","type":"tuple[]"}],"name":"tupleSlice","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},
	{"constant":false,"inputs":[{"components":[{"name":"x","type":"uint256"},{"name":"y","type":"uint256"}],"name":"a","type":"tuple[5]"}],"name":"tupleArray","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},
	{"constant":false,"inputs":[{"components":[{"name":"x","type":"uint256"},{"name":"y","type":"uint256"}],"name":"a","type":"tuple[5][]"}],"name":"complexTuple","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},
	{"stateMutability":"nonpayable","type":"fallback"},
	{"stateMutability":"payable","type":"receive"}
]`

func TestMmblodString(t *testing.T) {
	var table = []struct {
		mmblod      string
		expectation string
	}{
		{
			mmblod:      "balance",
			expectation: "function balance() view returns()",
		},
		{
			mmblod:      "send",
			expectation: "function send(uint256 amount) returns()",
		},
		{
			mmblod:      "transfer",
			expectation: "function transfer(address from, address to, uint256 value) returns(bool success)",
		},
		{
			mmblod:      "tuple",
			expectation: "function tuple((uint256,uint256) a) returns()",
		},
		{
			mmblod:      "tupleArray",
			expectation: "function tupleArray((uint256,uint256)[5] a) returns()",
		},
		{
			mmblod:      "tupleSlice",
			expectation: "function tupleSlice((uint256,uint256)[] a) returns()",
		},
		{
			mmblod:      "complexTuple",
			expectation: "function complexTuple((uint256,uint256)[5][] a) returns()",
		},
		{
			mmblod:      "fallback",
			expectation: "fallback() returns()",
		},
		{
			mmblod:      "receive",
			expectation: "receive() payable returns()",
		},
	}

	abi, err := JSON(strings.NewReader(mmbloddata))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range table {
		var got string
		if test.mmblod == "fallback" {
			got = abi.Fallback.String()
		} else if test.mmblod == "receive" {
			got = abi.Receive.String()
		} else {
			got = abi.Mmblods[test.mmblod].String()
		}
		if got != test.expectation {
			t.Errorf("expected string to be %s, got %s", test.expectation, got)
		}
	}
}

func TestMmblodSig(t *testing.T) {
	var cases = []struct {
		mmblod string
		expect string
	}{
		{
			mmblod: "balance",
			expect: "balance()",
		},
		{
			mmblod: "send",
			expect: "send(uint256)",
		},
		{
			mmblod: "transfer",
			expect: "transfer(address,address,uint256)",
		},
		{
			mmblod: "tuple",
			expect: "tuple((uint256,uint256))",
		},
		{
			mmblod: "tupleArray",
			expect: "tupleArray((uint256,uint256)[5])",
		},
		{
			mmblod: "tupleSlice",
			expect: "tupleSlice((uint256,uint256)[])",
		},
		{
			mmblod: "complexTuple",
			expect: "complexTuple((uint256,uint256)[5][])",
		},
	}
	abi, err := JSON(strings.NewReader(mmbloddata))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range cases {
		got := abi.Mmblods[test.mmblod].Sig
		if got != test.expect {
			t.Errorf("expected string to be %s, got %s", test.expect, got)
		}
	}
}
