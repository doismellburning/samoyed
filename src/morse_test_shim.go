package direwolf

// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <ctype.h>
// #include <time.h>
// #include <math.h>
import "C"

import (
	"testing"
)

func morseToFile(t *testing.T, filename string, message string) {
	t.Helper()

	// Copied from gen_packets without using all the CLI parsing...

	var modem audio_s
	modem.adev[0].defined = 1
	modem.adev[0].num_channels = DEFAULT_NUM_CHANNELS
	modem.adev[0].samples_per_sec = DEFAULT_SAMPLES_PER_SEC
	modem.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE
	for channel := range MAX_RADIO_CHANS {
		modem.achan[channel].modem_type = MODEM_AFSK
		modem.achan[channel].mark_freq = DEFAULT_MARK_FREQ
		modem.achan[channel].space_freq = DEFAULT_SPACE_FREQ
		modem.achan[channel].baud = DEFAULT_BAUD
	}
	modem.chan_medium[0] = MEDIUM_RADIO

	GEN_PACKETS = true

	audio_file_open(filename, &modem)
	var amplitude = 100
	gen_tone_init(&modem, amplitude, true)
	morse_init(&modem, C.int(amplitude))
	morse_send(0, message, 10, 100, 100)
	audio_file_close() // I just realised this all works on globals :s
}
