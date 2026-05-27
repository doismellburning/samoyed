package direwolf

import (
	"regexp"
	"strconv"
	"strings"
)

// handleDIGIPEAT handles the DIGIPEAT keyword.
func handleDIGIPEAT(ps *parseState) bool {
	/*
	 * ==================== APRS Digipeater parameters ====================
	 */

	/*
	 * DIGIPEAT  from-chan  to-chan  alias-pattern  wide-pattern  [ OFF|DROP|MARK|TRACE | ATGP=alias ]
	 *
	 * ATGP is an ugly hack for the specific need of ATGP which needs more that 8 digipeaters.
	 * DO NOT put this in the User Guide.  On a need to know basis.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_TOTAL_CHANS-1, ps.line)

		return true
	}

	// Channels specified must be radio channels or network TNCs.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO &&
		ps.audio.chan_medium[from_chan] != MEDIUM_NETTNC {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for TO-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_TOTAL_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_TOTAL_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO &&
		ps.audio.chan_medium[to_chan] != MEDIUM_NETTNC {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing alias pattern on line %d.\n", ps.line)

		return true
	}

	var r, err = regexp.Compile(t)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Invalid alias matching pattern on line %d:\n%s\n", ps.line, err)

		return true
	}

	ps.digi.alias[from_chan][to_chan] = r

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing wide pattern on line %d.\n", ps.line)

		return true
	}

	r, err = regexp.Compile(t)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Invalid wide matching pattern on line %d:\n%s\n", ps.line, err)

		return true
	}

	ps.digi.wide[from_chan][to_chan] = r

	ps.digi.enabled[from_chan][to_chan] = true
	ps.digi.preempt[from_chan][to_chan] = PREEMPT_OFF

	t = split("", false)
	if t != "" {
		if strings.EqualFold(t, "OFF") {
			ps.digi.preempt[from_chan][to_chan] = PREEMPT_OFF
			t = split("", false)
		} else if strings.EqualFold(t, "DROP") {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Preemptive digipeating DROP option is discouraged.\n", ps.line)
			dw_printf("It can create a via path which is misleading about the actual path taken.\n")
			dw_printf("PREEMPT is the best choice for this feature.\n")

			ps.digi.preempt[from_chan][to_chan] = PREEMPT_DROP
			t = split("", false)
		} else if strings.EqualFold(t, "MARK") {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Preemptive digipeating MARK option is discouraged.\n", ps.line)
			dw_printf("It can create a via path which is misleading about the actual path taken.\n")
			dw_printf("PREEMPT is the best choice for this feature.\n")

			ps.digi.preempt[from_chan][to_chan] = PREEMPT_MARK
			t = split("", false)
		} else if (strings.EqualFold(t, "TRACE")) || (strings.HasPrefix(strings.ToUpper(t), "PREEMPT")) {
			ps.digi.preempt[from_chan][to_chan] = PREEMPT_TRACE
			t = split("", false)
		} else if strings.HasPrefix(strings.ToUpper(t), "ATGP=") {
			ps.digi.atgp[from_chan][to_chan] = t[5:]
			t = split("", false)
		}
	}

	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Found \"%s\" where end of line was expected.\n", ps.line, t)
	}
	return false
}

// handleDEDUPE handles the DEDUPE keyword.
func handleDEDUPE(ps *parseState) bool {
	/*
	 * DEDUPE 		- Time to suppress digipeating of duplicate APRS packets.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing time for DEDUPE command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < 600 {
		ps.digi.dedupe_time = n
	} else {
		ps.digi.dedupe_time = DEFAULT_DEDUPE

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable value for dedupe time. Using %d.\n",
			ps.line, ps.digi.dedupe_time)
	}
	return false
}

// handleREGEN handles the REGEN keyword.
func handleREGEN(ps *parseState) bool {
	/*
	 * REGEN 		- Signal regeneration.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	// Only radio channels are valid for regenerate.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for TO-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)

		return true
	}

	ps.digi.regen[from_chan][to_chan] = true
	return false
}

// handleCDIGIPEAT handles the CDIGIPEAT keyword.
func handleCDIGIPEAT(ps *parseState) bool {
	/*
	 * ==================== Connected Digipeater parameters ====================
	 */

	/*
	 * CDIGIPEAT  from-chan  to-chan [ alias-pattern ]
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	// For connected mode Link layer, only internal modems should be allowed.
	// A network TNC probably would not provide information about channel status.
	// There is discussion about this in the document called
	// Why-is-9600-only-twice-as-fast-as-1200.pdf

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)
		dw_printf("Only internal modems can be used for connected mode packet.\n")

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for TO-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)
		dw_printf("Only internal modems can be used for connected mode packet.\n")

		return true
	}

	t = split("", false)
	if t != "" {
		var r, err = regexp.Compile(t)
		if err == nil {
			ps.cdigi.alias[from_chan][to_chan] = r
			ps.cdigi.has_alias[from_chan][to_chan] = true
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Invalid alias matching pattern on line %d:\n%s\n", ps.line, err)

			return true
		}

		t = split("", false)
	}

	ps.cdigi.enabled[from_chan][to_chan] = true

	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Found \"%s\" where end of line was expected.\n", ps.line, t)
	}
	return false
}

// handleFILTER handles the FILTER keyword.
func handleFILTER(ps *parseState) bool {
	/*
	 * ==================== Packet Filtering for APRS digipeater or IGate ====================
	 */

	/*
	 * FILTER  from-chan  to-chan  filter_specification_expression
	 * FILTER  from-chan  IG       filter_specification_expression
	 * FILTER  IG         to-chan  filter_specification_expression
	 *
	 *
	 * Note that we have three different config file filter commands:
	 *
	 *	FILTER		- Originally for APRS digipeating but later enhanced
	 *			  to include IGate client side.  Maybe it should be
	 *			  renamed AFILTER to make it clearer after adding CFILTER.
	 *
	 *			  Both internal modem and NET TNC channels allowed here.
	 *			  "IG" should be used for the IGate, NOT a virtual channel
	 *			  assigned to it.
	 *
	 *	CFILTER		- Similar for connected moded digipeater.
	 *
	 *			  Only internal modems can be used because they provide
	 *			  information about radio channel status.
	 *			  A remote network TNC might not provide the necessary
	 *			  status for correct operation.
	 *			  There is discussion about this in the document called
	 *			  Why-is-9600-only-twice-as-fast-as-1200.pdf
	 *
	 *	IGFILTER	- APRS-IS (IGate) server side - completely different.
	 *			  I'm not happy with this name because IG sounds like IGate
	 *			  which is really the client side.  More comments later.
	 *			  Maybe it should be called subscribe or something like that
	 *			  because the subscriptions are cumulative.
	 */
	var from_chan int
	var to_chan int

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if t[0] == 'i' || t[0] == 'I' {
		from_chan = MAX_TOTAL_CHANS

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FILTER IG ... on line %d.\n", ps.line)
		dw_printf("Warning! Don't mess with IS>RF filtering unless you are an expert and have an unusual situation.\n")
		dw_printf("Warning! The default is fine for nearly all situations.\n")
		dw_printf("Warning! Be sure to read carefully and understand  \"Successful-APRS-Gateway-Operation.pdf\" .\n")
		dw_printf("Warning! If you insist, be sure to add \" | i/180 \" so you don't break messaging.\n")
	} else {
		var fromChanErr error

		from_chan, fromChanErr = strconv.Atoi(t)
		if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS || fromChanErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Filter FROM-channel must be in range of 0 to %d or \"IG\" on line %d.\n",
				MAX_TOTAL_CHANS-1, ps.line)

			return true
		}

		if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO &&
			ps.audio.chan_medium[from_chan] != MEDIUM_NETTNC {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
				ps.line, from_chan)

			return true
		}

		if ps.audio.chan_medium[from_chan] == MEDIUM_IGATE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Use 'IG' rather than %d for FROM-channel.\n",
				ps.line, from_chan)

			return true
		}
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if t[0] == 'i' || t[0] == 'I' {
		to_chan = MAX_TOTAL_CHANS

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FILTER ... IG ... on line %d.\n", ps.line)
		dw_printf("Warning! Don't mess with RF>IS filtering unless you are an expert and have an unusual situation.\n")
		dw_printf("Warning! Expected behavior is for everything to go from RF to IS.\n")
		dw_printf("Warning! The default is fine for nearly all situations.\n")
		dw_printf("Warning! Be sure to read carefully and understand  \"Successful-APRS-Gateway-Operation.pdf\" .\n")
	} else {
		var toChanErr error

		to_chan, toChanErr = strconv.Atoi(t)
		if to_chan < 0 || to_chan >= MAX_TOTAL_CHANS || toChanErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Filter TO-channel must be in range of 0 to %d or \"IG\" on line %d.\n",
				MAX_TOTAL_CHANS-1, ps.line)

			return true
		}

		if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO &&
			ps.audio.chan_medium[to_chan] != MEDIUM_NETTNC {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
				ps.line, to_chan)

			return true
		}

		if ps.audio.chan_medium[to_chan] == MEDIUM_IGATE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Use 'IG' rather than %d for TO-channel.\n",
				ps.line, to_chan)

			return true
		}
	}

	t = split("", true) /* Take rest of ps.line including spaces. */

	if t == "" {
		t = " " /* Empty means permit nothing. */
	}

	if ps.digi.filter_str[from_chan][to_chan] != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Replacing previous filter for same from/to pair:\n        %s\n", ps.line, ps.digi.filter_str[from_chan][to_chan])
		ps.digi.filter_str[from_chan][to_chan] = ""
	}

	ps.digi.filter_str[from_chan][to_chan] = t

	// TODO:  Do a test run to see errors now instead of waiting.
	return false
}

// handleCFILTER handles the CFILTER keyword.
func handleCFILTER(ps *parseState) bool {
	/*
	 * ==================== Packet Filtering for connected digipeater ====================
	 */

	/*
	 * CFILTER  from-chan  to-chan  filter_specification_expression
	 *
	 * Why did I put this here?
	 * What would be a useful use case?  Perhaps block by source or destination?
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	var from_chan, fromChanErr = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS || fromChanErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Filter FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	// DO NOT allow a network TNC here.
	// Must be internal modem to have necessary knowledge about channel status.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	var to_chan, toChanErr = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS || toChanErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Filter TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)

		return true
	}

	t = split("", true) /* Take rest of ps.line including spaces. */

	if t == "" {
		t = " " /* Empty means permit nothing. */
	}

	ps.cdigi.cfilter_str[from_chan][to_chan] = t

	// TODO1.2:  Do a test run to see errors now instead of waiting.
	return false
}
