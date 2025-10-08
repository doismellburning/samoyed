/* Test fixture for the Dire Wolf demodulators */
package direwolf

/*-------------------------------------------------------------------
 *
 * Purpose:     Test fixture for the Dire Wolf demodulators.
 *
 * Inputs:	Takes audio from a .WAV file instead of the audio device.
 *
 * Description:	This can be used to test the demodulators under
 *		controlled and reproducible conditions for tweaking.
 *
 *		For example
 *
 *		(1) Download WA8LMF's TNC Test CD image file from
 *			http://wa8lmf.net/TNCtest/index.htm
 *
 *		(2) Burn a physical CD.
 *
 *		(3) "Rip" the desired tracks with Windows Media Player.
 *			Select .WAV file format.
 *
 *		"Track 2" is used for most tests because that is more
 *		realistic for most people using the speaker output.
 *
 *
 * 	Without ONE_CHAN defined:
 *
 *	  Notice that the number of packets decoded, as reported by
 *	  this test program, will be twice the number expected because
 *	  we are decoding the left and right audio channels separately.
 *
 *
 * 	With ONE_CHAN defined:
 *
 *	  Only process one channel.
 *
 *--------------------------------------------------------------------*/

// #define X 1
// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <time.h>
// #include <getopt.h>
// #include <ctype.h>
// #include <math.h>
// #include "audio.h"
// #include "demod.h"
// #include "multi_modem.h"
// #include "textcolor.h"
// #include "ax25_pad.h"
// #include "hdlc_rec2.h"
// #include "dlq.h"
// #include "ptt.h"
// #include "fx25.h"
// #include "il2p.h"
// #include "hdlc_rec.h"
// int audio_get_real (int a);
// int get_input_real (int it, int chan);
// void ptt_set_real (int ot, int chan, int ptt_signal);
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unsafe"

	"github.com/spf13/pflag"
)

type atest_header_t struct {
	riff     [4]C.char /* "RIFF" */
	filesize C.int     /* file length - 8 */
	wave     [4]C.char /* "WAVE" */
}

type atest_chunk_t struct {
	id       [4]C.char /* "LIST" or "fmt " */
	datasize C.int
}

type atest_format_t struct {
	wformattag      C.short /* 1 for PCM. */
	nchannels       C.short /* 1 for mono, 2 for stereo. */
	nsamplespersec  C.int   /* sampling freq, Hz. */
	navgbytespersec C.int   /* = nblockalign*nsamplespersec. */
	nblockalign     C.short /* = wbitspersample/8 * nchannels. */
	wbitspersample  C.short /* 16 or 8. */
	extras          [4]C.char
}

type atest_wav_data_t struct {
	data     [4]C.char /* "data" */
	datasize C.int
}

var ATEST_C = false

var header atest_header_t
var chunk atest_chunk_t
var format atest_format_t
var wav_data atest_wav_data_t

var atestFP *C.FILE // FIXME KG WAS fp
var e_o_f bool
var packets_decoded_one = 0
var packets_decoded_total = 0
var decimate = 0 /* Reduce that sampling rate if set. 1 = normal, 2 = half, 3 = 1/3, etc. */
var upsample = 0 /* Upsample for G3RUH decoder. Non-zero will override the default. */

var my_audio_config *C.struct_audio_s

var space_gain [MAX_SUBCHANS]C.float

var sample_number = -1 /* Sample number from the file. */
/* Incremented only for channel 0. */
/* Use to print timestamp, relative to beginning */
/* of file, when frame was decoded. */

// command line options.

var h_opt = false // Hexadecimal display of received packet.
var d_o_opt = 0   // "-d o" option for DCD output control. */
var dcd_count = 0
var dcd_missing_errors = 0

const EXPERIMENT_G = true
const EXPERIMENT_H = true

func AtestMain() {
	ATEST_C = true

	var count [C.MAX_SUBCHANS]int // Experiments G and H

	C.text_color_init(1)
	C.text_color_set(C.DW_COLOR_INFO)

	my_audio_config = (*C.struct_audio_s)(C.malloc(C.sizeof_struct_audio_s))

	/*
	 * First apply defaults.
	 */

	my_audio_config.adev[0].num_channels = C.DEFAULT_NUM_CHANNELS
	my_audio_config.adev[0].samples_per_sec = C.DEFAULT_SAMPLES_PER_SEC
	my_audio_config.adev[0].bits_per_sample = C.DEFAULT_BITS_PER_SAMPLE

	for channel := range C.MAX_RADIO_CHANS {
		my_audio_config.achan[channel].modem_type = C.MODEM_AFSK

		my_audio_config.achan[channel].mark_freq = C.DEFAULT_MARK_FREQ
		my_audio_config.achan[channel].space_freq = C.DEFAULT_SPACE_FREQ
		my_audio_config.achan[channel].baud = C.DEFAULT_BAUD

		C.strcpy(&my_audio_config.achan[channel].profiles[0], C.CString("A"))

		my_audio_config.achan[channel].num_freq = 1
		my_audio_config.achan[channel].offset = 0

		my_audio_config.achan[channel].fix_bits = C.RETRY_NONE

		my_audio_config.achan[channel].sanity_test = C.SANITY_APRS
		// my_audio_config.achan[channel].sanity_test = C.SANITY_AX25;
		// my_audio_config.achan[channel].sanity_test = C.SANITY_NONE;

		my_audio_config.achan[channel].passall = 0
		// my_audio_config.achan[channel].passall = 1;
	}

	var bitrateStr = pflag.StringP("bitrate", "B", strconv.Itoa(C.DEFAULT_BAUD), `Bits/second for data.  Proper modem automatically selected for speed.
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`)
	var g3ruh = pflag.BoolP("g3ruh", "g", false, "Use G3RUH modem rather than default for data rate.")
	var direwolf15compat = pflag.BoolP("direwolf-15-compat", "j", false, "2400 bps QPSK compatible with direwolf <= 1.5.")
	var mfj2400compat = pflag.BoolP("mfj-2400-compat", "J", false, "2400 bps QPSK compatible with MFJ-2400.")
	var modemProfile = pflag.StringP("modem-profile", "P", "", "Select the demodulator type such as D (default for 300 bps), E+ (default for 1200 bps), PQRS for 2400 bps, etc.")
	var decimate = pflag.IntP("decimate", "D", 0, "Divide audio sample rate by n. 0 is auto-select.")
	var upsample = pflag.IntP("upsample", "U", 0, "Upsample for G3RUH to improve performance when the sample rate to baud ratio is low.")
	var fixBits = pflag.IntP("fix-bits", "F", 0, `Amount of effort to try fixing frames with an invalid CRC.
0 (default) = consider only correct frames.
1 = Try to fix only a sigle bit.
Higher values = Try modifying more bits to get a good CRC.`)
	var errorIfLessThan = pflag.IntP("error-if-less-than", "L", -1, "Error if less than this number decoded.")
	var errorIfGreaterThan = pflag.IntP("error-if-greater-than", "G", -1, "Error if greater than this number decoded.")
	var channel0 = pflag.BoolP("channel-0", "0", true, "Use channel 0 (left) of stereo audio.")
	var channel1 = pflag.BoolP("channel-1", "1", false, "Use channel 1 (right) of stereo audio.")
	var channel2 = pflag.BoolP("channel-2", "2", false, "Use both channels of stereo audio.")
	var hexDisplay = pflag.BoolP("hex-display", "h", false, "Print frame contents as hexadecimal bytes.")
	var bitErrorRate = pflag.Float64P("bit-error-rate", "e", 0.0, "Receive Bit Error Rate (BER).")
	var debugFlags = pflag.StringSliceP("debug", "d", []string{}, `Debug (repeat for increased verbosity).
x = FX.25
o = DCD output control
2 = IL2P`)
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s is a test application which decodes AX.25 frames from audio recordings.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "This provides an easy way to test decoding performance and functionality much quicker than normal real-time.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... <WAV FILE>...\n", os.Args[0])
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ gen_packets -o test1.wav\n")
		fmt.Fprintf(os.Stderr, "$ atest test1.wav\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ gen_packets -B 300 -o test3.wav\n")
		fmt.Fprintf(os.Stderr, "$ atest -B 300 test3.wav\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ gen_packets -B 9600 -o test9.wav\n")
		fmt.Fprintf(os.Stderr, "$ atest -B 9600 test9.wav\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Try different combinations of options to compare decoding performance.\n")
	}

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	var d_x_opt = C.int(0)
	var d_2_opt = C.int(0)
	for _, debugFlag := range *debugFlags {
		switch debugFlag {
		case "x":
			d_x_opt++
		case "o":
			d_o_opt++
		case "2":
			d_2_opt++
		default:
			fmt.Fprintf(os.Stderr, "Unrecognised debug flag: %s\n", debugFlag)
			pflag.Usage()
			os.Exit(1)
		}
	}

	if *decimate < 0 || *decimate > 8 {
		fmt.Fprintf(os.Stderr, "Decimate should be between 0 and 8 inclusive, not %d.\n", *decimate)
		pflag.Usage()
		os.Exit(1)
	}
	my_audio_config.achan[0].decimate = C.int(*decimate)

	if *upsample != 0 {
		if *upsample < 1 || *upsample > 8 {
			fmt.Fprintf(os.Stderr, "Upsample should be between 1 and 4 inclusive, not %d.\n", *upsample)
			pflag.Usage()
			os.Exit(1)
		}
		my_audio_config.achan[0].upsample = C.int(*upsample)
	}

	if *fixBits < C.RETRY_NONE || *fixBits > C.RETRY_MAX {
		fmt.Fprintf(os.Stderr, "Fix Bits should be between %d and %d inclusive, not %d.\n", C.RETRY_NONE, C.RETRY_MAX, *fixBits)
		pflag.Usage()
		os.Exit(1)
	}
	my_audio_config.achan[0].fix_bits = uint32(*fixBits)

	var channelFlagCount int
	for _, b := range []bool{*channel0, *channel1, *channel2} {
		if b {
			channelFlagCount++
		}
	}
	if channelFlagCount != 1 {
		fmt.Fprintf(os.Stderr, "Exactly one of left/right/both channels must be selected.\n")
		pflag.Usage()
		os.Exit(1)
	}
	var decode_only = 0 /* Set to 0 or 1 to decode only one channel.  2 for both.  */
	if *channel0 {
		decode_only = 0
	}
	if *channel1 {
		decode_only = 1
	}
	if *channel2 {
		decode_only = 2
	}

	my_audio_config.recv_ber = C.float(*bitErrorRate)

	// Options from atest.c
	if *hexDisplay {
		h_opt = true
	}

	// Hacks for the magic strings
	var bitrate, bitrateParseErr = strconv.Atoi(*bitrateStr)
	if *bitrateStr == "AIS" {
		bitrate = 0xA15A15
	} else if *bitrateStr == "EAS" {
		bitrate = 0xEA5EA5
	} else if bitrateParseErr != nil {
		fmt.Fprintf(os.Stderr, "Invalid bitrate (should be an integer or 'AIS' or 'EAS'): %s\n", *bitrateStr)
		pflag.Usage()
		os.Exit(1)
	}

	/*
	 * Set modem type based on data rate.
	 * (Could be overridden by -g, -j, or -J later.)
	 */
	/*    300 implies 1600/1800 AFSK. */
	/*    1200 implies 1200/2200 AFSK. */
	/*    2400 implies V.26 QPSK. */
	/*    4800 implies V.27 8PSK. */
	/*    9600 implies G3RUH baseband scrambled. */

	my_audio_config.achan[0].baud = C.int(bitrate)

	/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
	/* that need to be kept in sync.  Maybe it could be a common function someday. */

	if my_audio_config.achan[0].baud == 100 { // What was this for?
		my_audio_config.achan[0].modem_type = C.MODEM_AFSK
		my_audio_config.achan[0].mark_freq = 1615
		my_audio_config.achan[0].space_freq = 1785
	} else if my_audio_config.achan[0].baud < 600 { // e.g. HF SSB packet
		my_audio_config.achan[0].modem_type = C.MODEM_AFSK
		my_audio_config.achan[0].mark_freq = 1600
		my_audio_config.achan[0].space_freq = 1800
		// Previously we had a "D" which was fine tuned for 300 bps.
		// In v1.7, it's not clear if we should use "B" or just stick with "A".
	} else if my_audio_config.achan[0].baud < 1800 { // common 1200
		my_audio_config.achan[0].modem_type = C.MODEM_AFSK
		my_audio_config.achan[0].mark_freq = C.DEFAULT_MARK_FREQ
		my_audio_config.achan[0].space_freq = C.DEFAULT_SPACE_FREQ
	} else if my_audio_config.achan[0].baud < 3600 {
		my_audio_config.achan[0].modem_type = C.MODEM_QPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(""))
	} else if my_audio_config.achan[0].baud < 7200 {
		my_audio_config.achan[0].modem_type = C.MODEM_8PSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(""))
	} else if my_audio_config.achan[0].baud == 0xA15A15 { // Hack for different use of 9600
		my_audio_config.achan[0].modem_type = C.MODEM_AIS
		my_audio_config.achan[0].baud = 9600
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(" ")) // avoid getting default later.
	} else if my_audio_config.achan[0].baud == 0xEA5EA5 {
		my_audio_config.achan[0].modem_type = C.MODEM_EAS
		my_audio_config.achan[0].baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		my_audio_config.achan[0].mark_freq = 2083  // Actually 2083.3 - logic 1.
		my_audio_config.achan[0].space_freq = 1563 // Actually 1562.5 - logic 0.
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString("A"))
	} else {
		my_audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(" ")) // avoid getting default later.
	}

	if my_audio_config.achan[0].baud < C.MIN_BAUD || my_audio_config.achan[0].baud > C.MAX_BAUD {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("Use a more reasonable bit rate in range of %d - %d.\n", C.MIN_BAUD, C.MAX_BAUD)
		os.Exit(1)
	}

	/*
	 * -g option means force g3RUH regardless of speed.
	 */

	if *g3ruh {
		my_audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(" ")) // avoid getting default later.
	}

	/*
	 * We have two different incompatible flavors of V.26.
	 */
	if *direwolf15compat {
		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j

		my_audio_config.achan[0].v26_alternative = C.V26_A
		my_audio_config.achan[0].modem_type = C.MODEM_QPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].baud = 2400
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(""))
	}
	if *mfj2400compat {
		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J

		my_audio_config.achan[0].v26_alternative = C.V26_B
		my_audio_config.achan[0].modem_type = C.MODEM_QPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].baud = 2400
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(""))
	}

	// Needs to be after -B, -j, -J.
	if *modemProfile != "" {
		fmt.Printf("Demodulator profile set to \"%s\"\n", *modemProfile)
		C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(*modemProfile))
	}

	C.memcpy(unsafe.Pointer(&my_audio_config.achan[1]), unsafe.Pointer(&my_audio_config.achan[0]), C.sizeof_struct_achan_param_s)

	if len(pflag.Args()) == 0 {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("Specify .WAV file name on command line.\n\n")
		pflag.Usage()
		os.Exit(1)
	}

	C.fx25_init(d_x_opt)
	C.il2p_init(d_2_opt)

	var start_time = time.Now()
	var total_filetime C.double
	var packets_decoded_total = 0

	for _, wavFileName := range pflag.Args() {
		atestFP = C.fopen(C.CString(wavFileName), C.CString("rb"))
		if atestFP == nil {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Couldn't open file for read: %s\n", wavFileName)
			// perror ("more info?");
			os.Exit(1)
		}

		/*
		 * Read the file header.
		 * Doesn't handle all possible cases but good enough for our purposes.
		 */

		C.fread(unsafe.Pointer(&header), 12, 1, atestFP)

		if C.strncmp(&header.riff[0], C.CString("RIFF"), 4) != 0 || C.strncmp(&header.wave[0], C.CString("WAVE"), 4) != 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("This is not a .WAV format file.\n")
			os.Exit(1)
		}

		C.fread(unsafe.Pointer(&chunk), 8, 1, atestFP)

		if C.strncmp(&chunk.id[0], C.CString("LIST"), 4) == 0 {
			C.fseek(atestFP, C.long(chunk.datasize), C.SEEK_CUR)
			C.fread(unsafe.Pointer(&chunk), 8, 1, atestFP)
		}

		if C.strncmp(&chunk.id[0], C.CString("fmt "), 4) != 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("WAV file error: Found \"%4.4s\" where \"fmt \" was expected.\n", C.GoString(&chunk.id[0]))
			os.Exit(1)
		}
		if chunk.datasize != 16 && chunk.datasize != 18 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("WAV file error: Need fmt chunk datasize of 16 or 18.  Found %d.\n", chunk.datasize)
			os.Exit(1)
		}

		C.fread(unsafe.Pointer(&format), C.ulong(chunk.datasize), 1, atestFP)

		C.fread(unsafe.Pointer(&wav_data), 8, 1, atestFP)

		if C.strncmp(&wav_data.data[0], C.CString("data"), 4) != 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("WAV file error: Found \"%4.4s\" where \"data\" was expected.\n", C.GoString(&wav_data.data[0]))
			os.Exit(1)
		}

		if format.wformattag != 1 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Sorry, I only understand audio format 1 (PCM).  This file has %d.\n", format.wformattag)
			os.Exit(1)
		}

		if format.nchannels != 1 && format.nchannels != 2 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Sorry, I only understand 1 or 2 channels.  This file has %d.\n", format.nchannels)
			os.Exit(1)
		}

		if format.wbitspersample != 8 && format.wbitspersample != 16 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Sorry, I only understand 8 or 16 bits per sample.  This file has %d.\n", format.wbitspersample)
			os.Exit(1)
		}

		my_audio_config.adev[0].samples_per_sec = format.nsamplespersec
		my_audio_config.adev[0].bits_per_sample = C.int(format.wbitspersample)
		my_audio_config.adev[0].num_channels = C.int(format.nchannels)

		my_audio_config.chan_medium[0] = C.MEDIUM_RADIO
		if format.nchannels == 2 {
			my_audio_config.chan_medium[1] = C.MEDIUM_RADIO
		}

		C.text_color_set(C.DW_COLOR_INFO)
		fmt.Printf("%d samples per second.  %d bits per sample.  %d audio channels.\n",
			my_audio_config.adev[0].samples_per_sec,
			my_audio_config.adev[0].bits_per_sample,
			(my_audio_config.adev[0].num_channels))
		// nnum_channels is known to be 1 or 2.
		var one_filetime = wav_data.datasize /
			((my_audio_config.adev[0].bits_per_sample / 8) * (my_audio_config.adev[0].num_channels) * my_audio_config.adev[0].samples_per_sec)
		total_filetime += C.double(one_filetime)

		fmt.Printf("%d audio bytes in file.  Duration = %.1f seconds.\n",
			wav_data.datasize,
			float64(one_filetime))
		fmt.Printf("Fix Bits level = %d\n", my_audio_config.achan[0].fix_bits)

		/*
		 * Initialize the AFSK demodulator and HDLC decoder.
		 * Needs to be done for each file because they could have different sample rates.
		 */
		multi_modem_init(my_audio_config)
		packets_decoded_one = 0

		e_o_f = false
		for !e_o_f {
			for c := C.int(0); c < (my_audio_config.adev[0].num_channels); c++ {
				/* This reads either 1 or 2 bytes depending on */
				/* bits per sample.  */

				var audio_sample = demod_get_sample(ACHAN2ADEV(c))

				if audio_sample >= 256*256 {
					e_o_f = true
					continue
				}

				if c == 0 {
					sample_number++
				}

				if decode_only == 0 && c != 0 {
					continue
				}
				if decode_only == 1 && c != 1 {
					continue
				}

				multi_modem_process_sample(c, audio_sample)
			}

			/* When a complete frame is accumulated, */
			/* process_rec_frame, below, is called. */
		}
		C.text_color_set(C.DW_COLOR_INFO)
		fmt.Printf("\n\n")

		if EXPERIMENT_G {
			for j := range C.MAX_SUBCHANS {
				var db = 20.0 * C.log10f(space_gain[j])
				fmt.Printf("%+.1f dB, %d\n", db, count[j])
			}
		}
		if EXPERIMENT_H {
			for j := range C.MAX_SUBCHANS {
				fmt.Printf("%d\n", count[j])
			}
		}

		fmt.Printf("%d from %s\n", packets_decoded_one, wavFileName)
		packets_decoded_total += packets_decoded_one

		C.fclose(atestFP)
	}

	var elapsed = time.Since(start_time)

	fmt.Printf("%d packets decoded in %.3f seconds.  %.1f x realtime\n", packets_decoded_total, elapsed.Seconds(), total_filetime/C.double(elapsed.Seconds()))
	if d_o_opt > 0 {
		fmt.Printf("DCD count = %d\n", dcd_count)
		fmt.Printf("DCD missing errors = %d\n", dcd_missing_errors)
	}

	if *errorIfLessThan != -1 && packets_decoded_total < *errorIfLessThan {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("\n * * * TEST FAILED: number decoded is less than %d * * * \n", *errorIfLessThan)
		os.Exit(1)
	}
	if *errorIfGreaterThan != -1 && packets_decoded_total > *errorIfGreaterThan {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("\n * * * TEST FAILED: number decoded is greater than %d * * * \n", *errorIfGreaterThan)
		os.Exit(1)
	}
}

/*
 * Simulate sample from the audio device.
 */

func audio_get_fake(a C.int) C.int {

	if wav_data.datasize <= 0 {
		e_o_f = true
		return (-1)
	}

	var ch = C.getc(atestFP)
	wav_data.datasize--

	if ch < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected end of file.\n")
		e_o_f = true
	}

	return (ch)
}

//export audio_get
func audio_get(a C.int) C.int {
	if ATEST_C {
		return audio_get_fake(a)
	} else {
		return audio_get_real(a)
	}
}

/*
 * This is called when we have a good frame.
 */

func dlq_rec_frame_fake(channel C.int, subchan C.int, slice C.int, pp C.packet_t, alevel C.alevel_t, fec_type C.fec_type_t, retries C.retry_t, spectrum *C.char) {

	packets_decoded_one++
	if C.hdlc_rec_data_detect_any(channel) == 0 {
		dcd_missing_errors++
	}

	var stemp [500]C.char
	ax25_format_addrs(pp, &stemp[0])

	var pinfo *C.uchar
	var info_len = ax25_get_info(pp, &pinfo)

	/* Print so we can see what is going on. */

	//TODO: quiet option - suppress packet printing, only the count at the end.

	/* Display audio input level. */
	/* Who are we hearing?   Original station or digipeater? */

	var h C.int
	var heard string
	if ax25_get_num_addr(pp) == 0 {
		/* Not AX.25. No station to display below. */
		h = -1
	} else {
		h = ax25_get_heard(pp)
		var _heard [2*AX25_MAX_ADDR_LEN + 20]C.char
		ax25_get_addr_with_ssid(pp, h, &_heard[0])
		heard = C.GoString(&_heard[0])
	}

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("\n")
	dw_printf("DECODED[%d] ", packets_decoded_one)

	/* Insert time stamp relative to start of file. */

	var sec = C.double(sample_number) / C.double(my_audio_config.adev[0].samples_per_sec)
	var minutes = int(sec / 60.)
	sec -= C.double(minutes * 60)

	dw_printf("%d:%06.3f ", minutes, sec)

	if h != AX25_SOURCE {
		dw_printf("Digipeater ")
	}

	var alevel_text [C.AX25_ALEVEL_TO_TEXT_SIZE]C.char
	ax25_alevel_to_text(alevel, &alevel_text[0])

	/* As suggested by KJ4ERJ, if we are receiving from */
	/* WIDEn-0, it is quite likely (but not guaranteed), that */
	/* we are actually hearing the preceding station in the path. */

	if h >= C.AX25_REPEATER_2 &&
		strings.HasPrefix(heard, "WIDE") &&
		unicode.IsDigit(rune(heard[4])) &&
		len(heard) == 5 {

		var probably_really [AX25_MAX_ADDR_LEN]C.char
		ax25_get_addr_with_ssid(pp, h-1, &probably_really[0])

		heard += " (probably " + C.GoString(&probably_really[0]) + ")"
	}

	switch fec_type {
	case C.fec_type_fx25:
		dw_printf("%s audio level = %s   FX.25  %s\n", heard, C.GoString(&alevel_text[0]), C.GoString(spectrum))
	case C.fec_type_il2p:
		dw_printf("%s audio level = %s   IL2P  %s\n", heard, C.GoString(&alevel_text[0]), C.GoString(spectrum))
	default:
		//case fec_type_none:
		if my_audio_config.achan[channel].fix_bits == RETRY_NONE && my_audio_config.achan[channel].passall == 0 {
			// No fix_bits or passall specified.
			dw_printf("%s audio level = %s     %s\n", heard, C.GoString(&alevel_text[0]), C.GoString(spectrum))
		} else {
			Assert(retries >= RETRY_NONE && retries <= C.RETRY_MAX) // validate array index.
			dw_printf("%s audio level = %s   [%s]   %s\n", heard, C.GoString(&alevel_text[0]), retry_text[int(retries)], C.GoString(spectrum))
		}
	}

	// Display non-APRS packets in a different color.

	// Display channel with subchannel/slice if applicable.

	if ax25_is_aprs(pp) != 0 {
		text_color_set(DW_COLOR_REC)
	} else {
		text_color_set(DW_COLOR_DEBUG)
	}

	if my_audio_config.achan[channel].num_subchan > 1 && my_audio_config.achan[channel].num_slicers == 1 {
		dw_printf("[%d.%d] ", channel, subchan)
	} else if my_audio_config.achan[channel].num_subchan == 1 && my_audio_config.achan[channel].num_slicers > 1 {
		dw_printf("[%d.%d] ", channel, slice)
	} else if my_audio_config.achan[channel].num_subchan > 1 && my_audio_config.achan[channel].num_slicers > 1 {
		dw_printf("[%d.%d.%d] ", channel, subchan, slice)
	} else {
		dw_printf("[%d] ", channel)
	}

	dw_printf("%s", C.GoString(&stemp[0])) /* stations followed by : */
	ax25_safe_print((*C.char)(unsafe.Pointer(pinfo)), info_len, 0)
	dw_printf("\n")

	/*
	 * -h option for hexadecimal display.  (new in 1.6)
	 */

	if h_opt {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		ax25_hex_dump(pp)
		dw_printf("------\n")
	}

	/*
		#if 0		// temp experiment

		#include "decode_aprs.h"

			if (ax25_is_aprs(pp)) {

			  decode_aprs_t A;

			  decode_aprs (&A, pp, 0, NULL);

			  // Temp experiment to see how different systems set the RR bits in the source and destination.
			  // log_rr_bits (&A, pp);

			}
		#endif
	*/

	ax25_delete(pp)

} /* end fake dlq_append */

var dcd_start_time [MAX_RADIO_CHANS]C.double

func ptt_set_fake(ot C.int, channel C.int, ptt_signal C.int) {
	// Should only get here for DCD output control.

	if d_o_opt > 0 {
		var t = C.double(sample_number) / C.double(my_audio_config.adev[0].samples_per_sec)

		text_color_set(DW_COLOR_INFO)

		if ptt_signal != 0 {
			//sec1 = t;
			//min1 = (int)(sec1 / 60.);
			//sec1 -= min1 * 60;
			//dw_printf ("DCD[%d] = ON    %d:%06.3f\n",  channel, min1, sec1);
			dcd_count++
			dcd_start_time[channel] = t
		} else {
			//dw_printf ("DCD[%d] = off   %d:%06.3f   %3.0f\n",  channel, min, sec, (t - dcd_start_time[channel]) * 1000.);

			var sec1 = dcd_start_time[channel]
			var min1 = (int)(sec1 / 60.)
			sec1 -= C.double(min1 * 60)

			var sec2 = t
			var min2 = (int)(sec2 / 60.)
			sec2 -= C.double(min2 * 60)

			dw_printf("DCD[%d]  %d:%06.3f - %d:%06.3f =  %3.0f\n", channel, min1, sec1, min2, sec2, (t-dcd_start_time[channel])*1000.)
		}
	}
}

//export ptt_set
func ptt_set(ot C.int, channel C.int, ptt_signal C.int) {
	if ATEST_C {
		ptt_set_fake(ot, channel, ptt_signal)
	} else {
		ptt_set_real(ot, channel, ptt_signal)
	}
}

func get_input_fake(it C.int, channel C.int) C.int {
	return -1
}

//export get_input
func get_input(it C.int, channel C.int) C.int {
	if ATEST_C {
		return get_input_fake(it, channel)
	} else {
		return C.get_input_real(it, channel)
	}
}
