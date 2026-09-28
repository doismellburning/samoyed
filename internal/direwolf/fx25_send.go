package direwolf

import (
	"github.com/doismellburning/samoyed/internal/fx25"
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
 * Outputs:	Bits are shipped out by calling ToneGenPutBit().
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
 *		ToneGenPutBit() are processed correctly.
 *
 * Errors:	If something goes wrong, return -1 and the caller should
 *		fallback to sending normal AX.25.
 *
 *		This could happen if the frame is too large.
 *
 *--------------------------------------------------------------*/

func (s *HDLCSender) sendFX25Frame(fbuf []byte, fx_mode int) int {
	var ctag_num, data, check = fx25.EncodeFrame(s.channel, fbuf, fx_mode, s.fx25Debug)
	if ctag_num < fx25.CTagMin {
		return (-1)
	}

	s.bitsSent = 0

	var ctag_value = fx25.TagValue(ctag_num)

	for k := range 8 {
		s.sendFX25Bytes([]byte{byte(ctag_value>>(k*8)) & 0xff})
	}

	s.sendFX25Bytes(data)
	s.sendFX25Bytes(check)

	return s.bitsSent
}

// sendFX25Bytes sends NRZI, with no stuffing: the codeblock was stuffed before
// it was encoded.  It shares the line level with AX.25, since the receiver
// sees only the one line.
func (s *HDLCSender) sendFX25Bytes(b []byte) {
	for _, x := range b {
		for range 8 {
			s.sendBitNRZI(x&0x01 != 0)
			x >>= 1
		}
	}
}
