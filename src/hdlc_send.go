package direwolf

import (
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/sirupsen/logrus"
)

// HDLCSender turns frames into the bits one radio channel sends.  It holds
// what has to carry over from one byte, or one frame, to the next: the NRZI
// line level and the run of ones that decides when to bit stuff.  Each
// channel wants its own, and only one goroutine may drive it at a time.
type HDLCSender struct {
	channel     int
	audioConfig *audio_s

	bitsSent int // Count number of bits sent by SendFrame or SendPreamblePostamble.

	// Count number of "1" bits to keep track of when we need to break up a
	// long run by "bit stuffing."
	stuff int

	nrziOutput int // The level the line was last left at.
}

// NewHDLCSender makes an HDLCSender for channel, sending the layer 2
// protocol audioConfig says to use there.
func NewHDLCSender(channel int, audioConfig *audio_s) *HDLCSender {
	var s = new(HDLCSender)
	s.channel = channel
	s.audioConfig = audioConfig

	return s
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
 * Outputs:	Bits are shipped out by calling tone_gen_put_bit().
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
 *		tone_gen_put_bit() are processed correctly.
 *
 *--------------------------------------------------------------*/

func (s *HDLCSender) SendFrame(pp *packet_t, badFCS bool) int {
	var achan = &s.audioConfig.achan[s.channel]

	if achan.layer2_xmit == LAYER2_IL2P { //nolint:staticcheck
		var n = il2p_send_frame(s.channel, pp, achan.il2p_version, achan.il2p_max_fec, achan.il2p_invert_polarity)
		if n > 0 {
			return n
		}

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to send IL2p frame.  Falling back to regular AX.25.\n")
		// Not sure if we should fall back to AX.25 or not here.
	} else if achan.layer2_xmit == LAYER2_FX25 {
		var fbuf = AX25Pack(pp)

		var n = FX25SendFrame(s.channel, fbuf, achan.fx25_strength)
		if n > 0 {
			return n
		}

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to send FX.25.  Falling back to regular AX.25.\n")
		// Definitely need to fall back to AX.25 here because
		// the FX.25 frame length is so limited.
	}

	var fbuf = AX25Pack(pp)

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
 * Outputs:	Bits are shipped out by calling tone_gen_put_bit().
 *
 * Returns:	Number of bits sent.
 *		There is no bit-stuffing so we would expect this to
 *		be 8 * nbytes.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		tone_gen_put_bit() are processed correctly.
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
		gen_tone_flush(s.channel)
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
		s.sendDataNRZI(byte(^frameFCS) & 0xff)
		s.sendDataNRZI(byte((^frameFCS)>>8) & 0xff)
	} else {
		s.sendDataNRZI(byte(frameFCS) & 0xff)
		s.sendDataNRZI(byte(frameFCS>>8) & 0xff)
	}

	s.sendControlNRZI(0x7e) /* End frame */

	return s.bitsSent
}

// The next one is only for IL2P.  No NRZI.
// MSB first, opposite of AX.25.

func (s *HDLCSender) sendByteMSBFirst(x int, polarity int) {
	for range 8 {
		var dbit = 0
		if (x & 0x80) != 0 {
			dbit = 1
		}

		tone_gen_put_bit(s.channel, (dbit^polarity)&1)

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
	if !b {
		s.nrziOutput = 1 - s.nrziOutput
	}

	tone_gen_put_bit(s.channel, s.nrziOutput)

	s.bitsSent++
}

//  The rest of this is for EAS SAME.
//  This is sort of a logical place because it serializes a frame, but not in HDLC.
//  We have a parallel where SAME deserialization is in hdlc_rec.
//  Maybe both should be pulled out and moved to a same.c.

/*-------------------------------------------------------------------
 *
 * Name:        eas_send
 *
 * Purpose:    	Serialize EAS SAME for transmission.
 *
 * Inputs:	channel	- Radio channel number.
 *		str	- Character string to send.
 *		repeat	- Number of times to repeat with 1 sec quiet between.
 *		txdelay	- Delay (ms) from PTT to first preamble bit.
 *		txtail	- Delay (ms) from last data bit to PTT off.
 *
 *
 * Returns:	Total number of milliseconds to activate PTT.
 *		This includes delays before the first character
 *		and after the last to avoid chopping off part of it.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send EAS SAME
 *		code.
 *
 *--------------------------------------------------------------------*/

func eas_put_byte(channel int, b byte) {
	for range 8 {
		tone_gen_put_bit(channel, int(b&1))
		b >>= 1
	}
}

func eas_send(channel int, str []byte, repeat int, txdelay int, txtail int) int {
	var bytes_sent = 0
	const gap = 1000
	var gaps_sent = 0

	gen_tone_put_quiet_ms(channel, txdelay)

	for r := range repeat {
		for range 16 {
			eas_put_byte(channel, 0xAB)

			bytes_sent++
		}

		for _, p := range str {
			eas_put_byte(channel, p)

			bytes_sent++
		}

		if r < repeat-1 {
			gen_tone_put_quiet_ms(channel, gap)

			gaps_sent++
		}
	}

	gen_tone_put_quiet_ms(channel, txtail)

	gen_tone_flush(channel)

	var elapsed = txdelay + int(float64(bytes_sent)*8*1.92) + (gaps_sent * gap) + txtail

	// dw_printf ("DEBUG:  EAS total time = %d ms\n", elapsed);

	return (elapsed)
} /* end eas_send */

/* end hdlc_send.c */
