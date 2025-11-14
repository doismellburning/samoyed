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
// #include "atest.h"
// extern int ATEST_C;
// extern struct audio_s my_audio_config;
// extern struct atest_header_t header;
// extern struct atest_chunk_t chunk;
// extern struct atest_format_t format;
// extern struct atest_wav_data_t wav_data;
// extern int h_opt;
// extern int d_o_opt;
// extern FILE *fp;
// extern int packets_decoded_one;
// extern int dcd_count;
// extern int dcd_missing_errors;
// extern int e_o_f;
// extern int sample_number;
// extern float space_gain[MAX_SUBCHANS];
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"time"
	"unsafe"

	"github.com/spf13/pflag"
)

const EXPERIMENT_G = true
const EXPERIMENT_H = true

func AtestMain() {
	C.ATEST_C = 1

	var count [C.MAX_SUBCHANS]int // Experiments G and H

	C.text_color_init(1)
	C.text_color_set(C.DW_COLOR_INFO)

	/*
	 * First apply defaults.
	 */

	C.memset(unsafe.Pointer(&C.my_audio_config), 0, C.sizeof_struct_audio_s)

	C.my_audio_config.adev[0].num_channels = C.DEFAULT_NUM_CHANNELS
	C.my_audio_config.adev[0].samples_per_sec = C.DEFAULT_SAMPLES_PER_SEC
	C.my_audio_config.adev[0].bits_per_sample = C.DEFAULT_BITS_PER_SAMPLE

	for channel := range C.MAX_RADIO_CHANS {
		C.my_audio_config.achan[channel].modem_type = C.MODEM_AFSK

		C.my_audio_config.achan[channel].mark_freq = C.DEFAULT_MARK_FREQ
		C.my_audio_config.achan[channel].space_freq = C.DEFAULT_SPACE_FREQ
		C.my_audio_config.achan[channel].baud = C.DEFAULT_BAUD

		C.strcpy(&C.my_audio_config.achan[channel].profiles[0], C.CString("A"))

		C.my_audio_config.achan[channel].num_freq = 1
		C.my_audio_config.achan[channel].offset = 0

		C.my_audio_config.achan[channel].fix_bits = C.RETRY_NONE

		C.my_audio_config.achan[channel].sanity_test = C.SANITY_APRS
		// C.my_audio_config.achan[channel].sanity_test = C.SANITY_AX25;
		// C.my_audio_config.achan[channel].sanity_test = C.SANITY_NONE;

		C.my_audio_config.achan[channel].passall = 0
		// C.my_audio_config.achan[channel].passall = 1;
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
			C.d_o_opt++
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
	C.my_audio_config.achan[0].decimate = C.int(*decimate)

	if *upsample != 0 {
		if *upsample < 1 || *upsample > 8 {
			fmt.Fprintf(os.Stderr, "Upsample should be between 1 and 4 inclusive, not %d.\n", *upsample)
			pflag.Usage()
			os.Exit(1)
		}
		C.my_audio_config.achan[0].upsample = C.int(*upsample)
	}

	if *fixBits < C.RETRY_NONE || *fixBits > C.RETRY_MAX {
		fmt.Fprintf(os.Stderr, "Fix Bits should be between %d and %d inclusive, not %d.\n", C.RETRY_NONE, C.RETRY_MAX, *fixBits)
		pflag.Usage()
		os.Exit(1)
	}
	C.my_audio_config.achan[0].fix_bits = uint32(*fixBits)

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

	C.my_audio_config.recv_ber = C.float(*bitErrorRate)

	// Options from atest.c
	if *hexDisplay {
		C.h_opt = 1
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

	C.my_audio_config.achan[0].baud = C.int(bitrate)

	/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
	/* that need to be kept in sync.  Maybe it could be a common function someday. */

	if C.my_audio_config.achan[0].baud == 100 { // What was this for?
		C.my_audio_config.achan[0].modem_type = C.MODEM_AFSK
		C.my_audio_config.achan[0].mark_freq = 1615
		C.my_audio_config.achan[0].space_freq = 1785
	} else if C.my_audio_config.achan[0].baud < 600 { // e.g. HF SSB packet
		C.my_audio_config.achan[0].modem_type = C.MODEM_AFSK
		C.my_audio_config.achan[0].mark_freq = 1600
		C.my_audio_config.achan[0].space_freq = 1800
		// Previously we had a "D" which was fine tuned for 300 bps.
		// In v1.7, it's not clear if we should use "B" or just stick with "A".
	} else if C.my_audio_config.achan[0].baud < 1800 { // common 1200
		C.my_audio_config.achan[0].modem_type = C.MODEM_AFSK
		C.my_audio_config.achan[0].mark_freq = C.DEFAULT_MARK_FREQ
		C.my_audio_config.achan[0].space_freq = C.DEFAULT_SPACE_FREQ
	} else if C.my_audio_config.achan[0].baud < 3600 {
		C.my_audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(""))
	} else if C.my_audio_config.achan[0].baud < 7200 {
		C.my_audio_config.achan[0].modem_type = C.MODEM_8PSK
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(""))
	} else if C.my_audio_config.achan[0].baud == 0xA15A15 { // Hack for different use of 9600
		C.my_audio_config.achan[0].modem_type = C.MODEM_AIS
		C.my_audio_config.achan[0].baud = 9600
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(" ")) // avoid getting default later.
	} else if C.my_audio_config.achan[0].baud == 0xEA5EA5 {
		C.my_audio_config.achan[0].modem_type = C.MODEM_EAS
		C.my_audio_config.achan[0].baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		C.my_audio_config.achan[0].mark_freq = 2083  // Actually 2083.3 - logic 1.
		C.my_audio_config.achan[0].space_freq = 1563 // Actually 1562.5 - logic 0.
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString("A"))
	} else {
		C.my_audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(" ")) // avoid getting default later.
	}

	if C.my_audio_config.achan[0].baud < C.MIN_BAUD || C.my_audio_config.achan[0].baud > C.MAX_BAUD {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("Use a more reasonable bit rate in range of %d - %d.\n", C.MIN_BAUD, C.MAX_BAUD)
		os.Exit(1)
	}

	/*
	 * -g option means force g3RUH regardless of speed.
	 */

	if *g3ruh {
		C.my_audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(" ")) // avoid getting default later.
	}

	/*
	 * We have two different incompatible flavors of V.26.
	 */
	if *direwolf15compat {
		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j

		C.my_audio_config.achan[0].v26_alternative = C.V26_A
		C.my_audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.my_audio_config.achan[0].baud = 2400
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(""))
	}
	if *mfj2400compat {
		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J

		C.my_audio_config.achan[0].v26_alternative = C.V26_B
		C.my_audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.my_audio_config.achan[0].mark_freq = 0
		C.my_audio_config.achan[0].space_freq = 0
		C.my_audio_config.achan[0].baud = 2400
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(""))
	}

	// Needs to be after -B, -j, -J.
	if *modemProfile != "" {
		fmt.Printf("Demodulator profile set to \"%s\"\n", *modemProfile)
		C.strcpy(&C.my_audio_config.achan[0].profiles[0], C.CString(*modemProfile))
	}

	C.memcpy(unsafe.Pointer(&C.my_audio_config.achan[1]), unsafe.Pointer(&C.my_audio_config.achan[0]), C.sizeof_struct_achan_param_s)

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
		C.fp = C.fopen(C.CString(wavFileName), C.CString("rb"))
		if C.fp == nil {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Couldn't open file for read: %s\n", wavFileName)
			// perror ("more info?");
			os.Exit(1)
		}

		/*
		 * Read the file header.
		 * Doesn't handle all possible cases but good enough for our purposes.
		 */

		C.fread(unsafe.Pointer(&C.header), 12, 1, C.fp)

		if C.strncmp(&C.header.riff[0], C.CString("RIFF"), 4) != 0 || C.strncmp(&C.header.wave[0], C.CString("WAVE"), 4) != 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("This is not a .WAV format file.\n")
			os.Exit(1)
		}

		C.fread(unsafe.Pointer(&C.chunk), 8, 1, C.fp)

		if C.strncmp(&C.chunk.id[0], C.CString("LIST"), 4) == 0 {
			C.fseek(C.fp, C.long(C.chunk.datasize), C.SEEK_CUR)
			C.fread(unsafe.Pointer(&C.chunk), 8, 1, C.fp)
		}

		if C.strncmp(&C.chunk.id[0], C.CString("fmt "), 4) != 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("WAV file error: Found \"%4.4s\" where \"fmt \" was expected.\n", C.GoString(&C.chunk.id[0]))
			os.Exit(1)
		}
		if C.chunk.datasize != 16 && C.chunk.datasize != 18 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("WAV file error: Need fmt chunk datasize of 16 or 18.  Found %d.\n", C.chunk.datasize)
			os.Exit(1)
		}

		C.fread(unsafe.Pointer(&C.format), C.ulong(C.chunk.datasize), 1, C.fp)

		C.fread(unsafe.Pointer(&C.wav_data), 8, 1, C.fp)

		if C.strncmp(&C.wav_data.data[0], C.CString("data"), 4) != 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("WAV file error: Found \"%4.4s\" where \"data\" was expected.\n", C.GoString(&C.wav_data.data[0]))
			os.Exit(1)
		}

		if C.format.wformattag != 1 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Sorry, I only understand audio format 1 (PCM).  This file has %d.\n", C.format.wformattag)
			os.Exit(1)
		}

		if C.format.nchannels != 1 && C.format.nchannels != 2 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Sorry, I only understand 1 or 2 channels.  This file has %d.\n", C.format.nchannels)
			os.Exit(1)
		}

		if C.format.wbitspersample != 8 && C.format.wbitspersample != 16 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Sorry, I only understand 8 or 16 bits per sample.  This file has %d.\n", C.format.wbitspersample)
			os.Exit(1)
		}

		C.my_audio_config.adev[0].samples_per_sec = C.format.nsamplespersec
		C.my_audio_config.adev[0].bits_per_sample = C.int(C.format.wbitspersample)
		C.my_audio_config.adev[0].num_channels = C.int(C.format.nchannels)

		C.my_audio_config.chan_medium[0] = C.MEDIUM_RADIO
		if C.format.nchannels == 2 {
			C.my_audio_config.chan_medium[1] = C.MEDIUM_RADIO
		}

		C.text_color_set(C.DW_COLOR_INFO)
		fmt.Printf("%d samples per second.  %d bits per sample.  %d audio channels.\n",
			C.my_audio_config.adev[0].samples_per_sec,
			C.my_audio_config.adev[0].bits_per_sample,
			(C.my_audio_config.adev[0].num_channels))
		// nnum_channels is known to be 1 or 2.
		var one_filetime = C.wav_data.datasize /
			((C.my_audio_config.adev[0].bits_per_sample / 8) * (C.my_audio_config.adev[0].num_channels) * C.my_audio_config.adev[0].samples_per_sec)
		total_filetime += C.double(one_filetime)

		fmt.Printf("%d audio bytes in file.  Duration = %.1f seconds.\n",
			C.wav_data.datasize,
			float64(one_filetime))
		fmt.Printf("Fix Bits level = %d\n", C.my_audio_config.achan[0].fix_bits)

		/*
		 * Initialize the AFSK demodulator and HDLC decoder.
		 * Needs to be done for each file because they could have different sample rates.
		 */
		multi_modem_init(&C.my_audio_config)
		C.packets_decoded_one = 0

		C.e_o_f = 0
		for C.e_o_f == 0 {
			for c := C.int(0); c < (C.my_audio_config.adev[0].num_channels); c++ {
				/* This reads either 1 or 2 bytes depending on */
				/* bits per sample.  */

				var audio_sample = demod_get_sample(ACHAN2ADEV(c))

				if audio_sample >= 256*256 {
					C.e_o_f = 1
					continue
				}

				if c == 0 {
					C.sample_number++
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
				var db = 20.0 * C.log10f(C.space_gain[j])
				fmt.Printf("%+.1f dB, %d\n", db, count[j])
			}
		}
		if EXPERIMENT_H {
			for j := range C.MAX_SUBCHANS {
				fmt.Printf("%d\n", count[j])
			}
		}

		fmt.Printf("%d from %s\n", C.packets_decoded_one, wavFileName)
		packets_decoded_total += int(C.packets_decoded_one)

		C.fclose(C.fp)
	}

	var elapsed = time.Since(start_time)

	fmt.Printf("%d packets decoded in %.3f seconds.  %.1f x realtime\n", packets_decoded_total, elapsed.Seconds(), total_filetime/C.double(elapsed.Seconds()))
	if C.d_o_opt > 0 {
		fmt.Printf("DCD count = %d\n", C.dcd_count)
		fmt.Printf("DCD missing errors = %d\n", C.dcd_missing_errors)
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
