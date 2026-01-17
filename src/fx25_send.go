package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include "fx25.h"
// #include "fcs_calc.h"
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
		fx_hex_dump(_fbuf, flen)
	}

	fx25BitsSent[channel] = 0

	var fbuf = C.GoBytes(unsafe.Pointer(_fbuf), flen)

	// Append the FCS.

	var fcs = C.fcs_calc(_fbuf, flen)
	fbuf = append(fbuf, byte(fcs)&0xff)
	fbuf = append(fbuf, byte(fcs>>8)&0xff)

	// Add bit-stuffing, filling to FX25_MAX_DATA bytes with flag patterns
	var stuffedBytes, meaningfulLen = bitStuff(fbuf, FX25_MAX_DATA)
	var dlen = C.int(meaningfulLen) // Use meaningful length, not total buffer size

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
	var k_data_radio = C.fx25_get_k_data_radio(ctag_num)
	var k_data_rs = C.fx25_get_k_data_rs(ctag_num)

	// Zero out part of data which won't be transmitted
	var shorten_by = FX25_MAX_DATA - k_data_radio
	if shorten_by > 0 {
		for i := k_data_radio; i < FX25_MAX_DATA; i++ {
			stuffedBytes[i] = 0
		}
	}

	var data = (*C.uchar)(C.CBytes(stuffedBytes))

	// Compute the check bytes.

	const fence C.uchar = 0xaa
	var check [FX25_MAX_CHECK + 1]C.uchar
	check[FX25_MAX_CHECK] = fence
	var rs = C.fx25_get_rs(ctag_num)

	Assert(k_data_rs+C.int(rs.nroots) == C.int(rs.nn))

	C.ENCODE_RS(rs, data, &check[0])
	Assert(check[FX25_MAX_CHECK] == fence)

	if C.fx25_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("FX.25[%d]: transmit %d data bytes, ctag number 0x%02x\n", channel, k_data_radio, ctag_num)
		fx_hex_dump(data, k_data_radio)
		dw_printf("FX.25[%d]: transmit %d check bytes:\n", channel, rs.nroots)
		fx_hex_dump(&check[0], C.int(rs.nroots))
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
		for k := 0; k < 8; k++ {
			var b = byte(ctag_value>>(k*8)) & 0xff // Should be portable to big endian too.
			fp.Write([]byte{b})
		}

		for j := 8; j < 16; j++ {
			*(*C.uchar)(unsafe.Pointer(uintptr(unsafe.Pointer(data)) + uintptr(j))) ^= 0xff
		}

		fp.Write(C.GoBytes(unsafe.Pointer(data), k_data_radio))
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
 * data 1 bit -> no change.
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
 * Name:	bitStuff
 *
 * Purpose:	Perform HDLC bit-stuffing and add "flag" octets in
 *		preparation for the RS encoding.
 *
 * Inputs:	in	- Frame, including FCS, in.
 *
 *		maxBytes - if >0, fill output to exactly this many bytes with flag patterns
 *
 * Returns:	Stuffed bytes, and meaningful length before flag padding
 *
 * Description:	Convert to stream of bits including:
 *			start flag
 *			bit stuffed data, including FCS
 *			end flag
 *
 *--------------------------------------------------------------*/

// Is it particularly time/space efficient? No.
// But it should work!
func bitStuff(in []byte, maxBytes int) ([]byte, int) {
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

	// Remember where meaningful data ends (before flag padding)
	meaningfulBits := len(outBits)

	// Fill remainder with flag patterns (rotating through flag bits)
	if maxBytes > 0 {
		maxBits := maxBytes * 8
		bitPos := 0 // Which bit position of flag to use (0-7)
		for len(outBits) < maxBits {
			v := flag&(1<<bitPos) > 0
			outBits = append(outBits, v)
			bitPos = (bitPos + 1) % 8
		}
	}

	// Now byte it up

	var outBytes []byte

	for len(outBits) >= 8 {
		var b byte
		for bitIdx := range 8 {
			if outBits[bitIdx] {
				b |= 1 << bitIdx
			}
		}

		outBytes = append(outBytes, b)
		outBits = outBits[8:]
	}

	// And the last 0-7 bits if present

	if len(outBits) > 0 {
		var b byte
		for bitIdx := 0; bitIdx < 8 && bitIdx < len(outBits); bitIdx++ {
			if outBits[bitIdx] {
				b |= 1 << bitIdx
			}
		}
		outBytes = append(outBytes, b)
	}

	var meaningfulLen = (meaningfulBits + 7) / 8 // Round up to bytes
	return outBytes, meaningfulLen
}
