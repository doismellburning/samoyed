package direwolf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_dtmf(t *testing.T) {
	const c = 0 // radio channel.
	const sampleRate = 44100

	var my_audio_config audio_s

	my_audio_config.adev[ACHAN2ADEV(c)].samples_per_sec = sampleRate
	my_audio_config.achan[c].dtmf_decode = DTMF_DECODE_ON

	// A decoded button raises the channel's DCD, which goes to the HDLC
	// receiver; nothing here wants to hear about it.
	var origReceiver = hdlcReceiver

	t.Cleanup(func() { hdlcReceiver = origReceiver })

	hdlcReceiver = NewHDLCReceiver(&my_audio_config, new(discardReceiveSink))

	dtmf_init(&my_audio_config, 50)

	var result strings.Builder

	var push_button_test = func(_ int, button rune, ms int) {
		for dtmf := range dtmfButtonSamples(button, ms, sampleRate) {
			/* Make sure it is insensitive to signal amplitude. */
			/* (Uncomment each of below when testing.) */
			var x = dtmf_sample(c, dtmf)
			//x = dtmf_sample (c, dtmf * 1000);
			//x = dtmf_sample (c, dtmf * 0.001);

			if x != ' ' && x != '.' {
				result.WriteRune(x)
			}
		}
	}

	dw_printf("\nFirst, check all button tone pairs. \n\n")
	/* Max auto dialing rate is 10 per second. */

	push_button_test(c, '1', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '2', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '3', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, 'A', 50)
	push_button_test(c, ' ', 50)

	push_button_test(c, '4', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '5', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '6', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, 'B', 50)
	push_button_test(c, ' ', 50)

	push_button_test(c, '7', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '8', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '9', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, 'C', 50)
	push_button_test(c, ' ', 50)

	push_button_test(c, '*', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '0', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, '#', 50)
	push_button_test(c, ' ', 50)
	push_button_test(c, 'D', 50)
	push_button_test(c, ' ', 50)

	dw_printf("\nShould reject very short pulses.\n\n")

	push_button_test(c, '1', 20)
	push_button_test(c, ' ', 50)
	push_button_test(c, '1', 20)
	push_button_test(c, ' ', 50)
	push_button_test(c, '1', 20)
	push_button_test(c, ' ', 50)
	push_button_test(c, '1', 20)
	push_button_test(c, ' ', 50)
	push_button_test(c, '1', 20)
	push_button_test(c, ' ', 50)

	dw_printf("\nTest timeout after inactivity.\n\n")

	push_button_test(c, '1', 250)
	push_button_test(c, ' ', 500)
	push_button_test(c, '2', 250)
	push_button_test(c, ' ', 500)
	push_button_test(c, '3', 250)
	push_button_test(c, ' ', 5200)

	push_button_test(c, '7', 250)
	push_button_test(c, ' ', 500)
	push_button_test(c, '8', 250)
	push_button_test(c, ' ', 500)
	push_button_test(c, '9', 250)
	push_button_test(c, ' ', 5200)

	/* Check for expected results. */

	require.NotEqual(t, "123A456B789C*0#D123789", result.String(), "Time-out failed, otherwise OK")
	require.Equal(t, "123A456B789C*0#D123$789$", result.String())
}

// discardReceiveSink is a ReceiveSink that ignores whatever it is told.
type discardReceiveSink struct{}

func (discardReceiveSink) RecFrame(int, int, int, *packet_t, ALevel, fec_type_t, BitFixLevel, string) {
}

func (discardReceiveSink) DCDChange(int, int) {}
