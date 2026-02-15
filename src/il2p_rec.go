package direwolf

/********************************************************************************
 *
 * Purpose:     Extract IL2P frames from a stream of bits and process them.
 *
 * References:	http://tarpn.net/t/il2p/il2p-specification0-4.pdf
 *
 *******************************************************************************/

import (
	"math/bits"
)

type IL2PState int

const IL2P_SEARCHING IL2PState = 0
const IL2P_HEADER IL2PState = 1
const IL2P_PAYLOAD IL2PState = 2
const IL2P_DECODE IL2PState = 3
const IL2P_CRC IL2PState = 4

type il2p_context_s struct {
	state IL2PState

	acc uint // Accumulate most recent 24 bits for sync word matching. Lower 8 bits are also used for accumulating bytes for the header and payload.

	bc int // Bit counter so we know when a complete byte has been accumulated.

	polarity bool // True if opposite of expected polarity.

	shdr [IL2P_HEADER_SIZE + IL2P_HEADER_PARITY]byte // Scrambled header as received over the radio.  Includes parity.
	hc   int                                         // Number if bytes placed in above.

	uhdr [IL2P_HEADER_SIZE]byte // Header after FEC and unscrambling.

	eplen int // Encoded payload length.  This is not the number from the header but rather the number of encoded bytes to gather.

	spayload [IL2P_MAX_ENCODED_PAYLOAD_SIZE]byte // Scrambled and encoded payload as received over the radio.
	pc       int                                 // Number of bytes placed in above.

	scrc [IL2P_CRC_ENCODED_SIZE]byte // Received Hamming-encoded CRC.
	cc   int                         // CRC byte counter.

	corrected int // Number of symbols corrected by RS FEC.
}

var il2p_context [MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS]*il2p_context_s

/***********************************************************************************
 *
 * Name:        il2p_rec_bit
 *
 * Purpose:     Extract IL2P packets from a stream of bits.
 *
 * Inputs:      channel    - Channel number.
 *
 *              subchannel - This allows multiple demodulators per channel.
 *
 *              slice   - Allows multiple slicers per demodulator (subchannel).
 *
 *              dbit	- One bit from the received data stream.
 *
 * Description: This is called once for each received bit.
 *              For each valid packet, process_rec_frame() is called for further processing.
 *		It can gather multiple candidates from different parallel demodulators
 *		("subchannels") and slicers, then decide which one is the best.
 *
 ***********************************************************************************/

func il2p_rec_bit(channel int, subchannel int, slice int, dbit int) {

	// Allocate context blocks only as needed.

	var F = il2p_context[channel][subchannel][slice]
	if F == nil {
		Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
		Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)
		Assert(slice >= 0 && slice < MAX_SLICERS)
		F = new(il2p_context_s)
		il2p_context[channel][subchannel][slice] = F

		Assert(F != nil)
	}

	// Accumulate most recent 24 bits received.  Most recent is LSB.

	F.acc = ((F.acc << 1) | uint(dbit&1)) & 0x00ffffff //nolint:gosec

	// State machine to look for sync word then gather appropriate number of header and payload bytes.

	switch F.state {

	case IL2P_SEARCHING: // Searching for the sync word.

		if bits.OnesCount(F.acc^IL2P_SYNC_WORD) <= 1 { // allow single bit mismatch
			//text_color_set (DW_COLOR_INFO);
			//dw_printf ("IL2P header has normal polarity\n");
			F.polarity = false
			F.state = IL2P_HEADER
			F.bc = 0
			F.hc = 0
		} else if bits.OnesCount((^F.acc&0x00ffffff)^IL2P_SYNC_WORD) <= 1 {
			text_color_set(DW_COLOR_INFO)
			// FIXME - this pops up occasionally with random noise.  Find better way to convey information.
			// This also happens for each slicer - to noisy.
			//dw_printf ("IL2P header has reverse polarity\n");
			F.polarity = true
			F.state = IL2P_HEADER
			F.bc = 0
			F.hc = 0
		}

	case IL2P_HEADER: // Gathering the header.

		F.bc++
		if F.bc == 8 { // full byte has been collected.
			F.bc = 0
			if !F.polarity {
				F.shdr[F.hc] = byte(F.acc & 0xff)
				F.hc++
			} else {
				F.shdr[F.hc] = byte(^F.acc) & 0xff
				F.hc++
			}
			if F.hc == IL2P_HEADER_SIZE+IL2P_HEADER_PARITY { // Have all of header

				if il2p_get_debug() >= 1 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("IL2P header as received [%d.%d.%d]:\n", channel, subchannel, slice)
					fx_hex_dump(F.shdr[:])
				}

				// Fix any errors and descramble.
				var uhdr, corrected = il2p_clarify_header(F.shdr[:])
				F.corrected = corrected
				copy(F.uhdr[:], uhdr)

				if F.corrected >= 0 { // Good header.
					// How much payload is expected?
					var hdr_type, max_fec, length = il2p_get_header_attributes(F.uhdr[:])

					var plprop, eplen = il2p_payload_compute(length, max_fec)
					F.eplen = eplen

					if il2p_get_debug() >= 1 {
						text_color_set(DW_COLOR_DEBUG)
						dw_printf("IL2P header after correcting %d symbols and unscrambling [%d.%d.%d]:\n", F.corrected, channel, subchannel, slice)
						fx_hex_dump(F.uhdr[:])
						dw_printf("Header type %d, max fec = %d\n", hdr_type, max_fec)
						dw_printf("Need to collect %d encoded bytes for %d byte payload.\n", F.eplen, length)
						dw_printf("%d small blocks of %d and %d large blocks of %d.  %d parity symbols per block\n",
							plprop.small_block_count, plprop.small_block_size,
							plprop.large_block_count, plprop.large_block_size, plprop.parity_symbols_per_block)
					}

					if F.eplen >= 1 { // Need to gather payload.
						F.pc = 0
						F.state = IL2P_PAYLOAD
					} else if F.eplen == 0 { // No payload.
						F.pc = 0
						if il2p_crc_enabled(channel) {
							F.cc = 0
							F.state = IL2P_CRC
						} else {
							F.state = IL2P_DECODE
						}
					} else { // Error.

						if il2p_get_debug() >= 1 {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("IL2P header INVALID.\n")
						}

						F.state = IL2P_SEARCHING
					}
					// good header after FEC.
				} else {
					F.state = IL2P_SEARCHING // Header failed FEC check.
				}
			} // entire header has been collected.
		} // full byte collected.

	case IL2P_PAYLOAD: // Gathering the payload, if any.

		F.bc++
		if F.bc == 8 { // full byte has been collected.
			F.bc = 0
			if !F.polarity {
				F.spayload[F.pc] = byte(F.acc & 0xff)
				F.pc++
			} else {
				F.spayload[F.pc] = byte(^F.acc) & 0xff
				F.pc++
			}
			if F.pc == F.eplen {

				// TODO?: for symmetry it seems like we should clarify the payload before combining.

				if il2p_crc_enabled(channel) {
					F.cc = 0
					F.state = IL2P_CRC
				} else {
					F.state = IL2P_DECODE
				}
			}
		}

	case IL2P_CRC: // Gathering 4 trailing CRC bytes.

		F.bc++
		if F.bc == 8 { // full byte has been collected.
			F.bc = 0
			if !F.polarity {
				F.scrc[F.cc] = byte(F.acc & 0xff)
				F.cc++
			} else {
				F.scrc[F.cc] = byte(^F.acc) & 0xff
				F.cc++
			}
			if F.cc == IL2P_CRC_ENCODED_SIZE {
				F.state = IL2P_DECODE
			}
		}

	case IL2P_DECODE:
		// We get here after a good header and any payload has been collected.
		// Processing is delayed by one bit but I think it makes the logic cleaner.
		// During unit testing be sure to send an extra bit to flush it out at the end.

		// in uhdr[IL2P_HEADER_SIZE];  // Header after FEC and descrambling.

		// TODO?:  for symmetry, we might decode the payload here and later build the frame.

		{
			// Compute encoded payload size (includes parity symbols).
			var _, max_fec, payload_len = il2p_get_header_attributes(F.uhdr[:])
			var _, encoded_payload_size = il2p_payload_compute(payload_len, max_fec)

			var pp = il2p_decode_header_payload(
				F.uhdr[:],
				F.spayload[:encoded_payload_size],
				&F.corrected,
			)

			if il2p_get_debug() >= 1 {
				if pp != nil {
					ax25_hex_dump(pp)
				} else {
					// Most likely too many FEC errors.
					text_color_set(DW_COLOR_ERROR)
					dw_printf("FAILED to construct frame in il2p_rec_bit.\n")
				}
			}

			// Validate trailing CRC if we collected one.
			if pp != nil && il2p_crc_enabled(channel) {
				var frame_data = ax25_get_frame_data(pp)
				if !il2p_crc_check(frame_data, F.scrc[:]) {
					if il2p_get_debug() >= 1 {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("IL2P trailing CRC mismatch.\n")
					}
					ax25_delete(pp)
					pp = nil

					// Retry with single erasure hints if there is payload to retry against.
					if F.eplen > 0 {
						pp = il2p_crc_retry_decode(F.uhdr[:], F.spayload[:encoded_payload_size], F.scrc[:])
					}
				}
			}

			if pp != nil {
				var alevel = demod_get_audio_level(channel, subchannel)
				var retries = retry_t(F.corrected)
				var fec_type = fec_type_il2p

				// TODO: Could we put last 3 arguments in packet object rather than passing around separately?

				multi_modem_process_rec_packet(channel, subchannel, slice, pp, alevel, retries, fec_type)
			}
		} // end block for local variables.

		if il2p_get_debug() >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("-----\n")
		}

		F.state = IL2P_SEARCHING

	} // end of switch

} // end il2p_rec_bit
