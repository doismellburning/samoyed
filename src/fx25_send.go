package direwolf

import "github.com/doismellburning/samoyed/internal/fcs"

/*-------------------------------------------------------------
 *
 * Name:	sendFX25Frame (fx25_send_frame in Dire Wolf)
 *
 * Purpose:	Convert HDLC frames to a stream of bits.
 *
 * Inputs:	fbuf	- Frame buffer address.
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

func (s *HDLCSender) sendFX25Frame(fbuf []byte, fx_mode int) int {
	var ctag_num, data, check = fx25_encode_frame(s.channel, fbuf, fx_mode)
	if ctag_num < CTAG_MIN {
		return (-1)
	}

	s.bitsSent = 0

	var ctag_value = fx25_get_ctag_value(ctag_num)

	for k := range 8 {
		s.sendFX25Bytes([]byte{byte(ctag_value>>(k*8)) & 0xff})
	}

	s.sendFX25Bytes(data)
	s.sendFX25Bytes(check)

	return s.bitsSent
}

/*-------------------------------------------------------------
 *
 * Name:	fx25_encode_frame
 *
 * Purpose:	Wrap an AX.25 frame up as an FX.25 codeblock.
 *
 * Inputs:	channel, fx_mode - As for sendFX25Frame.
 *
 *		fbuf	- Frame buffer, without the FCS.
 *
 * Returns:	The correlation tag number, the "data" part to be transmitted,
 *		and the check bytes.
 *		The tag number is -1, and the other two are nil, for failure.
 *
 *--------------------------------------------------------------*/

func fx25_encode_frame(channel int, fbuf []byte, fx_mode int) (int, []byte, []byte) {
	if fx25_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		dw_printf("FX.25[%d] send frame: FX.25 mode = %d\n", channel, fx_mode)
		fx_hex_dump(fbuf)
	}

	// Append the FCS.

	var frameFCS = fcs.Calc(fbuf)
	fbuf = append(fbuf, byte(frameFCS)&0xff)
	fbuf = append(fbuf, byte(frameFCS>>8)&0xff)

	// Add bit-stuffing, filling to FX25_MAX_DATA bytes with flag patterns
	var stuffedBytes, meaningfulLen = bitStuff(fbuf, FX25_MAX_DATA)
	var dlen = meaningfulLen // Use meaningful length, not total buffer size

	// Pick suitable correlation tag depending on
	// user's preference, for number of check bytes,
	// and the data size.
	var ctag_num = fx25_pick_mode(fx_mode, dlen)

	if ctag_num < CTAG_MIN || ctag_num > CTAG_MAX {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("FX.25[%d]: Could not find suitable format for requested %d and data length %d.\n", channel, fx_mode, dlen)

		return -1, nil, nil
	}

	var k_data_radio = fx25_get_k_data_radio(ctag_num)
	var k_data_rs = fx25_get_k_data_rs(ctag_num)

	// Zero out part of data which won't be transmitted
	var shorten_by = FX25_MAX_DATA - k_data_radio
	if shorten_by > 0 {
		for i := k_data_radio; i < FX25_MAX_DATA; i++ {
			stuffedBytes[i] = 0
		}
	}

	var data = stuffedBytes

	// Compute the check bytes.

	const fence byte = 0xaa
	var check [FX25_MAX_CHECK + 1]byte
	check[FX25_MAX_CHECK] = fence
	var rs = fx25_get_rs(ctag_num)
	var nroots = int(rs.nroots)

	Assert(k_data_rs+nroots == int(rs.nn))

	encode_rs_char(rs, data, check[:nroots])
	Assert(check[FX25_MAX_CHECK] == fence)

	if fx25_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("FX.25[%d]: transmit %d data bytes, ctag number 0x%02x\n", channel, k_data_radio, ctag_num)
		fx_hex_dump(data[:k_data_radio])
		dw_printf("FX.25[%d]: transmit %d check bytes:\n", channel, nroots)
		fx_hex_dump(check[:nroots])
		dw_printf("------\n")
	}

	return ctag_num, data[:k_data_radio], check[:nroots]
}

func (s *HDLCSender) sendFX25Bytes(b []byte) {
	for _, x := range b {
		for range 8 {
			s.sendFX25Bit(x&0x01 != 0)
			x >>= 1
		}
	}
}

/*
 * NRZI encoding, as sendBitNRZI, but with the line level FX.25 left it at.
 * data 1 bit -> no change.
 * data 0 bit -> invert signal.
 */

func (s *HDLCSender) sendFX25Bit(b bool) {
	if !b {
		s.fx25NRZIOutput = 1 - s.fx25NRZIOutput
	}

	tone_gen_put_bit(s.channel, s.fx25NRZIOutput)

	s.bitsSent++
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
