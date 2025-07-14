//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2011, 2013, 2014, 2015, 2016, 2019, 2021, 2023  John Langner, WB2OSZ
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



#include "direwolf.h"

#include <stdio.h>     
#include <stdlib.h>    
#include <getopt.h>
#include <string.h>
#include <assert.h>
#include <math.h>

#include "audio.h"
#include "ax25_pad.h"
#include "hdlc_send.h"
#include "gen_tone.h"
#include "textcolor.h"
#include "morse.h"
#include "dtmf.h"
#include "fx25.h"
#include "il2p.h"
#include "gen_packets.h"

int GEN_PACKETS = 0; // Switch between fakes and reals at runtime

/* Own random number generator so we can get */
/* same results on Windows and Linux. */

static int seed = 1;

int my_rand (void) {
	// Perform the calculation as unsigned to avoid signed overflow error.
	seed = (int)(((unsigned)seed * 1103515245) + 12345) & MY_RAND_MAX;
	return (seed);
}

static int audio_file_open (char *fname, struct audio_s *pa);

int g_add_noise = 0;
float g_noise_level = 0;

FILE *out_fp = NULL;

int byte_count;			/* Number of data bytes written to file. */
					/* Will be written to header when file is closed. */


/*------------------------------------------------------------------
 *
 * Name:        audio_put
 *
 * Purpose:     Send one byte to the audio output file.
 *
 * Inputs:	c	- One byte in range of 0 - 255.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 * Description:	The caller must deal with the details of mono/stereo
 *		and number of bytes per sample.
 *
 *----------------------------------------------------------------*/


int audio_put_fake (int a, int c)
{
	static short sample16;
	int s;

	if (g_add_noise) {

	  if ((byte_count & 1) == 0) {
	    sample16 = c & 0xff;		/* save lower byte. */
	    byte_count++;
	    return c;
	  }
	  else {
	    float r;

	    sample16 |= (c << 8) & 0xff00;	/* insert upper byte. */
	    byte_count++;
	    s = sample16;  // sign extend.

/* Add random noise to the signal. */
/* r should be in range of -1 .. +1. */

/* Use own function instead of rand() from the C library. */
/* Windows and Linux have different results, messing up my self test procedure. */
/* No idea what Mac OSX and BSD might do. */
 

	    r = (my_rand() - MY_RAND_MAX/2.0) / (MY_RAND_MAX/2.0);

	    s += 5 * r * g_noise_level * 32767;

	    if (s > 32767) s = 32767;
	    if (s < -32767) s = -32767;

	    putc(s & 0xff, out_fp);  
	    return (putc((s >> 8) & 0xff, out_fp));
	  }
	}
	else {
	  byte_count++;
	  return (putc(c, out_fp));
	}

} /* end audio_put */

int audio_put_real(int a, int c);
int audio_put(int a, int c) {
	if (GEN_PACKETS) {
		return audio_put_fake(a, c);
	} else {
		return audio_put_real(a, c);
	}
}

int audio_flush_fake (int a)
{
	return 0;
}

int audio_flush_real(int a);
int audio_flush(int a) {
	if (GEN_PACKETS) {
		return audio_flush_fake(a);
	} else {
		return audio_flush_real(a);
	}
}

// To keep dtmf.c happy.

#include "hdlc_rec.h"    // for dcd_change

void dcd_change_fake (int chan, int subchan, int slice, int state)
{
}

void dcd_change_real(int chan, int subchan, int slice, int state);
void dcd_change(int chan, int subchan, int slice, int state) {
	if (GEN_PACKETS) {
		dcd_change_fake(chan, subchan, slice, state);
	} else {
		dcd_change_real(chan, subchan, slice, state);
	}
}
