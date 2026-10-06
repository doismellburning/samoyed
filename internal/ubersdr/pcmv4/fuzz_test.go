// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package pcmv4

import (
	"encoding/binary"
	"os"
	"testing"
)

// fuzzSeedPackets returns the packets of upstream's recorded stream, in the layout
// readFixture reads, but for a fuzz target, which readFixture's *testing.T
// can't serve.
func fuzzSeedPackets(f *testing.F) [][]byte {
	f.Helper()

	var raw, err = os.ReadFile("testdata/pcmv4_stream.bin")
	if err != nil {
		f.Fatalf("fixture: %v", err)
	}

	var packets [][]byte

	for off := 9; off+4 <= len(raw); {
		var n = int(binary.LittleEndian.Uint32(raw[off:]))

		off += 4
		if n > len(raw)-off {
			f.Fatalf("fixture: truncated packet at %d", off)
		}

		packets = append(packets, raw[off:off+n])
		off += n
	}

	return packets
}

// FuzzDecodePacket covers the version 4 decoder, which whatever UberSDR server
// an ADEVICE names can feed.  Each input is a run of packets through one
// decoder, as they would arrive on one connection, each preceded by a
// little-endian uint16 length.  It is seeded with pairs of packets from
// upstream's recorded stream, so that the fuzzer starts from packets that get
// past the header and into the codec.
func FuzzDecodePacket(f *testing.F) {
	var packets = fuzzSeedPackets(f)

	for i := 0; i+1 < len(packets) && i < 16; i += 2 {
		var seed []byte

		for _, pkt := range packets[i : i+2] {
			if len(pkt) > 0xffff {
				f.Fatalf("fixture: packet of %d bytes won't fit a seed's length", len(pkt))
			}

			seed = binary.LittleEndian.AppendUint16(seed, uint16(len(pkt)&0xffff))
			seed = append(seed, pkt...)
		}

		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, data []byte) {
		var dec = NewPCMv4StreamDecoder()

		for len(data) >= 2 {
			var n = min(int(binary.LittleEndian.Uint16(data)), len(data)-2)

			_, _, _ = dec.DecodePacket(data[2 : 2+n])

			data = data[2+n:]
		}
	})
}
