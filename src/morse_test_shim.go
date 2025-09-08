package direwolf

// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <ctype.h>
// #include <time.h>
// #include <math.h>
// #include "textcolor.h"
// #include "audio.h"
// #include "ptt.h"
// #include "gen_tone.h"		/* for gen_tone_put_sample */
// #include "morse.h"
// extern int GEN_PACKETS;
import "C"

import (
	"testing"
)

func morseToFile(t *testing.T, filename string, message string) {
	t.Helper()

	// Copied from gen_packets without using all the CLI parsing...

	var modem C.struct_audio_s
	modem.adev[0].defined = 1
	modem.adev[0].num_channels = C.DEFAULT_NUM_CHANNELS
	modem.adev[0].samples_per_sec = C.DEFAULT_SAMPLES_PER_SEC
	modem.adev[0].bits_per_sample = C.DEFAULT_BITS_PER_SAMPLE
	for channel := range C.MAX_RADIO_CHANS {
		modem.achan[channel].modem_type = C.MODEM_AFSK
		modem.achan[channel].mark_freq = C.DEFAULT_MARK_FREQ
		modem.achan[channel].space_freq = C.DEFAULT_SPACE_FREQ
		modem.achan[channel].baud = C.DEFAULT_BAUD
	}
	modem.chan_medium[0] = C.MEDIUM_RADIO

	C.GEN_PACKETS = 1

	audio_file_open(filename, &modem)
	var amplitude C.int = 100
	C.gen_tone_init(&modem, amplitude, 1)
	morse_init(&modem, amplitude)
	morse_send(0, message, 10, 100, 100)
	audio_file_close() // I just realised this all works on globals :s
}
