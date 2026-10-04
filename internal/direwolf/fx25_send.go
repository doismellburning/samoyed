package direwolf

import (
	"github.com/doismellburning/samoyed/internal/bitstuff"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/sirupsen/logrus"
)

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
 * Outputs:	Bits are shipped out to the sender's tone generator.
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
 *		the tone generator are processed correctly.
 *
 * Errors:	If something goes wrong, return -1 and the caller should
 *		fallback to sending normal AX.25.
 *
 *		This could happen if the frame is too large.
 *
 *--------------------------------------------------------------*/

func (s *Layer2Sender) sendFX25Frame(fbuf []byte, fx_mode int) int {
	var ctag_num, data, check = fx25_encode_frame(s.channel, fbuf, fx_mode, s.fx25Debug)
	if ctag_num < CTAG_MIN {
		return (-1)
	}

	s.bitsSent = 0

	var ctag_value = fx25_get_ctag_value(ctag_num)

	for k := range 8 {
		s.sendFX25Bytes([]byte{byte((ctag_value >> (k * 8)) & 0xff)})
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
 *		debug	- FX.25's debug level.
 *
 *		fbuf	- Frame buffer, without the FCS.
 *
 * Returns:	The correlation tag number, the "data" part to be transmitted,
 *		and the check bytes.
 *		The tag number is -1, and the other two are nil, for failure.
 *
 *--------------------------------------------------------------*/

func fx25_encode_frame(channel int, fbuf []byte, fx_mode int, debug int) (int, []byte, []byte) {
	var logEntry = logrus.WithField("channel", channel)

	if debug >= 3 {
		logEntry.WithField("fx_mode", fx_mode).Debug("FX.25: send frame")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, fbuf)
	}

	// Append the FCS.

	var frameFCS = fcs.Calc(fbuf)
	fbuf = append(fbuf, byte(frameFCS&0xff))
	fbuf = append(fbuf, byte((frameFCS>>8)&0xff))

	// Add bit-stuffing, filling to FX25_MAX_DATA bytes with flag patterns
	var stuffedBytes, meaningfulLen = bitstuff.Stuff(fbuf, FX25_MAX_DATA)
	var dlen = meaningfulLen // Use meaningful length, not total buffer size

	// Pick suitable correlation tag depending on
	// user's preference, for number of check bytes,
	// and the data size.
	var ctag_num = fx25_pick_mode(fx_mode, dlen)

	if ctag_num < CTAG_MIN || ctag_num > CTAG_MAX {
		logEntry.WithFields(logrus.Fields{
			"fx_mode": fx_mode,
			"dlen":    dlen,
		}).Warn("FX.25: Could not find suitable format for requested mode and data length")

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

	var rs = fx25_get_rs(ctag_num)
	var nroots = rs.NRoots()

	dwutil.Assert(k_data_rs+nroots == rs.N())

	var check = rs.Encode(data[:k_data_rs])

	if debug >= 3 {
		logEntry.WithFields(logrus.Fields{
			"data_bytes": k_data_radio,
			"ctag":       ctag_num,
		}).Debug("FX.25: transmit data bytes")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, data[:k_data_radio])
		logEntry.WithField("check_bytes", nroots).Debug("FX.25: transmit check bytes")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, check[:nroots])
	}

	return ctag_num, data[:k_data_radio], check[:nroots]
}

// sendFX25Bytes sends NRZI, with no stuffing: the codeblock was stuffed before
// it was encoded.  It shares the line level with AX.25, since the receiver
// sees only the one line.
func (s *Layer2Sender) sendFX25Bytes(b []byte) {
	for _, x := range b {
		for range 8 {
			s.sendBitNRZI(x&0x01 != 0)
			x >>= 1
		}
	}
}
