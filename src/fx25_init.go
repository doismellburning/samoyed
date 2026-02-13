package direwolf

// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: The Samoyed Authors

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
)

const EXIT_FAILURE = 1

const FX25_NTAB = 3

var fx25Tab = [FX25_NTAB]struct {
	symsize uint  // Symbol size, bits (1-8).  Always 8 for this application.
	genpoly uint  // Field generator polynomial coefficients.
	fcs     uint  // First root of RS code generator polynomial, index form.
	prim    uint  // Primitive element to generate polynomial roots.
	nroots  uint  // RS code generator polynomial degree (number of roots).
	rs      *rs_t // Pointer to RS codec control block.  Filled in at init time.
}{
	{8, 0x11d, 1, 1, 16, nil}, // RS(255,239)
	{8, 0x11d, 1, 1, 32, nil}, // RS(255,223)
	{8, 0x11d, 1, 1, 64, nil}, // RS(255,191)
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

const CLOSE_ENOUGH = 8 // How many bits can be wrong in tag yet consider it a match?
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

// Given a 64 bit correlation tag value, find acceptable match in table.
// Return index into table or -1 for no match.

func fx25_tag_find_match(t uint64) int {
	for c := CTAG_MIN; c <= CTAG_MAX; c++ {
		if bits.OnesCount64(t^tags[c].value) <= CLOSE_ENOUGH {
			return c
		}
	}
	return -1
}

/*-------------------------------------------------------------
 *
 * Name:	fx25_init
 *
 * Purpose:	This must be called once before any of the other fx25 functions.
 *
 * Inputs:	debug_level - Controls level of informational / debug messages.
 *
 *			0		Only errors.
 *			1 (default)	Transmitting ctag. Currently no other way to know this.
 *			2 		Receive correlation tag detected.  FEC decode complete.
 *			3		Dump data going in and out.
 *
 *			Use command line -dx to increase level or -qx for quiet.
 *
 * Description:	Initialize 3 Reed-Solomon codecs, for 16, 32, and 64 check bytes.
 *
 *--------------------------------------------------------------*/

var g_debug_level int

func fx25_init(debug_level int) {
	g_debug_level = debug_level

	for i := 0; i < FX25_NTAB; i++ {
		fx25Tab[i].rs = init_rs_char(fx25Tab[i].symsize, fx25Tab[i].genpoly, fx25Tab[i].fcs, fx25Tab[i].prim, fx25Tab[i].nroots)
		if fx25Tab[i].rs == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("FX.25 internal error: init_rs_char failed!\n")
			exit(EXIT_FAILURE)
		}
	}

	// Verify integrity of tables and assumptions.
	// This also does a quick check for the popcount function.

	for j := 0; j < 16; j++ {
		for k := 0; k < 16; k++ {
			if j == k {
				Assert(bits.OnesCount64(tags[j].value^tags[k].value) == 0)
			} else {
				Assert(bits.OnesCount64(tags[j].value^tags[k].value) == 32)
			}
		}
	}

	for j := CTAG_MIN; j <= CTAG_MAX; j++ {
		Assert(tags[j].n_block_radio-tags[j].k_data_radio == int(fx25Tab[tags[j].itab].nroots))
		Assert(tags[j].n_block_rs-tags[j].k_data_rs == int(fx25Tab[tags[j].itab].nroots))
		Assert(tags[j].n_block_rs == FX25_BLOCK_SIZE)
	}

	Assert(fx25_pick_mode(100+1, 239) == 1)
	Assert(fx25_pick_mode(100+1, 240) == -1)

	Assert(fx25_pick_mode(100+5, 223) == 5)
	Assert(fx25_pick_mode(100+5, 224) == -1)

	Assert(fx25_pick_mode(100+9, 191) == 9)
	Assert(fx25_pick_mode(100+9, 192) == -1)

	Assert(fx25_pick_mode(16, 32) == 4)
	Assert(fx25_pick_mode(16, 64) == 3)
	Assert(fx25_pick_mode(16, 128) == 2)
	Assert(fx25_pick_mode(16, 239) == 1)
	Assert(fx25_pick_mode(16, 240) == -1)

	Assert(fx25_pick_mode(32, 32) == 8)
	Assert(fx25_pick_mode(32, 64) == 7)
	Assert(fx25_pick_mode(32, 128) == 6)
	Assert(fx25_pick_mode(32, 223) == 5)
	Assert(fx25_pick_mode(32, 234) == -1)

	Assert(fx25_pick_mode(64, 64) == 11)
	Assert(fx25_pick_mode(64, 128) == 10)
	Assert(fx25_pick_mode(64, 191) == 9)
	Assert(fx25_pick_mode(64, 192) == -1)

	Assert(fx25_pick_mode(1, 32) == 4)
	Assert(fx25_pick_mode(1, 33) == 3)
	Assert(fx25_pick_mode(1, 64) == 3)
	Assert(fx25_pick_mode(1, 65) == 6)
	Assert(fx25_pick_mode(1, 128) == 6)
	Assert(fx25_pick_mode(1, 191) == 9)
	Assert(fx25_pick_mode(1, 223) == 5)
	Assert(fx25_pick_mode(1, 239) == 1)
	Assert(fx25_pick_mode(1, 240) == -1)
}

// Get properties of specified CTAG number.

func fx25_get_rs(ctag_num int) *rs_t {
	Assert(ctag_num >= CTAG_MIN && ctag_num <= CTAG_MAX)
	Assert(tags[ctag_num].itab >= 0 && tags[ctag_num].itab < FX25_NTAB)
	Assert(fx25Tab[tags[ctag_num].itab].rs != nil)
	return fx25Tab[tags[ctag_num].itab].rs
}

func fx25_get_ctag_value(ctag_num int) uint64 {
	Assert(ctag_num >= CTAG_MIN && ctag_num <= CTAG_MAX)
	return tags[ctag_num].value
}

func fx25_get_k_data_radio(ctag_num int) int {
	Assert(ctag_num >= CTAG_MIN && ctag_num <= CTAG_MAX)
	return tags[ctag_num].k_data_radio
}

func fx25_get_k_data_rs(ctag_num int) int {
	Assert(ctag_num >= CTAG_MIN && ctag_num <= CTAG_MAX)
	return tags[ctag_num].k_data_rs
}

func fx25_get_nroots(ctag_num int) int {
	Assert(ctag_num >= CTAG_MIN && ctag_num <= CTAG_MAX)
	return int(fx25Tab[tags[ctag_num].itab].nroots)
}

func fx25_get_debug() int {
	return g_debug_level
}

/*-------------------------------------------------------------
 *
 * Name:	fx25_pick_mode
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
 * Returns:	Correlation tag number in range of CTAG_MIN thru CTAG_MAX.
 *		-1 is returned for failure.
 *		The caller should fall back to using plain old AX.25.
 *
 *--------------------------------------------------------------*/

func fx25_pick_mode(fx_mode int, dlen int) int {
	if fx_mode <= 0 {
		return -1
	}

	// Specify a specific tag by adding 100 to the number.
	// Fails if data won't fit.

	if fx_mode-100 >= CTAG_MIN && fx_mode-100 <= CTAG_MAX {
		if dlen <= fx25_get_k_data_radio(fx_mode-100) {
			return fx_mode - 100
		} else {
			return -1 // Assuming caller prints failure message.
		}
	}

	// Specify number of check bytes.
	// Pick the shortest one that can handle the required data length.

	if fx_mode == 16 || fx_mode == 32 || fx_mode == 64 {
		for k := CTAG_MAX; k >= CTAG_MIN; k-- {
			if fx_mode == fx25_get_nroots(k) && dlen <= fx25_get_k_data_radio(k) {
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
	for k := 0; k < 6; k++ {
		var m = prefer[k]
		if dlen <= fx25_get_k_data_radio(m) {
			return m
		}
	}
	return -1

	// TODO: revisit error messages, produced by caller, when this returns -1.
}

/* Initialize a Reed-Solomon codec
 *   symsize = symbol size, bits (1-8) - always 8 for this application.
 *   gfpoly = Field generator polynomial coefficients
 *   fcr = first root of RS code generator polynomial, index form
 *   prim = primitive element to generate polynomial roots
 *   nroots = RS code generator polynomial degree (number of roots)
 */

func init_rs_char(symsize uint, gfpoly uint, fcr uint, prim uint, nroots uint) *rs_t {
	if symsize > 8 {
		return nil // Need version with ints rather than chars
	}

	if fcr >= (1 << symsize) {
		return nil
	}
	if prim == 0 || prim >= (1<<symsize) {
		return nil
	}
	if nroots >= (1 << symsize) {
		return nil // Can't have more roots than symbol values!
	}

	var rs = new(rs_t)

	rs.mm = symsize
	rs.nn = uint((1 << symsize) - 1)

	rs.alpha_to = make([]byte, rs.nn+1)
	rs.index_of = make([]byte, rs.nn+1)

	// Generate Galois field lookup tables
	rs.index_of[0] = byte(rs.nn) // log(zero) = -inf (A0)
	rs.alpha_to[rs.nn] = 0       // alpha**-inf = 0
	var sr = 1
	for i := 0; i < int(rs.nn); i++ {
		rs.index_of[sr] = byte(i)
		rs.alpha_to[i] = byte(sr)
		sr <<= 1
		if sr&(1<<symsize) != 0 {
			sr ^= int(gfpoly)
		}
		sr &= int(rs.nn)
	}
	if sr != 1 {
		// field generator polynomial is not primitive!
		return nil
	}

	// Form RS code generator polynomial from its roots
	rs.genpoly = make([]byte, nroots+1)
	rs.fcr = byte(fcr)
	rs.prim = byte(prim)
	rs.nroots = nroots

	// Find prim-th root of 1, used in decoding
	var iprim = 1
	for (iprim % int(prim)) != 0 {
		iprim += int(rs.nn)
	}
	rs.iprim = byte(iprim / int(prim))

	rs.genpoly[0] = 1
	for i, root := 0, int(fcr)*int(prim); i < int(nroots); i, root = i+1, root+int(prim) {
		rs.genpoly[i+1] = 1

		// Multiply rs->genpoly[] by  @**(root + x)
		for j := i; j > 0; j-- {
			if rs.genpoly[j] != 0 {
				rs.genpoly[j] = rs.genpoly[j-1] ^ rs.alpha_to[modnn(rs, int(rs.index_of[rs.genpoly[j]])+root)]
			} else {
				rs.genpoly[j] = rs.genpoly[j-1]
			}
		}
		// rs->genpoly[0] can never be zero
		rs.genpoly[0] = rs.alpha_to[modnn(rs, int(rs.index_of[rs.genpoly[0]])+root)]
	}
	// convert rs->genpoly[] to index form for quicker encoding
	for i := 0; i <= int(nroots); i++ {
		rs.genpoly[i] = rs.index_of[rs.genpoly[i]]
	}

	return rs
}

// TEMPORARY!!!
// FIXME: We already have multiple copies of this.
// Consolidate them into one somewhere.

func fx_hex_dump(p []byte) {
	var offset = 0
	var length = len(p)

	for length > 0 {
		var n = min(length, 16)

		dw_printf("  %03x: ", offset)
		for i := 0; i < n; i++ {
			dw_printf(" %02x", p[i])
		}
		for i := n; i < 16; i++ {
			dw_printf("   ")
		}
		dw_printf("  ")
		for i := 0; i < n; i++ {
			if p[i] >= 0x20 && p[i] <= 0x7E {
				dw_printf("%c", p[i])
			} else {
				dw_printf(".")
			}
		}
		dw_printf("\n")
		p = p[n:]
		offset += n
		length -= n
	}
}
