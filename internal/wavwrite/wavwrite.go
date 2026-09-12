// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package wavwrite writes uncompressed PCM .WAV (RIFF) files.
//
// A [Writer] writes a placeholder header when the file is created, buffers the
// sample data subsequently written to it, and then seeks back to fill in the
// lengths when it is closed - so a file is only valid once [Writer.Close] has
// returned without error.
package wavwrite

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// header is the 44-byte canonical .WAV file header.
type header struct {
	riff            [4]byte /* "RIFF" */
	filesize        int32   /* file length - 8 */
	wave            [4]byte /* "WAVE" */
	fmt             [4]byte /* "fmt " */
	fmtsize         int32   /* 16. */
	wformattag      int16   /* 1 for PCM. */
	nchannels       int16   /* 1 for mono, 2 for stereo. */
	nsamplespersec  int32   /* sampling freq, Hz. */
	navgbytespersec int32   /* = nblockalign * nsamplespersec. */
	nblockalign     int16   /* = wbitspersample / 8 * nchannels. */
	wbitspersample  int16   /* 16 or 8. */
	data            [4]byte /* "data" */
	datasize        int32   /* number of bytes following. */
}

// HeaderSize is the number of bytes the .WAV header occupies at the start of
// the file.
const HeaderSize = 44

// ErrClosed is returned by the methods of a [Writer] that has already been
// closed.
var ErrClosed = errors.New("wavwrite: writer is closed")

// Format describes the sample format of the audio written to a file.
type Format struct {
	NumChannels   int // 1 for mono, 2 for stereo.
	SamplesPerSec int // Sampling frequency, Hz.
	BitsPerSample int // 8 or 16.
}

// Validate reports whether f describes a format we can write.
func (f Format) Validate() error {
	if f.NumChannels != 1 && f.NumChannels != 2 {
		return fmt.Errorf("wavwrite: unsupported number of channels %d, must be 1 or 2", f.NumChannels)
	}

	if f.BitsPerSample != 8 && f.BitsPerSample != 16 {
		return fmt.Errorf("wavwrite: unsupported bits per sample %d, must be 8 or 16", f.BitsPerSample)
	}

	if f.SamplesPerSec <= 0 {
		return fmt.Errorf("wavwrite: invalid sample rate %d, must be positive", f.SamplesPerSec)
	}

	// The header holds the sample rate, and the byte rate derived from it, as
	// int32s - so reject anything that wouldn't survive the conversion rather
	// than silently writing a header describing some other format.
	var bytesPerFrame = f.BitsPerSample / 8 * f.NumChannels // 1 to 4, so never zero.
	if f.SamplesPerSec > math.MaxInt32/bytesPerFrame {
		return fmt.Errorf("wavwrite: sample rate %d is too high for %d channel(s) at %d bits per sample, maximum is %d",
			f.SamplesPerSec, f.NumChannels, f.BitsPerSample, math.MaxInt32/bytesPerFrame)
	}

	return nil
}

// A Writer writes PCM sample data to a .WAV file.
//
// It is not safe for concurrent use.
type Writer struct {
	file      *os.File
	buf       *bufio.Writer
	header    header
	byteCount int
}

// Create creates (or truncates) the named file and writes a .WAV header for
// the given format. The caller must call [Writer.Close] to flush the sample
// data and fill in the lengths in the header.
func Create(name string, format Format) (*Writer, error) {
	var err = format.Validate()
	if err != nil {
		return nil, err
	}

	file, err := os.Create(name) //nolint:gosec // We expect to write to a user-supplied file from the CLI
	if err != nil {
		return nil, fmt.Errorf("wavwrite: couldn't open %s for write: %w", name, err)
	}

	var w = new(Writer)
	w.file = file
	w.buf = bufio.NewWriter(file)
	w.header = newHeader(format)

	err = w.writeHeader(file)
	if err != nil {
		file.Close()

		return nil, fmt.Errorf("wavwrite: couldn't write header to %s: %w", name, err)
	}

	return w, nil
}

func newHeader(format Format) header {
	var h = new(header)

	copy(h.riff[:], "RIFF")
	copy(h.wave[:], "WAVE")
	copy(h.fmt[:], "fmt ")
	copy(h.data[:], "data")

	h.filesize = 0   // Filled in on close.
	h.fmtsize = 16   // Always 16.
	h.wformattag = 1 // 1 for PCM.
	h.nchannels = int16(format.NumChannels)
	h.nsamplespersec = int32(format.SamplesPerSec)
	h.wbitspersample = int16(format.BitsPerSample)
	h.nblockalign = h.wbitspersample / 8 * h.nchannels
	h.navgbytespersec = int32(h.nblockalign) * h.nsamplespersec
	h.datasize = 0 // Filled in on close.

	return *h
}

// Write writes sample data to the file. The caller is responsible for the
// details of mono/stereo and the number of bytes per sample.
func (w *Writer) Write(p []byte) (int, error) {
	if w.buf == nil {
		return 0, ErrClosed
	}

	var n, err = w.buf.Write(p)

	w.byteCount += n

	if err != nil {
		return n, fmt.Errorf("wavwrite: couldn't write audio data: %w", err)
	}

	return n, nil
}

// WriteByte writes a single byte of sample data to the file.
func (w *Writer) WriteByte(c byte) error {
	var _, err = w.Write([]byte{c})

	return err
}

// BytesWritten returns the number of bytes of sample data written so far,
// i.e. not counting the header.
func (w *Writer) BytesWritten() int {
	return w.byteCount
}

// Close flushes any buffered sample data, goes back to the beginning of the
// file to fill in the lengths in the header, and closes the file.
//
// Closing an already-closed Writer returns [ErrClosed].
func (w *Writer) Close() error {
	if w.file == nil {
		return ErrClosed
	}

	var file = w.file
	var buf = w.buf

	w.file = nil
	w.buf = nil

	var err = w.finish(file, buf)
	if err != nil {
		file.Close()

		return err
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("wavwrite: couldn't close audio file: %w", err)
	}

	return nil
}

// finish flushes the buffered sample data and rewrites the header now that the
// lengths are known. It leaves the file open - closing it is Close's job, so
// that there is exactly one close on each path.
func (w *Writer) finish(file *os.File, buf *bufio.Writer) error {
	var err = buf.Flush()
	if err != nil {
		return fmt.Errorf("wavwrite: couldn't flush audio file: %w", err)
	}

	w.header.filesize = int32(w.byteCount + HeaderSize - 8)
	w.header.datasize = int32(w.byteCount)

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("wavwrite: couldn't seek in audio file: %w", err)
	}

	err = w.writeHeader(file)
	if err != nil {
		return fmt.Errorf("wavwrite: couldn't write header to audio file: %w", err)
	}

	return nil
}

// writeHeader writes the header at the current position of f, bypassing the
// buffer - it is only ever written at the start of the file, either before any
// sample data has been buffered or after the buffer has been flushed. The file
// is passed in rather than taken from w so that Close can use it after having
// marked w as closed.
func (w *Writer) writeHeader(f io.Writer) error {
	return binary.Write(f, binary.LittleEndian, w.header) //nolint:wrapcheck // Callers wrap this with more context.
}
