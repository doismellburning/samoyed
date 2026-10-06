package direwolf

/********************************************************************************
 *
 * Purpose:     Extract IL2P frames from a stream of bits and process them.
 *
 * References:	https://tarpn.net/t/il2p/il2p-specification_draft_v0-6.pdf
 *
 *******************************************************************************/

import (
	"math/bits"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
)

type IL2PState int

const IL2P_SEARCHING IL2PState = 0
const IL2P_HEADER IL2PState = 1
const IL2P_PAYLOAD IL2PState = 2
const IL2P_DECODE IL2PState = 3
const IL2P_CRC IL2PState = 4

// il2pReceiver is the IL2P receive state for one slicer of one demodulator
// ("subchannel") of one channel.
type il2pReceiver struct {
	channel, subchannel, slice int

	version il2p_version_t // IL2P protocol version spoken on this channel.
	crc     bool           // true if frames carry a trailing CRC.

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

	audioLevel audioLevelFunc // For the audio level to deliver each packet with.
	sink       il2pPacketSink // Where each extracted packet goes.
}

// il2pPacketSink is handed each packet extracted from the received bit
// stream, along with the audio level it was heard at and the number of
// symbols the FEC decoder had to correct.  In normal operation it is
// multi_modem_process_rec_packet.
type il2pPacketSink func(channel int, subchannel int, slice int, pp *ax25.Packet, alevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType)

func newIL2PReceiver(channel int, subchannel int, slice int, version il2p_version_t, crc bool, audioLevel audioLevelFunc, sink il2pPacketSink) *il2pReceiver {
	dwutil.Assert(channel >= 0 && channel < phy.MaxRadioChans)
	dwutil.Assert(subchannel >= 0 && subchannel < phy.MaxSubchans)
	dwutil.Assert(slice >= 0 && slice < phy.MaxSlicers)

	var F = new(il2pReceiver)
	F.channel = channel
	F.subchannel = subchannel
	F.slice = slice
	F.version = version
	F.crc = crc
	F.audioLevel = audioLevel
	F.sink = sink

	return F
}

func (F *il2pReceiver) logEntry() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"channel":    F.channel,
		"subchannel": F.subchannel,
		"slice":      F.slice,
	})
}

/***********************************************************************************
 *
 * Name:        il2pReceiver.recBit
 *
 * Purpose:     Extract IL2P packets from a stream of bits.
 *
 * Inputs:      dbit	- One bit from the received data stream.
 *
 * Description: This is called once for each received bit.
 *              Each valid packet is handed to the receiver's sink, which in
 *              normal operation is multi_modem_process_rec_packet.
 *		It can gather multiple candidates from different parallel demodulators
 *		("subchannels") and slicers, then decide which one is the best.
 *
 ***********************************************************************************/

func (F *il2pReceiver) recBit(dbit int) {
	var channel = F.channel
	var subchannel = F.subchannel
	var slice = F.slice

	// Accumulate most recent 24 bits received.  Most recent is LSB.

	F.acc = ((F.acc << 1) | uint(dbit&1)) & 0x00ffffff

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
				F.shdr[F.hc] = byte(^F.acc & 0xff)
				F.hc++
			}

			if F.hc == IL2P_HEADER_SIZE+IL2P_HEADER_PARITY { // Have all of header
				if il2p_get_debug() >= 1 {
					F.logEntry().Debug("IL2P header as received")
					dwutil.LogHexDump(F.logEntry(), logrus.DebugLevel, F.shdr[:])
				}

				// Fix any errors and descramble.
				var uhdr, corrected = il2p_clarify_header(F.shdr[:])
				F.corrected = corrected
				copy(F.uhdr[:], uhdr)

				if F.corrected >= 0 { // Good header.
					// How much payload is expected?
					var hdr_type, fec_level, length = il2p_get_header_attributes(F.uhdr[:])
					var max_fec = il2p_rx_max_fec(F.version, fec_level)

					var plprop, eplen = il2p_payload_compute(length, max_fec)
					F.eplen = eplen

					if il2p_get_debug() >= 1 {
						var logEntry = F.logEntry().WithField("corrected", F.corrected)
						logEntry.Debug("IL2P header after correcting symbols and unscrambling")
						dwutil.LogHexDump(logEntry, logrus.DebugLevel, F.uhdr[:])
						logEntry.WithFields(logrus.Fields{
							"hdr_type":          hdr_type,
							"max_fec":           max_fec,
							"encoded_bytes":     F.eplen,
							"payload_bytes":     length,
							"small_block_count": plprop.small_block_count,
							"small_block_size":  plprop.small_block_size,
							"large_block_count": plprop.large_block_count,
							"large_block_size":  plprop.large_block_size,
							"parity_per_block":  plprop.parity_symbols_per_block,
						}).Debug("IL2P payload to collect")
					}

					if F.eplen >= 1 { // Need to gather payload.
						F.pc = 0
						F.state = IL2P_PAYLOAD
					} else if F.eplen == 0 { // No payload.
						F.pc = 0
						if F.crc {
							F.cc = 0
							F.state = IL2P_CRC
						} else {
							F.state = IL2P_DECODE
						}
					} else { // Error.
						if il2p_get_debug() >= 1 {
							F.logEntry().Debug("IL2P header INVALID")
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
				F.spayload[F.pc] = byte(^F.acc & 0xff)
				F.pc++
			}

			if F.pc == F.eplen {
				// TODO?: for symmetry it seems like we should clarify the payload before combining.

				if F.crc {
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
				F.scrc[F.cc] = byte(^F.acc & 0xff)
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
			var version = F.version
			var _, fec_level, payload_len = il2p_get_header_attributes(F.uhdr[:])
			var max_fec = il2p_rx_max_fec(version, fec_level)
			var _, encoded_payload_size = il2p_payload_compute(payload_len, max_fec)

			var pp = il2p_decode_header_payload(
				F.uhdr[:],
				F.spayload[:encoded_payload_size],
				version,
				&F.corrected,
			)

			if il2p_get_debug() >= 1 {
				if pp != nil {
					pp.HexDump()
				} else {
					// Most likely too many FEC errors.
					F.logEntry().Debug("IL2P: FAILED to construct frame")
				}
			}

			// Validate trailing CRC if we collected one.
			if pp != nil && F.crc {
				var frame_data = pp.FrameData()
				if !il2p_crc_check(frame_data, F.scrc[:]) {
					if il2p_get_debug() >= 1 {
						F.logEntry().Debug("IL2P trailing CRC mismatch")
					}
					pp = nil
				}
			}

			if pp != nil {
				// TODO: Could we put last 3 arguments in packet object rather than passing around separately?

				F.sink(channel, subchannel, slice, pp, F.audioLevel(channel, subchannel), phy.BitFixLevel(F.corrected), phy.FECIL2P)
			}
		} // end block for local variables.

		F.state = IL2P_SEARCHING
	} // end of switch
}
