// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package wavwrite

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderSize(t *testing.T) {
	assert.Equal(t, HeaderSize, binary.Size(new(header)))
}

func TestFormatValidate(t *testing.T) {
	var valid = Format{NumChannels: 1, SamplesPerSec: 44100, BitsPerSample: 16}
	require.NoError(t, valid.Validate())

	valid.NumChannels = 2
	require.NoError(t, valid.Validate())

	valid.BitsPerSample = 8
	require.NoError(t, valid.Validate())

	require.Error(t, Format{NumChannels: 0, SamplesPerSec: 44100, BitsPerSample: 16}.Validate())
	require.Error(t, Format{NumChannels: 3, SamplesPerSec: 44100, BitsPerSample: 16}.Validate())
	require.Error(t, Format{NumChannels: 1, SamplesPerSec: 44100, BitsPerSample: 24}.Validate())
	require.Error(t, Format{NumChannels: 1, SamplesPerSec: 0, BitsPerSample: 16}.Validate())
	require.Error(t, Format{NumChannels: 1, SamplesPerSec: -44100, BitsPerSample: 16}.Validate())
}

// TestFormatValidateRejectsOverflow checks that we reject sample rates that
// can't be represented in the int32 header fields, rather than silently
// writing a header that describes some other format. The byte rate is the
// tighter bound of the two, as it is the sample rate times the frame size.
func TestFormatValidateRejectsOverflow(t *testing.T) {
	if math.MaxInt <= math.MaxInt32 {
		t.Skip("int is not wider than int32 on this platform, so these rates are unrepresentable anyway")
	}

	// Mono 8 bit is one byte per frame, so only the sample rate itself binds.
	var mono8 = Format{NumChannels: 1, SamplesPerSec: math.MaxInt32, BitsPerSample: 8}
	require.NoError(t, mono8.Validate())

	mono8.SamplesPerSec = math.MaxInt32 + 1
	require.Error(t, mono8.Validate())

	// Stereo 16 bit is four bytes per frame, so the byte rate overflows first.
	var stereo16 = Format{NumChannels: 2, SamplesPerSec: math.MaxInt32 / 4, BitsPerSample: 16}
	require.NoError(t, stereo16.Validate())

	stereo16.SamplesPerSec = math.MaxInt32/4 + 1
	require.Error(t, stereo16.Validate())
}

func TestCreateRejectsBadFormat(t *testing.T) {
	var path = filepath.Join(t.TempDir(), "bad.wav")

	var w, err = Create(path, Format{NumChannels: 7, SamplesPerSec: 44100, BitsPerSample: 16})
	require.Error(t, err)
	assert.Nil(t, w)
	assert.NoFileExists(t, path)
}

// TestWriteAndClose checks that a written file has the header we expect, with
// the lengths filled in, followed by the sample data.
func TestWriteAndClose(t *testing.T) {
	var path = filepath.Join(t.TempDir(), "out.wav")

	var w, err = Create(path, Format{NumChannels: 2, SamplesPerSec: 44100, BitsPerSample: 16})
	require.NoError(t, err)

	var data = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}

	n, err := w.Write(data[:4])
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	require.NoError(t, w.WriteByte(data[4]))
	require.NoError(t, w.WriteByte(data[5]))

	assert.Equal(t, len(data), w.BytesWritten())

	require.NoError(t, w.Close())

	contents, err := os.ReadFile(path) //nolint:gosec // Test file we just created.
	require.NoError(t, err)
	require.Len(t, contents, HeaderSize+len(data))

	assert.Equal(t, "RIFF", string(contents[0:4]))
	assert.Equal(t, uint32(HeaderSize+len(data)-8), binary.LittleEndian.Uint32(contents[4:8]))
	assert.Equal(t, "WAVE", string(contents[8:12]))
	assert.Equal(t, "fmt ", string(contents[12:16]))
	assert.Equal(t, uint32(16), binary.LittleEndian.Uint32(contents[16:20]))
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(contents[20:22])) // PCM
	assert.Equal(t, uint16(2), binary.LittleEndian.Uint16(contents[22:24])) // Channels
	assert.Equal(t, uint32(44100), binary.LittleEndian.Uint32(contents[24:28]))
	assert.Equal(t, uint32(44100*4), binary.LittleEndian.Uint32(contents[28:32])) // Bytes per second
	assert.Equal(t, uint16(4), binary.LittleEndian.Uint16(contents[32:34]))       // Block align
	assert.Equal(t, uint16(16), binary.LittleEndian.Uint16(contents[34:36]))      // Bits per sample
	assert.Equal(t, "data", string(contents[36:40]))
	assert.Equal(t, uint32(len(data)), binary.LittleEndian.Uint32(contents[40:44]))
	assert.Equal(t, data, contents[HeaderSize:])
}

// TestEmptyFile checks that a file that never had any samples written to it is
// still a valid (if silent) .WAV file.
func TestEmptyFile(t *testing.T) {
	var path = filepath.Join(t.TempDir(), "empty.wav")

	var w, err = Create(path, Format{NumChannels: 1, SamplesPerSec: 8000, BitsPerSample: 8})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	contents, err := os.ReadFile(path) //nolint:gosec // Test file we just created.
	require.NoError(t, err)
	require.Len(t, contents, HeaderSize)

	assert.Equal(t, uint32(HeaderSize-8), binary.LittleEndian.Uint32(contents[4:8]))
	assert.Equal(t, uint32(0), binary.LittleEndian.Uint32(contents[40:44]))
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(contents[32:34])) // Block align
}

func TestUseAfterClose(t *testing.T) {
	var path = filepath.Join(t.TempDir(), "closed.wav")

	var w, err = Create(path, Format{NumChannels: 1, SamplesPerSec: 8000, BitsPerSample: 16})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	require.ErrorIs(t, w.Close(), ErrClosed)

	n, writeErr := w.Write([]byte{0x00})
	assert.Equal(t, 0, n)
	require.ErrorIs(t, writeErr, ErrClosed)
	require.ErrorIs(t, w.WriteByte(0x00), ErrClosed)
}

func TestCreateUnwritable(t *testing.T) {
	var path = filepath.Join(t.TempDir(), "nonexistent-dir", "out.wav")

	var w, err = Create(path, Format{NumChannels: 1, SamplesPerSec: 8000, BitsPerSample: 16})
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Nil(t, w)
}
