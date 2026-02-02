package direwolf

// #include <stdlib.h>
// #include <stdio.h>
// #include <math.h>
// #include <assert.h>
// #include <string.h>
import "C"

import "testing"

func dtmf_test_main(t *testing.T) {
	t.Helper()

	var c C.int = 0 // radio channel.
	var my_audio_config audio_s

	my_audio_config.adev[ACHAN2ADEV(int(c))].defined = 1
	my_audio_config.adev[ACHAN2ADEV(int(c))].samples_per_sec = 44100
	my_audio_config.chan_medium[c] = MEDIUM_RADIO
	my_audio_config.achan[c].dtmf_decode = DTMF_DECODE_ON

	// Let's try to set up audio?
	my_audio_config.adev[ACHAN2ADEV(int(c))].num_channels = 1
	my_audio_config.adev[ACHAN2ADEV(int(c))].bits_per_sample = 8
	gen_tone_init(&my_audio_config, 100, 0)
	ptt_init(&my_audio_config)

	dtmf_init(&my_audio_config, C.int(50))

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

	push_button_test(c, '?', 0)
}
