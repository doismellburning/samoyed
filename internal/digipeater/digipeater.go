// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package digipeater

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

import (
	"regexp"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dedupe"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/pfilter"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
)

/*
 * Information required for digipeating.
 *
 * The configuration file reader fills in this information
 * and it is passed to New at application start up time.
 */

// DefaultDedupe is how many seconds a packet is remembered for, so as not to
// digipeat a duplicate of it, when the configuration doesn't say.
const DefaultDedupe = 30

// Preempt is what preemptive digipeating does with the unused
// digipeaters ahead of the one that matched us.
type Preempt int

const (
	PreemptOff Preempt = iota
	PreemptDrop
	PreemptMark
	PreemptTrace
)

// Config is the APRS digipeater's configuration.
type Config struct {
	DedupeTime int /* Don't digipeat duplicate packets */
	/* within this number of seconds. */

	/*
	 * Rules for each of the [from_chan][to_chan] combinations.
	 */

	Alias [phy.MaxTotalChans][phy.MaxTotalChans]*regexp.Regexp

	Wide [phy.MaxTotalChans][phy.MaxTotalChans]*regexp.Regexp

	Enabled [phy.MaxTotalChans][phy.MaxTotalChans]bool

	Preempt [phy.MaxTotalChans][phy.MaxTotalChans]Preempt

	// ATGP is an ugly hack for the specific need of ATGP which needs more that 8 digipeaters.
	// DO NOT put this in the User Guide.  On a need to know basis.

	ATGP [phy.MaxTotalChans][phy.MaxTotalChans]string

	FilterStr [phy.MaxTotalChans + 1][phy.MaxTotalChans + 1]string
	// NULL or optional Packet Filter strings such as "t/m".
	// Notice the size of arrays is one larger than normal.
	// That extra position is for the IGate.

	Regen [phy.MaxTotalChans][phy.MaxTotalChans]bool // Regenerate packet.
	// Sort of like digipeating but passed along unchanged.
}

// Port is what a digipeater needs to know of each channel it might
// hear on or transmit to: our callsign there, and what kind of medium it is.
type Port struct {
	MyCall string
	Medium phy.Medium
}

// Digipeater is the APRS digipeater: the configuration it was started with,
// the duplicate suppression it shares with APRStt object reports, and a count
// of packets digipeated for each combination of from/to channel.
type Digipeater struct {
	ports    [phy.MaxTotalChans]Port
	config   *Config
	filter   *pfilter.PacketFilter
	dedupe   *dedupe.Service
	transmit dwcore.FrameSender // Where digipeated packets go; nil to drop them.
	count    [phy.MaxTotalChans][phy.MaxTotalChans]int
}

/*------------------------------------------------------------------------------
 *
 * Name:	New
 *
 * Purpose:	Initialize with stuff from configuration file.
 *
 * Inputs:	ports		- Our callsign and the medium of each channel.
 *
 *		p_digi_config	- Digipeater configuration details.
 *
 *		filter		- What decides whether FILTER lets a packet through.
 *
 *		onRemember	- Told of each packet digipeated, and its channel,
 *				  so the IGate doesn't send the same to RF; nil
 *				  for nobody to tell.
 *
 *		transmit	- Where digipeated packets go, to be
 *				  transmitted; nil to drop them.
 *
 * Description:	Called once at application startup time.
 *
 *------------------------------------------------------------------------------*/

func New(
	ports [phy.MaxTotalChans]Port,
	p_digi_config *Config,
	filter *pfilter.PacketFilter,
	onRemember func(channel int, pp *ax25.Packet),
	transmit dwcore.FrameSender,
) *Digipeater {
	var d = new(Digipeater)
	d.ports = ports
	d.config = p_digi_config
	d.filter = filter
	d.transmit = transmit
	d.dedupe = dedupe.New(time.Duration(p_digi_config.DedupeTime)*time.Second, onRemember)

	return d
}

func (d *Digipeater) GetCount(from_chan, to_chan int) int {
	return d.count[from_chan][to_chan]
}

// Remember records a packet we are transmitting ourselves, so that the
// digipeater will not repeat it when it is heard again.  It does nothing on a
// nil receiver, so a caller need not check whether the digipeater has been
// started yet.
func (d *Digipeater) Remember(channel int, pp *ax25.Packet) {
	if d == nil {
		return
	}

	d.dedupe.Remember(channel, pp)
}

/*------------------------------------------------------------------------------
 *
 * Name:	Digipeat
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

func (d *Digipeater) Digipeat(from_chan int, pp *ax25.Packet) {
	// Network or serial TNC is OK for UI frames where we don't care about timing.
	if from_chan < 0 || from_chan >= phy.MaxTotalChans ||
		(d.ports[from_chan].Medium != phy.MediumRadio &&
			d.ports[from_chan].Medium != phy.MediumNetTNC &&
			d.ports[from_chan].Medium != phy.MediumSerialTNC &&
			d.ports[from_chan].Medium != phy.MediumAXUDP) {
		logrus.WithField("channel", from_chan).Error("APRS digipeater: Did not expect to receive on invalid channel")

		// Saying so and then carrying on meant indexing the per-channel
		// configuration with the very channel number just called invalid,
		// which takes the program down rather than rejecting the frame.
		// cdigipeater has always returned here.
		return
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

	for to_chan := range phy.MaxTotalChans {
		if d.config.Enabled[from_chan][to_chan] {
			if to_chan == from_chan {
				var result = d.match(from_chan, pp, d.ports[from_chan].MyCall,
					d.ports[to_chan].MyCall,
					d.config.Alias[from_chan][to_chan], d.config.Wide[from_chan][to_chan],
					to_chan, d.config.Preempt[from_chan][to_chan],
					d.config.ATGP[from_chan][to_chan],
					d.config.FilterStr[from_chan][to_chan])
				if result != nil {
					d.dedupe.Remember(to_chan, pp)
					if d.transmit != nil {
						d.transmit(to_chan, dwcore.PrioHigh, result) //  High priority queue.
					}

					d.count[from_chan][to_chan]++
				}
			}
		}
	}

	/*
	 * Second pass:  Look at packets being digipeated to different channel.
	 *
	 * These are lower priority
	 */

	for to_chan := range phy.MaxTotalChans {
		if d.config.Enabled[from_chan][to_chan] {
			if to_chan != from_chan {
				var result = d.match(from_chan, pp, d.ports[from_chan].MyCall,
					d.ports[to_chan].MyCall,
					d.config.Alias[from_chan][to_chan], d.config.Wide[from_chan][to_chan],
					to_chan, d.config.Preempt[from_chan][to_chan],
					d.config.ATGP[from_chan][to_chan],
					d.config.FilterStr[from_chan][to_chan])
				if result != nil {
					d.dedupe.Remember(to_chan, pp)
					if d.transmit != nil {
						d.transmit(to_chan, dwcore.PrioLow, result) // Low priority queue.
					}

					d.count[from_chan][to_chan]++
				}
			}
		}
	}
} /* end Digipeat */

/*------------------------------------------------------------------------------
 *
 * Name:	Regen
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

func (d *Digipeater) Regen(from_chan int, pp *ax25.Packet) {
	/*
		packet_t result;
	*/

	dwutil.Assert(from_chan >= 0 && from_chan < phy.MaxTotalChans)

	for to_chan := range phy.MaxTotalChans {
		if d.config.Regen[from_chan][to_chan] {
			var result = pp.Dup()
			if result != nil && d.transmit != nil {
				// TODO:  if AX.25 and has been digipeated, put in HI queue?
				d.transmit(to_chan, dwcore.PrioLow, result)
			}
		}
	}
} /* end Regen */

/*------------------------------------------------------------------------------
 *
 * Name:	match
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

func (d *Digipeater) match(
	from_chan int,
	pp *ax25.Packet,
	mycall_rec string,
	mycall_xmit string,
	alias *regexp.Regexp,
	wide *regexp.Regexp,
	to_chan int,
	preempt Preempt,
	atgp string,
	filter_str string,
) *ax25.Packet {
	/*
	 * First check if filtering has been configured.
	 */
	if filter_str != "" {
		var result, err = d.filter.Filter(from_chan, to_chan, filter_str, pp, true)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"from_channel": from_chan,
				"to_channel":   to_chan,
				"filter":       filter_str,
			}).Error("APRS digipeater: Bad filter")
		}

		if result != 1 {
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
	var r = pp.FirstNotRepeated()

	if r < ax25.Repeater1 {
		return (nil)
	}

	var repeater = pp.AddrWithSSID(r)
	var ssid = pp.SSID(r)

	logrus.WithFields(logrus.Fields{
		"repeater": repeater,
		"ssid":     ssid,
	}).Debug("First unused digipeater")

	/*
	 * First check for explicit use of my call, including SSID.
	 * Someone might explicitly specify a particular path for testing purposes.
	 * This will bypass the usual checks for duplicates and my call in the source.
	 *
	 * In this case, we don't check the history so it would be possible
	 * to have a loop (of limited size) if someone constructed the digipeater paths
	 * correctly.  I would expect it only for testing purposes.
	 */

	if repeater == mycall_rec {
		var result = pp.Dup()

		/* If using multiple radio channels, they */
		/* could have different calls. */
		result.SetAddr(r, mycall_xmit)
		result.SetH(r)

		return (result)
	}

	/*
	 * Don't digipeat my own.  Fixed in 1.4 dev H.
	 * Alternatively we might feed everything transmitted into
	 * dedupe_remember rather than only frames out of digipeater.
	 */
	var source = pp.AddrWithSSID(ax25.Source)
	if source == mycall_rec {
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

	if d.dedupe.Check(to_chan, pp) {
		/* Might be useful if people are wondering why */
		/* some are not repeated.  Might also cause confusion. */
		logrus.WithField("channel", to_chan).Debug("Digipeater: Drop redundant packet")

		return nil
	}

	/*
	 * For the alias pattern, we unconditionally digipeat it once.
	 * i.e.  Just replace it with MYCALL.
	 *
	 * My call should be an implied member of this set.
	 * In this implementation, we already caught it further up.
	 */

	if alias.MatchString(repeater) {
		var result = pp.Dup()

		result.SetAddr(r, mycall_xmit)
		result.SetH(r)

		return (result)
	}

	/*
	 * If preemptive digipeating is enabled, try matching my call
	 * and aliases against all remaining unused digipeaters.
	 *
	 * Bob says: "GENERIC XXXXn-N DIGIPEATING should not do preemptive digipeating."
	 *
	 * But consider this case:  https://github.com/wb2osz/direwolf/issues/488
	 */

	if preempt != PreemptOff {
		for r2 := r + 1; r2 < pp.NumAddr(); r2++ {
			var repeater2 = pp.AddrWithSSID(r2)

			// text_color_set (DW_COLOR_DEBUG);
			// dw_printf ("test match %d %s\n", r2, repeater2);

			if repeater2 == mycall_rec || alias.MatchString(repeater2) {
				var result = pp.Dup()

				result.SetAddr(r2, mycall_xmit)
				result.SetH(r2)

				switch preempt {
				case PreemptDrop: /* remove all prior */
					// TODO: deprecate this option.  Result is misleading.
					logrus.Warn("The digipeat DROP option will be removed in a future release.  Use PREEMPT for preemptive digipeating.")

					for r2 > ax25.Repeater1 {
						result.RemoveAddr(r2 - 1)
						r2--
					}
				case PreemptMark: // TODO: deprecate this option.  Result is misleading.
					logrus.Warn("The digipeat MARK option will be removed in a future release.  Use PREEMPT for preemptive digipeating.")

					r2--
					for r2 >= ax25.Repeater1 && result.H(r2) == 0 {
						result.SetH(r2)
						r2--
					}
				/* 2025-07-29 KG Commenting out the PreemptTrace handling so it falls through to the default case,
					which was the original behaviour of the C too
				case PreemptTrace:
				*/
				/* My enhancement - remove prior unused digis. */
				/* this provides an accurate path of where packet traveled. */

				// Uh oh.  It looks like sample config files went out
				// with this option.  Should it be renamed as
				// PREEMPT which is more descriptive?
				default:
					for r2 > ax25.Repeater1 && result.H(r2-1) == 0 {
						result.RemoveAddr(r2 - 1)
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

	if wide.MatchString(repeater) {
		// Special hack added for ATGP to behave like some combination of options in some old TNC
		// so the via path does not continue to grow and exceed the 8 available positions.
		// The strange thing about this is that the used up digipeater is left there but
		// removed by the next digipeater.
		if len(atgp) > 0 && strings.HasPrefix(strings.ToLower(repeater), strings.ToLower(atgp)) {
			if ssid >= 1 && ssid <= 7 {
				var result = pp.Dup()

				// First, remove any already used digipeaters.

				for result.NumAddr() >= 3 && result.H(ax25.Repeater1) == 1 {
					result.RemoveAddr(ax25.Repeater1)

					r--
				}

				ssid--
				result.SetSSID(r, ssid) // could be zero.

				if ssid == 0 {
					result.SetH(r)
				}

				// Insert own call at beginning and mark it used.

				if result.InsertAddr(ax25.Repeater1, mycall_xmit) {
					result.SetH(ax25.Repeater1)
				}

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
			var result = pp.Dup()

			result.SetAddr(r, mycall_xmit)
			result.SetH(r)

			return (result)
		}

		if ssid >= 2 && ssid <= 7 {
			var result = pp.Dup()

			result.SetSSID(r, ssid-1) // should be at least 1

			if pp.NumRepeaters() < ax25.MaxRepeaters && result.InsertAddr(r, mycall_xmit) {
				result.SetH(r)
			}

			return (result)
		}
	}

	/*
	 * Don't repeat it if we get here.
	 */

	return (nil)
}
