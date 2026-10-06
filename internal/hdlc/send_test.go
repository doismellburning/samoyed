// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hdlc

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The HDLC flag, and the byte a test uses when it wants bit stuffing: six
// consecutive ones, one more than a run may be.
const (
	hdlcFlag     byte = 0x7e
	hdlcSixtyOne byte = 0x3f
)

// captureHDLCBits collects the line levels a new Sender sends while fn
// runs.  The line starts low.
func captureHDLCBits(fn func(s *Sender)) []int {
	return testutils.LineLevels(func(line *linecode.Encoder) { fn(NewSender(line, 0)) })
}

// A frame goes out between flags, with its FCS appended and any run of more
// than five ones broken up.
func TestAX25FrameIsSentBetweenFlagsWithItsFCS(t *testing.T) {
	// The 0x3f bytes are six consecutive ones each, so the frame cannot go
	// out without stuffing.
	var fbuf = []byte{hdlcSixtyOne, 'Q', '1', 'T', 'E', 'S', 'T', hdlcSixtyOne}

	var sent int

	var bits = captureHDLCBits(func(s *Sender) {
		sent = s.SendFrame(fbuf, false)
	})

	assert.Equal(t, len(bits), sent, "the count returned should be the bits actually sent")

	var frameFCS = fcs.Calc(fbuf)
	var expected = append(append([]byte{}, fbuf...), byte(frameFCS)&0xff, byte(frameFCS>>8)&0xff) //nolint:gosec // G115: unchecked narrowing conversion, see #294

	assert.Equal(t, expected, testutils.HDLCFrameFromLevels(t, bits),
		"the frame between the flags should be what was asked for, plus its FCS")

	// Stuffing is what makes the frame longer than the bytes it carries.
	assert.Greater(t, len(bits), (len(expected)+2)*8, "the long runs of ones should have been broken up")
}

// The bad FCS option is for making a frame that a receiver has to reject.
func TestAX25BadFCSSendsTheComplementOfTheRealOne(t *testing.T) {
	var fbuf = []byte{'Q', '1', 'T', 'E', 'S', 'T'}

	var good = captureHDLCBits(func(s *Sender) {
		s.SendFrame(fbuf, false)
	})
	var bad = captureHDLCBits(func(s *Sender) {
		s.SendFrame(fbuf, true)
	})

	var goodData = testutils.HDLCFrameFromLevels(t, good)
	var badData = testutils.HDLCFrameFromLevels(t, bad)

	require.Len(t, badData, len(goodData), "only the FCS should differ")

	var frameFCS = fcs.Calc(fbuf)

	// The FCS is the last thing in the frame.
	assert.Equal(t, []byte{byte(frameFCS) & 0xff, byte(frameFCS>>8) & 0xff}, goodData[len(goodData)-2:])   //nolint:gosec // G115: unchecked narrowing conversion, see #294
	assert.Equal(t, []byte{byte(^frameFCS) & 0xff, byte((^frameFCS)>>8) & 0xff}, badData[len(badData)-2:]) //nolint:gosec // G115: unchecked narrowing conversion, see #294
	assert.Equal(t, fbuf, badData[:len(badData)-2], "the frame itself should be unchanged")
}

// A data byte gets a zero after five consecutive ones; a flag, which has six
// of them, must not, or it would no longer be a flag.
func TestOnlyDataIsBitStuffed(t *testing.T) {
	var asData = captureHDLCBits(func(s *Sender) {
		s.sendDataNRZI(hdlcFlag)
	})
	var asControl = captureHDLCBits(func(s *Sender) {
		s.sendControlNRZI(hdlcFlag)
	})

	assert.Len(t, asControl, 8, "a flag is sent as it stands")
	assert.Equal(t, []byte{hdlcFlag}, testutils.PackLSBFirst(t, testutils.NRZIDecode(asControl)))

	assert.Len(t, asData, 9, "the same byte as data needs a stuffed bit")
	assert.Equal(t, []byte{hdlcFlag}, testutils.PackLSBFirst(t, testutils.Destuff(testutils.NRZIDecode(asData))))

	// However long the run, a control byte is sent as it stands.
	var allOnes = captureHDLCBits(func(s *Sender) {
		s.sendControlNRZI(0xff)
	})

	assert.Len(t, allOnes, 8)
	assert.Equal(t, []byte{0xff}, testutils.PackLSBFirst(t, testutils.NRZIDecode(allOnes)))
}

// A run of ones long enough to need stuffing twice gets a zero each time.
func TestBitStuffingRepeatsForALongRunOfOnes(t *testing.T) {
	var bits = captureHDLCBits(func(s *Sender) {
		s.sendDataNRZI(0xff)
		s.sendDataNRZI(0xff)
	})

	assert.Len(t, bits, 16+3, "sixteen ones need three stuffed zeros")
	assert.Equal(t, []byte{0xff, 0xff}, testutils.PackLSBFirst(t, testutils.Destuff(testutils.NRZIDecode(bits))))
}

// NRZI: a one leaves the signal alone, a zero inverts it.
func TestNRZIInvertsOnAZeroOnly(t *testing.T) {
	var bits = captureHDLCBits(func(s *Sender) {
		s.sendBitNRZI(true)
		s.sendBitNRZI(true)
		s.sendBitNRZI(false)
		s.sendBitNRZI(true)
		s.sendBitNRZI(false)
	})

	assert.Equal(t, []int{0, 0, 1, 1, 0}, bits)
}
