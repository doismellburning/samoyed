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
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/sirupsen/logrus"
)

type IL2PState int

const IL2P_SEARCHING IL2PState = 0
const IL2P_HEADER IL2PState = 1
const IL2P_PAYLOAD IL2PState = 2
const IL2P_DECODE IL2PState = 3
const IL2P_CRC IL2PState = 4

// il2p_packet_sink is handed each packet an il2pReceiver decodes, along with
// the number of symbols the Reed-Solomon decoder had to correct.
type il2p_packet_sink func(channel int, subchannel int, slice int, pp *ax25.Packet, corrected int)

// il2p_deliver_packet is the sink used in normal operation, passing each
// packet the IL2P receiver decodes on to the rest of the receive path.
func il2p_deliver_packet(channel int, subchannel int, slice int, pp *ax25.Packet, corrected int) {
	var alevel = demod_get_audio_level(channel, subchannel)

	// TODO: Could we put last 3 arguments in packet object rather than passing around separately?

	multi_modem_process_rec_packet(channel, subchannel, slice, pp, alevel, BitFixLevel(corrected), fec_type_il2p)
}

// il2pReceiver is the IL2P receive state for one slicer of one demodulator
// ("subchannel") of one channel.
type il2pReceiver struct {
	channel, subchannel, slice int
	sink                       il2p_packet_sink // Where each decoded packet goes.

	version il2p.Version // IL2P protocol version spoken on this channel.
	crc     bool         // true if frames carry a trailing CRC.

	state IL2PState

	acc uint // Accumulate most recent 24 bits for sync word matching. Lower 8 bits are also used for accumulating bytes for the header and payload.

	bc int // Bit counter so we know when a complete byte has been accumulated.

	polarity bool // True if opposite of expected polarity.

	shdr [il2p.HeaderSize + il2p.HeaderParity]byte // Scrambled header as received over the radio.  Includes parity.
	hc   int                                       // Number if bytes placed in above.

	uhdr [il2p.HeaderSize]byte // Header after FEC and unscrambling.

	eplen int // Encoded payload length.  This is not the number from the header but rather the number of encoded bytes to gather.

	spayload [il2p.MaxEncodedPayloadSize]byte // Scrambled and encoded payload as received over the radio.
	pc       int                              // Number of bytes placed in above.

	scrc [il2p.CRCEncodedSize]byte // Received Hamming-encoded CRC.
	cc   int                       // CRC byte counter.

	corrected int // Number of symbols corrected by RS FEC.
}

func newIL2PReceiver(channel int, subchannel int, slice int, version il2p.Version, crc bool, sink il2p_packet_sink) *il2pReceiver {
	dwutil.Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	dwutil.Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)
	dwutil.Assert(slice >= 0 && slice < MAX_SLICERS)

	var F = new(il2pReceiver)
	F.channel = channel
	F.subchannel = subchannel
	F.slice = slice
	F.version = version
	F.crc = crc
	F.sink = sink

	return F
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
 *              Each valid packet is handed to the receiver's sink.
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
		if bits.OnesCount(F.acc^il2p.SyncWord) <= 1 { // allow single bit mismatch
			//text_color_set (DW_COLOR_INFO);
			//dw_printf ("IL2P header has normal polarity\n");
			F.polarity = false
			F.state = IL2P_HEADER
			F.bc = 0
			F.hc = 0
		} else if bits.OnesCount((^F.acc&0x00ffffff)^il2p.SyncWord) <= 1 {
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

			if F.hc == il2p.HeaderSize+il2p.HeaderParity { // Have all of header
				if il2p.Debug() >= 1 {
					F.log().Debug("IL2P header as received")
					dwutil.HexDump(F.shdr[:])
				}

				// Fix any errors and descramble.
				var uhdr, corrected = il2p.ClarifyHeader(F.shdr[:])
				F.corrected = corrected
				copy(F.uhdr[:], uhdr)

				if F.corrected >= 0 { // Good header.
					// How much payload is expected?
					var hdr_type, fec_level, length = il2p.HeaderAttributes(F.uhdr[:])
					var max_fec = il2p.RxMaxFEC(F.version, fec_level)

					var plprop, eplen = il2p.PayloadCompute(length, max_fec)
					F.eplen = eplen

					if il2p.Debug() >= 1 {
						F.log().WithField("corrected", F.corrected).Debug("IL2P header after correcting and unscrambling")
						dwutil.HexDump(F.uhdr[:])
						F.log().WithFields(logrus.Fields{
							"hdr_type":                 hdr_type,
							"max_fec":                  max_fec,
							"encoded_bytes":            F.eplen,
							"payload_bytes":            length,
							"small_block_count":        plprop.SmallBlockCount,
							"small_block_size":         plprop.SmallBlockSize,
							"large_block_count":        plprop.LargeBlockCount,
							"large_block_size":         plprop.LargeBlockSize,
							"parity_symbols_per_block": plprop.ParitySymbolsPerBlock,
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
						if il2p.Debug() >= 1 {
							F.log().Debug("IL2P header invalid")
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
				F.scrc[F.cc] = byte(^F.acc) & 0xff
				F.cc++
			}
			if F.cc == il2p.CRCEncodedSize {
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
			var _, fec_level, payload_len = il2p.HeaderAttributes(F.uhdr[:])
			var max_fec = il2p.RxMaxFEC(version, fec_level)
			var _, encoded_payload_size = il2p.PayloadCompute(payload_len, max_fec)

			var pp = il2p.DecodeHeaderPayload(
				F.uhdr[:],
				F.spayload[:encoded_payload_size],
				version,
				&F.corrected,
			)

			if il2p.Debug() >= 1 {
				if pp != nil {
					pp.HexDump()
				} else {
					// Most likely too many FEC errors.
					F.log().Debug("Failed to construct frame from IL2P")
				}
			}

			// Validate trailing CRC if we collected one.
			if pp != nil && F.crc {
				var frame_data = pp.FrameData()
				if !il2p.CRCCheck(frame_data, F.scrc[:]) {
					if il2p.Debug() >= 1 {
						F.log().Debug("IL2P trailing CRC mismatch")
					}
					pp = nil
				}
			}

			if pp != nil {
				F.sink(channel, subchannel, slice, pp, F.corrected)
			}
		} // end block for local variables.

		F.state = IL2P_SEARCHING
	} // end of switch
}

// log is a logrus entry naming where the receiver sits.
func (F *il2pReceiver) log() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"channel":    F.channel,
		"subchannel": F.subchannel,
		"slice":      F.slice,
	})
}
