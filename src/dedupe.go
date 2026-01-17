package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Avoid transmitting duplicate packets which are too
 *		close together.
 *
 *
 * Description:	We want to avoid digipeating duplicate packets to
 *		to help reduce radio channel congestion with
 *		redundant information.
 *		Duplicate packets can occur in several ways:
 *
 *		(1) A digipeated packet can loop between 2 or more
 *			digipeaters.  For example:
 *
 *			W1ABC>APRS,WIDE3-3
 *			W1ABC>APRS,mycall*,WIDE3-2
 *			W1ABC>APRS,mycall,RPT1*,WIDE3-1
 *			W1ABC>APRS,mycall,RPT1,mycall*
 *
 *		(2) We could hear our own original transmission
 *			repeated by someone else.  Example:
 *
 *			mycall>APRS,WIDE3-3
 *			mycall>APRS,RPT1*,WIDE3-2
 *			mycall>APRS,RPT1*,mycall*,WIDE3-1
 *
 *		(3) We could hear the same packet from multiple
 *			digipeaters (with or without the original).
 *
 *			W1ABC>APRS,WIDE3-2
 *			W1ABC>APRS,RPT1*,WIDE3-2
 *			W1ABC>APRS,RPT2*,WIDE3-2
 *			W1ABC>APRS,RPT3*,WIDE3-2
 *
 *		(4) Someone could be sending the same thing over and
 *			over with very little delay in between.
 *
 *			W1ABC>APRS,WIDE3-3
 *			W1ABC>APRS,WIDE3-3
 *			W1ABC>APRS,WIDE3-3
 *
 *		We can catch the first two by looking for 'mycall' in
 *		the source or digipeater fields.
 *
 *		The other two cases require us to keep a record of what
 *		we transmitted recently and test for duplicates that
 *		should be dropped.
 *
 *		Once we have the solution to catch cases (3) and (4)
 *		there is no reason for the special case of looking for
 *		mycall.  The same technique catches all four situations.
 *
 *		For detecting duplicates, we need to look
 *			+ source station
 *			+ destination
 *			+ information field
 *		but NOT the changing list of digipeaters.
 *
 *		Typically, only a checksum is kept to reduce memory
 *		requirements and amount of compution for comparisons.
 *		There is a very very small probability that two unrelated
 *		packets will result in the same checksum, and the
 *		undesired dropping of the packet.
 *
 * References:	Original APRS specification:
 *
 *			TBD...
 *
 *		"The New n-N Paradigm"
 *
 *			http://www.aprs.org/fix14439.html
 *
 *------------------------------------------------------------------*/

// #define DEDUPE_C
// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <time.h>
// #include "ax25_pad.h"
// #include "igate.h"
import "C"

import (
	"time"
)

/*------------------------------------------------------------------------------
 *
 * Name:	dedupe_init
 *
 * Purpose:	Initialize the duplicate detection subsystem.
 *
 * Input:	ttl	- Number of seconds to retain information
 *			  about recent transmissions.
 *
 *
 * Returns:	None
 *
 * Description:	This should be called at application startup.
 *
 *
 *------------------------------------------------------------------------------*/

var history_time = 30 * time.Second /* Number of seconds to keep information */
/* about recent transmissions. */

const HISTORY_MAX = 25 /* Maximum number of transmission */
/* records to keep.  If we run out of */
/* room the oldest ones are overwritten */
/* before they expire. */

var insert_next int /* Index, in array below, where next */
/* item should be stored. */

type historyEntry struct {
	time_stamp time.Time /* When the packet was transmitted. */

	checksum C.ushort /* Some sort of checksum for the */
	/* source, destination, and information. */
	/* is is not used anywhere else. */

	xmit_channel C.int /* Radio channel number. */

}

var history [HISTORY_MAX]historyEntry

func dedupe_init(ttl time.Duration) {
	history_time = ttl
	insert_next = 0
}

/*------------------------------------------------------------------------------
 *
 * Name:	dedupe_remember
 *
 * Purpose:	Save information about a packet being transmitted so we
 *		can detect, and avoid, duplicates later.
 *
 * Input:	pp	- Pointer to packet object.
 *
 *		chan	- Radio channel for transmission.
 *
 * Returns:	None
 *
 * Rambling:	At one time, my thinking is that we want to keep track of
 *		ALL transmitted packets regardless of origin or type.
 *
 *			+ my beacons
 *			+ anything from a connected application
 *			+ anything digipeated
 *
 *		The easiest way to catch all cases is to call dedup_remember()
 *		from inside tq_append().
 *
 *		But I don't think that is the right approach.
 *		When acting as a KISS TNC, we should just shovel everything
 *		through and not question what the application is doing.
 *		If the connected application has a digipeating function,
 *		it's responsible for those decisions.
 *
 *		My current thinking is that dedupe_remember() should be
 *		called BEFORE tq_append() in the digipeater case.
 *
 *		We should also capture our own beacon transmissions.
 *
 *------------------------------------------------------------------------------*/

func dedupe_remember(pp C.packet_t, channel C.int) {
	history[insert_next].time_stamp = time.Now()
	history[insert_next].checksum = ax25_dedupe_crc(pp)
	history[insert_next].xmit_channel = channel

	insert_next++
	if insert_next >= HISTORY_MAX {
		insert_next = 0
	}

	/* If we send something by digipeater, we don't */
	/* want to do it again if it comes from APRS-IS. */
	/* Not sure about the other way around. */

	ig_to_tx_remember(pp, channel, 1)
}

/*------------------------------------------------------------------------------
 *
 * Name:	dedupe_check
 *
 * Purpose:	Check whether this is a duplicate of another sent recently.
 *
 * Input:	pp	- Pointer to packet object.
 *
 *		chan	- Radio channel for transmission.
 *
 * Returns:	True if it is a duplicate.
 *
 *
 *------------------------------------------------------------------------------*/

func dedupe_check(pp C.packet_t, channel C.int) bool {
	var crc = ax25_dedupe_crc(pp)
	var now = time.Now()

	for _, h := range history {
		if h.checksum != crc {
			continue
		}

		if h.xmit_channel != channel {
			continue
		}

		var expiry = h.time_stamp.Add(history_time)
		if now.After(expiry) {
			continue
		}

		return true
	}

	return false
}

/* end dedupe.c */
