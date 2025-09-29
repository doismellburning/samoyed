package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Act as an APRS digital repeater.
 *		Similar cdigipeater.c is for connected mode.
 *
 *
 * Description:	Decide whether the specified packet should
 *		be digipeated and make necessary modifications.
 *
 *
 * References:	APRS Protocol Reference, document version 1.0.1
 *
 *			http://www.aprs.org/doc/APRS101.PDF
 *
 *		APRS SPEC Addendum 1.1
 *
 *			http://www.aprs.org/aprs11.html
 *
 *		APRS SPEC Addendum 1.2
 *
 *			http://www.aprs.org/aprs12.html
 *
 *		"The New n-N Paradigm"
 *
 *			http://www.aprs.org/fix14439.html
 *
 *		Preemptive Digipeating  (new in version 0.8)
 *
 *			http://www.aprs.org/aprs12/preemptive-digipeating.txt
 *			I ignored the part about the RR bits.
 *
 *------------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <ctype.h>	/* for isdigit, isupper */
// #include "regex.h"
// #include <unistd.h>
// #include "ax25_pad.h"
// #include "digipeater.h"
// #include "textcolor.h"
// #include "tq.h"
// #include "pfilter.h"
import "C"

import (
	"time"
)

/*
 * Keep pointer to configuration options.
 * Set by digipeater_init and used later.
 */

var digipeater_audio_config *C.struct_audio_s
var save_digi_config_p *C.struct_digi_config_s

/*
 * Maintain count of packets digipeated for each combination of from/to channel.
 */

var digi_count [MAX_TOTAL_CHANS][MAX_TOTAL_CHANS]int

func digipeater_get_count(from_chan, to_chan int) int {
	return (digi_count[from_chan][to_chan])
}

/*------------------------------------------------------------------------------
 *
 * Name:	digipeater_init
 *
 * Purpose:	Initialize with stuff from configuration file.
 *
 * Inputs:	p_audio_config	- Configuration for audio channels.
 *
 *		p_digi_config	- Digipeater configuration details.
 *
 * Outputs:	Save pointers to configuration for later use.
 *
 * Description:	Called once at application startup time.
 *
 *------------------------------------------------------------------------------*/

func digipeater_init(p_audio_config *C.struct_audio_s, p_digi_config *C.struct_digi_config_s) {
	digipeater_audio_config = p_audio_config
	save_digi_config_p = p_digi_config

	dedupe_init(time.Duration(p_digi_config.dedupe_time) * time.Second)
}

/*------------------------------------------------------------------------------
 *
 * Name:	digipeater
 *
 * Purpose:	Re-transmit packet if it matches the rules.
 *
 * Inputs:	chan	- Radio channel where it was received.
 *
 * 		pp	- Packet object.
 *
 * Returns:	None.
 *
 *
 *------------------------------------------------------------------------------*/

func digipeater(from_chan C.int, pp C.packet_t) {
	// Network TNC is OK for UI frames where we don't care about timing.

	if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS ||
		(digipeater_audio_config.chan_medium[from_chan] != MEDIUM_RADIO &&
			digipeater_audio_config.chan_medium[from_chan] != MEDIUM_NETTNC) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("APRS digipeater: Did not expect to receive on invalid channel %d.\n", from_chan)
	}

	/*
	 * First pass:  Look at packets being digipeated to same channel.
	 *
	 * We want these to get out quickly, bypassing the usual random wait time.
	 *
	 * Some may disagree but I followed what WB4APR had to say about it.
	 *
	 *	http://www.aprs.org/balloons.html
	 *
	 *		APRS NETWORK FRATRICIDE: Generally, all APRS digipeaters are supposed to transmit
	 *		immediately and all at the same time. They should NOT wait long enough for each
	 *		one to QRM the channel with the same copy of each packet. NO, APRS digipeaters
	 *		are all supposed to STEP ON EACH OTHER with every packet. This makes sure that
	 *		everyone in range of a digi will hear one and only one copy of each packet.
	 *		and that the packet will digipeat OUTWARD and not backward. The goal is that a
	 *		digipeated packet is cleared out of the local area in ONE packet time and not
	 *		N packet times for every N digipeaters that heard the packet. This means no
	 *		PERSIST times, no DWAIT times and no UIDWAIT times. Notice, this is contrary
	 *		to other packet systems that might want to guarantee delivery (but at the
	 *		expense of throughput). APRS wants to clear the channel quickly to maximize throughput.
	 *
	 *	http://www.aprs.org/kpc3/kpc3+WIDEn.txt
	 *
	 *		THIRD:  Eliminate the settings that are detrimental to the network.
	 *
	 *		* UIDWAIT should be OFF. (the default).  With it on, your digi is not doing the
	 *		fundamental APRS fratricide that is the primary mechanism for minimizing channel
	 *		loading.  All digis that hear the same packet are supposed to DIGI it at the SAME
	 *		time so that all those copies only take up one additional time slot. (but outward
	 *		located digs will hear it without collision (and continue outward propagation)
	 *
	 */

	for to_chan := range C.int(MAX_TOTAL_CHANS) {
		if save_digi_config_p.enabled[from_chan][to_chan] > 0 {
			if to_chan == from_chan {
				var result = digipeat_match(from_chan, pp, &digipeater_audio_config.mycall[from_chan][0],
					&digipeater_audio_config.mycall[to_chan][0],
					&save_digi_config_p.alias[from_chan][to_chan], &save_digi_config_p.wide[from_chan][to_chan],
					to_chan, save_digi_config_p.preempt[from_chan][to_chan],
					&save_digi_config_p.atgp[from_chan][to_chan][0],
					save_digi_config_p.filter_str[from_chan][to_chan])
				if result != nil {
					dedupe_remember(pp, to_chan)
					tq_append(to_chan, TQ_PRIO_0_HI, result) //  High priority queue.
					digi_count[from_chan][to_chan]++
				}
			}
		}
	}

	/*
	 * Second pass:  Look at packets being digipeated to different channel.
	 *
	 * These are lower priority
	 */

	for to_chan := range C.int(MAX_TOTAL_CHANS) {
		if save_digi_config_p.enabled[from_chan][to_chan] > 0 {
			if to_chan != from_chan {
				var result = digipeat_match(from_chan, pp, &digipeater_audio_config.mycall[from_chan][0],
					&digipeater_audio_config.mycall[to_chan][0],
					&save_digi_config_p.alias[from_chan][to_chan], &save_digi_config_p.wide[from_chan][to_chan],
					to_chan, save_digi_config_p.preempt[from_chan][to_chan],
					&save_digi_config_p.atgp[from_chan][to_chan][0],
					save_digi_config_p.filter_str[from_chan][to_chan])
				if result != nil {
					dedupe_remember(pp, to_chan)
					tq_append(to_chan, TQ_PRIO_1_LO, result) // Low priority queue.
					digi_count[from_chan][to_chan]++
				}
			}
		}
	}
} /* end digipeater */

/*------------------------------------------------------------------------------
 *
 * Name:	digipeat_match
 *
 * Purpose:	A simple digipeater for APRS.
 *
 * Input:	pp		- Pointer to a packet object.
 *
 *		mycall_rec	- Call of my station, with optional SSID,
 *				  associated with the radio channel where the
 *				  packet was received.
 *
 *		mycall_xmit	- Call of my station, with optional SSID,
 *				  associated with the radio channel where the
 *				  packet is to be transmitted.  Could be the same as
 *				  mycall_rec or different.
 *
 *		alias		- Compiled pattern for my station aliases or
 *				  "trapping" (repeating only once).
 *
 *		wide		- Compiled pattern for normal WIDEn-n digipeating.
 *
 *		to_chan		- Channel number that we are transmitting to.
 *				  This is needed to maintain a history for
 *			 	  removing duplicates during specified time period.
 *
 *		preempt		- Option for "preemptive" digipeating.
 *
 *		atgp		- No tracing if this matches alias prefix.
 *				  Hack added for special needs of ATGP.
 *
 *		filter_str	- Filter expression string or nil.
 *
 * Returns:	Packet object for transmission or nil.
 *		The original packet is not modified.  (with one exception, probably obsolete)
 *		We make a copy and return that modified copy!
 *		This is very important because we could digipeat from one channel to many.
 *
 * Description:	The packet will be digipeated if the next unused digipeater
 *		field matches one of the following:
 *
 *			- mycall_rec
 *			- udigi list (only once)
 *			- wide list (usual wideN-N rules)
 *
 *------------------------------------------------------------------------------*/

func digipeat_match(
	from_chan C.int,
	pp C.packet_t,
	mycall_rec *C.char,
	mycall_xmit *C.char,
	alias *C.regex_t,
	wide *C.regex_t,
	to_chan C.int,
	preempt C.enum_preempt_e,
	atgp *C.char,
	filter_str *C.char,
) C.packet_t {
	/*
	 * First check if filtering has been configured.
	 */
	if filter_str != nil {
		if pfilter(from_chan, to_chan, filter_str, pp, 1) != 1 {
			return (nil)
		}
	}

	/*
	 * The spec says:
	 *
	 * 	The SSID in the Destination Address field of all packets is coded to specify
	 * 	the APRS digipeater path.
	 * 	If the Destination Address SSID is -0, the packet follows the standard AX.25
	 * 	digipeater ("VIA") path contained in the Digipeater Addresses field of the
	 * 	AX.25 frame.
	 * 	If the Destination Address SSID is non-zero, the packet follows one of 15
	 * 	generic APRS digipeater paths.
	 *
	 *
	 * What if this is non-zero but there is also a digipeater path?
	 * I will ignore this if there is an explicit path.
	 *
	 * Note that this modifies the input.  But only once!
	 * Otherwise we don't want to modify the input because this could be called multiple times.
	 */

	/*
	 * Find the first repeater station which doesn't have "has been repeated" set.
	 *
	 * r = index of the address position in the frame.
	 */
	var r = C.ax25_get_first_not_repeated(pp)

	if r < C.AX25_REPEATER_1 {
		return (nil)
	}

	var repeater [AX25_MAX_ADDR_LEN]C.char
	C.ax25_get_addr_with_ssid(pp, r, &repeater[0])
	var ssid = C.ax25_get_ssid(pp, r)

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("First unused digipeater is %s, ssid=%d\n", repeater, ssid);
	#endif
	*/

	/*
	 * First check for explicit use of my call, including SSID.
	 * Someone might explicitly specify a particular path for testing purposes.
	 * This will bypass the usual checks for duplicates and my call in the source.
	 *
	 * In this case, we don't check the history so it would be possible
	 * to have a loop (of limited size) if someone constructed the digipeater paths
	 * correctly.  I would expect it only for testing purposes.
	 */

	if C.strcmp(&repeater[0], mycall_rec) == 0 {
		var result = C.ax25_dup(pp)
		// FIXME KG assert (result != nil);

		/* If using multiple radio channels, they */
		/* could have different calls. */
		C.ax25_set_addr(result, r, mycall_xmit)
		C.ax25_set_h(result, r)
		return (result)
	}

	/*
	 * Don't digipeat my own.  Fixed in 1.4 dev H.
	 * Alternatively we might feed everything transmitted into
	 * dedupe_remember rather than only frames out of digipeater.
	 */
	var source [AX25_MAX_ADDR_LEN]C.char
	C.ax25_get_addr_with_ssid(pp, AX25_SOURCE, &source[0])
	if C.strcmp(&source[0], mycall_rec) == 0 {
		return (nil)
	}

	/*
	 * Next try to avoid retransmitting redundant information.
	 * Duplicates are detected by comparing only:
	 *	- source
	 *	- destination
	 *	- info part
	 *	- but not the via path.  (digipeater addresses)
	 * A history is kept for some amount of time, typically 30 seconds.
	 * For efficiency, only a checksum, rather than the complete fields
	 * might be kept but the result is the same.
	 * Packets transmitted recently will not be transmitted again during
	 * the specified time period.
	 *
	 */

	if dedupe_check(pp, to_chan) {
		//#if DEBUG
		/* Might be useful if people are wondering why */
		/* some are not repeated.  Might also cause confusion. */

		text_color_set(DW_COLOR_INFO)
		dw_printf("Digipeater: Drop redundant packet to channel %d.\n", to_chan)
		//#endif
		return nil
	}

	/*
	 * For the alias pattern, we unconditionally digipeat it once.
	 * i.e.  Just replace it with MYCALL.
	 *
	 * My call should be an implied member of this set.
	 * In this implementation, we already caught it further up.
	 */
	var err = C.regexec(alias, &repeater[0], 0, nil, 0)
	if err == 0 {
		var result = C.ax25_dup(pp)
		// FIXME KG assert (result != nil);

		C.ax25_set_addr(result, r, mycall_xmit)
		C.ax25_set_h(result, r)
		return (result)
	} else if err != C.REG_NOMATCH {
		var err_msg [100]C.char
		C.regerror(err, alias, &err_msg[0], C.ulong(len(err_msg)))
		text_color_set(DW_COLOR_ERROR)
		dw_printf("%s\n", C.GoString(&err_msg[0]))
	}

	/*
	 * If preemptive digipeating is enabled, try matching my call
	 * and aliases against all remaining unused digipeaters.
	 *
	 * Bob says: "GENERIC XXXXn-N DIGIPEATING should not do preemptive digipeating."
	 *
	 * But consider this case:  https://github.com/wb2osz/direwolf/issues/488
	 */

	if preempt != C.PREEMPT_OFF {
		for r2 := r + 1; r2 < C.ax25_get_num_addr(pp); r2++ {
			var repeater2 [AX25_MAX_ADDR_LEN]C.char

			C.ax25_get_addr_with_ssid(pp, r2, &repeater2[0])

			// text_color_set (DW_COLOR_DEBUG);
			// dw_printf ("test match %d %s\n", r2, repeater2);

			if C.strcmp(&repeater2[0], mycall_rec) == 0 ||
				C.regexec(alias, &repeater2[0], 0, nil, 0) == 0 {
				var result = C.ax25_dup(pp)
				// FIXME KG assert (result != nil);

				C.ax25_set_addr(result, r2, mycall_xmit)
				C.ax25_set_h(result, r2)

				switch preempt {
				case C.PREEMPT_DROP: /* remove all prior */
					// TODO: deprecate this option.  Result is misleading.

					text_color_set(DW_COLOR_ERROR)
					dw_printf("The digipeat DROP option will be removed in a future release.  Use PREEMPT for preemptive digipeating.\n")

					for r2 > AX25_REPEATER_1 {
						C.ax25_remove_addr(result, r2-1)
						r2--
					}
				case C.PREEMPT_MARK: // TODO: deprecate this option.  Result is misleading.

					text_color_set(DW_COLOR_ERROR)
					dw_printf("The digipeat MARK option will be removed in a future release.  Use PREEMPT for preemptive digipeating.\n")

					r2--
					for r2 >= AX25_REPEATER_1 && C.ax25_get_h(result, r2) == 0 {
						C.ax25_set_h(result, r2)
						r2--
					}
				/* 2025-07-29 KG Commenting out the PREEMPT_TRACE handling so it falls through to the default case,
					which was the original behaviour of the C too
				case C.PREEMPT_TRACE:
				*/
				/* My enhancement - remove prior unused digis. */
				/* this provides an accurate path of where packet traveled. */

				// Uh oh.  It looks like sample config files went out
				// with this option.  Should it be renamed as
				// PREEMPT which is more descriptive?
				default:
					for r2 > AX25_REPEATER_1 && C.ax25_get_h(result, r2-1) == 0 {
						C.ax25_remove_addr(result, r2-1)
						r2--
					}
				}

				// Idea: Here is an interesting idea for a new option.  REORDER?
				// The preemptive digipeater could move its call after the (formerly) last used digi field
				// and preserve all the unused fields after that.  The list of used addresses would
				// accurately record the journey taken by the packet.

				// https://groups.yahoo.com/neo/groups/aprsisce/conversations/topics/31935

				// >  I was wishing for a non-marking preemptive digipeat so that the original packet would be left intact
				// >  or maybe something like WIDE1-1,WIDE2-1,KJ4OVQ-9 becoming KJ4OVQ-9*,WIDE1-1,WIDE2-1.

				return (result)
			}
		}
	}

	/*
	 * For the wide pattern, we check the ssid and decrement it.
	 */

	err = C.regexec(wide, &repeater[0], 0, nil, 0)
	if err == 0 {
		// Special hack added for ATGP to behave like some combination of options in some old TNC
		// so the via path does not continue to grow and exceed the 8 available positions.
		// The strange thing about this is that the used up digipeater is left there but
		// removed by the next digipeater.

		if C.strlen(atgp) > 0 && C.strncasecmp(&repeater[0], atgp, C.strlen(atgp)) == 0 {
			if ssid >= 1 && ssid <= 7 {
				var result = C.ax25_dup(pp)
				// FIXME KG assert (result != nil);

				// First, remove any already used digipeaters.

				for C.ax25_get_num_addr(result) >= 3 && C.ax25_get_h(result, AX25_REPEATER_1) == 1 {
					C.ax25_remove_addr(result, AX25_REPEATER_1)
					r--
				}

				ssid--
				C.ax25_set_ssid(result, r, ssid) // could be zero.
				if ssid == 0 {
					C.ax25_set_h(result, r)
				}

				// Insert own call at beginning and mark it used.

				C.ax25_insert_addr(result, AX25_REPEATER_1, mycall_xmit)
				C.ax25_set_h(result, AX25_REPEATER_1)
				return (result)
			}
		}

		/*
		 * If ssid == 1, we simply replace the repeater with my call and
		 *	mark it as being used.
		 *
		 * Otherwise, if ssid in range of 2 to 7,
		 *	Decrement y and don't mark repeater as being used.
		 * 	Insert own call ahead of this one for tracing if we don't already have the
		 *	maximum number of repeaters.
		 */

		if ssid == 1 {
			var result = C.ax25_dup(pp)
			// FIXME KG assert (result != nil);

			C.ax25_set_addr(result, r, mycall_xmit)
			C.ax25_set_h(result, r)
			return (result)
		}

		if ssid >= 2 && ssid <= 7 {
			var result = C.ax25_dup(pp)
			// FIXME KG assert (result != nil);

			C.ax25_set_ssid(result, r, ssid-1) // should be at least 1

			if C.ax25_get_num_repeaters(pp) < AX25_MAX_REPEATERS {
				C.ax25_insert_addr(result, r, mycall_xmit)
				C.ax25_set_h(result, r)
			}
			return (result)
		}
	} else if err != C.REG_NOMATCH {
		var err_msg [100]C.char
		C.regerror(err, wide, &err_msg[0], C.ulong(len(err_msg)))
		text_color_set(DW_COLOR_ERROR)
		dw_printf("%s\n", C.GoString(&err_msg[0]))
	}

	/*
	 * Don't repeat it if we get here.
	 */

	return (nil)
}

/*------------------------------------------------------------------------------
 *
 * Name:	digi_regen
 *
 * Purpose:	Send regenerated copy of what we received.
 *
 * Inputs:	chan	- Radio channel where it was received.
 *
 * 		pp	- Packet object.
 *
 * Returns:	None.
 *
 * Description:	TODO...
 *
 *		Initial reports were favorable.
 *		Should document what this is all about if there is still interest...
 *
 *------------------------------------------------------------------------------*/

func digi_regen(from_chan C.int, pp C.packet_t) {
	/*
		packet_t result;
	*/

	// dw_printf ("digi_regen()\n");

	// FIXME KG assert (from_chan >= 0 && from_chan < MAX_TOTAL_CHANS);

	for to_chan := range C.int(MAX_TOTAL_CHANS) {
		if save_digi_config_p.regen[from_chan][to_chan] > 0 {
			var result = C.ax25_dup(pp)
			if result != nil {
				// TODO:  if AX.25 and has been digipeated, put in HI queue?
				tq_append(to_chan, TQ_PRIO_1_LO, result)
			}
		}
	}
} /* end dig_regen */
