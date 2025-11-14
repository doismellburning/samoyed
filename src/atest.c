
//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2011, 2012, 2013, 2014, 2015, 2016, 2019, 2021, 2022, 2023  John Langner, WB2OSZ
//
//    This program is free software: you can redistribute it and/or modify
//    it under the terms of the GNU General Public License as published by
//    the Free Software Foundation, either version 2 of the License, or
//    (at your option) any later version.
//
//    This program is distributed in the hope that it will be useful,
//    but WITHOUT ANY WARRANTY; without even the implied warranty of
//    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//    GNU General Public License for more details.
//
//    You should have received a copy of the GNU General Public License
//    along with this program.  If not, see <http://www.gnu.org/licenses/>.
//


/*-------------------------------------------------------------------
 *
 * Name:        atest.c
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

#include "direwolf.h"

#include <stdio.h>
#include <unistd.h>
#include <stdlib.h>
#include <assert.h>
#include <string.h>
#include <time.h>
#include <getopt.h>
#include <ctype.h>



#include "audio.h"
#include "demod.h"
#include "multi_modem.h"
#include "textcolor.h"
#include "ax25_pad.h"
#include "hdlc_rec2.h"
#include "dlq.h"
#include "ptt.h"
#include "fx25.h"
#include "il2p.h"
#include "hdlc_rec.h"
#include "atest.h"


#if 0	/* Typical but not flexible enough. */

struct wav_header {             /* .WAV file header. */
        char riff[4];           /* "RIFF" */
        int filesize;          /* file length - 8 */
        char wave[4];           /* "WAVE" */
        char fmt[4];            /* "fmt " */
        int fmtsize;           /* 16. */
        short wformattag;       /* 1 for PCM. */
        short nchannels;        /* 1 for mono, 2 for stereo. */
        int nsamplespersec;    /* sampling freq, Hz. */
        int navgbytespersec;   /* = nblockalign*nsamplespersec. */
        short nblockalign;      /* = wbitspersample/8 * nchannels. */
        short wbitspersample;   /* 16 or 8. */
        char data[4];           /* "data" */
        int datasize;          /* number of bytes following. */
} ;
#endif
					/* 8 bit samples are unsigned bytes */
					/* in range of 0 .. 255. */
 
 					/* 16 bit samples are little endian signed short */
					/* in range of -32768 .. +32767. */

int ATEST_C = 0;

struct atest_header_t header;
struct atest_chunk_t chunk;
struct atest_format_t format;
struct atest_wav_data_t wav_data;


FILE *fp;
int e_o_f;
int packets_decoded_one = 0;
static int packets_decoded_total = 0;
static int decimate = 0;		/* Reduce that sampling rate if set. */
					/* 1 = normal, 2 = half, 3 = 1/3, etc. */

static int upsample = 0;		/* Upsample for G3RUH decoder. */
					/* Non-zero will override the default. */

struct audio_s my_audio_config;

//#define EXPERIMENT_G 1
//#define EXPERIMENT_H 1

#if EXPERIMENT_H
extern float space_gain[MAX_SUBCHANS];
#endif

static void usage (void);


int sample_number = -1;		/* Sample number from the file. */
					/* Incremented only for channel 0. */
					/* Use to print timestamp, relative to beginning */
					/* of file, when frame was decoded. */

// command line options.

int h_opt = 0;			// Hexadecimal display of received packet.
int d_o_opt = 0;			// "-d o" option for DCD output control. */	
int dcd_count = 0;
int dcd_missing_errors = 0;


/*
 * Simulate sample from the audio device.
 */

int audio_get_fake (int a)
{
	int ch;

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

int audio_get_real (int a);

int audio_get (int a) {
	if (ATEST_C) {
		return audio_get_fake(a);
	} else {
		return audio_get_real(a);
	}
}


/*
 * This is called when we have a good frame.
 */

void dlq_rec_frame_fake (int chan, int subchan, int slice, packet_t pp, alevel_t alevel, fec_type_t fec_type, retry_t retries, char *spectrum)
{	
	
	char stemp[500];
	unsigned char *pinfo;
	int info_len;
	int h;
	char heard[2 * AX25_MAX_ADDR_LEN + 20];
	char alevel_text[AX25_ALEVEL_TO_TEXT_SIZE];

	packets_decoded_one++;
	if ( ! hdlc_rec_data_detect_any(chan)) dcd_missing_errors++;

	ax25_format_addrs (pp, stemp);

	info_len = ax25_get_info (pp, &pinfo);

	/* Print so we can see what is going on. */

//TODO: quiet option - suppress packet printing, only the count at the end.

#if 1
	/* Display audio input level. */
        /* Who are we hearing?   Original station or digipeater? */

	if (ax25_get_num_addr(pp) == 0) {
	  /* Not AX.25. No station to display below. */
	  h = -1;
	  strlcpy (heard, "", sizeof(heard));
	}
	else {
	  h = ax25_get_heard(pp);
          ax25_get_addr_with_ssid(pp, h, heard);
	}

	text_color_set(DW_COLOR_DEBUG);
	dw_printf ("\n");
	dw_printf("DECODED[%d] ", packets_decoded_one );

	/* Insert time stamp relative to start of file. */

	double sec = (double)sample_number / my_audio_config.adev[0].samples_per_sec;
	int min = (int)(sec / 60.);
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
	      heard[5] == '\0') {

	  char probably_really[AX25_MAX_ADDR_LEN];
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
	    if (my_audio_config.achan[chan].fix_bits == RETRY_NONE && my_audio_config.achan[chan].passall == 0) {
	      // No fix_bits or passall specified.
	      dw_printf ("%s audio level = %s     %s\n", heard, alevel_text, spectrum);
	    }
	    else {
	      assert (retries >= RETRY_NONE && retries <= RETRY_MAX);   // validate array index.
	      dw_printf ("%s audio level = %s   [%s]   %s\n", heard, alevel_text, retry_text[(int)retries], spectrum);
	    }
	    break;
	}

#endif


// Display non-APRS packets in a different color.

// Display channel with subchannel/slice if applicable.

	if (ax25_is_aprs(pp)) {
	  text_color_set(DW_COLOR_REC);
	}
	else {
	  text_color_set(DW_COLOR_DEBUG);
	}

	if (my_audio_config.achan[chan].num_subchan > 1 && my_audio_config.achan[chan].num_slicers == 1) {
	  dw_printf ("[%d.%d] ", chan, subchan);
	}
	else if (my_audio_config.achan[chan].num_subchan == 1 && my_audio_config.achan[chan].num_slicers > 1) {
	  dw_printf ("[%d.%d] ", chan, slice);
	}
	else if (my_audio_config.achan[chan].num_subchan > 1 && my_audio_config.achan[chan].num_slicers > 1) {
	  dw_printf ("[%d.%d.%d] ", chan, subchan, slice);
	}
	else {
	  dw_printf ("[%d] ", chan);
	}

	dw_printf ("%s", stemp);			/* stations followed by : */
	ax25_safe_print ((char *)pinfo, info_len, 0);
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




#if 0		// temp experiment

#include "decode_aprs.h"

	if (ax25_is_aprs(pp)) {

	  decode_aprs_t A;

	  decode_aprs (&A, pp, 0, NULL);

	  // Temp experiment to see how different systems set the RR bits in the source and destination.
	  // log_rr_bits (&A, pp);

	}
#endif


	ax25_delete (pp);

} /* end fake dlq_append */


void ptt_set_fake (int ot, int chan, int ptt_signal)
{
	// Should only get here for DCD output control.
	static double dcd_start_time[MAX_RADIO_CHANS];

	if (d_o_opt) {
	  double t = (double)sample_number / my_audio_config.adev[0].samples_per_sec;
	  double sec1, sec2;
	  int min1, min2;

	  text_color_set(DW_COLOR_INFO);

	  if (ptt_signal) {
	    //sec1 = t;
	    //min1 = (int)(sec1 / 60.);
	    //sec1 -= min1 * 60;
	    //dw_printf ("DCD[%d] = ON    %d:%06.3f\n",  chan, min1, sec1);
	    dcd_count++;
	    dcd_start_time[chan] = t;
	  }
	  else {
	    //dw_printf ("DCD[%d] = off   %d:%06.3f   %3.0f\n",  chan, min, sec, (t - dcd_start_time[chan]) * 1000.);

	    sec1 = dcd_start_time[chan];
	    min1 = (int)(sec1 / 60.);
	    sec1 -= min1 * 60;

	    sec2 = t;
	    min2 = (int)(sec2 / 60.);
	    sec2 -= min2 * 60;

	    dw_printf ("DCD[%d]  %d:%06.3f - %d:%06.3f =  %3.0f\n",  chan, min1, sec1, min2, sec2, (t - dcd_start_time[chan]) * 1000.);
	  }
	}
	return;
}

void ptt_set_real (int ot, int chan, int ptt_signal);

void ptt_set (int ot, int chan, int ptt_signal) {
	if (ATEST_C) {
		ptt_set_fake(ot, chan, ptt_signal);
	} else {
		ptt_set_real(ot, chan, ptt_signal);
	}
}

int get_input_fake (int it, int chan)
{
	return -1;
}

int get_input_real (int it, int chan);

int get_input (int it, int chan) {
	if (ATEST_C) {
		return get_input_fake(it, chan);
	} else {
		return get_input_real(it, chan);
	}
}

/* end atest.c */
