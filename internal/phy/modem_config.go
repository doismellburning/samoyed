// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package phy

// V26 is which of the two incompatible flavours of V.26 QPSK a channel speaks.
type V26 int

const (
	V26Unspecified V26 = iota
	V26A
	V26B
)

const V26Default = V26B

// DTMFDecoding says whether a channel listens for DTMF ("Touch Tone").
type DTMFDecoding int

const (
	DTMFDecodeOff DTMFDecoding = iota
	DTMFDecodeOn
)

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

	DTMFDecode DTMFDecoding

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
