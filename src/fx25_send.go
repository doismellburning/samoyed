package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include "fx25.h"
// #include "fcs_calc.h"
// #include "textcolor.h"
// #include "audio.h"
// #include "gen_tone.h"
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

var fx25BitsSent [MAX_RADIO_CHANS]C.int // Count number of bits sent by "fx25_send_frame" or "???"

/*-------------------------------------------------------------
 *
 * Name:	fx25_send_frame
 *
 * Purpose:	Convert HDLC frames to a stream of bits.
 *
 * Inputs:	channel	- Audio channel number, 0 = first.
 *
 *		fbuf	- Frame buffer address.
 *
 *		flen	- Frame length, before bit-stuffing, not including the FCS.
 *
 *		fx_mode	- Normally, this would be 16, 32, or 64 for the desired number
 *			  of check bytes.  The shortest format, adequate for the
 *			  required data length will be picked automatically.
 *			  0x01 thru 0x0b may also be specified for a specific format
 *			  but this is expected to be mostly for testing, not normal
 *			  operation.
 *
 * Outputs:	Bits are shipped out by calling tone_gen_put_bit().
 *
 * Returns:	Number of bits sent including "flags" and the
 *		stuffing bits.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *		-1 is returned for failure.
 *
 * Description:	Generate an AX.25 frame in the usual way then wrap
 *		it inside of the FX.25 correlation tag and check bytes.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		tone_gen_put_bit() are processed correctly.
 *
 * Errors:	If something goes wrong, return -1 and the caller should
 *		fallback to sending normal AX.25.
 *
 *		This could happen if the frame is too large.
 *
 *--------------------------------------------------------------*/

func fx25_send_frame(channel C.int, _fbuf *C.uchar, flen C.int, fx_mode C.int, test_mode bool) C.int {
	if C.fx25_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		dw_printf("FX.25[%d] send frame: FX.25 mode = %d\n", channel, fx_mode)
		C.fx_hex_dump(_fbuf, flen)
	}

	fx25BitsSent[channel] = 0

	var fbuf = C.GoBytes(unsafe.Pointer(_fbuf), flen)

	// Append the FCS.

	var fcs = C.fcs_calc(_fbuf, flen)
	fbuf = append(fbuf, byte(fcs)&0xff)
	flen++
	fbuf = append(fbuf, byte(fcs>>8)&0xff)
	flen++

	// Add bit-stuffing.

	const fence C.uchar = 0xaa
	/* FIXME KG?
	data[FX25_MAX_DATA] = fence
	*/

	var stuffedBytes = bitStuff(fbuf)
	var dlen = C.int(len(stuffedBytes))

	// FIXME KG Assert(data[FX25_MAX_DATA] == fence)
	/* FIXME KG
	if dlen < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("FX.25[%d]: Frame length of %d + overhead is too large to encode.\n", channel, flen)
		return (-1)
	}
	*/

	// Pick suitable correlation tag depending on
	// user's preference, for number of check bytes,
	// and the data size.

	var ctag_num = C.fx25_pick_mode(fx_mode, dlen)

	if ctag_num < C.CTAG_MIN || ctag_num > C.CTAG_MAX {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("FX.25[%d]: Could not find suitable format for requested %d and data length %d.\n", channel, fx_mode, dlen)
		return (-1)
	}

	var ctag_value = C.fx25_get_ctag_value(ctag_num)

	// Zero out part of data which won't be transmitted.
	// It should all be filled by extra HDLC "flag" patterns.

	var k_data_radio = C.fx25_get_k_data_radio(ctag_num)
	var k_data_rs = C.fx25_get_k_data_rs(ctag_num)
	var shorten_by = FX25_MAX_DATA - k_data_radio
	if shorten_by > 0 {
		// FIXME KG memset(data+k_data_radio, 0, shorten_by)
	}

	// Compute the check bytes.

	var check [FX25_MAX_CHECK + 1]C.uchar
	check[FX25_MAX_CHECK] = fence
	var rs = C.fx25_get_rs(ctag_num)

	Assert(k_data_rs+C.int(rs.nroots) == C.int(rs.nn))

	var data = (*C.uchar)(C.CBytes(stuffedBytes))
	C.ENCODE_RS(rs, data, &check[0])
	Assert(check[FX25_MAX_CHECK] == fence)

	if C.fx25_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("FX.25[%d]: transmit %d data bytes, ctag number 0x%02x\n", channel, k_data_radio, ctag_num)
		C.fx_hex_dump(data, k_data_radio)
		dw_printf("FX.25[%d]: transmit %d check bytes:\n", channel, rs.nroots)
		C.fx_hex_dump(&check[0], C.int(rs.nroots))
		dw_printf("------\n")
	}

	if test_mode {
		// Standalone text application.

		var flags = []byte{0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e, 0x7e}
		var fname = fmt.Sprintf("fx%02x.dat", ctag_num)
		var fp, err = os.Create(fname)
		if err != nil {
			panic(err)
		}
		defer fp.Close()
		fp.Write(flags)
		//fwrite ((unsigned char *)(&ctag_value), sizeof(ctag_value), 1, fp);	// No - assumes little endian.
		for k := 0; k < 8; k++ {
			var b = byte(ctag_value>>(k*8)) & 0xff // Should be portable to big endian too.
			fp.Write([]byte{b})
		}

		for j := 8; j < 16; j++ { // Introduce errors.
			stuffedBytes[j] = ^stuffedBytes[j]
		}

		fp.Write(stuffedBytes)
		fp.Write(C.GoBytes(unsafe.Pointer(&check[0]), C.int(rs.nroots)))
		fp.Write(flags)
	} else {
		// Normal usage.  Send bits to modulator.

		// Temp hack for testing.  Corrupt first 8 bytes.
		//	for (int j = 0; j < 16; j++) {
		//	  data[j] = ~ data[j];
		//	}

		for k := 0; k < 8; k++ {
			var b = C.uchar(ctag_value>>(k*8)) & 0xff
			send_bytes(channel, &b, 1)
		}
		send_bytes(channel, data, k_data_radio)
		send_bytes(channel, &check[0], C.int(rs.nroots))
	}

	return (fx25BitsSent[channel])
}

func send_bytes(channel C.int, _b *C.uchar, count C.int) {
	var b = C.GoBytes(unsafe.Pointer(_b), count)
	for j := C.int(0); j < count; j++ {
		var x = b[j]
		for k := 0; k < 8; k++ {
			send_bit(channel, C.int(x&0x01))
			x >>= 1
		}
	}
}

/*
 * NRZI encoding.
 * data 1 bit -> no channel.
 * data 0 bit -> invert signal.
 */
var sendBitOutput [MAX_RADIO_CHANS]C.int

func send_bit(channel C.int, b C.int) {
	if b == 0 {
		sendBitOutput[channel] = 1 - sendBitOutput[channel]
	}
	C.tone_gen_put_bit(channel, sendBitOutput[channel])
	fx25BitsSent[channel]++
}

/*-------------------------------------------------------------
 *
 * Name:	stuff_it FIXME KG
 *
 * Purpose:	Perform HDLC bit-stuffing and add "flag" octets in
 *		preparation for the RS encoding.
 *
 * Inputs:	in	- Frame, including FCS, in.
 *
 *		ilen	- Number of bytes in.
 *
 *		osize	- Size of out area.
 *
 * Outputs:	out	- Location to receive result.
 *
 * Returns:	Number of bytes needed in output area including one trailing flag.
 *		-1 if it won't fit.
 *
 * Description:	Convert to stream of bits including:
 *			start flag
 *			bit stuffed data, including FCS
 *			end flag
 *
 *--------------------------------------------------------------*/

// Is it particularly time/space efficient? No.
// But it should work!
func bitStuff(in []byte) []byte {
	const flag byte = 0x7e

	var outBits []bool

	// Start flag

	for i := range 8 {
		var v = flag&(1<<i) > 0
		outBits = append(outBits, v)
	}

	// In data

	var ones = 0
	for _, b := range in {
		for i := range 8 {
			var v = b&(1<<i) > 0
			outBits = append(outBits, v)

			if v {
				ones++
				if ones == 5 {
					outBits = append(outBits, false)
					ones = 0
				}
			} else {
				ones = 0
			}
		}
	}

	// End flag

	for i := range 8 {
		var v = flag&(1<<i) > 0
		outBits = append(outBits, v)
	}

	Assert(len(outBits) >= 16) // Start and end flags
	Assert(len(outBits) >= 16+8*len(in))

	// Now byte it up

	var outBytes []byte

	for len(outBits) >= 8 {
		var b byte
		for bitIdx := range 8 {
			b <<= 1
			if outBits[bitIdx] {
				b |= 1
			}
		}

		outBytes = append(outBytes, b)
		outBits = outBits[8:]
	}

	// And the last 0-7 bits if present, left-aligned

	if len(outBits) > 0 {
		var b byte
		for range 8 {
			b <<= 1
			if len(outBits) > 0 {
				if outBits[0] {
					b |= 1
				}
				outBits = outBits[1:]
			}
		}
		outBytes = append(outBytes, b)
	}

	return outBytes
}
