package direwolf

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
)

/*
 * Information required for Connected mode digipeating.
 *
 * The configuration file reader fills in this information
 * and it is passed to NewConnectedDigipeater at application start up time.
 */

type cdigi_config_s struct {

	/*
	 * Rules for each of the [from_chan][to_chan] combinations.
	 */

	// For APRS digipeater, we use MAX_TOTAL_CHANS because we use external TNCs.
	// Connected mode packet must use internal modems we we use MAX_RADIO_CHANS.

	enabled [MAX_RADIO_CHANS][MAX_RADIO_CHANS]bool // Is it enabled for from/to pair?

	has_alias [MAX_RADIO_CHANS][MAX_RADIO_CHANS]bool // If there was no alias in the config file,
	// the structure below will not be set up
	// properly and an attempt to use it could
	// result in a crash.  (fixed v1.5)
	// Not needed for [APRS] DIGIPEAT because
	// the alias is mandatory there.
	alias [MAX_RADIO_CHANS][MAX_RADIO_CHANS]*regexp.Regexp

	cfilter_str [MAX_RADIO_CHANS][MAX_RADIO_CHANS]string
	// NULL or optional Packet Filter strings such as "t/m".
}

// ConnectedDigipeater is the connected mode digipeater: the configuration it
// was started with, and a count of packets digipeated for each combination of
// from/to channel.
type ConnectedDigipeater struct {
	audioConfig *audio_s
	config      *cdigi_config_s
	count       [MAX_RADIO_CHANS][MAX_RADIO_CHANS]int
}

/*------------------------------------------------------------------------------
 *
 * Name:	NewConnectedDigipeater
 *
 * Purpose:	Initialize with stuff from configuration file.
 *
 * Inputs:	p_audio_config	- Configuration for audio channels.
 *
 *		p_cdigi_config	- Connected Digipeater configuration details.
 *
 * Description:	Called once at application startup time.
 *
 *------------------------------------------------------------------------------*/

func NewConnectedDigipeater(p_audio_config *audio_s, p_cdigi_config *cdigi_config_s) *ConnectedDigipeater {
	var d = new(ConnectedDigipeater)
	d.audioConfig = p_audio_config
	d.config = p_cdigi_config

	return d
}

func (d *ConnectedDigipeater) GetCount(from_chan, to_chan int) int {
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

func (d *ConnectedDigipeater) Digipeat(from_chan int, pp *packet_t) {
	// Connected mode is allowed only for channels with internal modem.
	// It probably wouldn't matter for digipeating but let's keep that rule simple and consistent.
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS ||
		(d.audioConfig.chan_medium[from_chan] != MEDIUM_RADIO &&
			d.audioConfig.chan_medium[from_chan] != MEDIUM_NETTNC) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("cdigipeater: Did not expect to receive on invalid channel %d.\n", from_chan)

		return
	}

	/*
	 * First pass:  Look at packets being digipeated to same channel.
	 *
	 * There was a reason for two passes for APRS.
	 * Might not have a benefit here.
	 */

	for to_chan := range MAX_RADIO_CHANS {
		if d.config.enabled[from_chan][to_chan] && to_chan == from_chan {
			d.digipeatTo(from_chan, to_chan, pp)
		}
	}

	/*
	 * Second pass:  Look at packets being digipeated to different channel.
	 */

	for to_chan := range MAX_RADIO_CHANS {
		if d.config.enabled[from_chan][to_chan] && to_chan != from_chan {
			d.digipeatTo(from_chan, to_chan, pp)
		}
	}
} /* end Digipeat */

// digipeatTo queues pp for transmission on to_chan if the from/to channel
// pair's rules say it should be repeated.
func (d *ConnectedDigipeater) digipeatTo(from_chan int, to_chan int, pp *packet_t) {
	var result = cdigipeat_match(from_chan, pp, d.audioConfig.mycall[from_chan],
		d.audioConfig.mycall[to_chan],
		d.config.has_alias[from_chan][to_chan],
		d.config.alias[from_chan][to_chan], to_chan,
		d.config.cfilter_str[from_chan][to_chan])
	if result != nil {
		transmitQueue.Append(to_chan, TQ_PRIO_0_HI, result)
		d.count[from_chan][to_chan]++
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	cdigipeat_match
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

func cdigipeat_match(from_chan int, pp *packet_t, mycall_rec string, mycall_xmit string, has_alias bool, alias *regexp.Regexp, to_chan int, cfilter_str string) *packet_t {
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
		var result, err = pfilter(from_chan, to_chan, cfilter_str, pp, false)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%s\n", err)
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
	var r = ax25_get_first_not_repeated(pp)

	if r < AX25_REPEATER_1 {
		return (nil) // Nothing to do.
	}

	var repeater = ax25_get_addr_with_ssid(pp, r)

	/*
	 * First check for explicit use of my call.
	 * Note that receive and transmit channels could have different callsigns.
	 */

	if repeater == mycall_rec {
		var result = ax25_dup(pp)

		/* If using multiple radio channels, they could have different calls. */

		ax25_set_addr(result, r, mycall_xmit)
		ax25_set_h(result, r)

		return (result)
	}

	/*
	 * If we have an alias match, substitute MYCALL.
	 */
	if has_alias {
		if alias.MatchString(repeater) {
			var result = ax25_dup(pp)

			ax25_set_addr(result, r, mycall_xmit)
			ax25_set_h(result, r)

			return (result)
		}
	}

	/*
	 * Don't repeat it if we get here.
	 */
	return (nil)
}
