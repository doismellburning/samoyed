//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2011, 2012, 2013, 2014, 2015, 2016, 2017, 2019, 2020, 2021, 2023, 2024  John Langner, WB2OSZ
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


/*------------------------------------------------------------------
 *
 * Module:      direwolf.c
 *
 * Purpose:   	Main program for "Dire Wolf" which includes:
 *			
 *			Various DSP modems using the "sound card."
 *			AX.25 encoder/decoder.
 *			APRS data encoder / decoder.
 *			APRS digipeater.
 *			KISS TNC emulator.
 *			APRStt (touch tone input) gateway
 *			Internet Gateway (IGate)
 *			Ham Radio of Things - IoT with Ham Radio
 *			FX.25 Forward Error Correction.
 *			IL2P Forward Error Correction.
 *			Emergency Alert System (EAS) Specific Area Message Encoding (SAME) receiver.
 *			AIS receiver for tracking ships.
 *
 *---------------------------------------------------------------*/


#define DIREWOLF_C 1

#include "direwolf.h"



#include <stdio.h>
#include <math.h>
#include <stdlib.h>
#include <getopt.h>
#include <assert.h>
#include <string.h>
#include <signal.h>
#include <ctype.h>

#if __ARM__
//#include <asm/hwcap.h>
//#include <sys/auxv.h>		// Doesn't seem to be there.
				// We have libc 2.13.  Looks like we might need 2.17 & gcc 4.8
#endif

#if __WIN32__
#include <stdio.h>
#include <io.h>
#else
#include <unistd.h>
#include <fcntl.h>
#include <sys/types.h>
#include <sys/ioctl.h>
#if USE_SNDIO || __APPLE__
// no need to include <soundcard.h>
#else
#include <sys/soundcard.h>
#endif
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>
#endif

#if USE_HAMLIB
#include <hamlib/rig.h>
#endif



#include "version.h"
#include "audio.h"
#include "config.h"
#include "multi_modem.h"
#include "demod.h"
#include "hdlc_rec.h"
#include "hdlc_rec2.h"
#include "ax25_pad.h"
#include "xid.h"
#include "decode_aprs.h"
#include "encode_aprs.h"
#include "textcolor.h"
#include "server.h"
#include "kiss.h"
#include "kissnet.h"
#include "kissserial.h"
#include "kiss_frame.h"
#include "gen_tone.h"
#include "digipeater.h"
#include "cdigipeater.h"
#include "tq.h"
#include "ptt.h"
#include "dtmf.h"
#include "aprs_tt.h"
#include "tt_user.h"
#include "igate.h"
#include "pfilter.h"
#include "symbols.h"
#include "dwgps.h"
#include "log.h"
#include "recv.h"
#include "morse.h"
#include "mheard.h"
#include "ax25_link.h"
#include "dtime_now.h"
#include "fx25.h"
#include "il2p.h"
#include "dns_sd_dw.h"
#include "dlq.h"		// for fec_type_t definition.
#include "deviceid.h"
#include "nettnc.h"


//static int idx_decoded = 0;

#if __WIN32__
static BOOL cleanup_win (int);
#else
void cleanup_linux (int);
#endif

#if defined(__SSE__) && !defined(__APPLE__)

static void __cpuid(int cpuinfo[4], int infotype){
    __asm__ __volatile__ (
        "cpuid":
        "=a" (cpuinfo[0]),
        "=b" (cpuinfo[1]),
        "=c" (cpuinfo[2]),
        "=d" (cpuinfo[3]):
        "a" (infotype)
    );
}

#endif


struct audio_s audio_config;
struct tt_config_s tt_config;
struct misc_config_s misc_config;


static const int audio_amplitude = 100;	/* % of audio sample range. */
					/* This translates to +-32k for 16 bit samples. */
					/* Currently no option to change this. */

int d_u_opt = 0;			/* "-d u" command line option to print UTF-8 also in hexadecimal. */
int d_p_opt = 0;			/* "-d p" option for dumping packets over radio. */				

int q_h_opt = 0;			/* "-q h" Quiet, suppress the "heard" line with audio level. */
int q_d_opt = 0;			/* "-q d" Quiet, suppress the printing of description of APRS packets. */

int A_opt_ais_to_obj = 0;	/* "-A" Convert received AIS to APRS "Object Report." */

/* end direwolf.c */
