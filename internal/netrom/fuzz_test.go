// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
)

// Anyone on frequency can send a NODES broadcast or, once linked, a NET/ROM
// packet, so the decoders and the Router behind them must take anything.

func FuzzNodesDecode(f *testing.F) {
	var frames, _ = EncodeNodes("ALIAS", []NodesEntry{{Call: "Q1TEST", Alias: "ONE", BestNeighbour: "Q2TEST", Quality: 200}})
	f.Add(frames[0])
	f.Add([]byte{0xff})
	f.Add([]byte{0xff, 'A', ' ', ' ', ' ', ' ', ' ', 1, 2, 3})

	f.Fuzz(func(t *testing.T, info []byte) {
		var nb, err = DecodeNodes(info)
		if err != nil {
			return
		}

		// Whatever decoded must encode again.
		var _, eerr = EncodeNodes(nb.Alias, nb.Entries)
		if eerr != nil {
			t.Fatalf("decoded %v but could not encode it: %v", nb, eerr)
		}
	})
}

func FuzzPacketDecode(f *testing.F) {
	var p, _ = testPacket("Q1TEST", "Q2TEST", transport(1, 0, 0, 0, OpConnectRequest, 0, testConnectRequest())).Encode()
	f.Add(p)
	f.Add(make([]byte, L3HeaderLen+L4HeaderLen))

	f.Fuzz(func(t *testing.T, b []byte) {
		var p, err = DecodePacket(b)
		if err != nil {
			return
		}

		var again, eerr = p.Encode()
		if eerr != nil {
			t.Fatalf("decoded %+v but could not encode it: %v", p, eerr)
		}

		var p2, derr = DecodePacket(again)
		if derr != nil || p2.Origin != p.Origin || p2.Destination != p.Destination || p2.Opcode != p.Opcode {
			t.Fatalf("round trip changed %+v to %+v (%v)", p, p2, derr)
		}

		_, _ = DecodeConnectRequest(p.Payload)
	})
}

func testConnectRequest() []byte {
	var cr, _ = ConnectRequest{Window: 4, User: "Q1TEST", Node: "Q2TEST", Timeout: maybe.Nothing[int]()}.Encode()

	return cr
}

func testPacket(origin string, destination string, p Packet) Packet {
	p.Origin = origin
	p.Destination = destination
	p.TTL = 7

	return p
}

type discardLink struct{}

func (discardLink) SendPacket(NeighbourKey, []byte) {}
func (discardLink) BroadcastNodes(int, []byte)      {}

// FuzzRouter feeds a Router a run of packets and broadcasts, as a neighbour
// could send them.
func FuzzRouter(f *testing.F) {
	var connect, _ = testPacket("Q2TEST", "Q1TEST", transport(1, 1, 0, 0, OpConnectRequest, 0, testConnectRequest())).Encode()
	var info, _ = testPacket("Q2TEST", "Q1TEST", transport(1, 0, 0, 0, OpInfo, 0, []byte("hi"))).Encode()
	var nodes, _ = EncodeNodes("TWO", []NodesEntry{{Call: "Q3TEST", Alias: "THREE", BestNeighbour: "Q4TEST", Quality: 200}})

	f.Add(connect, info, nodes[0])
	f.Add([]byte{}, []byte{}, []byte{})

	f.Fuzz(func(t *testing.T, a []byte, b []byte, n []byte) {
		var cfg = DefaultConfig()
		cfg.Call = "Q1TEST"
		cfg.Alias = "ONE"
		cfg.Ports = []PortConfig{{Port: 0, Quality: 192, Broadcast: true}}

		var now = time.Unix(1_000_000, 0)

		var r, err = NewRouter(cfg, discardLink{}, now)
		if err != nil {
			t.Fatal(err)
		}

		_ = r.Listen("Q1TEST", "ONE", 0, func(c *Circuit) CircuitHandler {
			_ = c.Write([]byte("welcome"))

			return nopHandler{}
		})

		var from = NeighbourKey{Port: 0, Call: "Q2TEST"}

		r.HeardNodes(0, "Q2TEST", n, now)
		r.ReceivePacket(from, a, now)
		r.ReceivePacket(from, b, now)
		r.ReceivePacket(from, a, now)

		for range 10 {
			now = now.Add(time.Minute)
			r.Tick(now)
		}
	})
}
