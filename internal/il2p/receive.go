// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

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
	"github.com/sirupsen/logrus"
)

type rxState int

const rxSearching rxState = 0
const rxHeader rxState = 1
const rxPayload rxState = 2
const rxDecode rxState = 3
const rxCRC rxState = 4

// PacketSink is handed each packet a Receiver decodes, along with the number
// of symbols the Reed-Solomon decoder had to correct.
type PacketSink func(channel int, subchannel int, slice int, pp *ax25.Packet, corrected int)

// Receiver is the IL2P receive state for one slicer of one demodulator
// ("subchannel") of one channel.
type Receiver struct {
	channel, subchannel, slice int
	sink                       PacketSink // Where each decoded packet goes.

	version Version // IL2P protocol version spoken on this channel.
	crc     bool    // true if frames carry a trailing CRC.

	state rxState

	acc uint // Accumulate most recent 24 bits for sync word matching. Lower 8 bits are also used for accumulating bytes for the header and payload.

	bc int // Bit counter so we know when a complete byte has been accumulated.

	polarity bool // True if opposite of expected polarity.

	shdr [headerSize + headerParity]byte // Scrambled header as received over the radio.  Includes parity.
	hc   int                             // Number if bytes placed in above.

	uhdr [headerSize]byte // Header after FEC and unscrambling.

	eplen int // Encoded payload length.  This is not the number from the header but rather the number of encoded bytes to gather.

	spayload [maxEncodedPayloadSize]byte // Scrambled and encoded payload as received over the radio.
	pc       int                         // Number of bytes placed in above.

	scrc [CRCEncodedSize]byte // Received Hamming-encoded CRC.
	cc   int                  // CRC byte counter.

	corrected int // Number of symbols corrected by RS FEC.
}

// NewReceiver makes a receiver for one slicer of one demodulator of one
// channel, speaking the given IL2P version, expecting a trailing CRC if crc
// is set, and handing each packet it decodes to sink.  The channel,
// subchannel and slice are for the sink and log entries; the receiver makes
// no other use of them.
func NewReceiver(channel int, subchannel int, slice int, version Version, crc bool, sink PacketSink) *Receiver {
	dwutil.Assert(channel >= 0 && subchannel >= 0 && slice >= 0)

	var F = new(Receiver)
	F.channel = channel
	F.subchannel = subchannel
	F.slice = slice
	F.version = version
	F.crc = crc
	F.sink = sink

	return F
}

// Version is the IL2P version the receiver speaks.
func (F *Receiver) Version() Version {
	return F.version
}

// CRC reports whether the receiver expects a trailing CRC.
func (F *Receiver) CRC() bool {
	return F.crc
}

/***********************************************************************************
 *
 * Name:        Receiver.RecBit
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

func (F *Receiver) RecBit(dbit int) {
	var channel = F.channel
	var subchannel = F.subchannel
	var slice = F.slice

	// Accumulate most recent 24 bits received.  Most recent is LSB.

	F.acc = ((F.acc << 1) | uint(dbit&1)) & 0x00ffffff

	// State machine to look for sync word then gather appropriate number of header and payload bytes.

	switch F.state {
	case rxSearching: // Searching for the sync word.
		if bits.OnesCount(F.acc^SyncWord) <= 1 { // allow single bit mismatch
			//text_color_set (DW_COLOR_INFO);
			//dw_printf ("IL2P header has normal polarity\n");
			F.polarity = false
			F.state = rxHeader
			F.bc = 0
			F.hc = 0
		} else if bits.OnesCount((^F.acc&0x00ffffff)^SyncWord) <= 1 {
			// FIXME - this pops up occasionally with random noise.  Find better way to convey information.
			// This also happens for each slicer - to noisy.
			//dw_printf ("IL2P header has reverse polarity\n");
			F.polarity = true
			F.state = rxHeader
			F.bc = 0
			F.hc = 0
		}

	case rxHeader: // Gathering the header.
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

			if F.hc == headerSize+headerParity { // Have all of header
				if Debug() >= 1 {
					F.log().Debug("IL2P header as received")
					dwutil.HexDump(F.shdr[:])
				}

				// Fix any errors and descramble.
				var uhdr, corrected = clarifyHeader(F.shdr[:])
				F.corrected = corrected
				copy(F.uhdr[:], uhdr)

				if F.corrected >= 0 { // Good header.
					// How much payload is expected?
					var hdr_type, fec_level, length = headerAttributes(F.uhdr[:])
					var max_fec = rxMaxFEC(F.version, fec_level)

					var plprop, eplen = payloadCompute(length, max_fec)
					F.eplen = eplen

					if Debug() >= 1 {
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
						F.state = rxPayload
					} else if F.eplen == 0 { // No payload.
						F.pc = 0
						if F.crc {
							F.cc = 0
							F.state = rxCRC
						} else {
							F.state = rxDecode
						}
					} else { // Error.
						if Debug() >= 1 {
							F.log().Debug("IL2P header invalid")
						}

						F.state = rxSearching
					}
					// good header after FEC.
				} else {
					F.state = rxSearching // Header failed FEC check.
				}
			} // entire header has been collected.
		} // full byte collected.

	case rxPayload: // Gathering the payload, if any.
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
					F.state = rxCRC
				} else {
					F.state = rxDecode
				}
			}
		}

	case rxCRC: // Gathering 4 trailing CRC bytes.

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
			if F.cc == CRCEncodedSize {
				F.state = rxDecode
			}
		}

	case rxDecode:
		// We get here after a good header and any payload has been collected.
		// Processing is delayed by one bit but I think it makes the logic cleaner.
		// During unit testing be sure to send an extra bit to flush it out at the end.

		// in uhdr[IL2P_HEADER_SIZE];  // Header after FEC and descrambling.

		// TODO?:  for symmetry, we might decode the payload here and later build the frame.
		{
			// Compute encoded payload size (includes parity symbols).
			var version = F.version
			var _, fec_level, payload_len = headerAttributes(F.uhdr[:])
			var max_fec = rxMaxFEC(version, fec_level)
			var _, encoded_payload_size = payloadCompute(payload_len, max_fec)

			var pp = decodeHeaderPayload(
				F.uhdr[:],
				F.spayload[:encoded_payload_size],
				version,
				&F.corrected,
			)

			if Debug() >= 1 {
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
				if !crcCheck(frame_data, F.scrc[:]) {
					if Debug() >= 1 {
						F.log().Debug("IL2P trailing CRC mismatch")
					}
					pp = nil
				}
			}

			if pp != nil {
				F.sink(channel, subchannel, slice, pp, F.corrected)
			}
		} // end block for local variables.

		F.state = rxSearching
	} // end of switch
}

// log is a logrus entry naming where the receiver sits.
func (F *Receiver) log() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"channel":    F.channel,
		"subchannel": F.subchannel,
		"slice":      F.slice,
	})
}
