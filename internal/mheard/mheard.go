// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package mheard maintains a list of all stations heard, over the radio or
// from an Internet Server, for IGate statistics and for checking whether a
// station is local.
//
// From Dire Wolf's mheard.c:
//
//	This was added for IGate statistics and checking if a user is local
//	but would also be useful for the AGW network protocol 'H' request.
//
//	This application has no GUI and is not interactive so
//	I'm not sure what else we might do with the information.
//
//	Why mheard instead of just heard?  The KPC-3+ has an MHEARD command
//	to list stations heard.  I guess that stuck in my mind.
//	It should be noted that here "heard" refers to the AX.25 source station.
//	Before printing the received packet, the "heard" line refers to who
//	we heard over the radio.  This would be the digipeater with "*" after
//	its name.
//
//	Future Ideas: Someone suggested using SQLite to store the information
//	so other applications could access it.
package mheard

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

/*
 * Information for each station heard over the radio or from Internet Server.
 */

type station struct {
	callsign string // Callsign from the AX.25 source field.

	count int // Number of times heard.
	// We don't use this for anything.
	// Just something potentially interesting when looking at data dump.

	channel int // Most recent channel where heard.

	numDigiHops int // Number of digipeater hops before we heard it.
	// over radio.  Zero when heard directly.

	lastHeardRF time.Time // Timestamp when last heard over the radio.

	lastHeardIS time.Time // Timestamp when last heard from Internet Server.

	dlat, dlon maybe.Maybe[float64] // Last position.

	msp int // Allow message sender position report.
	// When non zero, an IS>RF position report is allowed.
	// Then decremented.

	// What else would be useful?
	// The AGW protocol is by channel and returns
	// first heard in addition to last heard.
}

// DB maintains a list of all stations heard over the radio or from an
// Internet Server.  It is getting updated from two different threads so we
// need a critical region for adding new nodes.
type DB struct {
	mu    sync.RWMutex
	db    map[string]*station
	debug int
}

/*------------------------------------------------------------------
 *
 * Function:	New
 *
 * Purpose:	Initialization at start of application.
 *
 * Inputs:	debug		- Debug level.
 *
 *------------------------------------------------------------------*/

func New(debug int) *DB {
	var mdb = new(DB)

	mdb.db = make(map[string]*station)
	mdb.debug = debug

	return mdb
} /* end New */

/*------------------------------------------------------------------
 *
 * Function:	dump
 *
 * Purpose:	Log list of stations heard for debugging.
 *
 *------------------------------------------------------------------*/

/* convert some time in past to hours:minutes text format, or - if never. */

func age(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	var d = now.Sub(t)

	return fmt.Sprintf("%d:%02d", int(d.Hours()), int(d.Minutes())%60)
}

/* Convert latitude, longitude to text or - if not defined. */

func latLon(dlat maybe.Maybe[float64], dlon maybe.Maybe[float64]) string {
	var text = maybe.LiftA2(func(lat float64, lon float64) string {
		return fmt.Sprintf("%.2f %.2f", lat, lon)
	}, dlat, dlon)

	return maybe.FromMaybe("-", text)
}

/*------------------------------------------------------------------
 *
 * Function:	SaveRF
 *
 * Purpose:	Save information about station heard over the radio.
 *
 * Inputs:	channel	- Radio channel where heard.
 *
 *		pp	- Received packet object.
 *
 *		lat, lon - Position it reported, if any.  Recorded only
 *			  when both are present.
 *
 *------------------------------------------------------------------*/

func (mdb *DB) SaveRF(channel int, pp *ax25.Packet, lat maybe.Maybe[float64], lon maybe.Maybe[float64]) {
	var now = time.Now()

	var source = pp.AddrWithSSID(ax25.Source)

	/*
	 * How many digipeaters has it gone thru before we hear it?
	 * We can count the number of digi addresses that are marked as "has been used."
	 * This is not always accurate because there is inconsistency in digipeater behavior.
	 * The base AX.25 spec seems clear in this regard.  The used digipeaters should
	 * should accurately reflict the path taken by the packet.  Sometimes we see excess
	 * stuff in there.  Even when you understand what is going on, it is still an ambiguous
	 * situation.  Look for my rant in the User Guide.
	 */

	var hops = pp.Heard() - ax25.Source
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
		for k := range pp.NumRepeaters() {
			var digi = pp.AddrNoSSID(ax25.Repeater1 + k)
			var ssid = pp.SSID(ax25.Repeater1 + k)
			var used = pp.H(ax25.Repeater1 + k)

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Examining %s-%d  used=%d.\n", digi, ssid, used);

			if used > 0 && len(digi) == 5 && strings.EqualFold(digi[:4], "WIDE") && unicode.IsDigit(rune(digi[4])) && ssid == 0 {
				hops--
				//text_color_set(DW_COLOR_DEBUG);
				//dw_printf ("Decrease hop count to %d for problematic %s.\n", hops, digi);
			}
		}
	}

	mdb.mu.Lock()
	var mptr = mdb.db[source]
	if mptr == nil {
		/*
		 * Not heard before.  Add it.
		 */
		if mdb.debug > 0 {
			logrus.WithFields(logrus.Fields{
				"callsign": source,
				"hops":     hops,
			}).Debug("mheard SaveRF: added new station")
		}

		mptr = new(station)
		mptr.callsign = source
		mptr.count = 1
		mptr.channel = channel
		mptr.numDigiHops = hops
		mptr.lastHeardRF = now
		// Why did I not save the location for a position report here?

		mdb.db[source] = mptr
	} else {
		/*
		 * Update existing entry.
		 * The only tricky part here is that we might hear the same transmission
		 * several times.  First direct, then thru various digipeater paths.
		 * We are interested in the shortest path if heard very recently.
		 */
		if hops > mptr.numDigiHops && now.Sub(mptr.lastHeardRF).Seconds() < 15 {
			if mdb.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"callsign":      source,
					"hops":          hops,
					"previous_hops": mptr.numDigiHops,
					"seconds_ago":   int(now.Sub(mptr.lastHeardRF).Seconds()),
				}).Debug("mheard SaveRF: skipped, heard with fewer hops just before")
			}
		} else {
			if mdb.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"callsign":      source,
					"hops":          hops,
					"previous_hops": mptr.numDigiHops,
					"seconds_ago":   int(now.Sub(mptr.lastHeardRF).Seconds()),
				}).Debug("mheard SaveRF: updated station")
			}

			mptr.count++
			mptr.channel = channel
			mptr.numDigiHops = hops
			mptr.lastHeardRF = now
		}
	}

	if lat.IsJust() && lon.IsJust() {
		mptr.dlat = lat
		mptr.dlon = lon
	}

	mdb.mu.Unlock()

	if mdb.debug >= 2 {
		var limit = 10 // normally 30 or 60.  more frequent when debugging.

		logrus.WithFields(logrus.Fields{
			"minutes": limit,
			"DIR_CNT": mdb.Count(0, limit),
			"LOC_CNT": mdb.Count(2, limit),
			"RF_CNT":  mdb.Count(8, limit),
		}).Debug("mheard station counts")
	}

	if mdb.debug > 0 {
		mdb.dump()
	}
} /* end SaveRF */

/*------------------------------------------------------------------
 *
 * Function:	SaveIS
 *
 * Purpose:	Save information about station heard via Internet Server.
 *
 * Inputs:	ptext	- Packet in monitoring text form as sent by the Internet server.
 *
 *			  Any trailing CRLF should have been removed.
 *			  Typical examples:
 *
 *			KA1BTK-5>APDR13,TCPIP*,qAC,T2IRELAND:=4237.62N/07040.68W$/A=-00054 http://aprsdroid.org/
 *			N1HKO-10>APJI40,TCPIP*,qAC,N1HKO-JS:<IGATE,MSG_CNT=0,LOC_CNT=0
 *			K1RI-2>APWW10,WIDE1-1,WIDE2-1,qAS,K1RI:/221700h/9AmA<Ct3_ sT010/002g005t045r000p023P020h97b10148
 *			KC1BOS-2>T3PQ3S,WIDE1-1,WIDE2-1,qAR,W1TG-1:`c)@qh\>/\"50}TinyTrak4 Mobile
 *			WHO-IS>APJIW4,TCPIP*,qAC,AE5PL-JF::WB2OSZ   :C/Billerica Amateur Radio Society/MA/United States{XF}WO
 *
 *			  Notice how the final address in the header might not
 *			  be a valid AX.25 address.  We see a 9 character address
 *			  (with no ssid) and an ssid of two letters.
 *
 *			  The "q construct"  ( http://www.aprs-is.net/q.aspx ) provides
 *			  a clue about the journey taken but I don't think we care here.
 *
 *			  All we should care about here is the the source address.
 *			  Note that the source address might not adhere to the AX.25 format.
 *
 * Description:
 *
 *------------------------------------------------------------------*/

func (mdb *DB) SaveIS(ptext string) {
	var now = time.Now()

	// It is possible that source won't adhere to the AX.25 restrictions.
	// So we simply extract the source address, as text, from the beginning rather than
	// using AX25FromText() and ax25_get_addr_with_ssid().

	var source, _, _ = strings.Cut(ptext, ">")

	/*
	    * Keep this here in case I want to revive it to get location.
	   	packet_t pp = AX25FromText(ptext, 0);

	   	if (pp == nil) {
	   	  if (mheard_debug) {
	   	    text_color_set(DW_COLOR_ERROR);
	   	    dw_printf ("mheard SaveIS: Could not parse message from server.\n");
	   	    dw_printf ("%s\n", ptext);
	   	  }
	   	  return;
	   	}

	   	//////ax25_get_addr_with_ssid (pp, AX25_SOURCE, source);
	*/

	mdb.mu.Lock()
	var mptr = mdb.db[source]
	if mptr == nil {
		/*
		 * Not heard before.  Add it.
		 * Observation years later:
		 * Hmmmm.  I wonder why I did not store the location if available.
		 * An earlier example has an APRSdroid station reporting location without using [ham] RF.
		 */
		if mdb.debug > 0 {
			logrus.WithField("callsign", source).Debug("mheard SaveIS: added new station")
		}

		mptr = new(station)
		mptr.callsign = source
		mptr.count = 1
		mptr.lastHeardIS = now

		mdb.db[source] = mptr
	} else {
		/* Already there.  Update last heard from IS time. */
		if mdb.debug > 0 {
			logrus.WithFields(logrus.Fields{
				"callsign":    source,
				"seconds_ago": int(now.Sub(mptr.lastHeardIS).Seconds()),
			}).Debug("mheard SaveIS: updated station")
		}

		mptr.count++
		mptr.lastHeardIS = now
	}

	mdb.mu.Unlock()

	// Is is desirable to save any location in this case?
	// I don't think it would help.
	// The whole purpose of keeping the location is for message sending filter.
	// We wouldn't want to try sending a message to the station if we didn't hear it over the radio.
	// On the other hand, I don't think it would hurt.
	// The filter always includes a time since last heard over the radio.

	if mdb.debug >= 2 {
		var limit = 10 // normally 30 or 60

		logrus.WithFields(logrus.Fields{
			"minutes": limit,
			"DIR_CNT": mdb.Count(0, limit),
			"LOC_CNT": mdb.Count(2, limit),
			"RF_CNT":  mdb.Count(8, limit),
		}).Debug("mheard station counts")
	}

	if mdb.debug > 0 {
		mdb.dump()
	}
} /* end SaveIS */

/*------------------------------------------------------------------
 *
 * Function:	Count
 *
 * Purpose:	Count local stations for IGate statistics report like this:
 *
 *			<IGATE,MSG_CNT=1,LOC_CNT=25
 *
 * Inputs:	maxHops	- Include only stations heard with this number of
 *				  digipeater hops or less.  For reporting, we might use:
 *
 *					0 for DIR_CNT (heard directly)
 *					IGate transmit path for LOC_CNT.
 *						e.g. 3 for WIDE1-1,WIDE2-2
 *					8 for RF_CNT.
 *
 *		timeLimit	- Include only stations heard within this many minutes.
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

func (mdb *DB) Count(maxHops int, timeLimit int) int {
	var limit = time.Duration(timeLimit) * time.Minute
	var since = time.Now().Add(-limit)

	var count = 0

	mdb.mu.RLock()
	for _, p := range mdb.db {
		if !p.lastHeardRF.Before(since) && p.numDigiHops <= maxHops {
			count++
		}
	}
	mdb.mu.RUnlock()

	if mdb.debug == 1 {
		logrus.WithFields(logrus.Fields{
			"max_hops": maxHops,
			"minutes":  int(limit.Minutes()),
			"count":    count,
		}).Debug("mheard Count")
	}

	return (count)
} /* end Count */

/*------------------------------------------------------------------
 *
 * Function:	WasRecentlyNearby
 *
 * Purpose:	Determine whether given station was heard recently on the radio.
 *
 * Inputs:	role		- "addressee" or "source" if debug out is desired.
 *				  Otherwise empty string.
 *
 *		callsign	- Callsign for station.
 *
 *		timeLimit	- Include only stations heard within this many minutes.
 *				  Typically 180.
 *
 *		maxHops	- Include only stations heard with this number of
 *				  digipeater hops or less.  For reporting, we might use:
 *
 *		dlat, dlon, km	- Include only stations within distance of location.
 *				  Not used unless all three are supplied.
 *
 * Returns:	True or false
 *
 *------------------------------------------------------------------*/

func (mdb *DB) WasRecentlyNearby(role string, callsign string, timeLimitMinutes int, maxHops int, dlat maybe.Maybe[float64], dlon maybe.Maybe[float64], km maybe.Maybe[float64]) bool {
	var timeLimit = time.Duration(timeLimitMinutes) * time.Minute

	// The distance check needs a complete location to measure from, and a
	// distance to compare against; without all three it is not applied.
	var targetLat, haveTargetLat = dlat.Get()
	var targetLon, haveTargetLon = dlon.Get()
	var limitKm, haveLimitKm = km.Get()
	var haveTarget = haveTargetLat && haveTargetLon && haveLimitKm

	mdb.mu.RLock()
	defer mdb.mu.RUnlock()

	var log = logrus.WithFields(logrus.Fields{
		"role":     role,
		"callsign": callsign,
	})

	if role != "" {
		var question = log.WithFields(logrus.Fields{
			"minutes":  int(timeLimit.Minutes()),
			"max_hops": maxHops,
		})

		if haveTarget {
			question = question.WithFields(logrus.Fields{
				"km":  limitKm,
				"lat": targetLat,
				"lon": targetLon,
			})
		}

		question.Info("Was the station heard recently nearby?")
	}

	var mptr = mdb.db[callsign]

	if mptr == nil || mptr.lastHeardRF.IsZero() {
		if role != "" {
			log.Info("No, it has not been heard over the radio")
		}

		return false
	}

	var now = time.Now()
	var heardAgo = now.Sub(mptr.lastHeardRF)

	log = log.WithFields(logrus.Fields{
		"minutes_ago": int(heardAgo.Minutes()),
		"hops":        mptr.numDigiHops,
	})

	if heardAgo > timeLimit {
		if role != "" {
			log.Info("No, it was not heard over the radio recently enough")
		}

		return false
	}

	if mptr.numDigiHops > maxHops {
		if role != "" {
			log.Info("No, it was heard over the radio through too many digipeaters")
		}

		return false
	}

	// Apply physical distance check?

	var stationLat, haveStationLat = mptr.dlat.Get()
	var stationLon, haveStationLon = mptr.dlon.Get()

	if haveTarget && haveStationLat && haveStationLon {
		var dist = latlong.DistanceKm(stationLat, stationLon, targetLat, targetLon)

		if dist > limitKm {
			if role != "" {
				log.WithField("km", dist).Info("No, it was too far away")
			}

			return false
		} else {
			if role != "" {
				log.WithField("km", dist).Info("Yes, it was heard recently nearby")
			}

			return true
		}
	}

	// Passed all the tests.

	if role != "" {
		log.Info("Yes, it was heard recently nearby")
	}

	return true
} /* end WasRecentlyNearby */

/*------------------------------------------------------------------
 *
 * Function:	SetMSP
 *
 * Purpose:	Set the "message sender position" count for specified station.
 *
 * Inputs:	callsign	- Callsign for station which sent the "message."
 *
 *		num		- Number of position reports to allow.  Typically 1.
 *
 *------------------------------------------------------------------*/

func (mdb *DB) SetMSP(callsign string, num int) {
	mdb.mu.Lock()
	defer mdb.mu.Unlock()

	var mptr = mdb.db[callsign]

	if mptr != nil {
		mptr.msp = num

		if mdb.debug > 0 {
			logrus.WithFields(logrus.Fields{
				"callsign": callsign,
				"msp":      num,
			}).Debug("mheard MSP set")
		}
	} else {
		logrus.WithField("callsign", callsign).Error("Internal error: can't find station to set MSP")
	}
} /* end SetMSP */

/*------------------------------------------------------------------
 *
 * Function:	GetMSP
 *
 * Purpose:	Get the "message sender position" count for specified station.
 *
 * Inputs:	callsign	- Callsign for station which sent the "message."
 *
 * Returns:	The count for the specified station.
 *		0 if not found.
 *
 *------------------------------------------------------------------*/

func (mdb *DB) GetMSP(callsign string) int {
	mdb.mu.RLock()
	defer mdb.mu.RUnlock()

	var mptr = mdb.db[callsign]

	if mptr != nil {
		if mdb.debug > 0 {
			logrus.WithFields(logrus.Fields{
				"callsign": callsign,
				"msp":      mptr.msp,
			}).Debug("mheard MSP")
		}

		return (mptr.msp) // Should we have a time limit?
	}

	return (0)
} /* end GetMSP */

func (mdb *DB) dump() {
	mdb.mu.RLock()
	defer mdb.mu.RUnlock()

	/* Get linear array of node pointers so they can be sorted easily. */
	var stations = slices.Collect(maps.Values(mdb.db))

	/* Sort most recently heard to the top then print. */
	slices.SortFunc(stations, func(ma, mb *station) int {
		var ta = ma.lastHeardRF
		if ma.lastHeardIS.After(ta) {
			ta = ma.lastHeardIS
		}

		var tb = mb.lastHeardRF
		if mb.lastHeardIS.After(tb) {
			tb = mb.lastHeardIS
		}

		if ta.Before(tb) {
			return 1
		} else if ta.After(tb) {
			return -1
		} else {
			return 0
		}
	})

	var now = time.Now()

	for _, mptr := range stations {
		logrus.WithFields(logrus.Fields{
			"callsign": mptr.callsign,
			"count":    mptr.count,
			"channel":  mptr.channel,
			"hops":     mptr.numDigiHops,
			"rf_ago":   age(now, mptr.lastHeardRF),
			"is_ago":   age(now, mptr.lastHeardIS),
			"position": latLon(mptr.dlat, mptr.dlon),
			"msp":      mptr.msp,
		}).Debug("mheard station")
	}
} /* end dump */

/* end mheard.go */
