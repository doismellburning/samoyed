// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sdr

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"time"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// rtl_tcp's commands: one byte saying which, then a 32 bit big-endian
// parameter.  See rtl_tcp.c in https://github.com/osmocom/rtl-sdr.
const (
	rtlTCPSetFrequency      = 0x01
	rtlTCPSetSampleRate     = 0x02
	rtlTCPSetGainMode       = 0x03
	rtlTCPSetGain           = 0x04
	rtlTCPSetFreqCorrection = 0x05
)

// rtlTCPMagic starts the header rtl_tcp sends when a client connects.
const rtlTCPMagic = "RTL0"

// rtlTCPHeaderTimeout is how long to wait for that header: rtl_tcp sends it as
// soon as it accepts, so a quiet server isn't rtl_tcp.
const rtlTCPHeaderTimeout = 10 * time.Second

// maxGain is the most tuner gain, in dB, to ask rtl_tcp for: more than any
// tuner has.
const maxGain = 100

// maxPPM is the biggest frequency correction, either way, in parts per
// million.
const maxPPM = 1000

// ErrNotRTLTCP is what DialRTLTCP reports when the far end doesn't start with
// rtl_tcp's header.
var ErrNotRTLTCP = errors.New("not an rtl_tcp server")

// RTLTCP is a connection to an rtl_tcp server.  Reading it gives the dongle's
// samples: unsigned 8 bit I and Q, interleaved.
type RTLTCP struct {
	conn net.Conn

	// TunerType and GainCount are what the server said of its dongle.
	TunerType uint32
	GainCount uint32
}

// DialRTLTCP connects to the rtl_tcp server at address, "host:port", and reads
// its header.
func DialRTLTCP(ctx context.Context, address string) (*RTLTCP, error) {
	var dialer = new(net.Dialer)

	var conn, err = dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connecting to rtl_tcp at %s: %w", address, err)
	}

	// rtl_tcp sends its header straight away, but don't hold up a shutdown
	// waiting for one that doesn't come.
	var release = dwutil.CloseOnDone(ctx, conn)

	var r, headerErr = newRTLTCP(conn)

	release()

	if headerErr == nil && ctx.Err() != nil {
		headerErr = ctx.Err()
	}

	if headerErr != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("rtl_tcp at %s: %w", address, headerErr)
	}

	return r, nil
}

// newRTLTCP reads rtl_tcp's header from conn.
func newRTLTCP(conn net.Conn) (*RTLTCP, error) {
	var header [12]byte

	_ = conn.SetReadDeadline(time.Now().Add(rtlTCPHeaderTimeout))

	var _, err = io.ReadFull(conn, header[:])
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	_ = conn.SetReadDeadline(time.Time{})

	if string(header[:4]) != rtlTCPMagic {
		return nil, fmt.Errorf("%w: header starts %q", ErrNotRTLTCP, header[:4])
	}

	var r = new(RTLTCP)
	r.conn = conn
	r.TunerType = binary.BigEndian.Uint32(header[4:8])
	r.GainCount = binary.BigEndian.Uint32(header[8:12])

	return r, nil
}

// command sends rtl_tcp one command.  rtl_tcp reads the parameter as unsigned,
// but a frequency correction is a signed number of parts per million, sent as
// its two's complement - which encoding an int32 gives.
func (r *RTLTCP) command(cmd byte, param int32) error {
	var buf, err = binary.Append([]byte{cmd}, binary.BigEndian, param)
	if err != nil {
		return fmt.Errorf("encoding rtl_tcp command %#x: %w", cmd, err)
	}

	_, err = r.conn.Write(buf)
	if err != nil {
		return fmt.Errorf("sending rtl_tcp command %#x: %w", cmd, err)
	}

	return nil
}

// commandInt sends rtl_tcp a command whose parameter is n, which has to fit.
func (r *RTLTCP) commandInt(cmd byte, n int) error {
	var param, err = safecast.Convert[int32](n)
	if err != nil {
		return fmt.Errorf("rtl_tcp command %#x: %w", cmd, err)
	}

	return r.command(cmd, param)
}

// SetFrequency tunes the dongle to hz.
func (r *RTLTCP) SetFrequency(hz int) error {
	return r.commandInt(rtlTCPSetFrequency, hz)
}

// SetSampleRate sets how many IQ samples a second the dongle sends.
func (r *RTLTCP) SetSampleRate(hz int) error {
	return r.commandInt(rtlTCPSetSampleRate, hz)
}

// SetGain sets the tuner gain in dB, or leaves it to the tuner's automatic
// gain control when there is none.
func (r *RTLTCP) SetGain(dB maybe.Maybe[float64]) error {
	var gain, manual = dB.Get()
	if !manual {
		return r.command(rtlTCPSetGainMode, 0)
	}

	var err = CheckGain(gain)
	if err != nil {
		return err
	}

	err = r.command(rtlTCPSetGainMode, 1)
	if err != nil {
		return err
	}

	// rtl_tcp takes tenths of a dB, and the tuner picks its nearest step.
	return r.commandInt(rtlTCPSetGain, int(math.Round(gain*10)))
}

// CheckGain reports a tuner gain, in dB, that rtl_tcp can't be asked for.
func CheckGain(dB float64) error {
	if !(dB >= 0 && dB <= maxGain) {
		return fmt.Errorf("gain %g dB is out of range 0 to %d", dB, maxGain)
	}

	return nil
}

// SetFrequencyCorrection corrects the dongle's oscillator by ppm parts per
// million.
func (r *RTLTCP) SetFrequencyCorrection(ppm int) error {
	var err = CheckFrequencyCorrection(ppm)
	if err != nil {
		return err
	}

	return r.commandInt(rtlTCPSetFreqCorrection, ppm)
}

// CheckFrequencyCorrection reports a frequency correction, in parts per
// million, too big to be a dongle's oscillator error.
func CheckFrequencyCorrection(ppm int) error {
	if ppm < -maxPPM || ppm > maxPPM {
		return fmt.Errorf("frequency correction %d ppm is out of range %d to %d", ppm, -maxPPM, maxPPM)
	}

	return nil
}

// Read reads IQ samples.
func (r *RTLTCP) Read(p []byte) (int, error) {
	return r.conn.Read(p) //nolint:wrapcheck // The caller wants to see io.EOF as it is.
}

// Close closes the connection.
func (r *RTLTCP) Close() error {
	return r.conn.Close() //nolint:wrapcheck // Nothing to add.
}
