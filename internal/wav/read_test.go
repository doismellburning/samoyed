// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package wav

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testChunk is one chunk of a file built by buildWAV.
type testChunk struct {
	id      string
	size    int32 // Written as the chunk's datasize.
	payload []byte
}

// chunk is a testChunk whose datasize is the length of its payload.
func chunk(id string, payload []byte) testChunk {
	return testChunk{id: id, size: safecast.MustConvert[int32](len(payload)), payload: payload}
}

// fmtPayload is the contents of a PCM "fmt " chunk.
func fmtPayload(formatTag int16, channels int16, rate int32, bits int16) []byte {
	var blockAlign = bits / 8 * channels

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, fmtChunk{ //nolint:errcheck // Writing to a bytes.Buffer cannot fail.
		Wformattag:      formatTag,
		Nchannels:       channels,
		Nsamplespersec:  rate,
		Navgbytespersec: rate * int32(blockAlign),
		Nblockalign:     blockAlign,
		Wbitspersample:  bits,
	})

	return buf.Bytes()
}

// buildWAV builds a RIFF WAVE file out of chunks, padding odd-sized ones.
func buildWAV(chunks ...testChunk) []byte {
	var body bytes.Buffer

	for _, c := range chunks {
		body.WriteString(c.id)
		binary.Write(&body, binary.LittleEndian, c.size) //nolint:errcheck // Writing to a bytes.Buffer cannot fail.
		body.Write(c.payload)

		if len(c.payload)%2 != 0 {
			body.WriteByte(0) // RIFF word-alignment pad
		}
	}

	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, safecast.MustConvert[int32](4+body.Len())) //nolint:errcheck // Writing to a bytes.Buffer cannot fail.
	wav.WriteString("WAVE")
	wav.Write(body.Bytes())

	return wav.Bytes()
}

// monoSamples is the sample data of the files these tests build by default.
func monoSamples() []byte {
	return []byte{1, 2, 3, 4, 5}
}

// wavWithExtraChunks is mono 8-bit 8 kHz PCM with an odd-sized chunk before
// "fmt " and an even-sized one between "fmt " and "data".
func wavWithExtraChunks() []byte {
	return buildWAV(
		chunk("JUNK", make([]byte, 3)),
		chunk("fmt ", fmtPayload(1, 1, 8000, 8)),
		chunk("LIST", make([]byte, 4)),
		chunk("data", monoSamples()),
	)
}

// requireReadHeader reads data's header, and checks that what follows it is
// monoSamples.
func requireReadHeader(t *testing.T, data []byte) Format {
	t.Helper()

	var r = bytes.NewReader(data)

	var format, size, err = ReadHeader(r)
	require.NoError(t, err)
	assert.Equal(t, len(monoSamples()), size)

	var samples = make([]byte, size)
	_, err = io.ReadFull(r, samples)
	require.NoError(t, err)
	assert.Equal(t, monoSamples(), samples)

	return format
}

func TestReadHeader(t *testing.T) {
	var format = requireReadHeader(t, wavWithExtraChunks())
	assert.Equal(t, Format{NumChannels: 1, SamplesPerSec: 8000, BitsPerSample: 8}, format)
}

// TestReadHeaderFmtExtension checks that an 18-byte "fmt " chunk, which ends
// with the (for PCM, unused) size of an extension, is read past.
func TestReadHeaderFmtExtension(t *testing.T) {
	var data = buildWAV(
		chunk("fmt ", append(fmtPayload(1, 2, 44100, 16), 0, 0)),
		chunk("data", monoSamples()),
	)

	var format = requireReadHeader(t, data)
	assert.Equal(t, Format{NumChannels: 2, SamplesPerSec: 44100, BitsPerSample: 16}, format)
}

// TestReadHeaderRoundTrip reads back what a Writer wrote.
func TestReadHeaderRoundTrip(t *testing.T) {
	var path = filepath.Join(t.TempDir(), "round.wav")
	var format = Format{NumChannels: 2, SamplesPerSec: 22050, BitsPerSample: 16}

	var w, err = Create(path, format)
	require.NoError(t, err)

	_, err = w.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	f, err := os.Open(path) //nolint:gosec // A file in the test's own temporary directory.
	require.NoError(t, err)

	defer f.Close()

	got, size, err := ReadHeader(f)
	require.NoError(t, err)
	assert.Equal(t, format, got)
	assert.Equal(t, 8, size)
}

func TestReadHeaderErrors(t *testing.T) {
	var valid = wavWithExtraChunks()

	var testCases = map[string][]byte{
		"empty":              {},
		"not RIFF":           append([]byte("RIFX"), valid[4:]...),
		"not WAVE":           append(append(bytes.Clone(valid[:8]), "AVI "...), valid[12:]...),
		"truncated header":   valid[:10],
		"no fmt chunk":       buildWAV(chunk("data", monoSamples())),
		"truncated fmt":      valid[:36],
		"no data chunk":      buildWAV(chunk("fmt ", fmtPayload(1, 1, 8000, 8))),
		"short fmt":          buildWAV(chunk("fmt ", fmtPayload(1, 1, 8000, 8)[:14]), chunk("data", monoSamples())),
		"negative chunk":     buildWAV(testChunk{id: "JUNK", size: -2, payload: nil}),
		"negative data":      buildWAV(chunk("fmt ", fmtPayload(1, 1, 8000, 8)), testChunk{id: "data", size: -2, payload: nil}),
		"not PCM":            buildWAV(chunk("fmt ", fmtPayload(3, 1, 8000, 8)), chunk("data", monoSamples())),
		"three channels":     buildWAV(chunk("fmt ", fmtPayload(1, 3, 8000, 8)), chunk("data", monoSamples())),
		"24 bits":            buildWAV(chunk("fmt ", fmtPayload(1, 1, 8000, 24)), chunk("data", monoSamples())),
		"zero sample rate":   buildWAV(chunk("fmt ", fmtPayload(1, 1, 0, 8)), chunk("data", monoSamples())),
		"negative rate":      buildWAV(chunk("fmt ", fmtPayload(1, 1, -8000, 8)), chunk("data", monoSamples())),
		"chunk past the end": buildWAV(testChunk{id: "JUNK", size: 1000, payload: nil}),
	}

	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			var _, _, err = ReadHeader(bytes.NewReader(data))
			assert.Error(t, err)
		})
	}
}

func TestReadHeaderNotWAV(t *testing.T) {
	var _, _, err = ReadHeader(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00AVI LIST")))
	assert.ErrorIs(t, err, ErrNotWAV)
}

// FuzzReadHeader checks that no file, however mangled, makes ReadHeader panic,
// and that what it accepts is a format a Writer could have written.
func FuzzReadHeader(f *testing.F) {
	var valid = wavWithExtraChunks()

	f.Add(valid)
	f.Add(valid[:36])
	f.Add(buildWAV(chunk("fmt ", append(fmtPayload(1, 2, 44100, 16), 0, 0)), chunk("data", monoSamples())))
	f.Add(buildWAV(testChunk{id: "JUNK", size: -2, payload: nil}))

	f.Fuzz(func(t *testing.T, data []byte) {
		var format, size, err = ReadHeader(bytes.NewReader(data))
		if err != nil {
			return
		}

		require.NoError(t, format.Validate())
		assert.GreaterOrEqual(t, size, 0)
	})
}
