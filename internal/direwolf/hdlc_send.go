package direwolf

import (
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/sirupsen/logrus"
)

// HDLCSender turns frames into the bits one radio channel sends.  It holds
// what has to carry over from one byte, or one frame, to the next: the NRZI
// line level and the run of ones that decides when to bit stuff.  Each
// channel wants its own, and only one goroutine may drive it at a time.
type HDLCSender struct {
	channel       int
	audioConfig   *RadioConfig
	toneGenerator *ToneGenerator // Where the bits go; nil for a channel with no radio.
	fx25Debug     int            // FX.25's debug level.

	bitsSent int // Count number of bits sent by SendFrame or SendPreamblePostamble.

	// Count number of "1" bits to keep track of when we need to break up a
	// long run by "bit stuffing."
	stuff int

	line *linecode.Encoder // Puts the bits on the line, keeping its NRZI level.
}

// NewHDLCSender makes an HDLCSender for channel, sending the layer 2
// protocol audioConfig says to use there to toneGenerator, with FX.25's
// debug level at fx25Debug.
func NewHDLCSender(channel int, audioConfig *RadioConfig, toneGenerator *ToneGenerator, fx25Debug int) *HDLCSender {
	var s = new(HDLCSender)
	s.channel = channel
	s.audioConfig = audioConfig
	s.toneGenerator = toneGenerator
	s.fx25Debug = fx25Debug
	s.line = linecode.NewEncoder(s.putBit)

	return s
}

// putQuietMs sends timeMs of silence.
func (s *HDLCSender) putQuietMs(timeMs int) {
	if s.toneGenerator == nil {
		logrus.WithField("channel", s.channel).Error("Invalid channel for tone generation")

		return
	}

	s.toneGenerator.PutQuietMs(timeMs)
}

// flush pushes out whatever the channel's samples are waiting in.
func (s *HDLCSender) flush() {
	if s.toneGenerator == nil {
		logrus.WithField("channel", s.channel).Error("Invalid channel for tone generation")

		return
	}

	s.toneGenerator.Flush()
}

/*-------------------------------------------------------------
 *
 * Name:	SendFrame (layer2_send_frame in Dire Wolf)
 *
 * Purpose:	Convert frames to a stream of bits.
 *		Originally this was for AX.25 only, hence the file name.
 *		Over time, FX.25 and IL2P were shoehorned in.
 *
 * Inputs:	pp	- Packet object.
 *
 *		badFCS	- Append an invalid FCS for testing purposes.
 *			  Applies only to regular AX.25.
 *
 * Outputs:	Bits are shipped out to the sender's tone generator.
 *
 * Returns:	Number of bits sent including "flags" and the
 *		stuffing bits.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *
 * Description:	For AX.25, send:
 *			start flag
 *			bit stuffed data
 *			calculated FCS
 *			end flag
 *		NRZI encoding for all but the "flags."
 *
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		the tone generator are processed correctly.
 *
 *--------------------------------------------------------------*/

func (s *HDLCSender) SendFrame(pp *ax25.Packet, badFCS bool) int {
	var achan = &s.audioConfig.achan[s.channel]

	if achan.layer2_xmit == LAYER2_IL2P { //nolint:staticcheck
		var n = s.sendIL2PFrame(pp, achan.il2p_version, achan.il2p_max_fec, achan.il2p_crc, achan.il2p_invert_polarity)
		if n > 0 {
			return n
		}

		logrus.WithField("channel", s.channel).Warn("Unable to send IL2P frame.  Falling back to regular AX.25.")
		// Not sure if we should fall back to AX.25 or not here.
	} else if achan.layer2_xmit == LAYER2_FX25 {
		var fbuf = pp.Pack()

		var n = s.sendFX25Frame(fbuf, achan.fx25_strength)
		if n > 0 {
			return n
		}

		logrus.WithField("channel", s.channel).Warn("Unable to send FX.25.  Falling back to regular AX.25.")
		// Definitely need to fall back to AX.25 here because
		// the FX.25 frame length is so limited.
	}

	var fbuf = pp.Pack()

	return s.sendAX25Frame(fbuf, badFCS)
}

/*-------------------------------------------------------------
 *
 * Name:	SendPreamblePostamble (layer2_preamble_postamble in Dire Wolf)
 *
 * Purpose:	Send filler pattern before and after the frame.
 *		For HDLC it is 01111110, for IL2P 01010101.
 *
 * Inputs:	nbytes	- Number of bytes to send.
 *
 *		finish	- True for end of transmission.
 *			  This causes the last audio buffer to be flushed.
 *
 * Outputs:	Bits are shipped out to the sender's tone generator.
 *
 * Returns:	Number of bits sent.
 *		There is no bit-stuffing so we would expect this to
 *		be 8 * nbytes.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		the tone generator are processed correctly.
 *
 *--------------------------------------------------------------*/

func (s *HDLCSender) SendPreamblePostamble(nbytes int, finish bool) int {
	s.bitsSent = 0

	logrus.WithFields(logrus.Fields{
		"channel": s.channel,
		"nbytes":  nbytes,
		"finish":  finish,
	}).Debug("layer2_preamble_postamble")

	// When the transmitter is on but not sending data, it should be sending
	// a stream of a filler pattern.
	// For AX.25, it is the 01111110 "flag" pattern with NRZI and no bit stuffing.
	// For IL2P, it is 01010101 without NRZI.

	var achan = &s.audioConfig.achan[s.channel]

	for range nbytes {
		if achan.layer2_xmit == LAYER2_IL2P {
			s.sendByteMSBFirst(IL2P_PREAMBLE, achan.il2p_invert_polarity)
		} else {
			s.sendControlNRZI(0x7e)
		}
	}

	/* Push out the final partial buffer! */

	if finish {
		s.flush()
	}

	return s.bitsSent
}

// sendAX25Frame is ax25_only_hdlc_send_frame in Dire Wolf.
func (s *HDLCSender) sendAX25Frame(fbuf []byte, badFCS bool) int {
	s.bitsSent = 0

	logrus.WithFields(logrus.Fields{
		"channel": s.channel,
		"flen":    len(fbuf),
		"bad_fcs": badFCS,
	}).Debug("hdlc_send_frame")

	s.sendControlNRZI(0x7e) /* Start frame */

	for j := range fbuf {
		s.sendDataNRZI(fbuf[j])
	}

	var frameFCS = fcs.Calc(fbuf)

	if badFCS {
		/* For testing only - Simulate a frame getting corrupted along the way. */
		s.sendDataNRZI(byte(^frameFCS & 0xff))
		s.sendDataNRZI(byte((^frameFCS)>>8) & 0xff)
	} else {
		s.sendDataNRZI(byte(frameFCS & 0xff))
		s.sendDataNRZI(byte((frameFCS >> 8) & 0xff))
	}

	s.sendControlNRZI(0x7e) /* End frame */

	return s.bitsSent
}

// The next one is only for IL2P.  No NRZI.
// MSB first, opposite of AX.25.
// NRZI would be applied for AX.25 but IL2P does not use it.
// However we do have an option to invert the signal.
// The direwolf receive implementation will automatically compensate
// for either polarity but other implementations might not.

func (s *HDLCSender) sendByteMSBFirst(x int, polarity int) {
	for range 8 {
		s.line.Write((x&0x80) != 0, polarity&1 != 0)

		x <<= 1
		s.bitsSent++
	}
}

// The following are only for HDLC.
// All bits are sent NRZI.
// Data (non flags) use bit stuffing.

func (s *HDLCSender) sendControlNRZI(x byte) {
	for range 8 {
		s.sendBitNRZI(x&1 != 0)
		x >>= 1
	}

	s.stuff = 0
}

func (s *HDLCSender) sendDataNRZI(x byte) {
	for range 8 {
		s.sendBitNRZI(x&1 != 0)

		if x&1 > 0 {
			s.stuff++
			if s.stuff == 5 {
				s.sendBitNRZI(false)
				s.stuff = 0
			}
		} else {
			s.stuff = 0
		}

		x >>= 1
	}
}

/*
 * NRZI encoding.
 * data 1 bit -> no change.
 * data 0 bit -> invert signal.
 */

func (s *HDLCSender) sendBitNRZI(b bool) {
	s.line.WriteNRZI(b)

	s.bitsSent++
}

/* end hdlc_send.c */
