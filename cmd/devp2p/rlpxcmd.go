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

package main

import (
	"fmt"
	"net"

	"github.com/mbali/go-mbali/cmd/devp2p/internal/mbltest"
	"github.com/mbali/go-mbali/crypto"
	"github.com/mbali/go-mbali/internal/utesting"
	"github.com/mbali/go-mbali/p2p"
	"github.com/mbali/go-mbali/p2p/rlpx"
	"github.com/mbali/go-mbali/rlp"
	"gopkg.in/urfave/cli.v1"
)

var (
	rlpxCommand = cli.Command{
		Name:  "rlpx",
		Usage: "RLPx Commands",
		Subcommands: []cli.Command{
			rlpxPingCommand,
			rlpxmblTestCommand,
			rlpxSnapTestCommand,
		},
	}
	rlpxPingCommand = cli.Command{
		Name:   "ping",
		Usage:  "ping <node>",
		Action: rlpxPing,
	}
	rlpxmblTestCommand = cli.Command{
		Name:      "mbl-test",
		Usage:     "Runs tests against a node",
		ArgsUsage: "<node> <chain.rlp> <genesis.json>",
		Action:    rlpxmblTest,
		Flags: []cli.Flag{
			testPatternFlag,
			testTAPFlag,
		},
	}
	rlpxSnapTestCommand = cli.Command{
		Name:      "snap-test",
		Usage:     "Runs tests against a node",
		ArgsUsage: "<node> <chain.rlp> <genesis.json>",
		Action:    rlpxSnapTest,
		Flags: []cli.Flag{
			testPatternFlag,
			testTAPFlag,
		},
	}
)

func rlpxPing(ctx *cli.Context) error {
	n := getNodeArg(ctx)
	fd, err := net.Dial("tcp", fmt.Sprintf("%v:%d", n.IP(), n.TCP()))
	if err != nil {
		return err
	}
	conn := rlpx.NewConn(fd, n.Pubkey())
	ourKey, _ := crypto.GenerateKey()
	_, err = conn.Handshake(ourKey)
	if err != nil {
		return err
	}
	code, data, _, err := conn.Read()
	if err != nil {
		return err
	}
	switch code {
	case 0:
		var h mbltest.Hello
		if err := rlp.DecodeBytes(data, &h); err != nil {
			return fmt.Errorf("invalid handshake: %v", err)
		}
		fmt.Printf("%+v\n", h)
	case 1:
		var msg []p2p.DiscReason
		if rlp.DecodeBytes(data, &msg); len(msg) == 0 {
			return fmt.Errorf("invalid disconnect message")
		}
		return fmt.Errorf("received disconnect message: %v", msg[0])
	default:
		return fmt.Errorf("invalid message code %d, expected handshake (code zero)", code)
	}
	return nil
}

// rlpxmblTest runs the mbl protocol test suite.
func rlpxmblTest(ctx *cli.Context) error {
	if ctx.NArg() < 3 {
		exit("missing path to chain.rlp as command-line argument")
	}
	suite, err := mbltest.NewSuite(getNodeArg(ctx), ctx.Args()[1], ctx.Args()[2])
	if err != nil {
		exit(err)
	}
	// check if given node supports mbl66, and if so, run mbl66 protocol tests as well
	is66Failed, _ := utesting.Run(utesting.Test{Name: "Is_66", Fn: suite.Is_66})
	if is66Failed {
		return runTests(ctx, suite.mblTests())
	}
	return runTests(ctx, suite.AllmblTests())
}

// rlpxSnapTest runs the snap protocol test suite.
func rlpxSnapTest(ctx *cli.Context) error {
	if ctx.NArg() < 3 {
		exit("missing path to chain.rlp as command-line argument")
	}
	suite, err := mbltest.NewSuite(getNodeArg(ctx), ctx.Args()[1], ctx.Args()[2])
	if err != nil {
		exit(err)
	}
	return runTests(ctx, suite.SnapTests())
}
