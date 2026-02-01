package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Maintain a list of all stations heard.
 *
 * Description: This was added for IGate statistics and checking if a user is local
 *		but would also be useful for the AGW network protocol 'H' request.
 *
 *		This application has no GUI and is not interactive so
 *		I'm not sure what else we might do with the information.
 *
 *		Why mheard instead of just heard?  The KPC-3+ has an MHEARD command
 *		to list stations heard.  I guess that stuck in my mind.
 *		It should be noted that here "heard" refers to the AX.25 source station.
 *		Before printing the received packet, the "heard" line refers to who
 *		we heard over the radio.  This would be the digipeater with "*" after
 *		its name.
 *
 * Future Ideas: Someone suggested using SQLite to store the information
 *		so other applications could access it.
 *
 *------------------------------------------------------------------*/

// #include <stdio.h>
// #include <time.h>
// #include <assert.h>
// #include <stdlib.h>
// #include <string.h>
// #include <ctype.h>
import "C"

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// This is getting updated from two different threads so we need a critical region
// for adding new nodes.

var mheard_mutex sync.Mutex

// I think we can get away without a critical region for reading if we follow these
// rules:
//
// (1) When adding a new node, make sure it is complete, including next ptr,
//	before adding it to the list.
// (2) Update the start of list pointer last.
// (2) Nothing gets deleted.

// If we ever decide to start cleaning out very old data, all access would then
// need to use the mutex.

/*
 * Information for each station heard over the radio or from Internet Server.
 */

type mheard_t struct {
	pnext *mheard_t // Pointer to next in list.

	callsign string // Callsign from the AX.25 source field.

	count C.int // Number of times heard.
	// We don't use this for anything.
	// Just something potentially interesting when looking at data dump.

	channel C.int // Most recent channel where heard.

	num_digi_hops C.int // Number of digipeater hops before we heard it.
	// over radio.  Zero when heard directly.

	last_heard_rf time.Time // Timestamp when last heard over the radio.

	last_heard_is time.Time // Timestamp when last heard from Internet Server.

	dlat, dlon C.double // Last position.  G_UNKNOWN for unknown.

	msp C.int // Allow message sender position report.
	// When non zero, an IS>RF position report is allowed.
	// Then decremented.

	// What else would be useful?
	// The AGW protocol is by channel and returns
	// first heard in addition to last heard.
}

var mheard_db map[string]*mheard_t

var mheard_debug = 0

/*------------------------------------------------------------------
 *
 * Function:	mheard_init
 *
 * Purpose:	Initialization at start of application.
 *
 * Inputs:	debug		- Debug level.
 *
 * Description:	Clear pointer table.
 *		Save debug level for later use.
 *
 *------------------------------------------------------------------*/

func mheard_init(debug int) {

	mheard_db = make(map[string]*mheard_t)

	mheard_debug = debug
} /* end mheard_init */

/*------------------------------------------------------------------
 *
 * Function:	mheard_dump
 *
 * Purpose:	Print list of stations heard for debugging.
 *
 *------------------------------------------------------------------*/

/* convert some time in past to hours:minutes text format. */

func mheard_age(now, t time.Time) string {

	if t.IsZero() {
		return "-  "
	}

	var d = now.Sub(t)

	return fmt.Sprintf("%4d:%02d", int(d.Hours()), int(d.Minutes())%60)
}

/* Convert latitude, longitude to text or - if not defined. */

func mheard_latlon(dlat C.double, dlon C.double) string {
	if dlat != G_UNKNOWN && dlon != G_UNKNOWN {
		return fmt.Sprintf("%6.2f %7.2f", dlat, dlon)
	} else {
		return "   -       -  "
	}
}

const MAXDUMP = 1000

func mheard_dump() {

	/* Get linear array of node pointers so they can be sorted easily. */

	var stations = slices.Collect(maps.Values(mheard_db))

	/* Sort most recently heard to the top then print. */
	slices.SortFunc(stations, func(ma, mb *mheard_t) int {
		var ta = ma.last_heard_rf
		if ma.last_heard_is.After(ta) {
			ta = ma.last_heard_is
		}

		var tb = mb.last_heard_rf
		if mb.last_heard_is.After(tb) {
			tb = mb.last_heard_is
		}

		if ta.Before(tb) {
			return -1
		} else if ta.After(tb) {
			return 1
		} else {
			return 0
		}
	})

	text_color_set(DW_COLOR_DEBUG)

	dw_printf("callsign  cnt chan hops    RF      IS    lat     long  msp\n")

	for _, mptr := range stations {
		var now = time.Now()
		var rf = mheard_age(now, mptr.last_heard_rf)
		var is = mheard_age(now, mptr.last_heard_is)
		var position = mheard_latlon(mptr.dlat, mptr.dlon)

		dw_printf("%-9s %3d   %d   %d  %7s %7s  %s  %d\n",
			mptr.callsign, mptr.count, mptr.channel, mptr.num_digi_hops, rf, is, position, mptr.msp)
	}

} /* end mheard_dump */

/*------------------------------------------------------------------
 *
 * Function:	mheard_save_rf
 *
 * Purpose:	Save information about station heard over the radio.
 *
 * Inputs:	channel	- Radio channel where heard.
 *
 *		A	- Exploded information from APRS packet.
 *
 *		pp	- Received packet object.
 *
 * 		alevel	- audio level.
 *
 *		retries	- Amount of effort to get a good CRC.
 *
 * Description:	Calling sequence was copied from "log_write."
 *		It has a lot more than what we currently keep but the
 *		hooks are there so it will be easy to capture additional
 *		information when the need arises.
 *
 *------------------------------------------------------------------*/

func mheard_save_rf(channel C.int, A *decode_aprs_t, pp *packet_t, alevel alevel_t, retries retry_t) {

	var now = time.Now()

	var source = ax25_get_addr_with_ssid(pp, AX25_SOURCE)

	/*
	 * How many digipeaters has it gone thru before we hear it?
	 * We can count the number of digi addresses that are marked as "has been used."
	 * This is not always accurate because there is inconsistency in digipeater behavior.
	 * The base AX.25 spec seems clear in this regard.  The used digipeaters should
	 * should accurately reflict the path taken by the packet.  Sometimes we see excess
	 * stuff in there.  Even when you understand what is going on, it is still an ambiguous
	 * situation.  Look for my rant in the User Guide.
	 */

	var hops = ax25_get_heard(pp) - AX25_SOURCE
	/*
	 *		Consider the following scenario:
	 *
	 *		(1) We hear AA1PR-9 by a path of 4 digipeaters.
	 *		    Looking closer, it's probably only two because there are left over WIDE1-0 and WIDE2-0.
	 *
	 *			Digipeater WIDE2 (probably N3LLO-3) audio level = 72(19/15)   [NONE]   _|||||___
	 *			[0.3] AA1PR-9>APY300,K1EQX-7,WIDE1,N3LLO-3,WIDE2*,ARISS::ANSRVR   :cq hotg vt aprsthursday{01<0x0d>
	 *			                             -----         -----
	 *
	 *		(2) APRS-IS sends a response to us.
	 *
	 *			[ig>tx] ANSRVR>APWW11,KJ4ERJ-15*,TCPIP*,qAS,KJ4ERJ-15::AA1PR-9  :N:HOTG 161 Messages Sent{JL}
	 *
	 *		(3) Here is our analysis of whether it should be sent to RF.
	 *
	 *			Was message addressee AA1PR-9 heard in the past 180 minutes, with 2 or fewer digipeater hops?
	 *			No, AA1PR-9 was last heard over the radio with 4 digipeater hops 0 minutes ago.
	 *
	 *		The wrong hop count caused us to drop a packet that should have been transmitted.
	 *		We could put in a hack to not count the "WIDEn-0"  addresses.
	 *		That is not correct because other prefixes could be used and we don't know
	 *		what they are for other digipeaters.
	 *		I think the best solution is to simply ignore the hop count.
	 *		Maybe next release will have a major cleanup.
	 */

	// HACK - Reduce hop count by number of used WIDEn-0 addresses.

	if hops > 1 {
		for k := 0; k < ax25_get_num_repeaters(pp); k++ {
			var digi = ax25_get_addr_no_ssid(pp, AX25_REPEATER_1+k)
			var ssid = ax25_get_ssid(pp, AX25_REPEATER_1+k)
			var used = ax25_get_h(pp, AX25_REPEATER_1+k)

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Examining %s-%d  used=%d.\n", digi, ssid, used);

			if used > 0 && len(digi) == 5 && strings.EqualFold(digi[:4], "WIDE") && unicode.IsDigit(rune(digi[4])) && ssid == 0 {
				hops--
				//text_color_set(DW_COLOR_DEBUG);
				//dw_printf ("Decrease hop count to %d for problematic %s.\n", hops, digi);
			}
		}
	}

	var mptr = mheard_db[source]
	if mptr == nil {

		/*
		 * Not heard before.  Add it.
		 */

		if mheard_debug > 0 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("mheard_save_rf: %s %d - added new\n", source, hops)
		}

		mptr = new(mheard_t)
		mptr.callsign = source
		mptr.count = 1
		mptr.channel = channel
		mptr.num_digi_hops = C.int(hops)
		mptr.last_heard_rf = now
		// Why did I do this instead of saving the location for a position report?
		mptr.dlat = G_UNKNOWN
		mptr.dlon = G_UNKNOWN

		mheard_mutex.Lock()
		mheard_db[source] = mptr
		mheard_mutex.Unlock()
	} else {

		/*
		 * Update existing entry.
		 * The only tricky part here is that we might hear the same transmission
		 * several times.  First direct, then thru various digipeater paths.
		 * We are interested in the shortest path if heard very recently.
		 */

		if C.int(hops) > mptr.num_digi_hops && now.Sub(mptr.last_heard_rf).Seconds() < 15 {

			if mheard_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("mheard_save_rf: %s %d - skip because hops was %d %d seconds ago.\n", source, hops, mptr.num_digi_hops, int(now.Sub(mptr.last_heard_rf).Seconds()))
			}
		} else {

			if mheard_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("mheard_save_rf: %s %d - update time, was %d hops %d seconds ago.\n", source, hops, mptr.num_digi_hops, int(now.Sub(mptr.last_heard_rf).Seconds()))
			}

			mptr.count++
			mptr.channel = channel
			mptr.num_digi_hops = C.int(hops)
			mptr.last_heard_rf = now
		}
	}

	// Issue 545.  This was not thought out well.
	// There was a case where a station sent a position report and the location was stored.
	// Later, the same station sent an object report and the stations's location was overwritten
	// by the object location.  Solution: Save location only if position report.

	if A.g_packet_type == packet_type_position {
		if A.g_lat != G_UNKNOWN && A.g_lon != G_UNKNOWN {
			mptr.dlat = C.double(A.g_lat)
			mptr.dlon = C.double(A.g_lon)
		}
	}

	if mheard_debug >= 2 {
		var limit C.int = 10 // normally 30 or 60.  more frequent when debugging.
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("mheard debug, %d min, DIR_CNT=%d,LOC_CNT=%d,RF_CNT=%d\n", limit, mheard_count(0, limit), mheard_count(2, limit), mheard_count(8, limit))
	}

	if mheard_debug > 0 {
		mheard_dump()
	}

} /* end mheard_save_rf */

/*------------------------------------------------------------------
 *
 * Function:	mheard_save_is
 *
 * Purpose:	Save information about station heard via Internet Server.
 *
 * Inputs:	ptext	- Packet in monitoring text form as sent by the Internet server.
 *
 *				  Any trailing CRLF should have been removed.
 *				  Typical examples:
 *
 *				KA1BTK-5>APDR13,TCPIP*,qAC,T2IRELAND:=4237.62N/07040.68W$/A=-00054 http://aprsdroid.org/
 *				N1HKO-10>APJI40,TCPIP*,qAC,N1HKO-JS:<IGATE,MSG_CNT=0,LOC_CNT=0
 *				K1RI-2>APWW10,WIDE1-1,WIDE2-1,qAS,K1RI:/221700h/9AmA<Ct3_ sT010/002g005t045r000p023P020h97b10148
 *				KC1BOS-2>T3PQ3S,WIDE1-1,WIDE2-1,qAR,W1TG-1:`c)@qh\>/"50}TinyTrak4 Mobile
 *				WHO-IS>APJIW4,TCPIP*,qAC,AE5PL-JF::WB2OSZ   :C/Billerica Amateur Radio Society/MA/United States{XF}WO
 *
 *				  Notice how the final address in the header might not
 *				  be a valid AX.25 address.  We see a 9 character address
 *				  (with no ssid) and an ssid of two letters.
 *
 *				  The "q construct"  ( http://www.aprs-is.net/q.aspx ) provides
 *				  a clue about the journey taken but I don't think we care here.
 *
 *				  All we should care about here is the the source address.
 *				  Note that the source address might not adhere to the AX.25 format.
 *
 * Description:
 *
 *------------------------------------------------------------------*/

func mheard_save_is(_ptext *C.char) {

	var now = time.Now()

	// It is possible that source won't adhere to the AX.25 restrictions.
	// So we simply extract the source address, as text, from the beginning rather than
	// using ax25_from_text() and ax25_get_addr_with_ssid().

	var ptext = C.GoString(_ptext)
	var source, _, _ = strings.Cut(ptext, ">")

	/*
	    * Keep this here in case I want to revive it to get location.
	   	packet_t pp = ax25_from_text(ptext, 0);

	   	if (pp == nil) {
	   	  if (mheard_debug) {
	   	    text_color_set(DW_COLOR_ERROR);
	   	    dw_printf ("mheard_save_is: Could not parse message from server.\n");
	   	    dw_printf ("%s\n", ptext);
	   	  }
	   	  return;
	   	}

	   	//////ax25_get_addr_with_ssid (pp, AX25_SOURCE, source);
	*/

	var mptr = mheard_db[source]
	if mptr == nil {
		/*
		 * Not heard before.  Add it.
		 * Observation years later:
		 * Hmmmm.  I wonder why I did not store the location if available.
		 * An earlier example has an APRSdroid station reporting location without using [ham] RF.
		 */

		if mheard_debug > 0 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("mheard_save_is: %s - added new\n", source)
		}

		mptr = new(mheard_t)
		mptr.callsign = source
		mptr.count = 1
		mptr.last_heard_is = now
		mptr.dlat = G_UNKNOWN
		mptr.dlon = G_UNKNOWN

		mheard_mutex.Lock()
		mheard_db[source] = mptr
		mheard_mutex.Unlock()
	} else {

		/* Already there.  Update last heard from IS time. */

		if mheard_debug > 0 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("mheard_save_is: %s - update time, was %d seconds ago.\n", source, int(now.Sub(mptr.last_heard_rf).Seconds()))
		}
		mptr.count++
		mptr.last_heard_is = now
	}

	// Is is desirable to save any location in this case?
	// I don't think it would help.
	// The whole purpose of keeping the location is for message sending filter.
	// We wouldn't want to try sending a message to the station if we didn't hear it over the radio.
	// On the other hand, I don't think it would hurt.
	// The filter always includes a time since last heard over the radio.

	if mheard_debug >= 2 {
		var limit C.int = 10 // normally 30 or 60
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("mheard debug, %d min, DIR_CNT=%d,LOC_CNT=%d,RF_CNT=%d\n", limit, mheard_count(0, limit), mheard_count(2, limit), mheard_count(8, limit))
	}

	if mheard_debug > 0 {
		mheard_dump()
	}

	/*
		#if 0
			ax25_delete (pp);
		#endif
	*/

} /* end mheard_save_is */

/*------------------------------------------------------------------
 *
 * Function:	mheard_count
 *
 * Purpose:	Count local stations for IGate statistics report like this:
 *
 *			<IGATE,MSG_CNT=1,LOC_CNT=25
 *
 * Inputs:	max_hops	- Include only stations heard with this number of
 *				  digipeater hops or less.  For reporting, we might use:
 *
 *					0 for DIR_CNT (heard directly)
 *					IGate transmit path for LOC_CNT.
 *						e.g. 3 for WIDE1-1,WIDE2-2
 *					8 for RF_CNT.
 *
 *		time_limit	- Include only stations heard within this many minutes.
 *				  Typically 180.
 *
 * Returns:	Number to be used in the statistics report.
 *
 * Description:	Look for discussion here:  http://www.tapr.org/pipermail/aprssig/2016-June/045837.html
 *
 *		Lynn KJ4ERJ:
 *
 *			For APRSISCE/32, "Local" is defined as those stations to which messages
 *			would be gated if any are received from the APRS-IS.  This currently
 *			means unique stations heard within the past 30 minutes with at most two
 *			used path hops.
 *
 *			I added DIR_CNT and RF_CNT with comma delimiters to APRSISCE/32's IGate
 *			status.  DIR_CNT is the count of unique stations received on RF in the
 *			past 30 minutes with no used hops.  RF_CNT is the total count of unique
 *			stations received on RF in the past 30 minutes.
 *
 *		Steve K4HG:
 *
 *			The number of hops defining local should match the number of hops of the
 *			outgoing packets from the IGate. So if the path is only WIDE, then local
 *			should only be stations heard direct or through one hop. From the beginning
 *			I was very much against on a standardization of the outgoing IGate path,
 *			hams should be free to manage their local RF network in a way that works
 *			for them. Busy areas one hop may be best, I lived in an area where three was
 *			a much better choice. I avoided as much as possible prescribing anything
 *			that might change between locations.
 *
 *			The intent was how many stations are there for which messages could be IGated.
 *			IGate software keeps an internal list of the 'local' stations so it knows
 *			when to IGate a message, and this number should be the length of that list.
 *			Some IGates have a parameter for local timeout, 1 hour was the original default,
 *			so if in an hour the IGate has not heard another local packet the station is
 *			dropped from the local list. Messages will no longer be IGated to that station
 *			and the station count would drop by one. The number should not just continue to rise.
 *
 *
 *------------------------------------------------------------------*/

func mheard_count(max_hops C.int, time_limit C.int) C.int {

	var limit = time.Duration(time_limit) * time.Minute
	var since = time.Now().Add(-limit)

	var count C.int = 0

	for _, p := range mheard_db {
		if !p.last_heard_rf.Before(since) && p.num_digi_hops <= max_hops {
			count++
		}
	}

	if mheard_debug == 1 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("mheard_count(<= %d digi hops, last %d minutes) returns %d\n", max_hops, int(limit.Minutes()), count)
	}

	return (count)

} /* end mheard_count */

/*------------------------------------------------------------------
 *
 * Function:	mheard_was_recently_nearby
 *
 * Purpose:	Determine whether given station was heard recently on the radio.
 *
 * Inputs:	role		- "addressee" or "source" if debug out is desired.
 *				  Otherwise empty string.
 *
 *		callsign	- Callsign for station.
 *
 *		time_limit	- Include only stations heard within this many minutes.
 *				  Typically 180.
 *
 *		max_hops	- Include only stations heard with this number of
 *				  digipeater hops or less.  For reporting, we might use:
 *
 *		dlat, dlon, km	- Include only stations within distance of location.
 *				  Not used if G_UNKNOWN is supplied.
 *
 * Returns:	1 for true, 0 for false.
 *
 *------------------------------------------------------------------*/

func mheard_was_recently_nearby(role *C.char, _callsign *C.char, _time_limit C.int, max_hops C.int, dlat C.double, dlon C.double, km C.double) bool {

	var time_limit = time.Duration(_time_limit) * time.Minute
	var callsign = C.GoString(_callsign)

	if role != nil && C.strlen(role) > 0 {

		text_color_set(DW_COLOR_INFO)
		if dlat != G_UNKNOWN && dlon != G_UNKNOWN && km != G_UNKNOWN {
			dw_printf(
				"Was message %s %s heard in the past %d minutes, with %d or fewer digipeater hops, and within %.1f km of %.2f %.2f?\n",
				C.GoString(role),
				callsign,
				int(time_limit.Minutes()),
				max_hops,
				km,
				dlat,
				dlon,
			)
		} else {
			dw_printf("Was message %s %s heard in the past %d minutes, with %d or fewer digipeater hops?\n", C.GoString(role), callsign, int(time_limit.Minutes()), max_hops)
		}
	}

	var mptr = mheard_db[callsign]

	if mptr == nil || mptr.last_heard_rf.IsZero() {

		if role != nil && C.strlen(role) > 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("No, we have not heard %s over the radio.\n", callsign)
		}
		return false
	}

	var now = time.Now()
	var heard_ago = now.Sub(mptr.last_heard_rf)

	if heard_ago > time_limit {

		if role != nil && C.strlen(role) > 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("No, %s was last heard over the radio %d minutes ago with %d digipeater hops.\n", callsign, int(heard_ago.Minutes()), mptr.num_digi_hops)
		}
		return false
	}

	if mptr.num_digi_hops > max_hops {

		if role != nil && C.strlen(role) > 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("No, %s was last heard over the radio with %d digipeater hops %d minutes ago.\n", callsign, mptr.num_digi_hops, int(heard_ago.Minutes()))
		}
		return false
	}

	// Apply physical distance check?

	if dlat != G_UNKNOWN && dlon != G_UNKNOWN && km != G_UNKNOWN && mptr.dlat != G_UNKNOWN && mptr.dlon != G_UNKNOWN {

		var dist = C.double(ll_distance_km(float64(mptr.dlat), float64(mptr.dlon), float64(dlat), float64(dlon)))

		if dist > km {

			if role != nil && C.strlen(role) > 0 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("No, %s was %.1f km away although it was %d digipeater hops %d minutes ago.\n", callsign, dist, mptr.num_digi_hops, int(heard_ago.Minutes()))
			}
			return false
		} else {
			if role != nil && C.strlen(role) > 0 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Yes, %s last heard over radio %d minutes ago, %d digipeater hops.  Last location %.1f km away.\n", callsign, int(heard_ago.Minutes()), mptr.num_digi_hops, dist)
			}
			return true
		}
	}

	// Passed all the tests.

	if role != nil && C.strlen(role) > 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Yes, %s last heard over radio %d minutes ago, %d digipeater hops.\n", callsign, int(heard_ago.Minutes()), mptr.num_digi_hops)
	}

	return true

} /* end mheard_was_recently_nearby */

/*------------------------------------------------------------------
 *
 * Function:	mheard_set_msp
 *
 * Purpose:	Set the "message sender position" count for specified station.
 *
 * Inputs:	callsign	- Callsign for station which sent the "message."
 *
 *		num		- Number of position reports to allow.  Typically 1.
 *
 *------------------------------------------------------------------*/

func mheard_set_msp(callsign string, num C.int) {

	var mptr = mheard_db[callsign]

	if mptr != nil {

		mptr.msp = num

		if mheard_debug > 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("MSP for %s set to %d\n", callsign, num)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: Can't find %s to set MSP.\n", callsign)
	}

} /* end mheard_set_msp */

/*------------------------------------------------------------------
 *
 * Function:	mheard_get_msp
 *
 * Purpose:	Get the "message sender position" count for specified station.
 *
 * Inputs:	callsign	- Callsign for station which sent the "message."
 *
 * Returns:	The count for the specified station.
 *		0 if not found.
 *
 *------------------------------------------------------------------*/

func mheard_get_msp(callsign string) C.int {

	var mptr = mheard_db[callsign]

	if mptr != nil {

		if mheard_debug > 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("MSP for %s is %d\n", callsign, mptr.msp)
		}
		return (mptr.msp) // Should we have a time limit?
	}

	return (0)

} /* end mheard_get_msp */

/* end mheard.c */
