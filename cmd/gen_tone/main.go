package main

// #include "direwolf.h"
// #include <stdio.h>
// #include <math.h>
// #include <unistd.h>
// #include <string.h>
// #include <stdlib.h>
// #include <assert.h>
// #include "audio.h"
// #include "gen_tone.h"
// #include "textcolor.h"
// #include "fsk_demod_state.h"	/* for MAX_FILTER_SIZE which might be overly generous for here* but safe if we use same size as for receive. */
// #include "dsp.h"
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0
import "C"

import (
	"fmt"

	_ "github.com/doismellburning/samoyed/src" // cgo
)

/*-------------------------------------------------------------------
 *
 * Name:        main
 *
 * Purpose:     Quick test program for generating tones
 *
 *--------------------------------------------------------------------*/

func main() {
	fmt.Println("Warning, known to fail with an assertion error, needs debugging and fixing.")

	const chan1 = 0
	const chan2 = 1

	/* to sound card */
	/* one channel.  2 times:  one second of each tone. */

	var my_audio_config C.struct_audio_s
	C.strcpy(&my_audio_config.adev[0].adevice_in[0], C.CString(C.DEFAULT_ADEVICE))
	C.strcpy(&my_audio_config.adev[0].adevice_out[0], C.CString(C.DEFAULT_ADEVICE))
	my_audio_config.chan_medium[0] = C.MEDIUM_RADIO // TODO KG ??

	C.audio_open(&my_audio_config)
	C.gen_tone_init(&my_audio_config, 100, 0)

	for range 2 {
		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			C.tone_gen_put_bit(chan1, 1)
		}

		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			C.tone_gen_put_bit(chan1, 0)
		}
	}

	C.audio_close()

	/* Now try stereo. */

	my_audio_config = C.struct_audio_s{} //nolint:exhaustruct
	C.strcpy(&my_audio_config.adev[0].adevice_in[0], C.CString(C.DEFAULT_ADEVICE))
	C.strcpy(&my_audio_config.adev[0].adevice_out[0], C.CString(C.DEFAULT_ADEVICE))
	my_audio_config.adev[0].num_channels = 2

	C.audio_open(&my_audio_config)
	C.gen_tone_init(&my_audio_config, 100, 0)

	for range 4 {
		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			C.tone_gen_put_bit(chan1, 1)
		}

		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			C.tone_gen_put_bit(chan1, 0)
		}

		for n := C.int(0); n < my_audio_config.achan[1].baud*2; n++ {
			C.tone_gen_put_bit(chan2, 1)
		}

		for n := C.int(0); n < my_audio_config.achan[1].baud*2; n++ {
			C.tone_gen_put_bit(chan2, 0)
		}
	}

	C.audio_close()
}
