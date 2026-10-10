// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package lzhuf

import (
	"bytes"
	"encoding/hex"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// golden is a message compressed, with the CRC, by another implementation:
// wl2k-go's, which Winlink clients use to talk to FBB and BPQ BBSes.
const golden = "bd9763000000ea7c7f187fbdc6b00f0bfd7be1adcce5bc1f4fafff023005edb00189695b62bfca8ec79b17c3d2c1a6ca75351ce95a4068cab30fcedd510520"

const goldenText = "Hello, world!\r\nThis is a test of LZHUF, as FBB uses it.\r\nThis is a test of LZHUF, as FBB uses it.\r\n"

func TestGolden(t *testing.T) {
	var want, err = hex.DecodeString(golden)
	require.NoError(t, err)

	assert.Equal(t, want, Compress([]byte(goldenText), true), "the same bytes another implementation makes")

	var got, derr = Decompress(want, true)
	require.NoError(t, derr)
	assert.Equal(t, goldenText, string(got))
}

func TestRoundTrip(t *testing.T) {
	var rng = rand.New(rand.NewPCG(1, 2))

	var inputs = [][]byte{
		{},
		[]byte("a"),
		bytes.Repeat([]byte("x"), 10000),
		bytes.Repeat([]byte("abcabcabd"), 1000),
	}

	for range 50 {
		var b = make([]byte, rng.IntN(10000)+1)
		var alphabet = rng.IntN(255) + 1

		for j := range b {
			if j > 10 && rng.IntN(3) == 0 {
				b[j] = b[j-rng.IntN(10)-1]
			} else {
				b[j] = byte(rng.IntN(alphabet) & 0xff)
			}
		}

		inputs = append(inputs, b)
	}

	for _, in := range inputs {
		for _, crc := range []bool{false, true} {
			var c = Compress(in, crc)

			var out, err = Decompress(c, crc)
			require.NoError(t, err)
			assert.Equal(t, in, out)
		}
	}
}

func TestCompresses(t *testing.T) {
	var text = bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog.\r\n"), 100)
	assert.Less(t, len(Compress(text, false)), len(text)/10)
}

func TestDecompressRejects(t *testing.T) {
	var c = Compress([]byte(goldenText), true)

	var spoilt = bytes.Clone(c)
	spoilt[len(spoilt)-1] ^= 0xff

	var _, err = Decompress(spoilt, true)
	require.ErrorIs(t, err, ErrCRC)

	_, err = Decompress([]byte{1}, true)
	require.ErrorIs(t, err, ErrCorrupt)

	_, err = Decompress([]byte{1, 2, 3}, false)
	require.ErrorIs(t, err, ErrCorrupt)

	// A length out of all proportion to the data.
	_, err = Decompress([]byte{0xff, 0xff, 0xff, 0x7f, 0x00}, false)
	require.ErrorIs(t, err, ErrCorrupt)
}

func TestDecompressMax(t *testing.T) {
	var c = Compress([]byte(goldenText), false)

	var _, err = DecompressMax(c, false, len(goldenText)-1)
	require.ErrorIs(t, err, ErrTooBig)

	var out, oerr = DecompressMax(c, false, len(goldenText))
	require.NoError(t, oerr)
	assert.Equal(t, goldenText, string(out))
}

func TestCRC16(t *testing.T) {
	// The CRC-16/XMODEM check value.
	assert.Equal(t, uint16(0x31C3), crc16([]byte("123456789")))
}

func FuzzDecompress(f *testing.F) {
	var c = Compress([]byte(goldenText), false)
	f.Add(c, false)
	f.Add(Compress([]byte(goldenText), true), true)
	f.Add([]byte{0, 0, 0, 0}, false)

	f.Fuzz(func(t *testing.T, data []byte, crc bool) {
		var out, err = Decompress(data, crc)
		if err != nil {
			return
		}

		// Whatever came out goes back in and out again unchanged.
		var again, rerr = Decompress(Compress(out, crc), crc)
		if rerr != nil || !bytes.Equal(again, out) {
			t.Fatalf("round trip of %d bytes failed: %v", len(out), rerr)
		}
	})
}
