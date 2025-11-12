package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Keep track of the APRStt users.
 *
 * Description: This maintains a list of recently heard APRStt users
 *		and prepares "object" format packets for transmission.
 *
 * References:	This is based upon APRStt (TM) documents but not 100%
 *		compliant due to ambiguities and inconsistencies in
 *		the specifications.
 *
 *		http://www.aprs.org/aprstt.html
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
// #include <unistd.h>
// #include <ctype.h>
// #include <time.h>
// #include <assert.h>
// #include "version.h"
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "aprs_tt.h"
// #include "tt_text.h"
// #include "dedupe.h"
// #include "tq.h"
// #include "igate.h"
// #include "encode_aprs.h"
// #include "latlong.h"
// #include "kiss.h"
// #include "kissserial.h"
// #include "kissnet.h"
// #include "kiss_frame.h"
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unsafe"
)

/*
 * Information kept about local APRStt users.
 *
 * For now, just use a fixed size array for simplicity.
 */

var TT_TESTS_RUNNING = false

const MAX_TT_USERS = 100

const MAX_CALLSIGN_LEN = 9 /* "Object Report" names can be up to 9 characters. */

const MAX_COMMENT_LEN = 43 /* Max length of comment in "Object Report." */

type tt_user_s struct {
	callsign string /* Callsign of station heard. */
	/* Does not include the "-12" SSID added later. */
	/* Possibly other tactical call / object label. */
	/* Null string indicates table position is not used. */

	count int /* Number of times we received information for this object. */
	/* Value 1 means first time and could be used to send */
	/* a welcome greeting. */

	ssid int /* SSID to add. */
	/* Default of 12 but not always. */

	overlay rune /* Overlay character. Should be 0-9, A-Z. */
	/* Could be / or \ for general object. */

	symbol rune /* 'A' for traditional.  */
	/* Can be any symbol for extended objects. */

	digit_suffix string /* Suffix abbreviation as 3 digits. */

	last_heard time.Time /* Timestamp when last heard.  */
	/* User information will be deleted at some */
	/* point after last time being heard. */

	xmits int /* Number of remaining times to transmit info */
	/* about the user.   This is set to 3 when */
	/* a station is heard and decremented each time */
	/* an object packet is sent.  The idea is to send */
	/* 3 within 30 seconds to improve chances of */
	/* being heard while using digipeater duplicate */
	/* removal. */
	// TODO:  I think implementation is different.

	next_xmit time.Time /* Time for next transmit.  Meaningful only if xmits > 0. */

	corral_slot int /* If location is known, set this to 0. */
	/* Otherwise, this is a display offset position */
	/* from the gateway. */

	loc_text string /* Text representation of location when a single */
	/* lat/lon point would be deceptive.  e.g.  */
	/* 32TPP8049 */
	/* 32TPP8179549363 */
	/* 32T 681795 4849363 */
	/* EM29QE78 */

	latitude, longitude float64 /* Location either from user or generated */
	/* position in the corral. */

	ambiguity int /* Number of digits to omit from location. */
	/* Default 0, max. 4. */

	freq string /* Frequency in format 999.999MHz */

	ctcss string /* CTCSS tone.  Exactly 3 digits for integer part. */
	/* For example 74.4 Hz becomes "074". */

	comment string /* Free form comment from user. */
	/* Comment sent in final object report includes */
	/* other information besides this. */

	mic_e rune /* Position status. */
	/* Should be a character in range of '1' to '9' for */
	/* the predefined status strings or '0' for none. */

	dao string /* Enhanced position information. */
}

var tt_user [MAX_TT_USERS]tt_user_s

/*------------------------------------------------------------------
 *
 * Name:        tt_user_init
 *
 * Purpose:     Initialize the APRStt gateway at system startup time.
 *
 * Inputs:      Configuration options gathered by config.c.
 *
 * Global out:	Make our own local copy of the structure here.
 *
 * Returns:     None
 *
 * Description:	The main program needs to call this at application
 *		start up time after reading the configuration file.
 *
 *		TT_TESTS_RUNNING is defined for unit testing.
 *
 *----------------------------------------------------------------*/

var save_tt_config_p *C.struct_tt_config_s

func tt_user_init(p_audio_config *C.struct_audio_s, p_tt_config *C.struct_tt_config_s) {
	save_audio_config_p = p_audio_config

	save_tt_config_p = p_tt_config

	for i := range MAX_TT_USERS {
		clear_user(i)
	}
}

/*------------------------------------------------------------------
 *
 * Name:        tt_user_search
 *
 * Purpose:     Search for user in recent history.
 *
 * Inputs:      callsign	- full or a old style 3 DIGIT suffix abbreviation
 *		overlay
 *
 * Returns:     Handle for referring to table position or -1 if not found.
 *		This happens to be an index into an array but
 *		the implementation could change so the caller should
 *		not make any assumptions.
 *
 *----------------------------------------------------------------*/

func tt_user_search(callsign string, overlay rune) int {
	/*
	 * First, look for exact match to full call and overlay.
	 */
	for i := range MAX_TT_USERS {
		if callsign == tt_user[i].callsign &&
			overlay == tt_user[i].overlay {
			return (i)
		}
	}

	/*
	 * Look for digits only suffix plus overlay.
	 */
	for i := range MAX_TT_USERS {
		if callsign == tt_user[i].digit_suffix &&
			overlay != ' ' &&
			overlay == tt_user[i].overlay {
			return (i)
		}
	}

	/*
	 * Look for digits only suffix if no overlay was specified.
	 */
	for i := range MAX_TT_USERS {
		if callsign == tt_user[i].digit_suffix &&
			overlay == ' ' {
			return (i)
		}
	}

	/*
	 * Not sure about the new spelled suffix yet...
	 */
	return (-1)
} /* end tt_user_search */

/*------------------------------------------------------------------
 *
 * Name:        tt_3char_suffix_search
 *
 * Purpose:     Search for new style 3 CHARACTER (vs. 3 digit) suffix in recent history.
 *
 * Inputs:      suffix	- full or a old style 3 DIGIT suffix abbreviation
 *
 * Outputs:	callsign - corresponding full callsign or empty string.
 *
 * Returns:     Handle for referring to table position (>= 0) or -1 if not found.
 *		This happens to be an index into an array but
 *		the implementation could change so the caller should
 *		not make any assumptions.
 *
 *----------------------------------------------------------------*/

func tt_3char_suffix_search(suffix string) (string, int) {
	/*
	 * Look for suffix in list of known calls.
	 */
	for i := range MAX_TT_USERS {
		var length = len(tt_user[i].callsign)

		if length >= 3 && length <= 6 && tt_user[i].callsign[length-3:] == suffix {
			return tt_user[i].callsign, i
		}
	}

	/*
	 * Not found.
	 */
	return "", -1
} /* end tt_3char_suffix_search */

/*------------------------------------------------------------------
 *
 * Name:        clear_user
 *
 * Purpose:     Clear specified user table entry.
 *
 * Inputs:      handle for user table entry.
 *
 *----------------------------------------------------------------*/

func clear_user(i int) {
	Assert(i >= 0 && i < MAX_TT_USERS)

	tt_user[i] = tt_user_s{} //nolint:exhaustruct
} /* end clear_user */

/*------------------------------------------------------------------
 *
 * Name:        find_avail
 *
 * Purpose:     Find an available user table location.
 *
 * Inputs:      none
 *
 * Returns:     Handle for referring to table position.
 *
 * Description:	If table is already full, this should delete the
 *		least recently heard user to make room.
 *
 *----------------------------------------------------------------*/

func find_avail() int {
	for i := range MAX_TT_USERS {
		if tt_user[i].callsign == "" {
			clear_user(i)
			return (i)
		}
	}

	/* Remove least recently heard. */

	var i_oldest = 0

	for i := range MAX_TT_USERS {
		if tt_user[i].last_heard.Before(tt_user[i_oldest].last_heard) {
			i_oldest = i
		}
	}

	clear_user(i_oldest)
	return (i_oldest)
} /* end find_avail */

/*------------------------------------------------------------------
 *
 * Name:        corral_slot
 *
 * Purpose:     Find an available position in the corral.
 *
 * Inputs:      none
 *
 * Returns:     Small integer >= 1 not already in use.
 *
 *----------------------------------------------------------------*/

func corral_slot() int {
	for slot := 1; ; slot++ {
		var used = false
		for i := 0; i < MAX_TT_USERS && !used; i++ {
			if tt_user[i].callsign != "" && tt_user[i].corral_slot == slot {
				used = true
			}
		}
		if !used {
			return (slot)
		}
	}
} /* end corral_slot */

/*------------------------------------------------------------------
 *
 * Name:        digit_suffix
 *
 * Purpose:     Find 3 digit only suffix code for given call.
 *
 * Inputs:      callsign
 *
 * Outputs:	3 digit suffix
 *
 *----------------------------------------------------------------*/

func digit_suffix(callsign string) string {
	var suffix = []byte{'0', '0', '0'}

	var _two_key [50]C.char
	C.tt_text_to_two_key(C.CString(callsign), 0, &_two_key[0])
	var two_key = C.GoString(&_two_key[0])

	for _, t := range two_key {
		if unicode.IsDigit(t) {
			suffix[0] = suffix[1]
			suffix[1] = suffix[2]
			suffix[2] = byte(t)
		}
	}

	return string(suffix)
}

/*------------------------------------------------------------------
 *
 * Name:        tt_user_heard
 *
 * Purpose:     Record information from an APRStt transmission.
 *
 * Inputs:      callsign	- full or an abbreviation
 *		ssid
 *		overlay		- or symbol table identifier
 *		symbol
 *		loc_text	- Original text for non lat/lon location
 *		latitude
 *		longitude
 *		ambiguity
 *		freq
 *		ctcss
 *		comment
 *		mic_e
 *		dao
 *
 * Outputs:	Information is stored in table above.
 *		Last heard time is updated.
 *		Object Report transmission is scheduled.
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 *----------------------------------------------------------------*/

func tt_user_heard(callsign string, ssid int, overlay rune, symbol rune, loc_text string, latitude float64,
	longitude float64, ambiguity int, freq string, ctcss string, comment string, mic_e rune, dao string) int {
	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("tt_user_heard (%s, %d, %c, %c, %s, ...)\n", callsign, ssid, overlay, symbol, loc_text);

	/*
	 * At this time all messages are expected to contain a callsign.
	 * Other types of messages, not related to a particular person/object
	 * are a future possibility.
	 */
	if callsign == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("APRStt tone sequence did not include callsign / object name.\n")
		return (C.TT_ERROR_NO_CALL)
	}

	/*
	 * Is it someone new or a returning user?
	 */
	var i = tt_user_search(callsign, overlay)
	if i == -1 {
		/*
		 * New person.  Create new table entry with all available information.
		 */
		i = find_avail()

		Assert(i >= 0 && i < MAX_TT_USERS)
		tt_user[i].callsign = callsign
		tt_user[i].count = 1
		tt_user[i].ssid = ssid
		tt_user[i].overlay = overlay
		tt_user[i].symbol = symbol
		tt_user[i].digit_suffix = digit_suffix(tt_user[i].callsign)
		tt_user[i].loc_text = loc_text

		if latitude != G_UNKNOWN && longitude != G_UNKNOWN {
			/* We have specific location. */
			tt_user[i].corral_slot = 0
			tt_user[i].latitude = latitude
			tt_user[i].longitude = longitude
		} else {
			/* Unknown location, put it in the corral. */
			tt_user[i].corral_slot = corral_slot()
		}

		tt_user[i].ambiguity = ambiguity

		tt_user[i].freq = freq
		tt_user[i].ctcss = ctcss
		tt_user[i].comment = comment
		tt_user[i].mic_e = mic_e
		tt_user[i].dao = dao
	} else {
		/*
		 * Known user.  Update with any new information.
		 * Keep any old values where not being updated.
		 */
		Assert(i >= 0 && i < MAX_TT_USERS)

		tt_user[i].count++

		/* Any reason to look at ssid here? */

		/* Update the symbol if not the default. */

		if overlay != C.APRSTT_DEFAULT_SYMTAB || symbol != C.APRSTT_DEFAULT_SYMBOL {
			tt_user[i].overlay = overlay
			tt_user[i].symbol = symbol
		}

		if loc_text != "" {
			tt_user[i].loc_text = loc_text
		}

		if latitude != G_UNKNOWN && longitude != G_UNKNOWN {
			/* We have specific location. */
			tt_user[i].corral_slot = 0
			tt_user[i].latitude = latitude
			tt_user[i].longitude = longitude
		}

		if ambiguity != G_UNKNOWN {
			tt_user[i].ambiguity = ambiguity
		}

		if freq != "" {
			tt_user[i].freq = freq
		}

		if ctcss != "" {
			tt_user[i].ctcss = ctcss
		}

		if comment != "" {
			tt_user[i].comment = comment
		}

		if mic_e != ' ' {
			tt_user[i].mic_e = mic_e
		}

		if dao != "" {
			tt_user[i].dao = dao
		}
	}

	/*
	 * In both cases, note last time heard and schedule object report transmission.
	 */
	tt_user[i].last_heard = time.Now()
	tt_user[i].xmits = 0
	tt_user[i].next_xmit = tt_user[i].last_heard.Add(time.Duration(save_tt_config_p.xmit_delay[0]) * time.Second)

	/*
	 * Send to applications and IGate immediately.
	 */

	xmit_object_report(i, true)

	/*
	 * Put properties into environment variables in preparation
	 * for calling a user-specified script.
	 */

	tt_setenv(i)

	return (0) /* Success! */
} /* end tt_user_heard */

/*------------------------------------------------------------------
 *
 * Name:        tt_user_background
 *
 * Purpose:
 *
 * Inputs:
 *
 * Outputs:	Append to transmit queue.
 *
 * Returns:     None
 *
 * Description:	...... TBD
 *
 *----------------------------------------------------------------*/

func tt_user_background() {
	var now = time.Now()

	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("tt_user_background()  now = %d\n", (int)now);

	for i := range MAX_TT_USERS {
		Assert(i >= 0 && i < MAX_TT_USERS)

		if tt_user[i].callsign != "" {
			if C.int(tt_user[i].xmits) < save_tt_config_p.num_xmits && !tt_user[i].next_xmit.After(now) {
				// text_color_set(DW_COLOR_DEBUG);
				// dw_printf ("tt_user_background()  now = %d\n", (int)now);
				// tt_user_dump ();

				xmit_object_report(i, false)

				/* Increase count of number times this one was sent. */
				tt_user[i].xmits++
				if C.int(tt_user[i].xmits) < save_tt_config_p.num_xmits {
					/* Schedule next one. */
					tt_user[i].next_xmit = tt_user[i].next_xmit.Add(time.Duration(save_tt_config_p.xmit_delay[tt_user[i].xmits]) * time.Second)
				}

				// tt_user_dump ();
			}
		}
	}

	/*
	 * Purge if too old.
	 */
	for i := range MAX_TT_USERS {
		if tt_user[i].callsign != "" {
			if tt_user[i].last_heard.Add(time.Duration(save_tt_config_p.retain_time) * time.Second).Before(now) {
				// dw_printf ("debug: purging expired user %d\n", i);

				clear_user(i)
			}
		}
	}
}

/*------------------------------------------------------------------
 *
 * Name:        xmit_object_report
 *
 * Purpose:     Create object report packet and put into transmit queue.
 *
 * Inputs:      i	   - Index into user table.
 *
 *		first_time - Is this being called immediately after the tone sequence
 *			 	was received or after some delay?
 *				For the former, we send to any attached applications
 *				and the IGate.
 *				For the latter, we transmit over radio.
 *
 * Outputs:	Append to transmit queue.
 *
 * Returns:     None
 *
 * Description:	Details for specified user are converted to
 *		"Object Report Format" and added to the transmit queue.
 *
 *		If the user did not report a position, we have to make
 *		up something so the corresponding object will show up on
 *		the map or other list of nearby stations.
 *
 *		The traditional approach is to put them in different
 *		positions in the "corral" by applying increments of an
 *		offset from the starting position.  This has two
 *		unfortunate properties.  It gives the illusion we know
 *		where the person is located.   Being in the ,,,
 *
 *----------------------------------------------------------------*/

func xmit_object_report(i int, first_time bool) {
	// text_color_set(DW_COLOR_DEBUG);
	// printf ("xmit_object_report (index = %d, first_time = %d) rx = %d, tx = %d\n", i, first_time,
	//			save_tt_config_p.obj_recv_chan, save_tt_config_p.obj_xmit_chan);

	Assert(i >= 0 && i < MAX_TT_USERS)

	/*
	 * Prepare the object name.
	 * Tack on "-12" if it is a callsign.
	 */
	var object_name = tt_user[i].callsign

	if len(object_name) <= 6 && tt_user[i].ssid != 0 {
		object_name += fmt.Sprintf("-%d", tt_user[i].ssid)
	}

	var olat, olong float64
	var oambig int
	if tt_user[i].corral_slot == 0 {
		/*
		 * Known location.
		 */
		olat = tt_user[i].latitude
		olong = tt_user[i].longitude
		oambig = tt_user[i].ambiguity
		if oambig == G_UNKNOWN {
			oambig = 0
		}
	} else {
		/*
		 * Use made up position in the corral.
		 */
		var c_lat = save_tt_config_p.corral_lat     // Corral latitude.
		var c_long = save_tt_config_p.corral_lon    // Corral longitude.
		var c_offs = save_tt_config_p.corral_offset // Corral (latitude) offset.

		olat = float64(c_lat - C.double(tt_user[i].corral_slot-1)*c_offs)
		olong = float64(c_long)
		oambig = 0
	}

	/*
	 * Build comment field from various information.
	 *
	 * 	usercomment [locationtext] /status !DAO!
	 *
	 * Any frequency is inserted at beginning later.
	 */
	var info_comment string

	if tt_user[i].comment != "" {
		info_comment = tt_user[i].comment
	}

	if tt_user[i].loc_text != "" {
		if info_comment != "" {
			info_comment += " "
		}
		info_comment += "["
		info_comment += tt_user[i].loc_text
		info_comment += "]"
	}

	if tt_user[i].mic_e >= '1' && tt_user[i].mic_e <= '9' {
		if len(info_comment) > 0 {
			info_comment += " "
		}

		// Insert "/" if status does not already begin with it.
		if save_tt_config_p.status[tt_user[i].mic_e-'0'][0] != '/' {
			info_comment += "/"
		}
		info_comment += C.GoString(&save_tt_config_p.status[tt_user[i].mic_e-'0'][0])
	}

	if tt_user[i].dao != "" {
		if len(info_comment) > 0 {
			info_comment += " "
		}
		info_comment += tt_user[i].dao
	}

	/* Official limit is 43 characters. */
	// info_comment[MAX_COMMENT_LEN] = 0;

	/*
	 * Packet header is built from mycall (of transmit channel) and software version.
	 */

	var stemp string
	if save_tt_config_p.obj_xmit_chan >= 0 {
		stemp = C.GoString(&save_audio_config_p.mycall[save_tt_config_p.obj_xmit_chan][0])
	} else {
		stemp = C.GoString(&save_audio_config_p.mycall[save_tt_config_p.obj_recv_chan][0])
	}
	stemp += ">"
	stemp += C.APP_TOCALL
	stemp += string('0' + C.MAJOR_VERSION)
	stemp += string('0' + C.MINOR_VERSION) // TODO KG This seems to assume some limits on version numbers...

	/*
	 * Append via path, for transmission, if specified.
	 */

	if !first_time && save_tt_config_p.obj_xmit_via[0] != 0 {
		stemp += ","
		stemp += C.GoString(&save_tt_config_p.obj_xmit_via[0])
	}

	stemp += ":"

	var freq float64 = G_UNKNOWN
	if tt_user[i].freq != "" {
		freq, _ = strconv.ParseFloat(tt_user[i].freq, 64)
	}

	var ctcss float64 = G_UNKNOWN
	if tt_user[i].ctcss != "" {
		ctcss, _ = strconv.ParseFloat(tt_user[i].ctcss, 64)
	}

	// info part of Object Report packet
	stemp += encode_object(C.CString(object_name), 0, C.long(tt_user[i].last_heard.Unix()), C.double(olat), C.double(olong), C.int(oambig),
		C.char(tt_user[i].overlay), C.char(tt_user[i].symbol),
		0, 0, 0, nil, G_UNKNOWN, G_UNKNOWN, /* PHGD, Course/Speed */
		C.float(freq),
		C.float(ctcss),
		G_UNKNOWN, /* CTCSS */
		C.CString(info_comment))

	if TT_TESTS_RUNNING {
		dw_printf("---> %s\n\n", stemp)
		return
	}

	if first_time {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("[APRStt] %s\n", stemp)
	}

	/*
	 * Convert text to packet.
	 */
	var pp = C.ax25_from_text(C.CString(stemp), 1)

	if pp == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\"%s\"\n", stemp)
		return
	}

	/*
	 * Send to one or more of the following depending on configuration:
	 *	Transmit queue.
	 *	Any attached application(s).
	 * 	IGate.
	 *
	 * When transmitting over the radio, it gets sent multiple times, to help
	 * probability of being heard, with increasing delays between.
	 *
	 * The other methods are reliable so we only want to send it once.
	 */

	if first_time && save_tt_config_p.obj_send_to_app > 0 {
		var fbuf [C.AX25_MAX_PACKET_LEN]C.uchar

		// TODO1.3:  Put a wrapper around this so we only call one function to send by all methods.
		// We see the same sequence in direwolf.c.

		var flen = C.ax25_pack(pp, &fbuf[0])

		server_send_rec_packet(save_tt_config_p.obj_recv_chan, pp, &fbuf[0], flen)
		kissnet_send_rec_packet(save_tt_config_p.obj_recv_chan, C.KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&fbuf[0]), flen), flen, nil, -1)
		kissserial_send_rec_packet(save_tt_config_p.obj_recv_chan, C.KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&fbuf[0]), flen), flen, nil, -1)
		kisspt_send_rec_packet(save_tt_config_p.obj_recv_chan, C.KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&fbuf[0]), flen), flen, nil, -1)
	}

	if first_time && save_tt_config_p.obj_send_to_ig > 0 {
		// text_color_set(DW_COLOR_DEBUG);
		// dw_printf ("xmit_object_report (): send to IGate\n");

		igate_send_rec_packet(save_tt_config_p.obj_recv_chan, pp)
	}

	if !first_time && save_tt_config_p.obj_xmit_chan >= 0 {
		/* Remember it so we don't digipeat our own. */

		dedupe_remember(pp, save_tt_config_p.obj_xmit_chan)

		tq_append(save_tt_config_p.obj_xmit_chan, TQ_PRIO_1_LO, pp)
	} else {
		C.ax25_delete(pp)
	}
}

var letters = []string{
	"Alpha",
	"Bravo",
	"Charlie",
	"Delta",
	"Echo",
	"Foxtrot",
	"Golf",
	"Hotel",
	"India",
	"Juliet",
	"Kilo",
	"Lima",
	"Mike",
	"November",
	"Oscar",
	"Papa",
	"Quebec",
	"Romeo",
	"Sierra",
	"Tango",
	"Uniform",
	"Victor",
	"Whiskey",
	"X-ray",
	"Yankee",
	"Zulu",
}

var digits = []string{
	"Zero",
	"One",
	"Two",
	"Three",
	"Four",
	"Five",
	"Six",
	"Seven",
	"Eight",
	"Nine",
}

/*------------------------------------------------------------------
 *
 * Name:        tt_setenv
 *
 * Purpose:     Put information in environment variables in preparation
 *		for calling a user-supplied script for custom processing.
 *
 * Inputs:      i	- Index into tt_user table.
 *
 * Description:	Timestamps displayed relative to current time.
 *
 *----------------------------------------------------------------*/

func tt_setenv(i int) {
	Assert(i >= 0 && i < MAX_TT_USERS)

	os.Setenv("TTCALL", tt_user[i].callsign)

	os.Setenv("TTCALLSP", strings.Join(strings.Split(tt_user[i].callsign, ""), " "))

	var phonetics []string
	for _, p := range tt_user[i].callsign {
		if unicode.IsUpper(p) {
			phonetics = append(phonetics, letters[p-'A'])
		} else if unicode.IsLower(p) {
			phonetics = append(phonetics, letters[p-'a'])
		} else if unicode.IsDigit(p) {
			phonetics = append(phonetics, digits[p-'0'])
		} else {
			phonetics = append(phonetics, string(p))
		}
	}
	os.Setenv("TTCALLPH", strings.Join(phonetics, " "))

	os.Setenv("TTSSID", strconv.Itoa(tt_user[i].ssid))

	os.Setenv("TTCOUNT", strconv.Itoa(tt_user[i].count))

	os.Setenv("TTSYMBOL", fmt.Sprintf("%c%c", tt_user[i].overlay, tt_user[i].symbol))

	os.Setenv("TTLAT", fmt.Sprintf("%.6f", tt_user[i].latitude))

	os.Setenv("TTLON", fmt.Sprintf("%.6f", tt_user[i].longitude))

	os.Setenv("TTFREQ", tt_user[i].freq)

	// TODO: Should convert to actual frequency. e.g.  074 becomes 74.4
	// There is some code for this in decode_aprs.c but not broken out
	// into a function that we could use from here.
	// TODO: Document this environment variable after converting.

	os.Setenv("TTCTCSS", tt_user[i].ctcss)

	os.Setenv("TTCOMMENT", tt_user[i].comment)

	os.Setenv("TTLOC", tt_user[i].loc_text)

	if tt_user[i].mic_e >= '1' && tt_user[i].mic_e <= '9' {
		os.Setenv("TTSTATUS", C.GoString(&save_tt_config_p.status[tt_user[i].mic_e-'0'][0]))
	} else {
		os.Setenv("TTSTATUS", "")
	}

	os.Setenv("TTDAO", tt_user[i].dao)
} /* end tt_setenv */

/*------------------------------------------------------------------
 *
 * Name:        tt_user_dump
 *
 * Purpose:     Print information about known users for debugging.
 *
 * Inputs:      None.
 *
 * Description:	Timestamps displayed relative to current time.
 *
 *----------------------------------------------------------------*/

func tt_user_dump() {
	var now = time.Now()

	dw_printf("call   ov suf lsthrd xmit nxt cor  lat    long freq     ctcss m comment\n")
	for i := range MAX_TT_USERS {
		if tt_user[i].callsign != "" {
			dw_printf("%-6s %c%c %-3s %6d %d %+6d %d %6.2f %7.2f %-10s %-3s %c %s\n",
				tt_user[i].callsign,
				tt_user[i].overlay,
				tt_user[i].symbol,
				tt_user[i].digit_suffix,
				int(tt_user[i].last_heard.Sub(now).Seconds()),
				tt_user[i].xmits,
				int(tt_user[i].next_xmit.Sub(now).Seconds()),
				tt_user[i].corral_slot,
				tt_user[i].latitude,
				tt_user[i].longitude,
				tt_user[i].freq,
				tt_user[i].ctcss,
				tt_user[i].mic_e,
				tt_user[i].comment)
		}
	}
}

/* end tt-user.c */
