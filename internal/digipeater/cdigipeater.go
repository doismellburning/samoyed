// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package digipeater

/*------------------------------------------------------------------
 *
 * Purpose:	Act as an digital repeater for connected AX.25 mode.
 *		Similar digipeater.c is for APRS.
 *
 *
 * Description:	Decide whether the specified packet should
 *		be digipeated.  Put my callsign in the digipeater field used.
 *
 *		APRS and connected mode were two split into two
 *		separate files.  Yes, there is duplicate code but they
 *		are significantly different and I thought it would be
 *		too confusing to munge them together.
 *
 * References:	The Ax.25 protocol barely mentions digipeaters and
 *		and doesn't describe how they should work.
 *
 *------------------------------------------------------------------*/

import (
	"regexp"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/pfilter"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
)

/*
 * Information required for Connected mode digipeating.
 *
 * The configuration file reader fills in this information
 * and it is passed to NewConnected at application start up time.
 */

// ConnectedConfig is the connected mode digipeater's configuration.
type ConnectedConfig struct {

	/*
	 * Rules for each of the [from_chan][to_chan] combinations.
	 */

	// For APRS digipeater, we use phy.MaxTotalChans because we use external TNCs.
	// Connected mode packet must use internal modems we we use phy.MaxRadioChans.

	Enabled [phy.MaxRadioChans][phy.MaxRadioChans]bool // Is it enabled for from/to pair?

	HasAlias [phy.MaxRadioChans][phy.MaxRadioChans]bool // If there was no alias in the config file,
	// the structure below will not be set up
	// properly and an attempt to use it could
	// result in a crash.  (fixed v1.5)
	// Not needed for [APRS] DIGIPEAT because
	// the Alias is mandatory there.
	Alias [phy.MaxRadioChans][phy.MaxRadioChans]*regexp.Regexp

	CFilterStr [phy.MaxRadioChans][phy.MaxRadioChans]string
	// NULL or optional Packet Filter strings such as "t/m".
}

// Connected is the connected mode digipeater: the configuration it
// was started with, and a count of packets digipeated for each combination of
// from/to channel.
type Connected struct {
	ports    [phy.MaxTotalChans]Port
	config   *ConnectedConfig
	filter   *pfilter.PacketFilter
	transmit dwcore.FrameSender // Where digipeated packets go; nil to drop them.
	count    [phy.MaxRadioChans][phy.MaxRadioChans]int
}

/*------------------------------------------------------------------------------
 *
 * Name:	NewConnected
 *
 * Purpose:	Initialize with stuff from configuration file.
 *
 * Inputs:	ports		- Our callsign and the medium of each channel.
 *
 *		p_cdigi_config	- Connected Digipeater configuration details.
 *
 *		filter		- What decides whether CFILTER lets a packet through.
 *
 *		transmit	- Where digipeated packets go, to be
 *				  transmitted; nil to drop them.
 *
 * Description:	Called once at application startup time.
 *
 *------------------------------------------------------------------------------*/

func NewConnected(ports [phy.MaxTotalChans]Port, p_cdigi_config *ConnectedConfig, filter *pfilter.PacketFilter, transmit dwcore.FrameSender) *Connected {
	var d = new(Connected)
	d.ports = ports
	d.config = p_cdigi_config
	d.filter = filter
	d.transmit = transmit

	return d
}

func (d *Connected) GetCount(from_chan, to_chan int) int {
	return d.count[from_chan][to_chan]
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
 *------------------------------------------------------------------------------*/

func (d *Connected) Digipeat(from_chan int, pp *ax25.Packet) {
	// Connected mode is allowed only for channels with internal modem.
	// It probably wouldn't matter for digipeating but let's keep that rule simple and consistent.
	if from_chan < 0 || from_chan >= phy.MaxRadioChans ||
		(d.ports[from_chan].Medium != phy.MediumRadio &&
			d.ports[from_chan].Medium != phy.MediumNetTNC &&
			d.ports[from_chan].Medium != phy.MediumSerialTNC) {
		logrus.WithField("channel", from_chan).Error("cdigipeater: Did not expect to receive on invalid channel")

		return
	}

	/*
	 * First pass:  Look at packets being digipeated to same channel.
	 *
	 * There was a reason for two passes for APRS.
	 * Might not have a benefit here.
	 */

	for to_chan := range phy.MaxRadioChans {
		if d.config.Enabled[from_chan][to_chan] && to_chan == from_chan {
			d.digipeatTo(from_chan, to_chan, pp)
		}
	}

	/*
	 * Second pass:  Look at packets being digipeated to different channel.
	 */

	for to_chan := range phy.MaxRadioChans {
		if d.config.Enabled[from_chan][to_chan] && to_chan != from_chan {
			d.digipeatTo(from_chan, to_chan, pp)
		}
	}
} /* end Digipeat */

// digipeatTo queues pp for transmission on to_chan if the from/to channel
// pair's rules say it should be repeated.
func (d *Connected) digipeatTo(from_chan int, to_chan int, pp *ax25.Packet) {
	var result = d.match(from_chan, pp, d.ports[from_chan].MyCall,
		d.ports[to_chan].MyCall,
		d.config.HasAlias[from_chan][to_chan],
		d.config.Alias[from_chan][to_chan], to_chan,
		d.config.CFilterStr[from_chan][to_chan])
	if result != nil {
		if d.transmit != nil {
			d.transmit(to_chan, dwcore.PrioHigh, result)
		}

		d.count[from_chan][to_chan]++
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	match
 *
 * Purpose:	A simple digipeater for connected mode AX.25.
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
 *		has_alias	- True if we have an alias.
 *
 *		alias		- Optional compiled pattern for my station aliases.
 *				  Do NOT attempt to use this if 'has_alias' is false.
 *
 *		to_chan		- Channel number that we are transmitting to.
 *
 *		cfilter_str	- Filter expression string for the from/to channel pair or nil.
 *				  Note that only a subset of the APRS filters are applicable here.
 *
 * Returns:	Packet object for transmission or nil.
 *		The original packet is not modified.  The caller is responsible for freeing it.
 *		We make a copy and return that modified copy!
 *		This is very important because we could digipeat from one channel to many.
 *
 * Description:	The packet will be digipeated if the next unused digipeater
 *		field matches one of the following:
 *
 *			- mycall_rec
 *			- alias list
 *
 *		APRS digipeating drops duplicates within 30 seconds but we don't do that here.
 *
 *------------------------------------------------------------------------------*/

func (d *Connected) match(from_chan int, pp *ax25.Packet, mycall_rec string, mycall_xmit string, has_alias bool, alias *regexp.Regexp, to_chan int, cfilter_str string) *ax25.Packet {
	/*
	 * First check if filtering has been configured.
	 * Note that we have three different config file filter commands:
	 *
	 *	FILTER		- APRS digipeating and IGate client side.
	 *				Originally this was the only one.
	 *				Should we change it to AFILTER to make it clearer?
	 *	CFILTER		- Similar for connected moded digipeater.
	 *	IGFILTER	- APRS-IS (IGate) server side - completely different.
	 *				Confusing with similar name but much different idea.
	 *				Maybe this should be renamed to SUBSCRIBE or something like that.
	 *
	 * Logically this should come later, after an address/alias match.
	 * But here we only have to do it once.
	 */
	if cfilter_str != "" {
		var result, err = d.filter.Filter(from_chan, to_chan, cfilter_str, pp, false)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"from_channel": from_chan,
				"to_channel":   to_chan,
				"filter":       cfilter_str,
			}).Error("cdigipeater: Bad filter")
		}

		if result != 1 {
			return (nil)
		}
	}

	/*
	 * Find the first repeater station which doesn't have "has been repeated" set.
	 *
	 * r = index of the address position in the frame.
	 */
	var r = pp.FirstNotRepeated()

	if r < ax25.Repeater1 {
		return (nil) // Nothing to do.
	}

	var repeater = pp.AddrWithSSID(r)

	/*
	 * First check for explicit use of my call.
	 * Note that receive and transmit channels could have different callsigns.
	 */

	if repeater == mycall_rec {
		var result = pp.Dup()

		/* If using multiple radio channels, they could have different calls. */

		var err = result.SetAddr(r, mycall_xmit)
		if err != nil {
			logrus.WithError(err).Error("Internal error: not digipeating")

			return nil
		}

		result.SetH(r)

		return (result)
	}

	/*
	 * If we have an alias match, substitute MYCALL.
	 */
	if has_alias {
		if alias.MatchString(repeater) {
			var result = pp.Dup()

			var err = result.SetAddr(r, mycall_xmit)
			if err != nil {
				logrus.WithError(err).Error("Internal error: not digipeating")

				return nil
			}

			result.SetH(r)

			return (result)
		}
	}

	/*
	 * Don't repeat it if we get here.
	 */
	return (nil)
}
