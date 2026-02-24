package direwolf

/********************************************************************************
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits and process them.
 *
 *******************************************************************************/

import (
	"math/bits"
)

type FX25RecState int

const (
	FX_TAG FX25RecState = iota
	FX_DATA
	FX_CHECK
)

type fx_context_s struct {
	state        FX25RecState
	accum        uint64 // Accumulate bits for matching to correlation tag.
	ctag_num     int    // Correlation tag number, CTAG_MIN to CTAG_MAX if approx. match found.
	k_data_radio int    // Expected size of "data" sent over radio.
	coffs        int    // Starting offset of the check part.
	nroots       int    // Expected number of check bytes.
	dlen         int    // Accumulated length in "data" below.
	clen         int    // Accumulated length in "check" below.
	imask        byte   // Mask for storing a bit.
	block        [FX25_BLOCK_SIZE + 1]byte
}

var fx_context [MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS]*fx_context_s

var FXTEST = false
var fx25_test_count = 0

/***********************************************************************************
 *
 * Name:        fx25_rec_bit
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits.
 *		In a completely integrated AX.25 / FX.25 receive system,
 *		this would see the same bit stream as hdlc_rec_bit.
 *
 * Inputs:      channel    - Channel number.
 *
 *              subchannel - This allows multiple demodulators per channel.
 *
 *              slice   - Allows multiple slicers per demodulator (subchannel).
 *
 *              dbit	- Data bit after NRZI and any descrambling.
 *			  Any non-zero value is logic '1'.
 *
 * Description: This is called once for each received bit.
 *              For each valid frame, process_rec_frame() is called for further processing.
 *		It can gather multiple candidates from different parallel demodulators
 *		("subchannels") and slicers, then decide which one is the best.
 *
 ***********************************************************************************/

const FENCE = 0x55 // to detect buffer overflow.

func fx25_rec_bit(channel int, subchannel int, slice int, dbit int) {
	// Allocate context blocks only as needed.
	var F = fx_context[channel][subchannel][slice]
	if F == nil {
		Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
		Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)
		Assert(slice >= 0 && slice < MAX_SLICERS)

		F = new(fx_context_s)
		fx_context[channel][subchannel][slice] = F
		Assert(F != nil)
	}

	// State machine to identify correlation tag then gather appropriate number of data and check bytes.

	switch F.state {
	case FX_TAG:
		F.accum >>= 1
		if dbit != 0 {
			F.accum |= 1 << 63
		}

		var c = fx25_tag_find_match(F.accum)
		if c >= CTAG_MIN && c <= CTAG_MAX {
			F.ctag_num = c
			F.k_data_radio = fx25_get_k_data_radio(F.ctag_num)
			F.nroots = fx25_get_nroots(F.ctag_num)
			F.coffs = fx25_get_k_data_rs(F.ctag_num)
			Assert(F.coffs == FX25_BLOCK_SIZE-F.nroots)

			if fx25_get_debug() >= 2 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("FX.25[%d.%d]: Matched correlation tag 0x%02x with %d bit errors.  Expecting %d data & %d check bytes.\n",
					channel, slice, // ideally subchannel too only if applicable
					c,
					bits.OnesCount(uint(F.accum^fx25_get_ctag_value(c))),
					F.k_data_radio, F.nroots)
			}

			F.imask = 0x01
			F.dlen = 0
			F.clen = 0
			F.block = [FX25_BLOCK_SIZE + 1]byte{}
			F.block[FX25_BLOCK_SIZE] = FENCE
			F.state = FX_DATA
		}

	case FX_DATA:
		if dbit != 0 {
			F.block[F.dlen] |= F.imask
		}

		F.imask <<= 1
		if F.imask == 0 {
			F.imask = 0x01

			F.dlen++
			if F.dlen >= F.k_data_radio {
				F.state = FX_CHECK
			}
		}

	case FX_CHECK:
		if dbit != 0 {
			F.block[F.coffs+F.clen] |= F.imask
		}

		F.imask <<= 1
		if F.imask == 0 {
			F.imask = 0x01

			F.clen++
			if F.clen >= F.nroots {
				process_rs_block(channel, subchannel, slice, F) // see below

				F.ctag_num = -1
				F.accum = 0
				F.state = FX_TAG
			}
		}
	}
}

/***********************************************************************************
 *
 * Name:        fx25_rec_busy
 *
 * Purpose:     Is FX.25 reception currently in progress?
 *
 * Inputs:      channel    - Channel number.
 *
 * Returns:	True if currently in progress for the specified channel.
 *
 * Description: This is required for duplicate removal.  One channel and can have
 *		multiple demodulators (called subchannels) running in parallel.
 *		Each of them can have multiple slicers.  Duplicates need to be
 *		removed.  Normally a delay of a couple bits (or more accurately
 *		symbols) was fine because they all took about the same amount of time.
 *		Now, we can have an additional delay of up to 64 check bytes and
 *		some filler in the data portion.  We can't simply wait that long.
 *		With normal AX.25 a couple frames can come and go during that time.
 *		We want to delay the duplicate removal while FX.25 block reception
 *		is going on.
 *
 ***********************************************************************************/

func fx25_rec_busy(channel int) bool {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	// This could be a little faster if we knew number of
	// subchannels and slicers but it is probably insignificant.

	for i := 0; i < MAX_SUBCHANS; i++ {
		for j := 0; j < MAX_SLICERS; j++ {
			if fx_context[channel][i][j] != nil {
				if fx_context[channel][i][j].state != FX_TAG {
					return true
				}
			}
		}
	}

	return false
} // end fx25_rec_busy

/***********************************************************************************
 *
 * Name:	process_rs_block
 *
 * Purpose:     After the correlation tag was detected and the appropriate number
 *		of data and check bytes are accumulated, this performs the processing
 *
 * Inputs:	channel, subchannel, slice
 *
 *		F.ctag_num	- Correlation tag number  (index into table)
 *
 *		F.dlen		- Number of "data" bytes.
 *
 *		F.clen		- Number of "check" bytes"
 *
 *		F.block	- Codeblock.  Always 255 total bytes.
 *				  Anything left over after data and check
 *				  bytes is filled with zeros.
 *
 *		<- - - - - - - - - - - 255 bytes total - - - - - - - - ->
 *		+-----------------------+---------------+---------------+
 *		|  dlen bytes "data"    |  zero fill    |  check bytes  |
 *		+-----------------------+---------------+---------------+
 *
 * Description:	Use Reed-Solomon decoder to fix up any errors.
 *		Extract the AX.25 frame from the corrected data.
 *
 ***********************************************************************************/

func process_rs_block(channel int, subchannel int, slice int, F *fx_context_s) {
	if fx25_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("FX.25[%d.%d]: Received RS codeblock.\n", channel, slice)
		fx_hex_dump(F.block[:FX25_BLOCK_SIZE])
	}

	Assert(F.block[FX25_BLOCK_SIZE] == FENCE)

	var derrlocs [FX25_MAX_CHECK]int // Half would probably be OK.
	var rs = fx25_get_rs(F.ctag_num)

	var derrors = decode_rs_char(rs, F.block[:FX25_BLOCK_SIZE], derrlocs[:], 0)

	if derrors >= 0 { // -1 for failure.  >= 0 for success, number of bytes corrected.
		if fx25_get_debug() >= 2 {
			text_color_set(DW_COLOR_INFO)

			if derrors == 0 {
				dw_printf("FX.25[%d.%d]: FEC complete with no errors.\n", channel, slice)
			} else {
				dw_printf("FX.25[%d.%d]: FEC complete, fixed %2d errors in byte positions:", channel, slice, derrors)

				for k := 0; k < derrors; k++ {
					dw_printf(" %d", derrlocs[k])
				}

				dw_printf("\n")
			}
		}

		var frame_buf = my_unstuff(channel, subchannel, slice, F.block[:], F.dlen)
		var frame_len = len(frame_buf)

		if frame_len >= 14+1+2 { // Minimum length: Two addresses & control & FCS.
			var actual_fcs = uint16(frame_buf[frame_len-2]) | (uint16(frame_buf[frame_len-1]) << 8)

			var expected_fcs = fcs_calc(frame_buf[:frame_len-2])
			if actual_fcs == expected_fcs {
				if fx25_get_debug() >= 3 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("FX.25[%d.%d]: Extracted AX.25 frame:\n", channel, slice)
					fx_hex_dump(frame_buf[:frame_len])
				}

				if FXTEST {
					fx25_test_count++
				} else {
					var alevel = demod_get_audio_level(channel, subchannel)

					multi_modem_process_rec_frame(channel, subchannel, slice, frame_buf[:frame_len-2], alevel, retry_t(derrors), 1) /* len-2 to remove FCS. */
				}
			} else {
				// Most likely cause is defective sender software.
				text_color_set(DW_COLOR_ERROR)
				dw_printf("FX.25[%d.%d]: Bad FCS for AX.25 frame.\n", channel, slice)
				fx_hex_dump(F.block[:F.dlen])
				fx_hex_dump(frame_buf[:frame_len])
			}
		} else {
			// Most likely cause is defective sender software.
			text_color_set(DW_COLOR_ERROR)
			dw_printf("FX.25[%d.%d]: AX.25 frame is shorter than minimum length.\n", channel, slice)
			fx_hex_dump(F.block[:F.dlen])

			if frame_len > 0 {
				fx_hex_dump(frame_buf[:frame_len])
			}
		}
	} else if fx25_get_debug() >= 2 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("FX.25[%d.%d]: FEC failed.  Too many errors.\n", channel, slice)
	}
} // process_rs_block

/***********************************************************************************
 *
 * Name:	my_unstuff
 *
 * Purpose:	Remove HDLC bit stuffing and surrounding flag delimiters.
 *
 * Inputs:      channel, subchannel, slice	- For error messages.
 *
 *		pin	- "data" part of RS codeblock.
 *			  First byte must be HDLC "flag".
 *			  May be followed by additional flags.
 *			  There must be terminating flag but it might not be byte aligned.
 *
 *		ilen	- Number of bytes in pin.
 *
 * Outputs:	frame_buf - Frame contents including FCS.
 *			    Bit stuffing is gone so it should be a whole number of bytes.
 *
 * Returns:	Number of bytes in frame_buf, including 2 for FCS.
 *		This can never be larger than the max "data" size.
 *		0 if any error.
 *
 * Errors:	First byte is not not flag.
 *		Found seven '1' bits in a row.
 *		Result is not whole number of bytes after removing bit stuffing.
 *		Trailing flag not found.
 *		Most likely cause, for all of these, is defective sender software.
 *
 ***********************************************************************************/

func my_unstuff(channel int, subchannel int, slice int, pin []byte, ilen int) []byte { //nolint:unparam
	var pat_det byte = 0 // Pattern detector.
	var oacc byte = 0    // Accumulator for a byte out.
	var olen = 0         // Number of good bits in oacc.

	if pin[0] != 0x7e {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("FX.25[%d.%d] error: Data section did not start with 0x7e.\n", channel, slice)
		fx_hex_dump(pin[:ilen])

		return nil
	}

	for ilen > 0 && pin[0] == 0x7e {
		ilen--
		pin = pin[1:] // Skip over leading flag byte(s).
	}

	var frame_buf []byte
	for i := 0; i < ilen; i++ {
		for imask := byte(0x01); imask != 0; imask <<= 1 {
			var dbit = byte(IfThenElse((pin[i]&imask) != 0, 1, 0))

			pat_det >>= 1 // Shift the most recent eight bits thru the pattern detector.
			pat_det |= dbit << 7

			if pat_det == 0xfe {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("FX.25[%d.%d]: Invalid AX.25 frame - Seven '1' bits in a row.\n", channel, slice)
				fx_hex_dump(pin[i:ilen])

				return nil
			}

			if dbit != 0 {
				oacc >>= 1
				oacc |= 0x80
			} else {
				if pat_det == 0x7e { // "flag" pattern - End of frame.
					if olen == 7 {
						return frame_buf // Whole number of bytes in result including CRC
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("FX.25[%d.%d]: Invalid AX.25 frame - Not a whole number of bytes.\n", channel, slice)
						fx_hex_dump(pin[i:ilen])

						return nil
					}
				} else if (pat_det >> 2) == 0x1f {
					continue // Five '1' bits in a row, followed by '0'.  Discard the '0'.
				}

				oacc >>= 1
			}

			olen++
			if olen&8 != 0 {
				olen = 0

				frame_buf = append(frame_buf, oacc)
			}
		}
	} /* end of loop on all bits in block */

	text_color_set(DW_COLOR_ERROR)
	dw_printf("FX.25[%d.%d]: Invalid AX.25 frame - Terminating flag not found.\n", channel, slice)
	fx_hex_dump(pin[:ilen])

	return nil // Should never fall off the end.
} // my_unstuff
