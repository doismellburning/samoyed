/* Test fixture for the Dire Wolf demodulators */
package main

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
// #include "audio.h"
// #include "demod.h"
// #include "multi_modem.h"
// #include "textcolor.h"
// #include "ax25_pad.h"
// #include "hdlc_rec2.h"
// #include "dlq.h"
// #include "ptt.h"
// #include "dtime_now.h"
// #include "fx25.h"
// #include "il2p.h"
// #include "hdlc_rec.h"
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0
import "C"
import "os"
import "fmt"
import "strconv"
import            "github.com/akamensky/argparse"

/* 8 bit samples are unsigned bytes */
/* in range of 0 .. 255. */

/* 16 bit samples are little endian signed short */
/* in range of -32768 .. +32767. */

type header struct {
        riff [4]C.char;          /* "RIFF" */
        filesize C.int;          /* file length - 8 */
        wave [4]C.char;          /* "WAVE" */
};

type chunk struct {
	id [4]C.char;		/* "LIST" or "fmt " */
	datasize C.int;
}

type format struct {
        wformattag C.short;       /* 1 for PCM. */
        nchannels C.short;        /* 1 for mono, 2 for stereo. */
        nsamplespersec C.int;    /* sampling freq, Hz. */
        navgbytespersec C.int;   /* = nblockalign*nsamplespersec. */
        nblockalign C.short;      /* = wbitspersample/8 * nchannels. */
         wbitspersample C.short;   /* 16 or 8. */
	 extras[4]C.char;
};

type wav_data struct {
	data [4]C.char;		/* "data" */
	 datasize C.int
}


var fp *os.File
var e_o_f C.int;
var packets_decoded_one = 0;
var packets_decoded_total = 0;
var decimate = 0;		/* Reduce that sampling rate if set. */
					/* 1 = normal, 2 = half, 3 = 1/3, etc. */

var upsample = 0;		/* Upsample for G3RUH decoder. */
					/* Non-zero will override the default. */

var my_audio_config C.struct_audio_s

var error_if_less_than = -1;	/* Exit with error status if this minimum not reached. */
					/* Can be used to check that performance has not decreased. */

var error_if_greater_than = -1;	/* Exit with error status if this maximum exceeded. */
					/* Can be used to check that duplicate removal is not broken. */



const EXPERIMENT_G  = true
const EXPERIMENT_H  = true

var count [C.MAX_SUBCHANS]int;

// FIXME KG extern float space_gain[C.MAX_SUBCHANS];

 var decode_only = 0;		/* Set to 0 or 1 to decode only one channel.  2 for both.  */

 var sample_number = -1;		/* Sample number from the file. */
					/* Incremented only for channel 0. */
					/* Use to print timestamp, relative to beginning */
					/* of file, when frame was decoded. */

// command line options.

var B_opt = C.DEFAULT_BAUD;	// Bits per second.  Need to change all baud references to bps.
var dcd_count = 0;
var dcd_missing_errors = 0;


func main() {
	/* FIXME KG
	int err;
	int c;

	double start_time;		// Time when we started so we can measure elapsed time.
	double one_filetime = 0;		// Length of one audio file in seconds.
	double total_filetime = 0;		// Length of all audio files in seconds.
	double elapsed;			// Time it took us to process it.
	*/


	if EXPERIMENT_G || EXPERIMENT_H {
	for j:=0; j<C.MAX_SUBCHANS; j++ {
	  count[j] = 0;
	}
	}

/* 
 * First apply defaults.
 */
	
	my_audio_config.adev[0].num_channels = C.DEFAULT_NUM_CHANNELS;		
	my_audio_config.adev[0].samples_per_sec = C.DEFAULT_SAMPLES_PER_SEC;	
	my_audio_config.adev[0].bits_per_sample = C.DEFAULT_BITS_PER_SAMPLE;	


	for channel:=0; channel<C.MAX_RADIO_CHANS; channel++ {

	  my_audio_config.achan[channel].modem_type = C.MODEM_AFSK;

	  my_audio_config.achan[channel].mark_freq = C.DEFAULT_MARK_FREQ;		
	  my_audio_config.achan[channel].space_freq = C.DEFAULT_SPACE_FREQ;		
	  my_audio_config.achan[channel].baud = C.DEFAULT_BAUD;	

	  //FIXME KG my_audio_config.achan[channel].profiles = C.CString("A")
 		
	  my_audio_config.achan[channel].num_freq = 1;				
	  my_audio_config.achan[channel].offset = 0;	

	  my_audio_config.achan[channel].fix_bits = C.RETRY_NONE;	

	  my_audio_config.achan[channel].sanity_test = C.SANITY_APRS;	
	  //my_audio_config.achan[channel].sanity_test = SANITY_AX25;	
	  //my_audio_config.achan[channel].sanity_test = SANITY_NONE;	

	  my_audio_config.achan[channel].passall = 0;				
	  //my_audio_config.achan[channel].passall = 1;				
	}

	var parser = argparse.NewParser("atest", "Test tool for the Dire Wolf demodulators")
	var bitrateStr = parser.String("B", "bitrate", &argparse.Options{Default: C.DEFAULT_BAUD, Help:`Bits/second  for data.  Proper modem automatically selected for speed.\n");
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`})
	var g3ruh = parser.Flag("g", "g3ruh", &argparse.Options{Help:"Use G3RUH modem rather than default for data rate"})
	var direwolf15compat = parser.Flag("j", "direwolf-15-compat", &argparse.Options{Help:"2400 bps QPSK compatible with direwolf <= 1.5"})
	var mfj2400compat = parser.Flag("J", "mfj-2400-compat", &argparse.Options{Help:"2400 bps QPSK compatible with MFJ-2400"})
	var modemProfile = parser.String("P", "modem-profile", &argparse.Options{Help:"Select the demodulator type such as D (default for 300 bps), E+ (default for 1200 bps), PQRS for 2400 bps, etc."})
	var decimate = parser.Int("D", "decimate", &argparse.Options{Help:"Divide audio sample rate by n"})
	// TODO KG 1-8
        // my_audio_config.achan[0].decimate = decimate;
	var upsample = parser.Int("U", "upsample", &argparse.Options{Help:"Upsample for G3RUH to improve performance when the sample rate to baud ratio is low"})
	// TODO KG 1-4
        // my_audio_config.achan[0].upsample = upsample;
	var fixBits = parser.Int("F", "fix-bits", &argparse.Options{Help:"Amount of effort to try fixing frames with an invalid CRC. 0 (default) = consider only correct frames. 1 = Try to fix only a sigle bit. Higher values = Try modifying more bits to get a good CRC."})
	// TODO KG
      // my_audio_config.achan[0].fix_bits = atoi(optarg);
      	var errorIfLessThan = parser.Int("L", "error-if-less-than", &argparse.Options{Help: "Error if less than this number decoded."})
	var errorIfGreaterThan = parser.Int("G", "error-if-greater-than", &argparse.Options{Help: "Error if greater than this number decoded."})
	var channel0 = parser.Flag("0", "channel-0", &argparse.Options{Help: "Use channel 0 (left) of stereo audio (default)."})
	var channel1 = parser.Flag("1", "channel-1", &argparse.Options{Help: "Use channel 1 (right) of stereo audio."})
	var channel2 = parser.Flag("2", "channel-2", &argparse.Options{Help: "Use both channels of stereo audio."})
	var hexDisplay = parser.Flag("h", "hex-display", &argparse.Options{Help: "Print frame contents as hexadecimal bytes."})
	var bitErrorRate = parser.Int("e", "bit-error-rate", &argparse.Options{Help: "Receive Bit Error Rate (BER)."}) 

	// TODO KG -d

// 	     case 'd':				/* Debug message options. */
// 
// 	       for (char *p=optarg; *p!='\0'; p++) {
// 	        switch (*p) {
// 	           case 'x':  d_x_opt++; break;			// FX.25
// 	           case 'o':  d_o_opt++; break;			// DCD output control
// 	           case '2':  d_2_opt++; break;			// IL2P debug out
// 	           default: break;
// 	        }
// 	       }
// 	       break;

	var parseErr = parser.Parse(os.Args)
	if parseErr != nil {
		fmt.Print(parser.Usage(parseErr))
		return
	}

	// Hacks for the magic strings
	var bitrate, bitrateParseErr = strconv.Atoi(*bitrateStr)
	if *bitrateStr == "AIS" {
		bitrate = 0xA15A15
	}
	if *bitrateStr == "EAS" {
		bitrate = 0xEA5EA5
	}
    	// TODO KG check for error?

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

	if (my_audio_config.achan[0].baud == 100) {		// What was this for?
	  my_audio_config.achan[0].modem_type = C.MODEM_AFSK;
	  my_audio_config.achan[0].mark_freq = 1615;
	  my_audio_config.achan[0].space_freq = 1785;
	} else if (my_audio_config.achan[0].baud < 600) {		// e.g. HF SSB packet
	  my_audio_config.achan[0].modem_type = C.MODEM_AFSK;
	  my_audio_config.achan[0].mark_freq = 1600;
	  my_audio_config.achan[0].space_freq = 1800;
	  // Previously we had a "D" which was fine tuned for 300 bps.
	  // In v1.7, it's not clear if we should use "B" or just stick with "A".
	} else if (my_audio_config.achan[0].baud < 1800) {	// common 1200
	  my_audio_config.achan[0].modem_type = C.MODEM_AFSK;
	  my_audio_config.achan[0].mark_freq = C.DEFAULT_MARK_FREQ;
	  my_audio_config.achan[0].space_freq = C.DEFAULT_SPACE_FREQ;
	} else if (my_audio_config.achan[0].baud < 3600) {
	  my_audio_config.achan[0].modem_type = C.MODEM_QPSK;
	  my_audio_config.achan[0].mark_freq = 0;
	  my_audio_config.achan[0].space_freq = 0;
	  C.strcpy(&my_audio_config.achan[0].profiles[0], C.CString(""))
	} else if (my_audio_config.achan[0].baud < 7200) {
	  my_audio_config.achan[0].modem_type = C.MODEM_8PSK;
	  my_audio_config.achan[0].mark_freq = 0;
	  my_audio_config.achan[0].space_freq = 0;
	  C.strcpy(&my_audio_config.achan[0].profiles[0] ,C.CString(""))
	} else if (my_audio_config.achan[0].baud == 0xA15A15) {	// Hack for different use of 9600
	  my_audio_config.achan[0].modem_type = C.MODEM_AIS;
	  my_audio_config.achan[0].baud = 9600;
	  my_audio_config.achan[0].mark_freq = 0;
	  my_audio_config.achan[0].space_freq = 0;
	  C.strcpy(&my_audio_config.achan[0].profiles[0] , C.CString(" "))
	} else if (my_audio_config.achan[0].baud == 0xEA5EA5) {
	  my_audio_config.achan[0].modem_type = C.MODEM_EAS;
	  my_audio_config.achan[0].baud = 521;	// Actually 520.83 but we have an integer field here.
						// Will make more precise in afsk demod init.
	  my_audio_config.achan[0].mark_freq = 2083;	// Actually 2083.3 - logic 1.
	  my_audio_config.achan[0].space_freq = 1563;	// Actually 1562.5 - logic 0.
	  C.strcpy(&my_audio_config.achan[0].profiles[0] , C.CString("A"))
	} else {
	  my_audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE;
	  my_audio_config.achan[0].mark_freq = 0;
	  my_audio_config.achan[0].space_freq = 0;
	  C.strcpy(&my_audio_config.achan[0].profiles[0] , C.CString(" ")) // avoid getting default later.
	}

        if (my_audio_config.achan[0].baud < C.MIN_BAUD || my_audio_config.achan[0].baud > C.MAX_BAUD) {
          fmt.Print ("Use a more reasonable bit rate in range of %d - %d.\n", C.MIN_BAUD, C.MAX_BAUD);
          os.Exit (C.EXIT_FAILURE);
        }

/*
 * -g option means force g3RUH regardless of speed.
 */

	if (*g3ruh) {
          my_audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE;
          my_audio_config.achan[0].mark_freq = 0;
          my_audio_config.achan[0].space_freq = 0;
	  C.strcpy(&my_audio_config.achan[0].profiles[0] , C.CString( " "))	// avoid getting default later.
	}

/*
 * We have two different incompatible flavors of V.26.
 */
	if (*direwolf15compat) {

	  // V.26 compatible with earlier versions of direwolf.
	  //   Example:   -B 2400 -j    or simply   -j

	  my_audio_config.achan[0].v26_alternative = C.V26_A;
          my_audio_config.achan[0].modem_type = C.MODEM_QPSK;
          my_audio_config.achan[0].mark_freq = 0;
          my_audio_config.achan[0].space_freq = 0;
	  my_audio_config.achan[0].baud = 2400;
	  C.strcpy (&my_audio_config.achan[0].profiles[0], C.CString(""));
	}
	if (*mfj2400compat) {

	  // V.26 compatible with MFJ and maybe others.
	  //   Example:   -B 2400 -J     or simply   -J

	  my_audio_config.achan[0].v26_alternative = C.V26_B;
          my_audio_config.achan[0].modem_type = C.MODEM_QPSK;
          my_audio_config.achan[0].mark_freq = 0;
          my_audio_config.achan[0].space_freq = 0;
	  my_audio_config.achan[0].baud = 2400;
	  C.strcpy (&my_audio_config.achan[0].profiles[0], C.CString(""))
	}

	// Needs to be after -B, -j, -J.
	if *modemProfile != "" {
	  dw_printf ("Demodulator profile set to \"%s\"\n", *modemProfile);
	  C.strcpy (&my_audio_config.achan[0].profiles[0], C.CString(*modemProfile))
	}

	memcpy (&my_audio_config.achan[1], &my_audio_config.achan[0], sizeof(my_audio_config.achan[0]));


	if (optind >= argc) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Specify .WAV file name on command line.\n");
	  usage ();
	}

	fx25_init (d_x_opt);
	il2p_init (d_2_opt);

	start_time = dtime_now();

	for optind < argc {

	fp = fopen(argv[optind], "rb");
        if (fp == NULL) {
	  text_color_set(DW_COLOR_ERROR);
          dw_printf ("Couldn't open file for read: %s\n", argv[optind]);
	  //perror ("more info?");
          exit (EXIT_FAILURE);
        }

/*
 * Read the file header.  
 * Doesn't handle all possible cases but good enough for our purposes.
 */

        err= fread (&header, C.size_t(12), C.size_t(1), fp);
	(void)(err);

	if (strncmp(header.riff, "RIFF", 4) != 0 || strncmp(header.wave, "WAVE", 4) != 0) {
	  text_color_set(DW_COLOR_ERROR);
          dw_printf ("This is not a .WAV format file.\n");
          exit (EXIT_FAILURE);
	}

	err = fread (&chunk, C.size_t(8), C.size_t(1), fp);

	if (strncmp(chunk.id, "LIST", 4) == 0) {
	  err = fseek (fp, C.long(chunk.datasize), C.SEEK_CUR);
	  err = fread (&chunk, C.size_t(8), C.size_t(1), fp);
	}

	if (strncmp(chunk.id, "fmt ", 4) != 0) {
	  text_color_set(DW_COLOR_ERROR);
          dw_printf ("WAV file error: Found \"%4.4s\" where \"fmt \" was expected.\n", chunk.id);
	  exit(EXIT_FAILURE);
	}

	if (chunk.datasize != 16 && chunk.datasize != 18) {
	  text_color_set(DW_COLOR_ERROR);
          dw_printf ("WAV file error: Need fmt chunk datasize of 16 or 18.  Found %d.\n", chunk.datasize);
	  exit(EXIT_FAILURE);
	}

        err = fread (&format, C.size_t(chunk.datasize), C.size_t(1), fp);	

	err = fread (&wav_data, C.size_t(8), C.size_t(1), fp);

	if (strncmp(wav_data.data, "data", 4) != 0) {
	  text_color_set(DW_COLOR_ERROR);
          dw_printf ("WAV file error: Found \"%4.4s\" where \"data\" was expected.\n", wav_data.data);
	  exit(EXIT_FAILURE);
	}

	if (format.wformattag != 1) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Sorry, I only understand audio format 1 (PCM).  This file has %d.\n", format.wformattag);
	  exit (EXIT_FAILURE);
	}

	if (format.nchannels != 1 && format.nchannels != 2) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Sorry, I only understand 1 or 2 channels.  This file has %d.\n", format.nchannels);
	  exit (EXIT_FAILURE);
	}

	if (format.wbitspersample != 8 && format.wbitspersample != 16) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Sorry, I only understand 8 or 16 bits per sample.  This file has %d.\n", format.wbitspersample);
	  exit (EXIT_FAILURE);
	}

        my_audio_config.adev[0].samples_per_sec = format.nsamplespersec;
	my_audio_config.adev[0].bits_per_sample = format.wbitspersample;
 	my_audio_config.adev[0].num_channels = format.nchannels;

	my_audio_config.chan_medium[0] = MEDIUM_RADIO;
	if (format.nchannels == 2) {
	  my_audio_config.chan_medium[1] = MEDIUM_RADIO;
	}

	text_color_set(DW_COLOR_INFO);
	dw_printf ("%d samples per second.  %d bits per sample.  %d audio channels.\n",
		my_audio_config.adev[0].samples_per_sec,
		my_audio_config.adev[0].bits_per_sample,
		(int)(my_audio_config.adev[0].num_channels));
	// nnum_channels is known to be 1 or 2.
	one_filetime = C.double( wav_data.datasize) /
		((my_audio_config.adev[0].bits_per_sample / 8) * C.int(my_audio_config.adev[0].num_channels) * my_audio_config.adev[0].samples_per_sec);
	total_filetime += one_filetime;

	dw_printf ("%d audio bytes in file.  Duration = %.1f seconds.\n",
		C.int(wav_data.datasize),
		one_filetime);
	dw_printf ("Fix Bits level = %d\n", my_audio_config.achan[0].fix_bits);
		
/*
 * Initialize the AFSK demodulator and HDLC decoder.
 * Needs to be done for each file because they could have different sample rates.
 */
	multi_modem_init (&my_audio_config);
	packets_decoded_one = 0;


	for eof := false; !eof; {
	  for c:=0; c<int(my_audio_config.adev[0].num_channels); c++ {

            /* This reads either 1 or 2 bytes depending on */
            /* bits per sample.  */

            var audio_sample = demod_get_sample (ACHAN2ADEV(c));

            if (audio_sample >= 256 * 256) {
               e_o_f = 1;
	       continue;
	    }

	    if (c == 0) {
		    sample_number++;
		}

            if (decode_only == 0 && c != 0) {
	    continue;
    }
            if (decode_only == 1 && c != 1) {
		    continue;
	    }

            multi_modem_process_sample(c,audio_sample);
          }

                /* When a complete frame is accumulated, */
                /* process_rec_frame, below, is called. */

	}
	text_color_set(DW_COLOR_INFO);
	dw_printf ("\n\n");

if EXPERIMENT_G {

	for j=0; j<C.MAX_SUBCHANS; j++ {
	  var db = 20.0 * log10f(space_gain[j]);
	  dw_printf ("%+.1f dB, %d\n", db, count[j]);
	}
}
if EXPERIMENT_H {

	for j=0; j<C.MAX_SUBCHANS; j++ {
	  dw_printf ("%d\n", count[j]);
	}
}

	dw_printf ("%d from %s\n", packets_decoded_one, argv[optind]);
	packets_decoded_total += packets_decoded_one;

	fclose (fp);
	optind++;
	}

	elapsed = dtime_now() - start_time;

	dw_printf ("%d packets decoded in %.3f seconds.  %.1f x realtime\n", packets_decoded_total, elapsed, total_filetime/elapsed);
	if (d_o_opt) {
	  dw_printf ("DCD count = %d\n", dcd_count);
	  dw_printf ("DCD missing errors = %d\n", dcd_missing_errors);
	}

	if (error_if_less_than != -1 && packets_decoded_total < error_if_less_than) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("\n * * * TEST FAILED: number decoded is less than %d * * * \n", error_if_less_than);
	  exit (EXIT_FAILURE);
	}
	if (error_if_greater_than != -1 && packets_decoded_total > error_if_greater_than) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("\n * * * TEST FAILED: number decoded is greater than %d * * * \n", error_if_greater_than);
	  exit (EXIT_FAILURE);
	}

	exit (EXIT_SUCCESS);
}


/*
 * Simulate sample from the audio device.
 */

func audio_get_fake (a C.int) C.int {
	var ch C.int

	if (wav_data.datasize <= 0) {
	  e_o_f = 1;
	  return (-1);
	}

	ch = getc(fp);
	wav_data.datasize--;

	if (ch < 0) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Unexpected end of file.\n");
	  e_o_f = 1;
	}

	return (ch);
}

// TODO KG
// #ifdef ATEST_C
// #define audio_get audio_get_fake
// #endif


/*
 * This is called when we have a good frame.
 */

func dlq_rec_frame_fake (channel C.int, subchan int, slice int, pp C.packet_t, alevel C.alevel_t, fec_type C.fec_type_t, retries C.retry_t, spectrum *C.char) {	
	
	// FIXME KG
	/*
	char stemp[500];
	unsigned char *pinfo;
	int info_len;
	int h;
	char heard[2 * AX25_MAX_ADDR_LEN + 20];
	char alevel_text[AX25_ALEVEL_TO_TEXT_SIZE];
	*/

	packets_decoded_one++;
	if ( ! hdlc_rec_data_detect_any(channel)) {
		dcd_missing_errors++;
	}

	ax25_format_addrs (pp, stemp);

	info_len = ax25_get_info (pp, &pinfo);

	/* Print so we can see what is going on. */

	//TODO: quiet option - suppress packet printing, only the count at the end.

	/* Display audio input level. */
        /* Who are we hearing?   Original station or digipeater? */

	if (ax25_get_num_addr(pp) == 0) {
	  /* Not AX.25. No station to display below. */
	  h = -1;
	  strlcpy (heard, "", sizeof(heard));
	} else {
	  h = ax25_get_heard(pp);
          ax25_get_addr_with_ssid(pp, h, heard);
	}

	text_color_set(DW_COLOR_DEBUG);
	dw_printf ("\n");
	dw_printf("DECODED[%d] ", packets_decoded_one );

	/* Insert time stamp relative to start of file. */

	var sec = C.double(sample_number) / my_audio_config.adev[0].samples_per_sec;
	var min = int(sec / 60.);
	sec -= min * 60;

	dw_printf ("%d:%06.3f ", min, sec);

	if (h != AX25_SOURCE) {
	  dw_printf ("Digipeater ");
	}
	ax25_alevel_to_text (alevel, alevel_text);

	/* As suggested by KJ4ERJ, if we are receiving from */
	/* WIDEn-0, it is quite likely (but not guaranteed), that */
	/* we are actually hearing the preceding station in the path. */

	if (h >= AX25_REPEATER_2 &&
	      strncmp(heard, "WIDE", 4) == 0 &&
	      isdigit(heard[4]) &&
	      heard[5] == rune(0)) {

	  var probably_really [AX25_MAX_ADDR_LEN]C.char;
	  ax25_get_addr_with_ssid(pp, h-1, probably_really);

	  strlcat (heard, " (probably ", sizeof(heard));
	  strlcat (heard, probably_really, sizeof(heard));
	  strlcat (heard, ")", sizeof(heard));
	}

	switch (fec_type) {

	  case fec_type_fx25:
	    dw_printf ("%s audio level = %s   FX.25  %s\n", heard, alevel_text, spectrum);
	    break;

	  case fec_type_il2p:
	    dw_printf ("%s audio level = %s   IL2P  %s\n", heard, alevel_text, spectrum);
	    break;

	  case fec_type_none:
	  default:
	    if (my_audio_config.achan[channel].fix_bits == RETRY_NONE && my_audio_config.achan[channel].passall == 0) {
	      // No fix_bits or passall specified.
	      dw_printf ("%s audio level = %s     %s\n", heard, alevel_text, spectrum);
	    } else {
	      assert (retries >= RETRY_NONE && retries <= RETRY_MAX);   // validate array index.
	      dw_printf ("%s audio level = %s   [%s]   %s\n", heard, alevel_text, retry_text[int(retries)], spectrum);
	    }
	    break;
	}



// Display non-APRS packets in a different color.

// Display channel with subchannel/slice if applicable.

	if (ax25_is_aprs(pp)) {
	  text_color_set(DW_COLOR_REC);
	} else {
	  text_color_set(DW_COLOR_DEBUG);
	}

	if (my_audio_config.achan[channel].num_subchan > 1 && my_audio_config.achan[channel].num_slicers == 1) {
	  dw_printf ("[%d.%d] ", channel, subchan);
	} else if (my_audio_config.achan[channel].num_subchan == 1 && my_audio_config.achan[channel].num_slicers > 1) {
	  dw_printf ("[%d.%d] ", channel, slice);
	} else if (my_audio_config.achan[channel].num_subchan > 1 && my_audio_config.achan[channel].num_slicers > 1) {
	  dw_printf ("[%d.%d.%d] ", channel, subchan, slice);
	} else {
	  dw_printf ("[%d] ", channel);
	}

	dw_printf ("%s", stemp);			/* stations followed by : */
	ax25_safe_print (*C.char(pinfo), info_len, 0);
	dw_printf ("\n");

/*
 * -h option for hexadecimal display.  (new in 1.6)
 */

	if (h_opt) {

	  text_color_set(DW_COLOR_DEBUG);
	  dw_printf ("------\n");
	  ax25_hex_dump (pp);
	  dw_printf ("------\n");
	}




// #if 0		// temp experiment
// 
// #include "decode_aprs.h"
// #include "log.h"
// 
// 	if (ax25_is_aprs(pp)) {
// 
// 	  decode_aprs_t A;
// 
// 	  decode_aprs (&A, pp, 0, NULL);
// 
// 	  // Temp experiment to see how different systems set the RR bits in the source and destination.
// 	  // log_rr_bits (&A, pp);
// 
// 	}
// #endif


	ax25_delete (pp);

} /* end fake dlq_append */

// FIXME KG
// #ifdef ATEST_C
// #define dlq_req_frame dlq_req_frame_fake
// #endif

func ptt_set_fake (ot int , channel int, ptt_signal int) {
	// Should only get here for DCD output control.
	// FIXME KG static double dcd_start_time[C.MAX_RADIO_CHANS];

	if (d_o_opt) {
	  var t = C.double(sample_number) / my_audio_config.adev[0].samples_per_sec;
	  var sec1, sec2 C.double
	  var min1, min2 C.int

	  text_color_set(DW_COLOR_INFO);

	  if (ptt_signal) {
	    //sec1 = t;
	    //min1 = (int)(sec1 / 60.);
	    //sec1 -= min1 * 60;
	    //dw_printf ("DCD[%d] = ON    %d:%06.3f\n",  channel, min1, sec1);
	    dcd_count++;
	    dcd_start_time[channel] = t;
	  } else {
	    //dw_printf ("DCD[%d] = off   %d:%06.3f   %3.0f\n",  channel, min, sec, (t - dcd_start_time[channel]) * 1000.);

	    sec1 = dcd_start_time[channel];
	    min1 = (int)(sec1 / 60.);
	    sec1 -= min1 * 60;

	    sec2 = t;
	    min2 = (int)(sec2 / 60.);
	    sec2 -= min2 * 60;

	    dw_printf ("DCD[%d]  %d:%06.3f - %d:%06.3f =  %3.0f\n",  channel, min1, sec1, min2, sec2, (t - dcd_start_time[channel]) * 1000.);
	  }
	}
	return;
}

// #ifdef ATEST_C
// #define ptt_set ptt_set_fake
// #endif

func get_input_fake (it int, channel int) {
	return -1;
}

// #ifdef ATEST_C
// #define get_input get_input_fake
// #endif

/*
static void usage (void) {

	text_color_set(DW_COLOR_ERROR);

	dw_printf ("\n");
	dw_printf ("atest is a test application which decodes AX.25 frames from an audio\n");
	dw_printf ("recording.  This provides an easy way to test Dire Wolf decoding\n");
	dw_printf ("performance much quicker than normal real-time.   \n"); 
	dw_printf ("\n");
	dw_printf ("usage:\n");
	dw_printf ("\n");
	dw_printf ("        atest [ options ] wav-file-in\n");
	dw_printf ("\n");
	dw_printf ("        -B n   Bits/second  for data.  Proper modem automatically selected for speed.\n");
	dw_printf ("               300 bps defaults to AFSK tones of 1600 & 1800.\n");
	dw_printf ("               1200 bps uses AFSK tones of 1200 & 2200.\n");
	dw_printf ("               2400 bps uses QPSK based on V.26 standard.\n");
	dw_printf ("               4800 bps uses 8PSK based on V.27 standard.\n");
	dw_printf ("               9600 bps and up uses K9NG/G3RUH standard.\n");
	dw_printf ("               AIS for ship Automatic Identification System.\n");
	dw_printf ("               EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).\n");
	dw_printf ("\n");
	dw_printf ("        -g     Use G3RUH modem rather rather than default for data rate.\n");
	dw_printf ("        -j     2400 bps QPSK compatible with direwolf <= 1.5.\n");
	dw_printf ("        -J     2400 bps QPSK compatible with MFJ-2400.\n");
	dw_printf ("\n");
	dw_printf ("        -D n   Divide audio sample rate by n.\n");
	dw_printf ("\n");
	dw_printf ("        -h     Print frame contents as hexadecimal bytes.\n");
	dw_printf ("\n");
	dw_printf ("        -F n   Amount of effort to try fixing frames with an invalid CRC.  \n");
	dw_printf ("               0 (default) = consider only correct frames.  \n");
	dw_printf ("               1 = Try to fix only a single bit.  \n");
	dw_printf ("               more = Try modifying more bits to get a good CRC.\n");
	dw_printf ("\n");
	dw_printf ("        -d x   Debug information for FX.25.  Repeat for more detail.\n");
	dw_printf ("\n");
	dw_printf ("        -L     Error if less than this number decoded.\n");
	dw_printf ("\n");
	dw_printf ("        -G     Error if greater than this number decoded.\n");
	dw_printf ("\n");
	dw_printf ("        -P m   Select  the  demodulator  type such as D (default for 300 bps),\n");
	dw_printf ("               E+ (default for 1200 bps), PQRS for 2400 bps, etc.\n");
	dw_printf ("\n");
	dw_printf ("        -0     Use channel 0 (left) of stereo audio (default).\n");
	dw_printf ("        -1     use channel 1 (right) of stereo audio.\n");
	dw_printf ("        -2     decode both channels of stereo audio.\n");
	dw_printf ("\n");
	dw_printf ("        wav-file-in is a WAV format audio file.\n");
	dw_printf ("\n");
	dw_printf ("Examples:\n");
	dw_printf ("\n");
	dw_printf ("        gen_packets -o test1.wav\n");
	dw_printf ("        atest test1.wav\n");
	dw_printf ("\n");
	dw_printf ("        gen_packets -B 300 -o test3.wav\n");
	dw_printf ("        atest -B 300 test3.wav\n");
	dw_printf ("\n");
	dw_printf ("        gen_packets -B 9600 -o test9.wav\n");
	dw_printf ("        atest -B 9600 test9.wav\n");
	dw_printf ("\n");
	dw_printf ("              This generates and decodes 3 test files with 1200, 300, and 9600\n");
	dw_printf ("              bits per second.\n");
	dw_printf ("\n");
	dw_printf ("        atest 02_Track_2.wav\n");
	dw_printf ("        atest -P E- 02_Track_2.wav\n");
	dw_printf ("        atest -F 1 02_Track_2.wav\n");
	dw_printf ("        atest -P E- -F 1 02_Track_2.wav\n");
	dw_printf ("\n");
	dw_printf ("              Try  different combinations of options to compare decoding\n");
	dw_printf ("              performance.\n");

	exit (1);
}
*/


/* end atest.c */
