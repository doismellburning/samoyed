// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package phy

import (
	"fmt"
	"strconv"
	"strings"
)

// V26 is which of the two incompatible flavours of V.26 QPSK a channel speaks.
type V26 int

const (
	V26Unspecified V26 = iota
	V26A
	V26B
)

const V26Default = V26B

/*
 * Standard for AFSK on VHF FM.
 * Reversing mark and space makes no difference because
 * NRZI encoding only cares about change or lack of change
 * between the two tones.
 *
 * HF SSB uses 300 baud and 200 Hz shift.
 * 1600 & 1800 Hz is a popular tone pair, sometimes
 * called the KAM tones.
 */

const DefaultMarkFreq = 1200
const DefaultSpaceFreq = 2200
const DefaultBaud = 1200

/* Used for sanity checking in config file and command line options. */
/* 9600 baud is known to work.  */
/* TODO: Is 19200 possible with a soundcard at 44100 samples/sec or do we need a higher sample rate? */

const MinBaud = 100

const MaxBaud = 40000 // Anyone want to try 38.4 k baud?

// ModemConfig is how one radio channel's modem is chosen and tuned: what the
// demodulators listen with and the modulator sends with.
type ModemConfig struct {
	ModemType Modem

	/* Usual AFSK. */
	/* Baseband signal. Not used yet. */
	/* Scrambled http://www.amsat.org/amsat/articles/g3ruh/109/fig03.gif */
	/* Might try MFJ-2400 / CCITT v.26 / Bell 201 someday. */
	/* No modem.  Might want this for DTMF only channel. */

	V26Alternative V26

	// Original implementation used alternative A for 2400 bbps PSK.
	// Years later, we discover that MFJ-2400 used alternative B.
	// It's likely the others did too.  it also works a little better.
	// Default to MFJ compatible and print warning if user did not
	// pick one explicitly.

	DTMFDecode bool // Listen for DTMF ("Touch Tone").

	/* Originally the DTMF ("Touch Tone") decoder was always */
	/* enabled because it took a negligible amount of CPU. */
	/* There were complaints about the false positives when */
	/* hearing other modulation schemes on HF SSB so now it */
	/* is enabled only when needed. */

	/* "On" will send special "t" packet to attached applications */
	/* and process as APRStt.  Someday we might want to separate */
	/* these but for now, we have a single off/on. */

	Decimate int /* Reduce AFSK sample rate by this factor to */
	/* decrease computational requirements. */

	Upsample int /* Upsample by this factor for G3RUH. */

	MarkFreq  int /* Two tones for AFSK modulation, in Hz. */
	SpaceFreq int /* Standard tones are 1200 and 2200 for 1200 baud. */

	Baud int /* Data bits per second. */
	/* Standard rates are 1200 for VHF and 300 for HF. */
	/* This should really be called bits per second. */

	/* Next 3 come from config file or command line. */

	Profiles string /* zero or more of ABC etc, optional + */

	NumFreq int /* Number of different frequency pairs for decoders. */

	Offset int /* Spacing between filter frequencies. */

	/* How many demodulators and slicers these come to is the Demodulator's. */
}

// SetBitrate sets up the modem for a -B option or a MODEM line's speed: a bit rate,
// which brings the usual modem for that rate, or AIS or EAS, which name one.
// The modem's tones go with it, and so does the demodulator profile, unless
// the channel was AFSK and stays AFSK.  The modem is left alone if bitrate is no
// good.
//
//	300 implies 1600/1800 AFSK, for HF SSB.
//	1200 implies 1200/2200 AFSK.
//	2400 implies V.26 QPSK.
//	4800 implies V.27 8PSK.
//	9600 and up implies G3RUH baseband scrambled.
func (m *ModemConfig) SetBitrate(bitrate string) error {
	if strings.EqualFold(bitrate, "AIS") {
		m.ModemType = AIS
		m.Baud = 9600
		m.MarkFreq = 0
		m.SpaceFreq = 0
		m.Profiles = ""

		return nil
	}

	if strings.EqualFold(bitrate, "EAS") {
		m.ModemType = EAS
		m.Baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		m.MarkFreq = 2083  // Actually 2083.3 - logic 1.
		m.SpaceFreq = 1563 // Actually 1562.5 - logic 0.
		m.Profiles = "A"

		return nil
	}

	var baud, err = strconv.Atoi(bitrate)
	if err != nil {
		return fmt.Errorf("invalid bitrate (should be an integer or 'AIS' or 'EAS'): %s", bitrate)
	}

	if baud < MinBaud || baud > MaxBaud {
		return fmt.Errorf("use a more reasonable bit rate in range of %d - %d", MinBaud, MaxBaud)
	}

	m.Baud = baud

	// The profile belongs to the modem it was chosen for.  AFSK keeps an
	// AFSK one, and every other modem starts afresh.
	if baud < 1800 && m.ModemType != AFSK {
		m.Profiles = ""
	}

	switch {
	case baud < 600:
		m.ModemType = AFSK
		m.MarkFreq = 1600
		m.SpaceFreq = 1800
	case baud < 1800:
		m.ModemType = AFSK
		m.MarkFreq = DefaultMarkFreq
		m.SpaceFreq = DefaultSpaceFreq
	case baud < 3600:
		m.ModemType = QPSK
		m.MarkFreq = 0
		m.SpaceFreq = 0
		m.Profiles = ""
	case baud < 7200:
		m.ModemType = PSK8
		m.MarkFreq = 0
		m.SpaceFreq = 0
		m.Profiles = ""
	default:
		m.ModemType = Scramble
		m.MarkFreq = 0
		m.SpaceFreq = 0
		m.Profiles = ""
	}

	return nil
}

// Settled is what Settle changed, for the caller to report.
type Settled struct {
	// V26Defaulted is set when no V.26 alternative had been chosen for
	// 2400 bps QPSK, and V26Default was taken.
	V26Defaulted bool

	// DecimationDropped is the decimation asked for on a PSK channel, which
	// has no decimating path, and so was turned off; 0 when there was none.
	DecimationDropped int
}

// Settle applies the rules about which of the modem's options go together, so
// that the configuration is final before any modem - receive or transmit - is
// set up from it, and says what it changed.
func (m *ModemConfig) Settle() Settled {
	var settled Settled

	switch m.ModemType {
	case QPSK:
		// In versions 1.4 and 1.5, V.26 "Alternative A" was used.
		// years later, I discover that the MFJ-2400 used "Alternative B."
		// It looks like the other two manufacturers use the same but we
		// can't be sure until we find one for compatibility testing.
		// In version 1.6 we add a choice for the user.
		// If neither one was explicitly specified, print a message and take
		// a default.  My current thinking is that we default to direwolf <= 1.5
		// compatible for version 1.6 and MFJ compatible after that.
		//
		// The transmitter reads this too, so it has to be settled before
		// either end is set up.
		if m.V26Alternative == V26Unspecified {
			m.V26Alternative = V26Default
			settled.V26Defaulted = true
		}

		settled.DecimationDropped = m.forbidPSKDecimation()

	case PSK8, BPSK:
		settled.DecimationDropped = m.forbidPSKDecimation()

	default:
	}

	return settled
}

// forbidPSKDecimation turns decimation off for a PSK channel, returning the
// decimation it turned off, if any.  PSK is always demodulated at the full
// sample rate; the decimating path was never implemented for it.
func (m *ModemConfig) forbidPSKDecimation() int {
	var dropped = 0
	if m.Decimate > 1 {
		dropped = m.Decimate
	}

	m.Decimate = 1

	return dropped
}
