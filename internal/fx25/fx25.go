// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fx25 is FX.25 - forward error correction wrapped around an
// unmodified AX.25 frame: the correlation tags that mark the start of a
// codeblock, the Reed-Solomon codec each tag calls for, and EncodeFrame,
// which turns a frame into a codeblock.  Putting the bits on the air is left
// to the caller, which is the HDLC transmit path in internal/direwolf.
//
// Reference: http://www.stensat.org/docs/FX-25_01_06.pdf
//
//nolint:gochecknoglobals
package fx25

// -----------------------------------------------------------------------
//
//
// Some of this is based on:
//
// FX.25 Encoder
//	Author: Jim McGuire KB3MPL
//	Date: 	23 October 2007
//
// This program is a single-file implementation of the FX.25 encapsulation
// structure for use with AX.25 data packets.  Details of the FX.25
// specification are available at:
//     http://www.stensat.org/Docs/Docs.htm
//
// This program implements a single RS(255,239) FEC structure.  Future
// releases will incorporate more capabilities as accommodated in the FX.25
// spec.
//
// The Reed Solomon encoding routines are based on work performed by
// Phil Karn.  Phil was kind enough to release his code under the GPL, as
// noted below.  Consequently, this FX.25 implementation is also released
// under the terms of the GPL.
//
// Phil Karn's original copyright notice:
/* Test the Reed-Solomon codecs
 * for various block sizes and with random data and random error patterns
 *
 * Copyright 2002 Phil Karn, KA9Q
 * May be used under the terms of the GNU General Public License (GPL)
 *
 */

import (
	"math/bits"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/reedsolomon"
	"github.com/sirupsen/logrus"
)

const CTagMin = 0x01
const CTagMax = 0x0B

// Maximum sizes of "data" and "check" parts.

const MaxData = 239   // i.e. RS(255,239)
const maxCheck = 64   // e.g. RS(255, 191)
const BlockSize = 255 // Block size always 255 for 8 bit symbols.

const nTab = 3

// fx25TabEntry is one of the Reed-Solomon codes FX.25 uses, and its codec.
type fx25TabEntry struct {
	symsize uint               // Symbol size, bits (1-8).  Always 8 for this application.
	genpoly uint               // Field generator polynomial coefficients.
	fcs     uint               // First root of RS code generator polynomial, index form.
	prim    uint               // Primitive element to generate polynomial roots.
	nroots  uint               // RS code generator polynomial degree (number of roots).
	rs      *reedsolomon.Codec // RS codec control block.
}

// fx25Tab is built once, when the package is initialised, and only read after
// that, so every FX.25 sender and receiver can share it without a lock.
var fx25Tab = [nTab]fx25TabEntry{
	newFX25TabEntry(8, 0x11d, 1, 1, 16), // RS(255,239)
	newFX25TabEntry(8, 0x11d, 1, 1, 32), // RS(255,223)
	newFX25TabEntry(8, 0x11d, 1, 1, 64), // RS(255,191)
}

// newFX25TabEntry sets up the codec for one of FX.25's Reed-Solomon codes.
// The parameters are all constants, so a failure is a bug, not something a
// user can do anything about.
func newFX25TabEntry(symsize uint, genpoly uint, fcs uint, prim uint, nroots uint) fx25TabEntry {
	var rs, err = reedsolomon.New(symsize, genpoly, fcs, prim, nroots)
	if err != nil {
		logrus.WithError(err).Fatal("FX.25 internal error: Could not set up Reed-Solomon codec")
	}

	return fx25TabEntry{symsize: symsize, genpoly: genpoly, fcs: fcs, prim: prim, nroots: nroots, rs: rs}
}

/*
 * Reference:	http://www.stensat.org/docs/FX-25_01_06.pdf
 *				FX.25
 *		Forward Error Correction Extension to
 *		AX.25 Link Protocol For Amateur Packet Radio
 *		Version: 0.01 DRAFT
 *		Date: 01 September 2006
 */

type correlation_tag_s struct {
	value         uint64 // 64 bit value, send LSB first.
	n_block_radio int    // Size of transmitted block, all in bytes.
	k_data_radio  int    // Size of transmitted data part.
	n_block_rs    int    // Size of RS algorithm block.
	k_data_rs     int    // Size of RS algorithm data part.
	itab          int    // Index into Tab array.
}

var tags = [16]correlation_tag_s{
	/* Tag_00 */ {0x566ED2717946107E, 0, 0, 0, 0, -1}, //  Reserved

	/* Tag_01 */ {0xB74DB7DF8A532F3E, 255, 239, 255, 239, 0}, //  RS(255, 239) 16-byte check value, 239 information bytes
	/* Tag_02 */ {0x26FF60A600CC8FDE, 144, 128, 255, 239, 0}, //  RS(144,128) - shortened RS(255, 239), 128 info bytes
	/* Tag_03 */ {0xC7DC0508F3D9B09E, 80, 64, 255, 239, 0}, //  RS(80,64) - shortened RS(255, 239), 64 info bytes
	/* Tag_04 */ {0x8F056EB4369660EE, 48, 32, 255, 239, 0}, //  RS(48,32) - shortened RS(255, 239), 32 info bytes

	/* Tag_05 */ {0x6E260B1AC5835FAE, 255, 223, 255, 223, 1}, //  RS(255, 223) 32-byte check value, 223 information bytes
	/* Tag_06 */ {0xFF94DC634F1CFF4E, 160, 128, 255, 223, 1}, //  RS(160,128) - shortened RS(255, 223), 128 info bytes
	/* Tag_07 */ {0x1EB7B9CDBC09C00E, 96, 64, 255, 223, 1}, //  RS(96,64) - shortened RS(255, 223), 64 info bytes
	/* Tag_08 */ {0xDBF869BD2DBB1776, 64, 32, 255, 223, 1}, //  RS(64,32) - shortened RS(255, 223), 32 info bytes

	/* Tag_09 */ {0x3ADB0C13DEAE2836, 255, 191, 255, 191, 2}, //  RS(255, 191) 64-byte check value, 191 information bytes
	/* Tag_0A */ {0xAB69DB6A543188D6, 192, 128, 255, 191, 2}, //  RS(192, 128) - shortened RS(255, 191), 128 info bytes
	/* Tag_0B */ {0x4A4ABEC4A724B796, 128, 64, 255, 191, 2}, //  RS(128, 64) - shortened RS(255, 191), 64 info bytes

	/* Tag_0C */ {0x0293D578626B67E6, 0, 0, 0, 0, -1}, //  Undefined
	/* Tag_0D */ {0xE3B0B0D6917E58A6, 0, 0, 0, 0, -1}, //  Undefined
	/* Tag_0E */ {0x720267AF1BE1F846, 0, 0, 0, 0, -1}, //  Undefined
	/* Tag_0F */ {0x93210201E8F4C706, 0, 0, 0, 0, -1}, //  Undefined
}

const closeEnough = 8 // How many bits can be wrong in tag yet consider it a match?
// Needs to be large enough to match with significant errors
// but not so large to get frequent false matches.
// Probably don't want >= 16 because the hamming distance between
// any two pairs is 32.
// What is a good number?  8??  12??  15??
// 12 got many false matches with random noise.
// Even 8 might be too high.  We see 2 or 4 bit errors here
// at the point where decoding the block is very improbable.
// After 2 months of continuous operation as a digipeater/iGate,
// no false triggers were observed.  So 8 doesn't seem to be too
// high for 1200 bps.  No study has been done for 9600 bps.

// FindTag finds an acceptable match in the table for a 64 bit correlation
// tag value, allowing for up to closeEnough bits in error.
// Return index into table or -1 for no match.
func FindTag(t uint64) int {
	for c := CTagMin; c <= CTagMax; c++ {
		if bits.OnesCount64(t^tags[c].value) <= closeEnough {
			return c
		}
	}

	return -1
}

// FX.25's debug level, from the -dx and -qx options, is handed to each
// sender and receiver when it is made:
//
//	0		Only errors.
//	1 (default)	Transmitting ctag. Currently no other way to know this.
//	2		Receive correlation tag detected.  FEC decode complete.
//	3		Dump data going in and out.

// Get properties of specified CTAG number.

// Codec is the Reed-Solomon codec for a correlation tag.
func Codec(ctag_num int) *reedsolomon.Codec {
	dwutil.Assert(ctag_num >= CTagMin && ctag_num <= CTagMax)
	dwutil.Assert(tags[ctag_num].itab >= 0 && tags[ctag_num].itab < nTab)
	dwutil.Assert(fx25Tab[tags[ctag_num].itab].rs != nil)

	return fx25Tab[tags[ctag_num].itab].rs
}

// TagValue is the 64 bit correlation tag itself, sent LSB first.
func TagValue(ctag_num int) uint64 {
	dwutil.Assert(ctag_num >= CTagMin && ctag_num <= CTagMax)

	return tags[ctag_num].value
}

// KDataRadio is the number of data bytes transmitted in a codeblock.
func KDataRadio(ctag_num int) int {
	dwutil.Assert(ctag_num >= CTagMin && ctag_num <= CTagMax)

	return tags[ctag_num].k_data_radio
}

// KDataRS is the number of data bytes in the Reed-Solomon block, before
// shortening to KDataRadio.
func KDataRS(ctag_num int) int {
	dwutil.Assert(ctag_num >= CTagMin && ctag_num <= CTagMax)

	return tags[ctag_num].k_data_rs
}

// NRoots is the number of check bytes in a codeblock.
func NRoots(ctag_num int) int {
	dwutil.Assert(ctag_num >= CTagMin && ctag_num <= CTagMax)

	return int(fx25Tab[tags[ctag_num].itab].nroots)
}

/*-------------------------------------------------------------
 *
 * Name:	pickMode
 *
 * Purpose:	Pick suitable transmission format based on user preference
 *		and size of data part required.
 *
 * Inputs:	fx_mode	- 0 = none.
 *			1 = pick a tag automatically.
 *			16, 32, 64 = use this many check bytes.
 *			100 + n = use tag n.
 *
 *			0 and 1 would be the most common.
 *			Others are mostly for testing.
 *
 *		dlen - 	Required size for transmitted "data" part, in bytes.
 *			This includes the AX.25 frame with bit stuffing and a flag
 *			pattern on each end.
 *
 * Returns:	Correlation tag number in range of CTagMin thru CTagMax.
 *		-1 is returned for failure.
 *		The caller should fall back to using plain old AX.25.
 *
 *--------------------------------------------------------------*/

func pickMode(fx_mode int, dlen int) int {
	if fx_mode <= 0 {
		return -1
	}

	// Specify a specific tag by adding 100 to the number.
	// Fails if data won't fit.

	if fx_mode-100 >= CTagMin && fx_mode-100 <= CTagMax {
		if dlen <= KDataRadio(fx_mode-100) {
			return fx_mode - 100
		} else {
			return -1 // Assuming caller prints failure message.
		}
	}

	// Specify number of check bytes.
	// Pick the shortest one that can handle the required data length.

	if fx_mode == 16 || fx_mode == 32 || fx_mode == 64 {
		for k := CTagMax; k >= CTagMin; k-- {
			if fx_mode == NRoots(k) && dlen <= KDataRadio(k) {
				return k
			}
		}

		return -1
	}

	// For any other number, [[ or if the preference was not possible, ?? ]]
	// try to come up with something reasonable.  For shorter frames,
	// use smaller overhead.  For longer frames, where an error is
	// more probable, use more check bytes.  When the data gets even
	// larger, check bytes must be reduced to fit in block size.
	// When all else fails, fall back to normal AX.25.
	// Some of this is from observing UZ7HO Soundmodem behavior.
	//
	//	Tag 	Data 	Check 	Max Num
	//	Number	Bytes	Bytes	Repaired
	//	------	-----	-----	-----
	//	0x04	32	16	8
	//	0x03	64	16	8
	//	0x06	128	32	16
	//	0x09	191	64	32
	//	0x05	223	32	16
	//	0x01	239	16	8
	//	none	larger
	//
	// The PRUG FX.25 TNC has additional modes that will handle larger frames
	// by using multiple RS blocks.  This is a future possibility but needs
	// to be coordinated with other FX.25 developers so we maintain compatibility.
	// See https://web.tapr.org/meetings/DCC_2020/JE1WAZ/DCC-2020-PRUG-FINAL.pptx

	var prefer = [6]int{0x04, 0x03, 0x06, 0x09, 0x05, 0x01}
	for k := range 6 {
		var m = prefer[k]
		if dlen <= KDataRadio(m) {
			return m
		}
	}

	return -1

	// TODO: revisit error messages, produced by caller, when this returns -1.
}
