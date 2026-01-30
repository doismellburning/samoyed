package direwolf

// #include <stdio.h>
// #include <string.h>
import "C"

import (
	"unsafe"
)

var number_of_bits_sent [MAX_RADIO_CHANS]C.int // Count number of bits sent by "hdlc_send_frame" or "hdlc_send_flags"

/*-------------------------------------------------------------
 *
 * Name:	layer2_send_frame
 *
 * Purpose:	Convert frames to a stream of bits.
 *		Originally this was for AX.25 only, hence the file name.
 *		Over time, FX.25 and IL2P were shoehorned in.
 *
 * Inputs:	channel	- Audio channel number, 0 = first.
 *
 *		pp	- Packet object.
 *
 *		bad_fcs	- Append an invalid FCS for testing purposes.
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

func layer2_send_frame(channel C.int, pp *packet_t, bad_fcs C.int, audio_config_p *audio_s) C.int {

	if audio_config_p.achan[channel].layer2_xmit == LAYER2_IL2P { //nolint:staticcheck

		var n = il2p_send_frame(channel, pp, C.int(audio_config_p.achan[channel].il2p_max_fec), C.int(audio_config_p.achan[channel].il2p_invert_polarity))
		if n > 0 {
			return (n)
		}
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to send IL2p frame.  Falling back to regular AX.25.\n")
		// Not sure if we should fall back to AX.25 or not here.
	} else if audio_config_p.achan[channel].layer2_xmit == LAYER2_FX25 {
		var fbuf [AX25_MAX_PACKET_LEN + 2]C.uchar
		var flen = ax25_pack(pp, &fbuf[0])
		var n = fx25_send_frame(channel, &fbuf[0], C.int(flen), C.int(audio_config_p.achan[channel].fx25_strength), false)
		if n > 0 {
			return (n)
		}
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to send FX.25.  Falling back to regular AX.25.\n")
		// Definitely need to fall back to AX.25 here because
		// the FX.25 frame length is so limited.
	}

	var fbuf [AX25_MAX_PACKET_LEN + 2]C.uchar
	var flen = ax25_pack(pp, &fbuf[0])
	return (ax25_only_hdlc_send_frame(channel, &fbuf[0], C.int(flen), bad_fcs))
}

func ax25_only_hdlc_send_frame(channel C.int, _fbuf *C.uchar, flen C.int, bad_fcs C.int) C.int {

	number_of_bits_sent[channel] = 0

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("hdlc_send_frame ( channel = %d, fbuf = %p, flen = %d, bad_fcs = %d)\n", channel, fbuf, flen, bad_fcs);
		fflush (stdout);
	#endif
	*/

	send_control_nrzi(channel, 0x7e) /* Start frame */

	var fbuf = C.GoBytes(unsafe.Pointer(_fbuf), flen)
	for j := C.int(0); j < flen; j++ {
		send_data_nrzi(channel, C.int(fbuf[j]))
	}

	var fcs = fcs_calc(_fbuf, flen)

	if bad_fcs > 0 {
		/* For testing only - Simulate a frame getting corrupted along the way. */
		send_data_nrzi(channel, C.int(^fcs)&0xff)
		send_data_nrzi(channel, C.int((^fcs)>>8)&0xff)
	} else {
		send_data_nrzi(channel, C.int(fcs)&0xff)
		send_data_nrzi(channel, C.int(fcs>>8)&0xff)
	}

	send_control_nrzi(channel, 0x7e) /* End frame */

	return (number_of_bits_sent[channel])
}

/*-------------------------------------------------------------
 *
 * Name:	layer2_preamble_postamble
 *
 * Purpose:	Send filler pattern before and after the frame.
 *		For HDLC it is 01111110, for IL2P 01010101.
 *
 * Inputs:	channel	- Audio channel number, 0 = first.
 *
 *		nbytes	- Number of bytes to send.
 *
 *		finish	- True for end of transmission.
 *			  This causes the last audio buffer to be flushed.
 *
 *		audio_config_p - Configuration for audio and modems.
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

func layer2_preamble_postamble(channel C.int, nbytes C.int, finish C.int, audio_config_p *audio_s) C.int {

	number_of_bits_sent[channel] = 0

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("hdlc_send_flags ( channel = %d, nflags = %d, finish = %d )\n", channel, nflags, finish);
		fflush (stdout);
	#endif
	*/

	// When the transmitter is on but not sending data, it should be sending
	// a stream of a filler pattern.
	// For AX.25, it is the 01111110 "flag" pattern with NRZI and no bit stuffing.
	// For IL2P, it is 01010101 without NRZI.

	for j := C.int(0); j < nbytes; j++ {
		if audio_config_p.achan[channel].layer2_xmit == LAYER2_IL2P {
			send_byte_msb_first(channel, IL2P_PREAMBLE, C.int(audio_config_p.achan[channel].il2p_invert_polarity))
		} else {
			send_control_nrzi(channel, 0x7e)
		}
	}

	/* Push out the final partial buffer! */

	if finish > 0 {
		audio_flush(ACHAN2ADEV(channel))
	}

	return (number_of_bits_sent[channel])
}

// The next one is only for IL2P.  No NRZI.
// MSB first, opposite of AX.25.

func send_byte_msb_first(channel C.int, x C.int, polarity C.int) {

	for i := 0; i < 8; i++ {
		var dbit C.int = 0
		if (x & 0x80) != 0 {
			dbit = 1
		}
		tone_gen_put_bit(int(channel), int(dbit^polarity)&1)
		x <<= 1
		number_of_bits_sent[channel]++
	}
}

// The following are only for HDLC.
// All bits are sent NRZI.
// Data (non flags) use bit stuffing.

var stuff [MAX_RADIO_CHANS]C.int // Count number of "1" bits to keep track of when we
// need to break up a long run by "bit stuffing."
// Needs to be array because we could be transmitting
// on multiple channels at the same time.

func send_control_nrzi(channel C.int, x C.int) {

	for i := 0; i < 8; i++ {
		send_bit_nrzi(channel, x&1)
		x >>= 1
	}

	stuff[channel] = 0
}

func send_data_nrzi(channel C.int, x C.int) {

	for i := 0; i < 8; i++ {
		send_bit_nrzi(channel, x&1)
		if x&1 > 0 {
			stuff[channel]++
			if stuff[channel] == 5 {
				send_bit_nrzi(channel, 0)
				stuff[channel] = 0
			}
		} else {
			stuff[channel] = 0
		}
		x >>= 1
	}
}

/*
 * NRZI encoding.
 * data 1 bit -> no change.
 * data 0 bit -> invert signal.
 */

var nrziBitOutput [MAX_RADIO_CHANS]int

func send_bit_nrzi(channel C.int, b C.int) {

	if b == 0 {
		nrziBitOutput[channel] = 1 - nrziBitOutput[channel]
	}

	tone_gen_put_bit(int(channel), nrziBitOutput[channel])

	number_of_bits_sent[channel]++
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

func eas_put_byte(channel C.int, b C.uchar) {
	for n := 0; n < 8; n++ {
		tone_gen_put_bit(int(channel), int(b&1))
		b >>= 1
	}
}

func eas_send(channel C.int, _str *C.uchar, repeat C.int, txdelay C.int, txtail C.int) C.int {
	var bytes_sent C.int = 0
	const gap C.int = 1000
	var gaps_sent C.int = 0

	gen_tone_put_quiet_ms(channel, txdelay)

	for r := C.int(0); r < repeat; r++ {
		for j := 0; j < 16; j++ {
			eas_put_byte(channel, 0xAB)
			bytes_sent++
		}

		var str = C.GoBytes(unsafe.Pointer(_str), C.int(C.strlen((*C.char)(unsafe.Pointer(_str)))))
		for _, p := range str {
			eas_put_byte(channel, C.uchar(p))
			bytes_sent++
		}

		if r < repeat-1 {
			gen_tone_put_quiet_ms(channel, gap)
			gaps_sent++
		}
	}

	gen_tone_put_quiet_ms(channel, txtail)

	audio_flush(ACHAN2ADEV(channel))

	var elapsed = txdelay + C.int(float64(bytes_sent)*8*1.92) + (gaps_sent * gap) + txtail

	// dw_printf ("DEBUG:  EAS total time = %d ms\n", elapsed);

	return (elapsed)

} /* end eas_send */

/* end hdlc_send.c */
