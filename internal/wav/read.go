// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/ccoveille/go-safecast/v2"
)

// riffHeader is the start of every .WAV file, followed by a series of chunks.
type riffHeader struct {
	RIFF     [4]byte /* "RIFF" */
	Filesize int32   /* file length - 8 */
	WAVE     [4]byte /* "WAVE" */
}

// chunkHeader precedes each chunk's contents.
type chunkHeader struct {
	ID       [4]byte /* e.g. "fmt ", "LIST" or "data" */
	Datasize int32   /* not counting the pad byte after an odd-sized chunk */
}

// fmtChunk is the contents of the "fmt " chunk for PCM audio.
type fmtChunk struct {
	Wformattag      int16 /* 1 for PCM. */
	Nchannels       int16 /* 1 for mono, 2 for stereo. */
	Nsamplespersec  int32 /* sampling freq, Hz. */
	Navgbytespersec int32 /* = nblockalign*nsamplespersec. */
	Nblockalign     int16 /* = wbitspersample/8 * nchannels. */
	Wbitspersample  int16 /* 16 or 8. */
}

// ErrNotWAV is returned by [ReadHeader] for a file that doesn't start the way
// a .WAV file does.
var ErrNotWAV = errors.New("wav: not a .WAV format file")

// ReadHeader reads a .WAV file's header from r, skipping any chunks other than
// "fmt " and "data", and leaves r positioned at the first byte of sample data.
// It returns the format of the audio and the number of bytes of sample data.
//
// It only understands the uncompressed PCM files a [Writer] writes - 1 or 2
// channels of 8 or 16 bits per sample - which is good enough for our purposes.
func ReadHeader(r io.ReadSeeker) (Format, int, error) {
	var header riffHeader

	var err = binary.Read(r, binary.LittleEndian, &header)
	if err != nil {
		return Format{}, 0, fmt.Errorf("wav: couldn't read file header: %w", err)
	}

	if string(header.RIFF[:]) != "RIFF" || string(header.WAVE[:]) != "WAVE" {
		return Format{}, 0, ErrNotWAV
	}

	chunk, err := findChunk(r, "fmt ")
	if err != nil {
		return Format{}, 0, err
	}

	var format fmtChunk

	var formatSize = safecast.MustConvert[int32](binary.Size(format))
	if chunk.Datasize != formatSize && chunk.Datasize != formatSize+2 {
		return Format{}, 0, fmt.Errorf("wav: need fmt chunk datasize of %d or %d, found %d", formatSize, formatSize+2, chunk.Datasize)
	}

	err = binary.Read(r, binary.LittleEndian, &format)
	if err != nil {
		return Format{}, 0, fmt.Errorf("wav: couldn't read fmt chunk: %w", err)
	}

	// The extension size field of an 18-byte fmt chunk, which PCM doesn't use.
	_, err = r.Seek(int64(chunk.Datasize-formatSize), io.SeekCurrent)
	if err != nil {
		return Format{}, 0, fmt.Errorf("wav: couldn't seek past fmt chunk: %w", err)
	}

	chunk, err = findChunk(r, "data")
	if err != nil {
		return Format{}, 0, err
	}

	if format.Wformattag != 1 {
		return Format{}, 0, fmt.Errorf("wav: only audio format 1 (PCM) is supported, this file has %d", format.Wformattag)
	}

	var f = Format{
		NumChannels:   int(format.Nchannels),
		SamplesPerSec: int(format.Nsamplespersec),
		BitsPerSample: int(format.Wbitspersample),
	}

	err = f.Validate()
	if err != nil {
		return Format{}, 0, err
	}

	return f, int(chunk.Datasize), nil
}

// findChunk reads chunk headers from r, skipping the contents of each, until
// it finds the one with the given id, leaving r at the start of its contents.
func findChunk(r io.ReadSeeker, id string) (chunkHeader, error) {
	for {
		var chunk chunkHeader

		var err = binary.Read(r, binary.LittleEndian, &chunk)
		if err != nil {
			return chunkHeader{}, fmt.Errorf("wav: couldn't find %q chunk: %w", id, err)
		}

		if chunk.Datasize < 0 {
			return chunkHeader{}, fmt.Errorf("wav: invalid %q chunk datasize %d", chunk.ID[:], chunk.Datasize)
		}

		if string(chunk.ID[:]) == id {
			return chunk, nil
		}

		// Chunks are word-aligned, so an odd-sized one is followed by a pad byte.
		_, err = r.Seek(int64(chunk.Datasize)+int64(chunk.Datasize%2), io.SeekCurrent)
		if err != nil {
			return chunkHeader{}, fmt.Errorf("wav: couldn't seek past %q chunk: %w", chunk.ID[:], err)
		}
	}
}
