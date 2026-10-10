// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package dtmf

// Sending DTMF, "touch tones": the audio for each button comes from
// buttonSamples, and goes to whatever SampleSink the caller hands over,
// typically a channel's tone generator.

// SampleSink is where Send puts the audio it generates.
type SampleSink interface {
	// PutSample ships out one audio sample, in the range of a signed 16 bit
	// integer.
	PutSample(sam int)

	// Flush pushes out whatever audio is still buffered.
	Flush()
}

/*-------------------------------------------------------------------
 *
 * Name:        Send
 *
 * Purpose:    	Generate DTMF tones from text string.
 *
 * Inputs:	out	- Where the audio goes.
 *
 *		sampleRate - Samples per second of out.
 *		amplitude - Signal amplitude on scale of 0 .. 100.
 *		str	- Character string to send.  0-9, A-D, *, #
 *		speed	- Number of tones per second.  Range 1 to 10.
 *		txdelay	- Delay (ms) from PTT to start.
 *		txtail	- Delay (ms) from end to PTT off.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send DTMF.
 *		Duration says how long it takes, PTT included.
 *
 *--------------------------------------------------------------------*/

func Send(out SampleSink, sampleRate int, amplitude int, str string, speed int, txdelay int, txtail int) {
	// Length of tone or gap between.
	var len_ms = int((500.0 / float64(speed)) + 0.5)

	pushButton(out, sampleRate, amplitude, ' ', txdelay)

	for _, p := range str {
		pushButton(out, sampleRate, amplitude, p, len_ms)
		pushButton(out, sampleRate, amplitude, ' ', len_ms)
	}

	pushButton(out, sampleRate, amplitude, ' ', txtail)

	out.Flush()
}

// Duration returns the total number of milliseconds to activate PTT to Send
// str at speed.  This includes delays before the first tone and after the last
// to avoid chopping off part of it.
func Duration(str string, speed int, txdelay int, txtail int) int {
	return (txdelay +
		int(1000.0*float64(len(str))/float64(speed)+0.5) +
		txtail)
}

/*------------------------------------------------------------------
 *
 * Name:        pushButton
 *
 * Purpose:     Generate DTMF tone for a button push.
 *
 * Inputs:	button	- One of 0-9, A-D, *, #.  Others result in silence.
 *
 *		ms	- Duration in milliseconds.
 *			  Use 50 ms for tone and 50 ms of silence for max rate of 10 per second.
 *
 * Outputs:	Audio is sent to out.
 *
 *----------------------------------------------------------------*/

func pushButton(out SampleSink, sampleRate int, amplitude int, button rune, ms int) {
	for sample := range buttonSamples(button, ms, sampleRate) {
		// 'sample' can be in range of +-2.0 because it is sum of two sine waves.
		// Amplitude of 100 would use full +-32k range.
		out.PutSample(int(sample * 16383.0 * float64(amplitude) / 100.0))
	}
}
