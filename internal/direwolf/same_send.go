// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

/********************************************************************************
 *
 * Purpose:	Serialize EAS SAME for transmission.
 *
 *		SAME is not HDLC: it has no flags, bit stuffing or NRZI.
 *
 *******************************************************************************/

/*-------------------------------------------------------------------
 *
 * Name:        sendEAS (eas_send in Dire Wolf)
 *
 * Purpose:    	Serialize EAS SAME for transmission.
 *
 * Inputs:	str	- Character string to send.
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

func (s *HDLCSender) easPutByte(b byte) {
	for range 8 {
		s.putBit(int(b & 1))
		b >>= 1
	}
}

func (s *HDLCSender) sendEAS(str []byte, repeat int, txdelay int, txtail int) int {
	var bytes_sent = 0
	const gap = 1000
	var gaps_sent = 0

	s.putQuietMs(txdelay)

	for r := range repeat {
		for range 16 {
			s.easPutByte(0xAB)

			bytes_sent++
		}

		for _, p := range str {
			s.easPutByte(p)

			bytes_sent++
		}

		if r < repeat-1 {
			s.putQuietMs(gap)

			gaps_sent++
		}
	}

	s.putQuietMs(txtail)

	s.flush()

	var elapsed = txdelay + int(float64(bytes_sent)*8*1.92) + (gaps_sent * gap) + txtail

	// dw_printf ("DEBUG:  EAS total time = %d ms\n", elapsed);

	return (elapsed)
} /* end sendEAS */
