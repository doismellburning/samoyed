package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <math.h>
// #include <assert.h>
// #include <string.h>
// #include "dtmf.h"
// #include "hdlc_rec.h"	// for dcd_change
// #include "textcolor.h"
// #include "gen_tone.h"
// void push_button_test (int chan, char button, int ms);
// void ptt_init (struct audio_s *p_modem);
import "C"

import "testing"

func push_button(channel int, button rune, ms int) {
	C.push_button_test(C.int(channel), C.char(button), C.int(ms))
}

// #define ACHAN2ADEV(n) ((n)>>1)
func ACHAN2ADEV(n int) int {
	return n >> 1
}

func dtmf_test_main(t *testing.T) {
	t.Helper()

	var c = 0 // radio channel.
	var my_audio_config C.struct_audio_s

	my_audio_config.adev[ACHAN2ADEV(c)].defined = 1
	my_audio_config.adev[ACHAN2ADEV(c)].samples_per_sec = 44100
	my_audio_config.chan_medium[c] = C.MEDIUM_RADIO
	my_audio_config.achan[c].dtmf_decode = C.DTMF_DECODE_ON

	// Let's try to set up audio?
	my_audio_config.adev[ACHAN2ADEV(c)].num_channels = 1
	my_audio_config.adev[ACHAN2ADEV(c)].bits_per_sample = 8
	C.gen_tone_init(&my_audio_config, 100, 0)
	C.ptt_init(&my_audio_config)

	C.dtmf_init(&my_audio_config, C.int(50))

	dw_printf("\nFirst, check all button tone pairs. \n\n")
	/* Max auto dialing rate is 10 per second. */

	push_button(c, '1', 50)
	push_button(c, ' ', 50)
	push_button(c, '2', 50)
	push_button(c, ' ', 50)
	push_button(c, '3', 50)
	push_button(c, ' ', 50)
	push_button(c, 'A', 50)
	push_button(c, ' ', 50)

	push_button(c, '4', 50)
	push_button(c, ' ', 50)
	push_button(c, '5', 50)
	push_button(c, ' ', 50)
	push_button(c, '6', 50)
	push_button(c, ' ', 50)
	push_button(c, 'B', 50)
	push_button(c, ' ', 50)

	push_button(c, '7', 50)
	push_button(c, ' ', 50)
	push_button(c, '8', 50)
	push_button(c, ' ', 50)
	push_button(c, '9', 50)
	push_button(c, ' ', 50)
	push_button(c, 'C', 50)
	push_button(c, ' ', 50)

	push_button(c, '*', 50)
	push_button(c, ' ', 50)
	push_button(c, '0', 50)
	push_button(c, ' ', 50)
	push_button(c, '#', 50)
	push_button(c, ' ', 50)
	push_button(c, 'D', 50)
	push_button(c, ' ', 50)

	dw_printf("\nShould reject very short pulses.\n\n")

	push_button(c, '1', 20)
	push_button(c, ' ', 50)
	push_button(c, '1', 20)
	push_button(c, ' ', 50)
	push_button(c, '1', 20)
	push_button(c, ' ', 50)
	push_button(c, '1', 20)
	push_button(c, ' ', 50)
	push_button(c, '1', 20)
	push_button(c, ' ', 50)

	dw_printf("\nTest timeout after inactivity.\n\n")

	push_button(c, '1', 250)
	push_button(c, ' ', 500)
	push_button(c, '2', 250)
	push_button(c, ' ', 500)
	push_button(c, '3', 250)
	push_button(c, ' ', 5200)

	push_button(c, '7', 250)
	push_button(c, ' ', 500)
	push_button(c, '8', 250)
	push_button(c, ' ', 500)
	push_button(c, '9', 250)
	push_button(c, ' ', 5200)

	/* Check for expected results. */

	push_button(c, '?', 0)
}
