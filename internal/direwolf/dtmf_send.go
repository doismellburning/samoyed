package direwolf

// Sending DTMF, "touch tones", through a channel's tone generator.  The audio
// for each button comes from dtmfButtonSamples, beside the decoder in dtmf.go.

import (
	"github.com/sirupsen/logrus"
)

/*-------------------------------------------------------------------
 *
 * Name:        dtmf_send
 *
 * Purpose:    	Generate DTMF tones from text string.
 *
 * Inputs:	toneGenerator	- The channel's tone generator.
 *
 *		channel	- Radio channel number.
 *		str	- Character string to send.  0-9, A-D, *, #
 *		speed	- Number of tones per second.  Range 1 to 10.
 *		txdelay	- Delay (ms) from PTT to start.
 *		txtail	- Delay (ms) from end to PTT off.
 *
 * Returns:	Total number of milliseconds to activate PTT.
 *		This includes delays before the first tone
 *		and after the last to avoid chopping off part of it.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send DTMF.
 *
 *--------------------------------------------------------------------*/

func dtmf_send(toneGenerator *ToneGenerator, channel int, str string, speed int, txdelay int, txtail int) int {
	if toneGenerator == nil {
		logrus.WithField("channel", channel).Error("Invalid channel for tone generation")
	} else {
		toneGenerator.SendDTMF(str, speed, txdelay, txtail)
	}

	return (txdelay +
		int(1000.0*float64(len(str))/float64(speed)+0.5) +
		txtail)
} /* end dtmf_send */

// SendDTMF generates the tones for str, as dtmf_send describes, on the
// generator's channel.
func (tg *ToneGenerator) SendDTMF(str string, speed int, txdelay int, txtail int) {
	// Length of tone or gap between.
	var len_ms = int((500.0 / float64(speed)) + 0.5)

	tg.pushButton(' ', txdelay)

	for _, p := range str {
		tg.pushButton(p, len_ms)
		tg.pushButton(' ', len_ms)
	}

	tg.pushButton(' ', txtail)

	tg.Flush()
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
 * Outputs:	Audio is sent to radio.
 *
 *----------------------------------------------------------------*/

func (tg *ToneGenerator) pushButton(button rune, ms int) {
	var sampleRate = tg.audioConfig.adev[tg.adevIndex].samples_per_sec

	for dtmf := range dtmfButtonSamples(button, ms, sampleRate) {
		// 'dtmf' can be in range of +-2.0 because it is sum of two sine waves.
		// Amplitude of 100 would use full +-32k range.
		tg.PutSample(int(dtmf * 16383.0 * float64(tg.amplitude) / 100.0))
	}
}
