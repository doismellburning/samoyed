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
#include "waypoint.h"
#include "gen_tone.h"
#include "digipeater.h"
#include "cdigipeater.h"
#include "tq.h"
#include "xmit.h"
#include "ptt.h"
#include "beacon.h"
#include "dtmf.h"
#include "aprs_tt.h"
#include "tt_user.h"
#include "igate.h"
#include "pfilter.h"
#include "symbols.h"
#include "dwgps.h"
#include "waypoint.h"
#include "log.h"
#include "recv.h"
#include "morse.h"
#include "mheard.h"
#include "ax25_link.h"
#include "dtime_now.h"
#include "fx25.h"
#include "il2p.h"
#include "dwsock.h"
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


/*-------------------------------------------------------------------
 *
 * Name:        app_process_rec_frame
 *
 * Purpose:     This is called when we receive a frame with a valid 
 *		FCS and acceptable size.
 *
 * Inputs:	chan	- Audio channel number, 0 or 1.
 *		subchan	- Which modem caught it.  
 *			  Special cases:
 *				-1 for DTMF decoder.
 *				-2 for channel mapped to APRS-IS.
 *				-3 for channel mapped to network TNC.
 *		slice	- Slicer which caught it.
 *		pp	- Packet handle.
 *		alevel	- Audio level, range of 0 - 100.
 *				(Special case, use negative to skip
 *				 display of audio level line.
 *				 Use -2 to indicate DTMF message.)
 *		retries	- Level of bit correction used.
 *		spectrum - Display of how well multiple decoders did.
 *
 *
 * Description:	Print decoded packet.
 *		Optionally send to another application.
 *
 *--------------------------------------------------------------------*/

// TODO:  Use only one printf per line so output doesn't get jumbled up with stuff from other threads.

void app_process_rec_packet (int chan, int subchan, int slice, packet_t pp, alevel_t alevel, fec_type_t fec_type, retry_t retries, char *spectrum)
{	
	
	char stemp[500];
	unsigned char *pinfo;
	int info_len;
	char heard[AX25_MAX_ADDR_LEN];
	//int j;
	int h;
	char display_retries[32];				// Extra stuff before slice indicators.
								// Can indicate FX.25/IL2P or fix_bits.

	assert (chan >= 0 && chan < MAX_TOTAL_CHANS);		// TOTAL for virtual channels
	assert (subchan >= -3 && subchan < MAX_SUBCHANS);
	assert (slice >= 0 && slice < MAX_SLICERS);
	assert (pp != NULL);	// 1.1J+
     
	strlcpy (display_retries, "", sizeof(display_retries));

	switch (fec_type) {
	  case fec_type_fx25:
	    strlcpy (display_retries, " FX.25 ", sizeof(display_retries));
	    break;
	  case fec_type_il2p:
	    strlcpy (display_retries, " IL2P ", sizeof(display_retries));
	    break;
	  case fec_type_none:
	  default:
	    // Possible fix_bits indication.
	    if (audio_config.achan[chan].fix_bits != RETRY_NONE || audio_config.achan[chan].passall) {
	      assert (retries >= RETRY_NONE && retries <= RETRY_MAX);
	      snprintf (display_retries, sizeof(display_retries), " [%s] ", retry_text[(int)retries]);
	    }
	    break;
	}

	ax25_format_addrs (pp, stemp);

	info_len = ax25_get_info (pp, &pinfo);

	/* Print so we can see what is going on. */

	/* Display audio input level. */
        /* Who are we hearing?   Original station or digipeater. */

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

// The HEARD line.

	if (( ! q_h_opt ) && alevel.rec >= 0) {    /* suppress if "-q h" option */
// FIXME: rather than checking for ichannel, how about checking medium==radio
	 if (chan != audio_config.igate_vchannel) {	// suppress if from ICHANNEL
	  if (h != -1 && h != AX25_SOURCE) {
	    dw_printf ("Digipeater ");
	  }

	  char alevel_text[AX25_ALEVEL_TO_TEXT_SIZE];

	  ax25_alevel_to_text (alevel, alevel_text);

// Experiment: try displaying the DC bias.
// Should be 0 for soundcard but could show mistuning with SDR.

#if 0
	  char bias[16];
	  snprintf (bias, sizeof(bias), " DC%+d", multi_modem_get_dc_average (chan));
	  strlcat (alevel_text, bias, sizeof(alevel_text));
#endif

	  /* As suggested by KJ4ERJ, if we are receiving from */
	  /* WIDEn-0, it is quite likely (but not guaranteed), that */
	  /* we are actually hearing the preceding station in the path. */

	  if (h >= AX25_REPEATER_2 && 
	        strncmp(heard, "WIDE", 4) == 0 &&
	        isdigit(heard[4]) &&
	        heard[5] == '\0') {

	    char probably_really[AX25_MAX_ADDR_LEN];


	    ax25_get_addr_with_ssid(pp, h-1, probably_really);

	    // audio level applies only for internal modem channels.
	    if (subchan >=0) {
	      dw_printf ("%s (probably %s) audio level = %s  %s  %s\n", heard, probably_really, alevel_text, display_retries, spectrum);
	    }
	    else {
	      dw_printf ("%s (probably %s)\n", heard, probably_really);
	    }

	  }
	  else if (strcmp(heard, "DTMF") == 0) {

	    dw_printf ("%s audio level = %s  tt\n", heard, alevel_text);
	  }
	  else {

	    // audio level applies only for internal modem channels.
	    if (subchan >= 0) {
	      dw_printf ("%s audio level = %s  %s  %s\n", heard, alevel_text, display_retries, spectrum);
	    }
	    else {
	      dw_printf ("%s\n", heard);
	    }
	  }
	 }
	}

	/* Version 1.2:   Cranking the input level way up produces 199. */
	/* Keeping it under 100 gives us plenty of headroom to avoid saturation. */

	// TODO:  suppress this message if not using soundcard input.
	// i.e. we have no control over the situation when using SDR.

	if (alevel.rec > 110) {

	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Audio input level is too high. This may cause distortion and reduced decode performance.\n");
	  dw_printf ("Solution is to decrease the audio input level.\n");
	  dw_printf ("Setting audio input level so most stations are around 50 will provide good dyanmic range.\n");
	}
// FIXME: rather than checking for ichannel, how about checking medium==radio
	else if (alevel.rec < 5 && chan != audio_config.igate_vchannel && subchan != -3) {

	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Audio input level is too low.  Increase so most stations are around 50.\n");
	}


// Display non-APRS packets in a different color.

// Display subchannel only when multiple modems configured for channel.

// -1 for APRStt DTMF decoder.

	char ts[100];		// optional time stamp

	if (strlen(audio_config.timestamp_format) > 0) {
	  char tstmp[100];
	  timestamp_user_format (tstmp, sizeof(tstmp), audio_config.timestamp_format);
	  strlcpy (ts, " ", sizeof(ts));	// space after channel.
	  strlcat (ts, tstmp, sizeof(ts));
	}
	else {
	  strlcpy (ts, "", sizeof(ts));
	}

	if (subchan == -1) {	// dtmf
	  text_color_set(DW_COLOR_REC);
	  dw_printf ("[%d.dtmf%s] ", chan, ts);
	}
	else if (subchan == -2) {	// APRS-IS
	  text_color_set(DW_COLOR_REC);
	  dw_printf ("[%d.is%s] ", chan, ts);
	}
	else if (subchan == -3) {	// nettnc
	  text_color_set(DW_COLOR_REC);
	  dw_printf ("[%d%s] ", chan, ts);
	}
	else {
	  if (ax25_is_aprs(pp)) {
	    text_color_set(DW_COLOR_REC);
	  }
	  else {
	    text_color_set(DW_COLOR_DECODED);
	  }

	  if (audio_config.achan[chan].num_subchan > 1 && audio_config.achan[chan].num_slicers == 1) {
	    dw_printf ("[%d.%d%s] ", chan, subchan, ts);
	  }
	  else if (audio_config.achan[chan].num_subchan == 1 && audio_config.achan[chan].num_slicers > 1) {
	    dw_printf ("[%d.%d%s] ", chan, slice, ts);
	  }
	  else if (audio_config.achan[chan].num_subchan > 1 && audio_config.achan[chan].num_slicers > 1) {
	    dw_printf ("[%d.%d.%d%s] ", chan, subchan, slice, ts);
	  }
	  else {
	    dw_printf ("[%d%s] ", chan, ts);
	  }
	}

	dw_printf ("%s", stemp);			/* stations followed by : */

/* Demystify non-APRS.  Use same format for transmitted frames in xmit.c. */

	if ( ! ax25_is_aprs(pp)) {
	  ax25_frame_type_t ftype;
	  cmdres_t cr;
	  char desc[80];
	  int pf;
	  int nr;
	  int ns;

	  ftype = ax25_frame_type (pp, &cr, desc, &pf, &nr, &ns);

	  /* Could change by 1, since earlier call, if we guess at modulo 128. */
	  info_len = ax25_get_info (pp, &pinfo);

	  dw_printf ("(%s)", desc);
	  if (ftype == frame_type_U_XID) {
	    struct xid_param_s param;
	    char info2text[150];

	    xid_parse (pinfo, info_len, &param, info2text, sizeof(info2text));
	    dw_printf (" %s\n", info2text);
	  }
	  else {
	    ax25_safe_print ((char *)pinfo, info_len, ( ! ax25_is_aprs(pp)) && ( ! d_u_opt) );
	    dw_printf ("\n");
	  }
	}
	else {

	  // for APRS we generally want to display non-ASCII to see UTF-8.
	  // for other, probably want to restrict to ASCII only because we are
	  // more likely to have compressed data than UTF-8 text.

	  // TODO: Might want to use d_u_opt for transmitted frames too.

	  ax25_safe_print ((char *)pinfo, info_len, ( ! ax25_is_aprs(pp)) && ( ! d_u_opt) );
	  dw_printf ("\n");
	}


// Also display in pure ASCII if non-ASCII characters and "-d u" option specified.

	if (d_u_opt) {

	  unsigned char *p;
	  int n = 0;

	  for (p = pinfo; *p != '\0'; p++) {
	    if (*p >= 0x80) n++;
	  }

	  if (n > 0) {
	    text_color_set(DW_COLOR_DEBUG);
	    ax25_safe_print ((char *)pinfo, info_len, 1);
	    dw_printf ("\n");
	  }
	}

/* Optional hex dump of packet. */

	if (d_p_opt) {

	  text_color_set(DW_COLOR_DEBUG);
	  dw_printf ("------\n");
	  ax25_hex_dump (pp);
	  dw_printf ("------\n");
	}


/*
 * Decode the contents of UI frames and display in human-readable form.
 * Could be APRS or anything random for old fashioned packet beacons.
 *
 * Suppress printed decoding if "-q d" option used.
 */
	char ais_obj_packet[300];
	strcpy (ais_obj_packet, "");

	if (ax25_is_aprs(pp)) {

	  decode_aprs_t A;

	  // we still want to decode it for logging and other processing.
	  // Just be quiet about errors if "-qd" is set.

	  decode_aprs (&A, pp, q_d_opt, NULL);

	  if ( ! q_d_opt ) {

	    // Print it all out in human readable format unless "-q d" option used.

	    decode_aprs_print (&A);
	  }

	  /*
	   * Perform validity check on each address.
	   * This should print an error message if any issues.
	   */
	  (void)ax25_check_addresses(pp);

	  // Send to log file.

	  log_write (chan, &A, pp, alevel, retries);

	  // temp experiment.
	  //log_rr_bits (&A, pp);

	  // Add to list of stations heard over the radio.

	  mheard_save_rf (chan, &A, pp, alevel, retries);

// For AIS, we have an option to convert the NMEA format, in User Defined data,
// into an APRS "Object Report" and send that to the clients as well.

// FIXME: partial implementation.

	  static const char user_def_da[4] = { '{', USER_DEF_USER_ID, USER_DEF_TYPE_AIS, '\0' };

	  if (strncmp((char*)pinfo, user_def_da, 3) == 0) {

	    waypoint_send_ais((char*)pinfo + 3);

	    if (A_opt_ais_to_obj && A.g_lat != G_UNKNOWN && A.g_lon != G_UNKNOWN) {

	      char ais_obj_info[256];
	      (void)encode_object (A.g_name, 0, time(NULL),
	        A.g_lat, A.g_lon, 0,	// no ambiguity
		A.g_symbol_table, A.g_symbol_code,
		0, 0, 0, "",	// power, height, gain, direction.
	        // Unknown not handled properly.
		// Should encode_object take floating point here?
		(int)(A.g_course+0.5), (int)(DW_MPH_TO_KNOTS(A.g_speed_mph)+0.5),
		0, 0, 0, A.g_comment,	// freq, tone, offset
		ais_obj_info, sizeof(ais_obj_info));

	      snprintf (ais_obj_packet, sizeof(ais_obj_packet), "%s>%s%1d%1d,NOGATE:%s", A.g_src, APP_TOCALL, MAJOR_VERSION, MINOR_VERSION, ais_obj_info);

	      dw_printf ("[%d.AIS] %s\n", chan, ais_obj_packet);

	      // This will be sent to client apps after the User Defined Data representation.
	    }
	  }

	  // Convert to NMEA waypoint sentence if we have a location.

 	  if (A.g_lat != G_UNKNOWN && A.g_lon != G_UNKNOWN) {
	    waypoint_send_sentence (strlen(A.g_name) > 0 ? A.g_name : A.g_src, 
		A.g_lat, A.g_lon, A.g_symbol_table, A.g_symbol_code, 
		DW_FEET_TO_METERS(A.g_altitude_ft), A.g_course, DW_MPH_TO_KNOTS(A.g_speed_mph), 
		A.g_comment);
	  }
	}


/* Send to another application if connected. */
// TODO:  Put a wrapper around this so we only call one function to send by all methods.
// We see the same sequence in tt_user.c.

	int flen;
	unsigned char fbuf[AX25_MAX_PACKET_LEN];

	flen = ax25_pack(pp, fbuf);

	server_send_rec_packet (chan, pp, fbuf, flen);					// AGW net protocol
	kissnet_send_rec_packet (chan, KISS_CMD_DATA_FRAME, fbuf, flen, NULL, -1);	// KISS TCP
	kissserial_send_rec_packet (chan, KISS_CMD_DATA_FRAME, fbuf, flen, NULL, -1);	// KISS serial port
	kisspt_send_rec_packet (chan, KISS_CMD_DATA_FRAME, fbuf, flen, NULL, -1);	// KISS pseudo terminal

	if (A_opt_ais_to_obj && strlen(ais_obj_packet) != 0) {
	  packet_t ao_pp = ax25_from_text (ais_obj_packet, 1);
	  if (ao_pp != NULL) {
	    unsigned char ao_fbuf[AX25_MAX_PACKET_LEN];
	    int ao_flen = ax25_pack(ao_pp, ao_fbuf);

	    server_send_rec_packet (chan, ao_pp, ao_fbuf, ao_flen);
	    kissnet_send_rec_packet (chan, KISS_CMD_DATA_FRAME, ao_fbuf, ao_flen, NULL, -1);
	    kissserial_send_rec_packet (chan, KISS_CMD_DATA_FRAME, ao_fbuf, ao_flen, NULL, -1);
	    kisspt_send_rec_packet (chan, KISS_CMD_DATA_FRAME, ao_fbuf, ao_flen, NULL, -1);
	    ax25_delete (ao_pp);
	  }
	}

/*
 * If it is from the ICHANNEL, we are done.
 * Don't digipeat.  Don't IGate.
 * Don't do anything with it after printing and sending to client apps.
 */

	if (chan == audio_config.igate_vchannel) {
	    return;
	}

/* 
 * If it came from DTMF decoder (subchan == -1), send it to APRStt gateway.
 * Otherwise, it is a candidate for IGate and digipeater.
 *
 * It is also useful to have some way to simulate touch tone
 * sequences with BEACON sendto=R0 for testing.
 */

	if (subchan == -1) {		// from DTMF decoder
	  if (tt_config.gateway_enabled && info_len >= 2) {
	    aprs_tt_sequence (chan, (char*)(pinfo+1));
	  }
	}
	else if (*pinfo == 't' && info_len >= 2 && tt_config.gateway_enabled) {
				// For testing.
				// Would be nice to verify it was generated locally,
				// not received over the air.
	  aprs_tt_sequence (chan, (char*)(pinfo+1));
	}
	else { 
	
/*
 * Send to the IGate processing.
 * Use only those with correct CRC; We don't want to spread corrupted data!
 * Our earlier "fix bits" hack could allow corrupted information to get thru.
 * However, if it used FEC mode (FX.25. IL2P), we have much higher level of
 * confidence that it is correct.
 */
	  if (ax25_is_aprs(pp) && ( retries == RETRY_NONE || fec_type == fec_type_fx25 || fec_type == fec_type_il2p) ) {

	    igate_send_rec_packet (chan, pp);
	  }


/* Send out a regenerated copy. Applies to all types, not just APRS. */
/* This was an experimental feature never documented in the User Guide. */
/* Initial feedback was positive but it fell by the wayside. */
/* Should follow up with testers and either document this or clean out the clutter. */

	  digi_regen (chan, pp);


/*
 * Send to APRS digipeater.
 * Use only those with correct CRC; We don't want to spread corrupted data!
 * Our earlier "fix bits" hack could allow corrupted information to get thru.
 * However, if it used FEC mode (FX.25. IL2P), we have much higher level of
 * confidence that it is correct.
 */
	  if (ax25_is_aprs(pp) && ( retries == RETRY_NONE || fec_type == fec_type_fx25 || fec_type == fec_type_il2p) ) {

	    digipeater (chan, pp);
	  }

/*
 * Connected mode digipeater.
 * Use only those with correct CRC (or using FEC.)
 */

	  if (chan < MAX_RADIO_CHANS) {
	    if (retries == RETRY_NONE || fec_type == fec_type_fx25 || fec_type == fec_type_il2p) {
	      cdigipeater (chan, pp);
	    }
	  }
	}

} /* end app_process_rec_packet */



/* Process control C and window close events. */

#if __WIN32__

static BOOL cleanup_win (int ctrltype)
{
	if (ctrltype == CTRL_C_EVENT || ctrltype == CTRL_CLOSE_EVENT) {
	  text_color_set(DW_COLOR_INFO);
	  dw_printf ("\nQRT\n");
	  log_term ();
	  ptt_term ();
	  waypoint_term ();
	  dwgps_term ();
	  SLEEP_SEC(1);
	  ExitProcess (0);
	}
	return (TRUE);
}


#else

void cleanup_linux (int x)
{
	text_color_set(DW_COLOR_INFO);
	dw_printf ("\nQRT\n");
	log_term ();
	ptt_term ();
	dwgps_term ();
	SLEEP_SEC(1);
	exit(0);
}

#endif

// Because passing a function to signal() from Go is nontrivial
void setup_sigint_handler() {
	signal(SIGINT, cleanup_linux);
}

/* end direwolf.c */
