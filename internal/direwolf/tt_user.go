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

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/touchtone"
)

/*
 * Information kept about local APRStt users.
 *
 * For now, just use a fixed size array for simplicity.
 */

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

	ambiguity maybe.Maybe[int] /* Number of digits to omit from location. */
	/* Max. 4; Nothing until a message says otherwise. */

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

// ttUsers is the APRStt gateway's table of recently heard users.
//
// The receive processing goroutine records users as their tone sequences
// arrive, and the audio goroutine of the TTOBJ receive channel polls the
// table to send the object reports it has scheduled, so mu guards user.
// The reports are sent with it released.
type ttUsers struct {
	audioConfig *RadioConfig
	ttConfig    *tt_config_s
	apps        *clientApps // Where object reports go to client applications, or nil.

	// remember is told each object report we transmit, so the digipeater
	// doesn't repeat our own, or is nil.
	remember func(pp *ax25.Packet, channel int)

	// toIGate sends an object report to APRS-IS, or is nil for no IGate.
	toIGate func(channel int, pp *ax25.Packet)

	mu   sync.Mutex
	user [MAX_TT_USERS]tt_user_s
}

// newTTUsers makes an empty user table.  audioConfig supplies the mycall
// object reports are sent from, and ttConfig when and where they are sent.
func newTTUsers(audioConfig *RadioConfig, ttConfig *tt_config_s) *ttUsers {
	var u = new(ttUsers)

	u.audioConfig = audioConfig
	u.ttConfig = ttConfig

	return u
}

/*------------------------------------------------------------------
 *
 * Name:        search
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

func (u *ttUsers) search(callsign string, overlay rune) int {
	/*
	 * First, look for exact match to full call and overlay.
	 */
	for i := range MAX_TT_USERS {
		if callsign == u.user[i].callsign &&
			overlay == u.user[i].overlay {
			return (i)
		}
	}

	/*
	 * Look for digits only suffix plus overlay.
	 */
	for i := range MAX_TT_USERS {
		if callsign == u.user[i].digit_suffix &&
			overlay != ' ' &&
			overlay == u.user[i].overlay {
			return (i)
		}
	}

	/*
	 * Look for digits only suffix if no overlay was specified.
	 */
	for i := range MAX_TT_USERS {
		if callsign == u.user[i].digit_suffix &&
			overlay == ' ' {
			return (i)
		}
	}

	/*
	 * Not sure about the new spelled suffix yet...
	 */
	return (-1)
} /* end search */

/*------------------------------------------------------------------
 *
 * Name:        threeCharSuffixSearch
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

func (u *ttUsers) threeCharSuffixSearch(suffix string) (string, int) {
	u.mu.Lock()
	defer u.mu.Unlock()

	/*
	 * Look for suffix in list of known calls.
	 */
	for i := range MAX_TT_USERS {
		var length = len(u.user[i].callsign)

		if length >= 3 && length <= 6 && u.user[i].callsign[length-3:] == suffix {
			return u.user[i].callsign, i
		}
	}

	/*
	 * Not found.
	 */
	return "", -1
} /* end threeCharSuffixSearch */

/*------------------------------------------------------------------
 *
 * Name:        clear
 *
 * Purpose:     Clear specified user table entry.
 *
 * Inputs:      handle for user table entry.
 *
 *----------------------------------------------------------------*/

func (u *ttUsers) clear(i int) {
	dwutil.Assert(i >= 0 && i < MAX_TT_USERS)

	u.user[i] = tt_user_s{} //nolint:exhaustruct_v5
} /* end clear */

/*------------------------------------------------------------------
 *
 * Name:        findAvail
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

func (u *ttUsers) findAvail() int {
	for i := range MAX_TT_USERS {
		if u.user[i].callsign == "" {
			u.clear(i)

			return (i)
		}
	}

	/* Remove least recently heard. */

	var i_oldest = 0

	for i := range MAX_TT_USERS {
		if u.user[i].last_heard.Before(u.user[i_oldest].last_heard) {
			i_oldest = i
		}
	}

	u.clear(i_oldest)

	return (i_oldest)
} /* end findAvail */

/*------------------------------------------------------------------
 *
 * Name:        corralSlot
 *
 * Purpose:     Find an available position in the corral.
 *
 * Inputs:      none
 *
 * Returns:     Small integer >= 1 not already in use.
 *
 *----------------------------------------------------------------*/

func (u *ttUsers) corralSlot() int {
	for slot := 1; ; slot++ {
		var used = false
		for i := 0; i < MAX_TT_USERS && !used; i++ {
			if u.user[i].callsign != "" && u.user[i].corral_slot == slot {
				used = true
			}
		}

		if !used {
			return (slot)
		}
	}
} /* end corralSlot */

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

	var two_key, _ = touchtone.TextToTwoKey(callsign, false)

	for _, t := range two_key {
		if t >= '0' && t <= '9' {
			suffix[0] = suffix[1]
			suffix[1] = suffix[2]
			suffix[2] = byte(t)
		}
	}

	return string(suffix)
}

/*------------------------------------------------------------------
 *
 * Name:        heard
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

func (u *ttUsers) heard(callsign string, ssid int, overlay rune, symbol rune, loc_text string, latitude maybe.Maybe[float64],
	longitude maybe.Maybe[float64], ambiguity maybe.Maybe[int], freq string, ctcss string, comment string, mic_e rune, dao string) int {
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

		return (TT_ERROR_NO_CALL)
	}

	var report = u.record(callsign, ssid, overlay, symbol, loc_text, latitude, longitude, ambiguity, freq, ctcss, comment, mic_e, dao)

	/*
	 * Send to applications and IGate immediately.
	 */

	u.sendObjectReport(report, true)

	return (0) /* Success! */
} /* end heard */

// record stores what heard was told about a user, schedules its object
// report transmissions, and returns the report to send straight away.
func (u *ttUsers) record(callsign string, ssid int, overlay rune, symbol rune, loc_text string, latitude maybe.Maybe[float64],
	longitude maybe.Maybe[float64], ambiguity maybe.Maybe[int], freq string, ctcss string, comment string, mic_e rune, dao string) string {
	u.mu.Lock()
	defer u.mu.Unlock()

	/*
	 * Is it someone new or a returning user?
	 */
	var i = u.search(callsign, overlay)
	if i == -1 {
		/*
		 * New person.  Create new table entry with all available information.
		 */
		i = u.findAvail()

		dwutil.Assert(i >= 0 && i < MAX_TT_USERS)
		u.user[i].callsign = callsign
		u.user[i].count = 1
		u.user[i].ssid = ssid
		u.user[i].overlay = overlay
		u.user[i].symbol = symbol
		u.user[i].digit_suffix = digit_suffix(u.user[i].callsign)
		u.user[i].loc_text = loc_text

		var lat, haveLat = latitude.Get()
		var lon, haveLon = longitude.Get()

		if haveLat && haveLon {
			/* We have specific location. */
			u.user[i].corral_slot = 0
			u.user[i].latitude = lat
			u.user[i].longitude = lon
		} else {
			/* Unknown location, put it in the corral. */
			u.user[i].corral_slot = u.corralSlot()
		}

		u.user[i].ambiguity = ambiguity

		u.user[i].freq = freq
		u.user[i].ctcss = ctcss
		u.user[i].comment = comment
		u.user[i].mic_e = mic_e
		u.user[i].dao = dao
	} else {
		/*
		 * Known user.  Update with any new information.
		 * Keep any old values where not being updated.
		 */
		dwutil.Assert(i >= 0 && i < MAX_TT_USERS)

		u.user[i].count++

		/* Any reason to look at ssid here? */

		/* Update the symbol if not the default. */

		if overlay != APRSTT_DEFAULT_SYMTAB || symbol != APRSTT_DEFAULT_SYMBOL {
			u.user[i].overlay = overlay
			u.user[i].symbol = symbol
		}

		if loc_text != "" {
			u.user[i].loc_text = loc_text
		}

		var lat, haveLat = latitude.Get()
		var lon, haveLon = longitude.Get()

		if haveLat && haveLon {
			/* We have specific location. */
			u.user[i].corral_slot = 0
			u.user[i].latitude = lat
			u.user[i].longitude = lon
		}

		u.user[i].ambiguity = ambiguity.Or(u.user[i].ambiguity)

		if freq != "" {
			u.user[i].freq = freq
		}

		if ctcss != "" {
			u.user[i].ctcss = ctcss
		}

		if comment != "" {
			u.user[i].comment = comment
		}

		if mic_e != ' ' {
			u.user[i].mic_e = mic_e
		}

		if dao != "" {
			u.user[i].dao = dao
		}
	}

	/*
	 * In both cases, note last time heard and schedule object report transmission.
	 */
	u.user[i].last_heard = time.Now()
	u.user[i].xmits = 0
	u.user[i].next_xmit = u.user[i].last_heard.Add(time.Duration(u.ttConfig.xmit_delay[0]) * time.Second)

	/*
	 * Put properties into environment variables in preparation
	 * for calling a user-specified script.
	 */

	u.setenv(i)

	return u.objectReportText(i, true)
} /* end record */

/*------------------------------------------------------------------
 *
 * Name:        background
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

func (u *ttUsers) background() {
	for _, report := range u.dueReports(time.Now()) {
		u.sendObjectReport(report, false)
	}
}

// dueReports returns the object reports whose transmission time has come,
// scheduling the next of each, and purges users not heard for too long.
func (u *ttUsers) dueReports(now time.Time) []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	var reports []string

	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("tt_user_background()  now = %d\n", (int)now);

	for i := range MAX_TT_USERS {
		dwutil.Assert(i >= 0 && i < MAX_TT_USERS)

		if u.user[i].callsign != "" {
			if u.user[i].xmits < u.ttConfig.num_xmits && !u.user[i].next_xmit.After(now) {
				// text_color_set(DW_COLOR_DEBUG);
				// dw_printf ("tt_user_background()  now = %d\n", (int)now);
				// tt_user_dump ();
				reports = append(reports, u.objectReportText(i, false))

				/* Increase count of number times this one was sent. */
				u.user[i].xmits++
				if u.user[i].xmits < u.ttConfig.num_xmits {
					/* Schedule next one. */
					u.user[i].next_xmit = u.user[i].next_xmit.Add(time.Duration(u.ttConfig.xmit_delay[u.user[i].xmits]) * time.Second)
				}

				// tt_user_dump ();
			}
		}
	}

	/*
	 * Purge if too old.
	 */
	for i := range MAX_TT_USERS {
		if u.user[i].callsign != "" {
			if u.user[i].last_heard.Add(time.Duration(u.ttConfig.retain_time) * time.Second).Before(now) {
				// dw_printf ("debug: purging expired user %d\n", i);
				u.clear(i)
			}
		}
	}

	return reports
}

/*------------------------------------------------------------------
 *
 * Name:        sendObjectReport
 *
 * Purpose:     Create object report packet and put into transmit queue.
 *
 * Inputs:      stemp	   - The report, from objectReportText.
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

func (u *ttUsers) sendObjectReport(stemp string, first_time bool) {
	if first_time {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("[APRStt] %s\n", stemp)
	}

	/*
	 * Convert text to packet.
	 */
	var pp = ax25.FromText(stemp, true)

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

	if first_time && u.ttConfig.obj_send_to_app > 0 {
		u.apps.SendRecPacket(u.ttConfig.obj_recv_chan, pp)
	}

	if first_time && u.ttConfig.obj_send_to_ig > 0 {
		// text_color_set(DW_COLOR_DEBUG);
		// dw_printf ("xmit_object_report (): send to IGate\n");
		if u.toIGate != nil {
			u.toIGate(u.ttConfig.obj_recv_chan, pp)
		}
	}

	if !first_time && u.ttConfig.obj_xmit_chan >= 0 {
		/* Remember it so we don't digipeat our own. */
		if u.remember != nil {
			u.remember(pp, u.ttConfig.obj_xmit_chan)
		}

		transmitQueue.Append(u.ttConfig.obj_xmit_chan, TQ_PRIO_1_LO, pp)
	}
}

/*------------------------------------------------------------------
 *
 * Name:        objectReportText
 *
 * Purpose:     Build the text form of the object report packet for
 *		sendObjectReport.  u.mu must be held.
 *
 * Inputs:      i	   - Index into user table.
 *
 *		first_time - As for sendObjectReport; the via path is
 *				only added for the later, radio, transmissions.
 *
 * Returns:     Monitor format packet, e.g. "MYCALL>SMYD00:;WB2OSZ-12*..."
 *
 *----------------------------------------------------------------*/

func (u *ttUsers) objectReportText(i int, first_time bool) string {
	dwutil.Assert(i >= 0 && i < MAX_TT_USERS)

	/*
	 * Prepare the object name.
	 * Tack on "-12" if it is a callsign.
	 */
	var object_name = u.user[i].callsign

	if len(object_name) <= 6 && u.user[i].ssid != 0 {
		object_name += fmt.Sprintf("-%d", u.user[i].ssid)
	}

	var olat, olong float64
	var oambig int

	if u.user[i].corral_slot == 0 {
		/*
		 * Known location.
		 */
		olat = u.user[i].latitude
		olong = u.user[i].longitude

		oambig = maybe.FromMaybe(0, u.user[i].ambiguity)
	} else {
		/*
		 * Use made up position in the corral.
		 */
		var c_lat = u.ttConfig.corral_lat     // Corral latitude.
		var c_long = u.ttConfig.corral_lon    // Corral longitude.
		var c_offs = u.ttConfig.corral_offset // Corral (latitude) offset.

		olat = float64(c_lat - float64(u.user[i].corral_slot-1)*c_offs)
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

	if u.user[i].comment != "" {
		info_comment = u.user[i].comment
	}

	if u.user[i].loc_text != "" {
		if info_comment != "" {
			info_comment += " "
		}

		info_comment += "["
		info_comment += u.user[i].loc_text
		info_comment += "]"
	}

	if u.user[i].mic_e >= '1' && u.user[i].mic_e <= '9' {
		if len(info_comment) > 0 {
			info_comment += " "
		}

		// Insert "/" if status does not already begin with it.
		if !strings.HasPrefix(u.ttConfig.status[u.user[i].mic_e-'0'], "/") {
			info_comment += "/"
		}

		info_comment += u.ttConfig.status[u.user[i].mic_e-'0']
	}

	if u.user[i].dao != "" {
		if len(info_comment) > 0 {
			info_comment += " "
		}

		info_comment += u.user[i].dao
	}

	/* Official limit is 43 characters. */
	// info_comment[MAX_COMMENT_LEN] = 0;

	/*
	 * Packet header is built from mycall (of transmit channel) and software version.
	 */

	var stemp string
	if u.ttConfig.obj_xmit_chan >= 0 {
		stemp = u.audioConfig.mycall[u.ttConfig.obj_xmit_chan]
	} else {
		stemp = u.audioConfig.mycall[u.ttConfig.obj_recv_chan]
	}

	stemp += ">"
	stemp += APP_TOCALL
	stemp += string(rune('0' + MAJOR_VERSION))
	stemp += string(rune('0' + MINOR_VERSION)) // TODO KG This seems to assume some limits on version numbers...

	/*
	 * Append via path, for transmission, if specified.
	 */

	if !first_time && u.ttConfig.obj_xmit_via != "" {
		stemp += ","
		stemp += u.ttConfig.obj_xmit_via
	}

	stemp += ":"

	var freq maybe.Maybe[float64]
	if u.user[i].freq != "" {
		freq = maybe.Just(leadingFloat(u.user[i].freq))
	}

	var ctcss maybe.Maybe[float64]
	if u.user[i].ctcss != "" {
		ctcss = maybe.Just(leadingFloat(u.user[i].ctcss))
	}

	// info part of Object Report packet
	stemp += aprs.EncodeObject(object_name, false, u.user[i].last_heard, olat, olong, oambig,
		byte(u.user[i].overlay), byte(u.user[i].symbol), //nolint:gosec // G115: unchecked narrowing conversion, see #294
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "", /* PHGD */
		maybe.Nothing[int](), maybe.Nothing[int](), /* Course/Speed */
		freq,
		ctcss,
		maybe.Nothing[float64](), /* offset */
		info_comment)

	return stemp
}

// leadingFloat reads the number at the start of s, ignoring leading spaces and
// anything after it, as C's atof does - so "146.520MHz" is 146.52.  With no
// number there it is 0.
func leadingFloat(s string) float64 {
	s = strings.TrimLeft(s, " \t")

	for end := len(s); end > 0; end-- {
		var f, err = strconv.ParseFloat(s[:end], 64)
		if err == nil {
			return f
		}
	}

	return 0
}

// phoneticLetters names A to Z for TTCALLPH.
func phoneticLetters() [26]string {
	return [26]string{
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
}

// phoneticDigits names 0 to 9 for TTCALLPH.
func phoneticDigits() [10]string {
	return [10]string{
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
}

/*------------------------------------------------------------------
 *
 * Name:        setenv
 *
 * Purpose:     Put information in environment variables in preparation
 *		for calling a user-supplied script for custom processing.
 *
 * Inputs:      i	- Index into user table.
 *
 * Description:	Timestamps displayed relative to current time.
 *
 *----------------------------------------------------------------*/

func (u *ttUsers) setenv(i int) {
	dwutil.Assert(i >= 0 && i < MAX_TT_USERS)

	os.Setenv("TTCALL", u.user[i].callsign)

	os.Setenv("TTCALLSP", strings.Join(strings.Split(u.user[i].callsign, ""), " "))

	var letters, digits = phoneticLetters(), phoneticDigits()

	var phonetics []string

	for _, p := range u.user[i].callsign {
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

	os.Setenv("TTSSID", strconv.Itoa(u.user[i].ssid))

	os.Setenv("TTCOUNT", strconv.Itoa(u.user[i].count))

	os.Setenv("TTSYMBOL", fmt.Sprintf("%c%c", u.user[i].overlay, u.user[i].symbol))

	os.Setenv("TTLAT", fmt.Sprintf("%.6f", u.user[i].latitude))

	os.Setenv("TTLON", fmt.Sprintf("%.6f", u.user[i].longitude))

	os.Setenv("TTFREQ", u.user[i].freq)

	// TODO: Should convert to actual frequency. e.g.  074 becomes 74.4
	// There is some code for this in decode_aprs.c but not broken out
	// into a function that we could use from here.
	// TODO: Document this environment variable after converting.

	os.Setenv("TTCTCSS", u.user[i].ctcss)

	os.Setenv("TTCOMMENT", u.user[i].comment)

	os.Setenv("TTLOC", u.user[i].loc_text)

	if u.user[i].mic_e >= '1' && u.user[i].mic_e <= '9' {
		os.Setenv("TTSTATUS", u.ttConfig.status[u.user[i].mic_e-'0'])
	} else {
		os.Setenv("TTSTATUS", "")
	}

	os.Setenv("TTDAO", u.user[i].dao)
} /* end setenv */

/*------------------------------------------------------------------
 *
 * Name:        dump
 *
 * Purpose:     Print information about known users for debugging.
 *
 * Inputs:      None.
 *
 * Description:	Timestamps displayed relative to current time.
 *
 *----------------------------------------------------------------*/

func (u *ttUsers) dump() {
	u.mu.Lock()
	defer u.mu.Unlock()

	var now = time.Now()

	dw_printf("call   ov suf lsthrd xmit nxt cor  lat    long freq     ctcss m comment\n")

	for i := range MAX_TT_USERS {
		if u.user[i].callsign != "" {
			dw_printf("%-6s %c%c %-3s %6d %d %+6d %d %6.2f %7.2f %-10s %-3s %c %s\n",
				u.user[i].callsign,
				u.user[i].overlay,
				u.user[i].symbol,
				u.user[i].digit_suffix,
				int(u.user[i].last_heard.Sub(now).Seconds()),
				u.user[i].xmits,
				int(u.user[i].next_xmit.Sub(now).Seconds()),
				u.user[i].corral_slot,
				u.user[i].latitude,
				u.user[i].longitude,
				u.user[i].freq,
				u.user[i].ctcss,
				u.user[i].mic_e,
				u.user[i].comment)
		}
	}
}

/* end tt-user.c */
