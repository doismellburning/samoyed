//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Packet filtering based on characteristics.
 *
 * Description:	Sometimes it is desirable to digipeat or drop packets based on rules.
 *		For example, you might want to pass only weather information thru
 *		a cross band digipeater or you might want to drop all packets from
 *		an abusive user that is overloading the channel.
 *
 *		The filter specifications are loosely modeled after the IGate Server-side Filter
 *		Commands:   http://www.aprs-is.net/javaprsfilter.aspx
 *
 *		We add AND, OR, NOT, and ( ) to allow very flexible control.
 *
 *---------------------------------------------------------------*/

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

/*
 * Global stuff (to this file)
 *
 * These are set by init function.
 */

// TODO KG var save_igate_config_p *igate_config_s
var pfilter_debug = 0
var pftest_running = false

/*-------------------------------------------------------------------
 *
 * Name:        pfilter_init
 *
 * Purpose:     One time initialization when main application starts up.
 *
 * Inputs:	p_igate_config	- IGate configuration.
 *
 *		debug_level	- 0	no debug output.
 *				  1	single summary line with final result. Indent by 1.
 *				  2	details from each filter specification.  Indent by 3.
 *				  3	Logical operators.  Indent by 2.
 *
 *--------------------------------------------------------------------*/

func pfilter_init(p_igate_config *igate_config_s, debug_level int) {
	pfilter_debug = debug_level
	save_igate_config_p = p_igate_config
}

type token_type_t int

const (
	TOKEN_AND token_type_t = iota
	TOKEN_OR
	TOKEN_NOT
	TOKEN_LPAREN
	TOKEN_RPAREN
	TOKEN_FILTER_SPEC
	TOKEN_EOL
)

const MAX_FILTER_LEN = 1024
const MAX_TOKEN_LEN = 1024

type pfstate_t struct {
	from_chan int /* From and to channels.   MAX_TOTAL_CHANS is used for IGate. */
	to_chan   int /* Used only for debug and error messages. */

	/*
	 * Original filter string from config file.
	 * All control characters should be replaced by spaces.
	 */
	filter_str string
	toBeParsed string // Remaining filter to be parsed

	/*
	 * Packet object.
	 */
	pp *packet_t

	/*
	 * Are we processing APRS or connected mode?
	 * This determines which types of filters are available.
	 */
	is_aprs bool

	/*
	 * Packet split into separate parts if APRS.
	 * Most interesting fields are:
	 *
	 *		g_symbol_table	- / \ or overlay
	 *		g_symbol_code
	 *		g_lat, g_lon	- Location
	 *		g_name		- for object or item
	 *		g_comment
	 */
	decoded *decode_aprs_t

	/*
	 * These are set by next_token.
	 */
	token_type token_type_t
	token_str  string /* Printable string representation for use in error messages. */ // TODO KG It's not just used for error messages!
	tokeni     int    /* Index in original string for enhanced error messages. */
}

// TODO KG Rename!
func bool2text(val int) string {
	switch val {
	case 1:
		return "TRUE"
	case 0:
		return "FALSE"
	case -1:
		return "ERROR"
	default:
		return "OOPS!"
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        pfilter.c
 *
 * Purpose:     Decide whether a packet should be allowed thru.
 *
 * Inputs:	from_chan - Channel packet is coming from.
 *		to_chan	  - Channel packet is going to.
 *				Both are 0 .. MAX_TOTAL_CHANS-1 or MAX_TOTAL_CHANS for IGate.
 *			 	For debug/error messages only.
 *
 *		filter	- String of filter specs and logical operators to combine them.
 *
 *		pp	- Packet object handle.
 *
 *		is_aprs	- True for APRS, false for connected mode digipeater.
 *			  Connected mode allows a subset of the filter types, only
 *			  looking at the addresses, not information part contents.
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 * Description:	This might be running in multiple threads at the same time so
 *		no static data allowed and take other thread-safe precautions.
 *
 *--------------------------------------------------------------------*/

func pfilter(from_chan int, to_chan int, filter string, pp *packet_t, is_aprs bool) (int, error) {
	Assert(from_chan >= 0 && from_chan <= MAX_TOTAL_CHANS)
	Assert(to_chan >= 0 && to_chan <= MAX_TOTAL_CHANS)

	if pp == nil {
		return -1, fmt.Errorf("INTERNAL ERROR in pfilter: nil packet pointer, please report this")
	}

	var pfstate pfstate_t

	pfstate.from_chan = from_chan
	pfstate.to_chan = to_chan

	/* Copy filter string, changing any control characters to spaces. */

	pfstate.filter_str = filter
	pfstate.filter_str = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		} else {
			return r
		}
	}, pfstate.filter_str)

	pfstate.toBeParsed = pfstate.filter_str

	pfstate.pp = pp
	pfstate.is_aprs = is_aprs

	if is_aprs {
		pfstate.decoded = decode_aprs(pp, true, "")
	}

	next_token(&pfstate)

	var result int
	var err error
	if pfstate.token_type == TOKEN_EOL {
		/* Empty filter means reject all. */
		result = 0
	} else {
		result, err = parse_expr(&pfstate)

		if err == nil &&
			pfstate.token_type != TOKEN_AND &&
			pfstate.token_type != TOKEN_OR &&
			pfstate.token_type != TOKEN_EOL {
			err = newFilterError(&pfstate, "Expected logical operator or end of line here.")

			result = -1
		}
	}

	if pfilter_debug >= 1 {
		text_color_set(DW_COLOR_DEBUG)

		if from_chan == MAX_TOTAL_CHANS {
			dw_printf(" Packet filter from IGate to radio channel %d returns %s\n", to_chan, bool2text(result))
		} else if to_chan == MAX_TOTAL_CHANS {
			dw_printf(" Packet filter from radio channel %d to IGate returns %s\n", from_chan, bool2text(result))
		} else if is_aprs {
			dw_printf(" Packet filter for APRS digipeater from radio channel %d to %d returns %s\n", from_chan, to_chan, bool2text(result))
		} else {
			dw_printf(" Packet filter for traditional digipeater from radio channel %d to %d returns %s\n", from_chan, to_chan, bool2text(result))
		}
	}

	return result, err
} /* end pfilter */

/*-------------------------------------------------------------------
 *
 * Name:   	next_token
 *
 * Purpose:     Extract the next token from input string.
 *
 * Inputs:	pf	- Pointer to current state information.
 *
 * Outputs:	See definition of the structure.
 *
 * Description:	Look for these special operators:   & | ! ( ) end-of-line
 *		Anything else is considered a filter specification.
 *		Note that a filter-spec must be followed by space or
 *		end of line.  This is so the magic characters can appear in one.
 *
 * Future:	Maybe allow words like 'OR' as alternatives to symbols like '|'.
 *
 * Unresolved Issue:
 *
 *		Adding the special operators adds a new complication.
 *		How do we handle the case where we want those characters in
 *		a filter specification?   For example how do we know if the
 *		last character of /#& means HF gateway or AND the next part
 *		of the expression.
 *
 *		Approach 1:  Require white space after all filter specifications.
 *			     Currently implemented.
 *			     Simple. Easy to explain.
 *			     More readable than having everything squashed together.
 *
 *		Approach 2:  Use escape character to get literal value.  e.g.  s/#\&
 *			     Linux people would be comfortable with this but
 *			     others might have a problem with it.
 *
 *		Approach 3:  use quotation marks if it contains special characters or space.
 *			     "s/#&"  Simple.  Allows embedded space but I'm not sure
 *		 	     that's useful.  Doesn't hurt to always put the quotes there
 *			     if you can't remember which characters are special.
 *
 *--------------------------------------------------------------------*/

func next_token(pf *pfstate_t) {
	pf.toBeParsed = strings.TrimLeft(pf.toBeParsed, " ")

	pf.tokeni = len(pf.filter_str) - len(pf.toBeParsed)

	if len(pf.toBeParsed) == 0 {
		pf.token_type = TOKEN_EOL
		pf.token_str = "end-of-line"

		return
	}

	switch pf.toBeParsed[0] {
	case '&':
		pf.toBeParsed = pf.toBeParsed[1:]
		pf.token_type = TOKEN_AND
		pf.token_str = "\"&\""
	case '|':
		pf.toBeParsed = pf.toBeParsed[1:]
		pf.token_type = TOKEN_OR
		pf.token_str = "\"|\""
	case '!':
		pf.toBeParsed = pf.toBeParsed[1:]
		pf.token_type = TOKEN_NOT
		pf.token_str = "\"!\""
	case '(':
		pf.toBeParsed = pf.toBeParsed[1:]
		pf.token_type = TOKEN_LPAREN
		pf.token_str = "\"(\""
	case ')':
		pf.toBeParsed = pf.toBeParsed[1:]
		pf.token_type = TOKEN_RPAREN
		pf.token_str = "\")\""
	default:
		// At this point, toBeParsed isn't empty and doesn't start with a space
		var s strings.Builder
		for {
			s.WriteRune(rune(pf.toBeParsed[0]))
			pf.toBeParsed = pf.toBeParsed[1:]

			if len(pf.toBeParsed) == 0 || pf.toBeParsed[0] == ' ' {
				break
			}
		}

		pf.token_type = TOKEN_FILTER_SPEC
		pf.token_str = s.String()
	}
} /* end next_token */

/*-------------------------------------------------------------------
 *
 * Name:   	parse_expr
 *		parse_or_expr
 *		parse_and_expr
 *		parse_primary
 *
 * Purpose:     Recursive descent parser to evaluate filter specifications
 *		contained within expressions with & | ! ( ).
 *
 * Inputs:	pf	- Pointer to current state information.
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 *--------------------------------------------------------------------*/

func parse_expr(pf *pfstate_t) (int, error) {
	return parse_or_expr(pf)
}

/* or_expr::	and_expr [ | and_expr ] ... */

func parse_or_expr(pf *pfstate_t) (int, error) {
	var result, err = parse_and_expr(pf)
	if err != nil {
		return -1, err
	}

	for pf.token_type == TOKEN_OR {
		next_token(pf)
		var e, eerr = parse_and_expr(pf)

		if pfilter_debug >= 3 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("  %s | %s\n", bool2text(result), bool2text(e))
		}

		if eerr != nil {
			return -1, eerr
		}

		result |= e
	}

	return result, nil
}

/* and_expr::	primary [ & primary ] ... */

func parse_and_expr(pf *pfstate_t) (int, error) {
	var result, err = parse_primary(pf)
	if err != nil {
		return -1, err
	}

	for pf.token_type == TOKEN_AND {
		next_token(pf)
		var e, eerr = parse_primary(pf)

		if pfilter_debug >= 3 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("  %s & %s\n", bool2text(result), bool2text(e))
		}

		if eerr != nil {
			return -1, eerr
		}

		result &= e
	}

	return result, nil
}

/* primary::	( expr )	*/
/* 		! primary	*/
/*		filter_spec	*/

func parse_primary(pf *pfstate_t) (int, error) {
	var result int
	var err error

	if pf.token_type == TOKEN_LPAREN { //nolint:staticcheck
		next_token(pf)
		result, err = parse_expr(pf)

		if pf.token_type == TOKEN_RPAREN {
			next_token(pf)
		} else if err == nil {
			err = newFilterError(pf, "Expected \")\" here.")

			result = -1
		}
	} else if pf.token_type == TOKEN_NOT {
		next_token(pf)
		var e, eerr = parse_primary(pf)

		if pfilter_debug >= 3 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("  ! %s\n", bool2text(e))
		}

		if eerr != nil {
			result = -1
			err = eerr
		} else {
			result = 1 - e
		}
	} else if pf.token_type == TOKEN_FILTER_SPEC {
		result, err = parse_filter_spec(pf)
	} else {
		err = newFilterError(pf, "Expected filter specification, (, or ! here.")

		result = -1
	}

	return result, err
}

/*-------------------------------------------------------------------
 *
 * Name:   	parse_filter_spec
 *
 * Purpose:     Parse and evaluate filter specification.
 *
 * Inputs:	pf	- Pointer to current state information.
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 * Description:	All filter specifications are allowed for APRS.
 *		Only those dealing with addresses are allowed for connected digipeater.
 *
 *		b	- budlist (source)
 *		d	- digipeaters used
 *		v	- digipeaters not used
 *		u	- unproto (destination)
 *
 *--------------------------------------------------------------------*/

func parse_filter_spec(pf *pfstate_t) (int, error) {
	// Yes this is always assigned over, but that requires a fair bit of reading to be sure of, so let's have an explicit default
	var result = -1 //nolint:ineffassign,wastedassign
	var err error

	if (!pf.is_aprs) && !strings.ContainsRune("01bdvu", rune(pf.token_str[0])) {
		err = newFilterError(pf, "Only b, d, v, and u specifications are allowed for connected mode digipeater filtering.")

		result = -1

		next_token(pf)

		return result, err
	}

	/* undocumented: can use 0 or 1 for testing. */

	if pf.token_str == "0" {
		result = 0
	} else if pf.token_str == "1" {
		result = 1
	} else if pf.token_str[0] == 'b' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* simple string matching */

		/* b - budlist */
		/* Budlist - AX.25 source address */
		/* Could be different than source encapsulated by 3rd party header. */
		var addr = ax25_get_addr_with_ssid(pf.pp, AX25_SOURCE)
		result, err = filt_bodgu(pf, addr)

		if pfilter_debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), addr)
		}
	} else if pf.token_str[0] == 'o' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* o - object or item name */
		result, err = filt_bodgu(pf, pf.decoded.g_name)

		if pfilter_debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), pf.decoded.g_name)
		}
	} else if pf.token_str[0] == 'd' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* d - was digipeated by */
		// Loop on all AX.25 digipeaters.
		result = 0
		for n := AX25_REPEATER_1; result == 0 && err == nil && n < ax25_get_num_addr(pf.pp); n++ {
			// Consider only those with the H (has-been-used) bit set.
			if ax25_get_h(pf.pp, n) > 0 {
				var addr = ax25_get_addr_with_ssid(pf.pp, n)
				result, err = filt_bodgu(pf, addr)
			}
		}

		if pfilter_debug >= 2 {
			var path = ax25_format_via_path(pf.pp)

			if len(path) == 0 {
				path = "no digipeater path"
			}

			text_color_set(DW_COLOR_DEBUG)
			dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), path)
		}
	} else if pf.token_str[0] == 'v' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* v - via not used */
		// loop on all AX.25 digipeaters (mnemonic Via)
		result = 0
		for n := AX25_REPEATER_1; result == 0 && err == nil && n < ax25_get_num_addr(pf.pp); n++ {
			// This is different than the previous "d" filter.
			// Consider only those where the the H (has-been-used) bit is NOT set.
			if ax25_get_h(pf.pp, n) == 0 {
				var addr = ax25_get_addr_with_ssid(pf.pp, n)
				result, err = filt_bodgu(pf, addr)
			}
		}

		if pfilter_debug >= 2 {
			var path = ax25_format_via_path(pf.pp)

			if len(path) == 0 {
				path = "no digipeater path"
			}

			text_color_set(DW_COLOR_DEBUG)
			dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), path)
		}
	} else if pf.token_str[0] == 'g' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* g - Addressee of message. e.g. "BLN*" for bulletins. */
		if pf.decoded.g_message_subtype == message_subtype_message ||
			pf.decoded.g_message_subtype == message_subtype_ack ||
			pf.decoded.g_message_subtype == message_subtype_rej ||
			pf.decoded.g_message_subtype == message_subtype_bulletin ||
			pf.decoded.g_message_subtype == message_subtype_nws ||
			pf.decoded.g_message_subtype == message_subtype_directed_query {
			result, err = filt_bodgu(pf, pf.decoded.g_addressee)

			if pfilter_debug >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), pf.decoded.g_addressee)
			}
		} else {
			result = 0

			if pfilter_debug >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), "not a message")
			}
		}
	} else if pf.token_str[0] == 'u' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* u - unproto (AX.25 destination) */
		/* Probably want to exclude mic-e types */
		/* because destination is used for part of location. */
		if ax25_get_dti(pf.pp) != '\'' && ax25_get_dti(pf.pp) != '`' {
			var addr = ax25_get_addr_with_ssid(pf.pp, AX25_DESTINATION)
			result, err = filt_bodgu(pf, addr)

			if pfilter_debug >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), addr)
			}
		} else {
			result = 0

			if pfilter_debug >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), "MIC-E packet type")
			}
		}
	} else if pf.token_str[0] == 't' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* t - packet type: position, weather, telemetry, etc. */
		result, err = filt_t(pf)

		if pfilter_debug >= 2 {
			var infop = AX25GetInfo(pf.pp)

			if len(infop) > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("   %s returns %s for %c data type indicator\n", pf.token_str, bool2text(result), infop[0])
			} else {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("   %s returns %s for empty info part\n", pf.token_str, bool2text(result))
			}
		}
	} else if pf.token_str[0] == 'r' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* r - range */
		/* range */
		var sdist string
		result, sdist, err = filt_r(pf)

		if pfilter_debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("   %s returns %s for %s\n", pf.token_str, bool2text(result), sdist)
		}
	} else if pf.token_str[0] == 's' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* s - symbol */
		result, err = filt_s(pf)

		if pfilter_debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)

			if pf.decoded.g_symbol_table == '/' { //nolint:staticcheck
				dw_printf("   %s returns %s for symbol %c in primary table\n", pf.token_str, bool2text(result), pf.decoded.g_symbol_code)
			} else if pf.decoded.g_symbol_table == '\\' {
				dw_printf("   %s returns %s for symbol %c in alternate table\n", pf.token_str, bool2text(result), pf.decoded.g_symbol_code)
			} else {
				dw_printf("   %s returns %s for symbol %c with overlay %c\n", pf.token_str, bool2text(result), pf.decoded.g_symbol_code, pf.decoded.g_symbol_table)
			}
		}
	} else if pf.token_str[0] == 'i' && unicode.IsPunct(rune(pf.token_str[1])) {
		/* i - IGate messaging default */
		/* IGatge messaging */
		result, err = filt_i(pf)

		if pfilter_debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)

			if pf.decoded.g_packet_type == packet_type_message {
				dw_printf("   %s returns %s for message to %s\n", pf.token_str, bool2text(result), pf.decoded.g_addressee)
			} else {
				dw_printf("   %s returns %s for not an APRS 'message'\n", pf.token_str, bool2text(result))
			}
		}
	} else {
		/* unrecognized filter type */
		err = newFilterError(pf, fmt.Sprintf("Unrecognized filter type '%c'", pf.token_str[0]))

		result = -1
	}

	next_token(pf)

	return result, err
}

/*------------------------------------------------------------------------------
 *
 * Name:	filt_bodgu
 *
 * Purpose:	Filter with text pattern matching
 *
 * Inputs:	pf	- Pointer to current state information.
 *			  token_str should have one of these filter specs:
 *
 * 				Budlist		b/call1/call2...
 * 				Object		o/obj1/obj2...
 * 				Digipeater	d/digi1/digi2...
 * 				Group Msg	g/call1/call2...
 * 				Unproto		u/unproto1/unproto2...
 *				Via-not-yet	v/digi1/digi2...noteapd
 *
 *		arg	- Value to match from source addr, destination,
 *			  used digipeater, object name, etc.
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 * Description:	Same function is used for all of these because they are so similar.
 *		Look for exact match to any of the specified strings.
 *		All of them allow wildcarding with single * at the end.
 *
 *------------------------------------------------------------------------------*/

func filt_bodgu(pf *pfstate_t, arg string) (int, error) {
	var result = 0
	var str = pf.token_str
	var sep = str[1]
	var cp = str[2:]

	var parts = strings.Split(cp, string(sep))
	for _, part := range parts {
		var idx = strings.Index(part, "*")
		if idx != -1 {
			/* Wildcarding.  Should have single * on end. */
			if idx != (len(part) - 1) {
				return -1, newFilterError(pf, "Any wildcard * must be at the end of pattern.")
			}

			if strings.HasPrefix(arg, part[:idx]) {
				result = 1
			}
		} else {
			/* Try for exact match. */
			if part == arg {
				result = 1
			}
		}
	}

	return result, nil
}

/*------------------------------------------------------------------------------
 *
 * Name:	filt_t
 *
 * Purpose:	Filter by packet type.
 *
 * Inputs:	pf	- Pointer to current state information.
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 * Description:	The filter is loosely based the type filtering described here:
 *		http://www.aprs-is.net/javAPRSFilter.aspx
 *
 *		Mostly use g_packet_type and g_message_subtype from decode_aprs.
 *
 * References:
 *		http://www.aprs-is.net/WX/
 *		http://wxsvr.aprs.net.au/protocol-new.html	(has disappeared)
 *
 *------------------------------------------------------------------------------*/

func filt_t(pf *pfstate_t) (int, error) {
	// TODO KG Why was this here? var src = ax25_get_addr_with_ssid(pf.pp, AX25_SOURCE)
	var infop = AX25GetInfo(pf.pp)

	Assert(len(infop) > 0)

	for _, f := range pf.token_str[2:] {
		switch f {
		case 'p': /* Position */
			if pf.decoded.g_packet_type == packet_type_position {
				return 1, nil
			}

		case 'o': /* Object */
			if pf.decoded.g_packet_type == packet_type_object {
				return 1, nil
			}

		case 'i': /* Item */
			if pf.decoded.g_packet_type == packet_type_item {
				return 1, nil
			}

		case 'm': // Any "message."
			if pf.decoded.g_packet_type == packet_type_message {
				return 1, nil
			}

		case 'q': /* Query */
			if pf.decoded.g_packet_type == packet_type_query {
				return 1, nil
			}

		case 'c': /* station Capabilities - my extension */
			/* Most often used for IGate statistics. */
			if pf.decoded.g_packet_type == packet_type_capabilities {
				return 1, nil
			}

		case 's': /* Status */
			if pf.decoded.g_packet_type == packet_type_status {
				return 1, nil
			}

		case 't': /* Telemetry data or metadata */
			if pf.decoded.g_packet_type == packet_type_telemetry {
				return 1, nil
			}

		case 'u': /* User-defined */
			if pf.decoded.g_packet_type == packet_type_userdefined {
				return 1, nil
			}

		case 'h': /* has third party Header - my extension */
			if pf.decoded.g_has_thirdparty_header {
				return 1, nil
			}

		case 'w': /* Weather */
			if pf.decoded.g_packet_type == packet_type_weather {
				return 1, nil
			}

			/* Positions !=/@  with symbol code _ are weather. */
			/* Object with _ symbol is also weather.  APRS protocol spec page 66. */
			// Can't use *infop because it would not work with 3rd party header.

			if (pf.decoded.g_packet_type == packet_type_position ||
				pf.decoded.g_packet_type == packet_type_object) && pf.decoded.g_symbol_code == '_' {
				return 1, nil
			}

		case 'n': /* NWS format */
			if pf.decoded.g_packet_type == packet_type_nws {
				return 1, nil
			}

		default:
			return -1, newFilterError(pf, "Invalid letter in t/ filter.")
		}
	}

	return 0, nil /* Didn't match anything.  Reject */
} /* end filt_t */

/*------------------------------------------------------------------------------
 *
 * Name:	filt_r
 *
 * Purpose:	Is it in range (kilometers) of given location.
 *
 * Inputs:	pf	- Pointer to current state information.
 *			  token_str should contain something of format:
 *
 *				r/lat/lon/dist
 *
 *			  We also need to know the location (if any) from the packet.
 *
 *				decoded.g_lat & decoded.g_lon
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 *      sdist	- Distance as a string for troubleshooting.
 *
 *
 * Description:
 *
 *------------------------------------------------------------------------------*/

func filt_r(pf *pfstate_t) (int, string, error) {
	if pf.decoded.g_lat == G_UNKNOWN || pf.decoded.g_lon == G_UNKNOWN {
		return 0, "", nil
	}

	var str = pf.token_str
	var sep = string(str[1])
	var cp = str[2:]

	var parts = strings.Split(cp, sep)

	if len(parts) < 1 {
		return -1, "", newFilterError(pf, "Missing latitude for Range filter.")
	}
	var dlat, _ = strconv.ParseFloat(parts[0], 64)

	if len(parts) < 2 {
		return -1, "", newFilterError(pf, "Missing longitude for Range filter.")
	}
	var dlon, _ = strconv.ParseFloat(parts[1], 64)

	if len(parts) < 3 {
		return -1, "", newFilterError(pf, "Missing distance for Range filter.")
	}
	var ddist, _ = strconv.ParseFloat(parts[2], 64)

	if len(parts) > 3 {
		return -1, "", newFilterError(pf, "Too many parts for Range filter.")
	}

	var km = ll_distance_km(dlat, dlon, float64(pf.decoded.g_lat), float64(pf.decoded.g_lon))
	var sdist = fmt.Sprintf("%.2f km", km)

	if km <= ddist {
		return 1, sdist, nil
	}

	return 0, sdist, nil
}

/*------------------------------------------------------------------------------
 *
 * Name:	filt_s
 *
 * Purpose:	Filter by symbol.
 *
 * Inputs:	pf	- Pointer to current state information.
 *			  token_str should contain something of format:
 *
 *				s/pri/alt/over
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 * Description:
 *
 *		s/pri
 *		s/pri/alt
 *		s/pri/alt/
 *		s/pri/alt/over
 *
 *		"pri" is zero or more symbols from the primary symbol set.
 *			Symbol codes are any printable ASCII character other than | or ~.
 *			(Zero symbols here would be sensible only if later alt part is specified.)
 *		"alt" is one or more symbols from the alternate symbol set.
 *		"over" is overlay characters for the alternate symbol set.
 *			Only upper case letters, digits, and \ are allowed here.
 *			If the last part is not specified, any overlay or lack of overlay, is ignored.
 *			If the last part is specified, only the listed overlays will match.
 *			An explicit lack of overlay is represented by the \ character.
 *
 *		Examples:
 *			s/O		Balloon.
 *			s/->		House or car from primary symbol table.
 *
 *			s//#		Alternate table digipeater, with or without overlay.
 *			s//#/\		Alternate table digipeater, only if no overlay.
 *			s//#/SL1	Alternate table digipeater, with overlay S, L, or 1.
 *			s//#/SL\	Alternate table digipeater, with S, L, or no overlay.
 *
 *			s/s/s		Any variation of watercraft.  Either symbol table.  With or without overlay.
 *			s/s/s/		Ship or ship sideview, only if no overlay.
 *			s//s/J		Jet Ski.
 *
 *		What if you want to use the / symbol when / is being used as a delimiter here?  Recall that you
 *		can use some other special character after the initial lower case letter and this becomes the
 *		delimiter for the rest of the specification.
 *
 *		Examples:
 *
 *			s:/		Red Dot.
 *			s::/		Waypoint Destination, with or without overlay.
 *			s:/:/		Either Red Dot or Waypoint Destination.
 *			s:/:/:		Either Red Dot or Waypoint Destination, no overlay.
 *
 *		Bad example:
 *
 *			Someone tried using this to include ballons:   s/'/O/-/#/_
 *			probably following the buddy filter pattern of / between each alternative.
 *			There should be an error message because it has more than 3 delimiter characters.
 *
 *
 *------------------------------------------------------------------------------*/

func filt_s(pf *pfstate_t) (int, error) {
	var str = pf.token_str
	var sep = string(str[1])
	var cp = str[2:]
	var pri, alt, over string

	var unacceptableChar = func(r rune) bool {
		return !unicode.IsPrint(r) || r == '|' || r == '~'
	}

	// First, separate the parts and do a strict syntax check.

	var parts = strings.Split(cp, sep)

	if len(parts) > 0 {
		pri = parts[0]

		// Zero length is acceptable if alternate symbol(s) specified.  Will check that later.

		if strings.ContainsFunc(pri, unacceptableChar) {
			return -1, newFilterError(pf, "Symbol filter, primary must be printable ASCII character(s) other than | or ~.")
		}

		if len(parts) > 1 {
			alt = parts[1]

			// Zero length after second / would be pointless.

			if len(alt) == 0 {
				return -1, newFilterError(pf, "Nothing specified for alternate symbol table.")
			}

			if strings.ContainsFunc(alt, unacceptableChar) {
				return -1, newFilterError(pf, "Symbol filter, alternate must be printable ASCII character(s) other than | or ~.")
			}

			if len(parts) > 2 {
				over = parts[2]

				// Zero length is acceptable and is not the same as missing.

				if strings.ContainsFunc(over, func(r rune) bool {
					return !(unicode.IsUpper(r) || unicode.IsDigit(r) || r == '\\')
				}) {
					return -1, newFilterError(pf, "Symbol filter, overlay must be upper case letter, digit, or \\.")
				}

				if len(parts) > 3 {
					return -1, newFilterError(pf, "More than 3 delimiter characters in Symbol filter.")
				}
			}
		} else {
			// No alt part is OK if at least one primary symbol was specified.
			if len(pri) == 0 {
				return -1, newFilterError(pf, "No symbols specified for Symbol filter.")
			}
		}
	} else {
		return -1, newFilterError(pf, "Missing arguments for Symbol filter.")
	}

	// This applies only for Position, Object, Item.
	// decode_aprs() should set symbol code to space to mean undefined.

	if pf.decoded.g_symbol_code == ' ' {
		return 0, nil
	}

	// Look for Primary symbols.

	if pf.decoded.g_symbol_table == '/' {
		if len(pri) > 0 {
			if strings.Contains(pri, string(rune(pf.decoded.g_symbol_code))) {
				return 1, nil
			} else {
				return 0, nil
			}
		}
	}

	if alt == "" {
		return 0, nil
	}

	//printf ("alt=\"%s\"  sym='%c'\n", alt, pf.decoded.g_symbol_code);

	// Look for Alternate symbols.

	if strings.Contains(alt, string(rune(pf.decoded.g_symbol_code))) {
		// We have a match but that might not be enough.
		// We must see if there was an overlay part specified.
		if len(parts) > 2 {
			if len(over) > 0 {
				// Non-zero length overlay part was specified.
				// Need to match one of them.
				if strings.Contains(over, string(rune(pf.decoded.g_symbol_table))) {
					return 1, nil
				} else {
					return 0, nil
				}
			} else {
				// Zero length overlay part was specified.
				// We must have no overlay, i.e.  table is \.
				if pf.decoded.g_symbol_table == '\\' {
					return 1, nil
				} else {
					return 0, nil
				}
			}
		} else {
			// No check of overlay part.  Just make sure it is not primary table.
			if pf.decoded.g_symbol_table != '/' {
				return 1, nil
			} else {
				return 0, nil
			}
		}
	}

	return 0, nil
} /* end filt_s */

/*------------------------------------------------------------------------------
 *
 * Name:	filt_i
 *
 * Purpose:	IGate messaging filter.
 *		This would make sense only for IS>RF direction.
 *
 * Inputs:	pf	- Pointer to current state information.
 *			  token_str should contain something of format:
 *
 *				i/time/hops/lat/lon/km
 *
 * Returns:	 1 = yes
 *		 0 = no
 *		-1 = error detected
 *
 * Description: Selection is based on time since last heard on RF, and distance
 *		in terms of digipeater hops and/or physical location.
 *
 *		i/time
 *		i/time/hops
 *		i/time/hops/lat/lon/km
 *
 *
 *		"time" is maximum number of minutes since message addressee was last heard.
 *			This is required.  APRS-IS uses 3 hours so that would be a good value here.
 *
 *		"hops" is maximum number of digpeater hops.  (i.e. 0 for heard directly).
 * 			If hops is not specified, the maximum transmit digipeater hop count,
 *			from the IGTXVIA configuration will be used.

 *		The rest is distanced, in kilometers, from given point.
 *
 *		Examples:
 *			i/180/0		Heard in past 3 hours directly.
 *			i/45		Past 45 minutes, default max digi hops.
 *			i/180/3		Default time (3 hours), max 3 digi hops.
 *			i/180/8/42.6/-71.3/50.
 *
 *
 *		It only makes sense to use this for the IS>RF direction.
 *		The basic idea is that we want to transmit a "message" only if the
 *		addressee has been heard recently and is not too far away.
 *
 *		That is so we can distinguish messages addressed to a specific
 *		station, and other sundry uses of the addressee field.
 *
 *		After passing along a "message" we will also allow the next
 *		position report from the sender of the "message."
 *		That is done somewhere else.  We are not concerned with it here.
 *
 *		IMHO, the rules here are too restrictive.
 *
 *		    The APRS-IS would send a "message" to my IGate only if the addressee
 *		    has been heard nearby recently.  180 minutes, I believe.
 *		    Why would I not want to transmit it?
 *
 * Discussion:	In retrospect, I think this is far too complicated.
 *		In a future release, I think at options other than time should be removed.
 *		Messages have more value than most packets.  Why reduce the chance of successful delivery?
 *
 *		Consider the following scenario:
 *
 *		(1) We hear AA1PR-9 by a path of 4 digipeaters.
 *		    Looking closer, it's probably only two because there are left over WIDE1-0 and WIDE2-0.
 *
 *			Digipeater WIDE2 (probably N3LLO-3) audio level = 72(19/15)   [NONE]   _|||||___
 *			[0.3] AA1PR-9>APY300,K1EQX-7,WIDE1,N3LLO-3,WIDE2*,ARISS::ANSRVR   :cq hotg vt aprsthursday{01<0x0d>
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
 *		We could put in a hack to not count the "WIDE*-0"  addresses.
 *		That is not correct because other prefixes could be used and we don't know
 *		what they are for other digipeaters.
 *		I think the best solution is to simply ignore the hop count.
 *
 * Release 1.7:	I got overly ambitious and now realize this is just giving people too much
 *		"rope to hang themselves," drop messages unexpectedly, and accidentally break messaging.
 *		Change documentation to mention only the time limit.
 *		The other functionality will be undocumented and maybe disappear over time.
 *
 *------------------------------------------------------------------------------*/

func filt_i(pf *pfstate_t) (int, error) {
	// http://lists.tapr.org/pipermail/aprssig_lists.tapr.org/2020-July/048656.html
	// Default of 3 hours should be good.
	// One might question why to have a time limit at all.  Messages are very rare
	// the the APRS-IS wouldn't be sending it to me unless the addressee was in the
	// vicinity recently.
	// TODO: Should produce a warning if a user specified filter does not include "i".
	// 3 hours * 60 min/hr = 180 minutes
	// TODO KG: This was unused in the original C, but I think that was accidental given all the context here
	var heardtime = 180                             //nolint:ineffassign,wastedassign
	var maxhops = save_igate_config_p.max_digi_hops // from IGTXVIA config.
	var dlat float64 = G_UNKNOWN
	var dlon float64 = G_UNKNOWN
	var km float64 = G_UNKNOWN

	//char src[AX25_MAX_ADDR_LEN];
	//char *infop = nil;
	//int info_len;
	//char *f;
	//char addressee[AX25_MAX_ADDR_LEN];

	var str = pf.token_str
	var sep = string(str[1])
	var cp = str[2:]

	var parts = strings.Split(cp, sep)

	// Get parameters or defaults.

	if len(parts) > 0 && len(parts[0]) > 0 {
		heardtime, _ = strconv.Atoi(parts[0])
	} else {
		return -1, newFilterError(pf, "Missing time limit for IGate message filter.")
	}

	if len(parts) > 1 {
		if len(parts[1]) > 0 {
			maxhops, _ = strconv.Atoi(parts[1])
		} else {
			return -1, newFilterError(pf, "Missing max digipeater hops for IGate message filter.")
		}

		if len(parts) > 2 && len(parts[2]) > 0 {
			dlat, _ = strconv.ParseFloat(parts[2], 64)

			if len(parts) > 3 && len(parts[3]) > 0 {
				dlon, _ = strconv.ParseFloat(parts[3], 64)
			} else {
				return -1, newFilterError(pf, "Missing longitude for IGate message filter.")
			}

			if len(parts) > 4 && len(parts[4]) > 0 {
				km, _ = strconv.ParseFloat(parts[4], 64)
			} else {
				return -1, newFilterError(pf, "Missing distance, in km, for IGate message filter.")
			}
		}

		if len(parts) > 5 {
			return -1, newFilterError(pf, "Something unexpected after distance for IGate message filter.")
		}
	}

	/*
	 * Get source address and info part.
	 * Addressee has already been extracted into pf.decoded.g_addressee.
	 */
	if pf.decoded.g_packet_type != packet_type_message {
		return 0, nil
	}

	if pftest_running {
		return 1, nil // Replacement for old #ifdef PFTEST
	}

	/* TODO KG Is this still needed? Digipeater tests still seem to pass fine without it...
	#if defined(DIGITEST)	// TODO: test functionality too, not just syntax.

		(void)dlat;	// Suppress set and not used warning.
		(void)dlon;
		(void)km;
		(void)maxhops;
		(void)heardtime;

		return (1);
	#else
	*/

	/*
	 * Condition 1:
	 *	"the receiving station has been heard within range within a predefined time
	 *	 period (range defined as digi hops, distance, or both)."
	 */

	var was_heard = mheardDB.WasRecentlyNearby("addressee", pf.decoded.g_addressee, heardtime, maxhops, dlat, dlon, km)

	if was_heard {
		return 0, nil
	}

	/*
	 * Condition 2:
	 *	"the sending station has not been heard via RF within a predefined time period
	 *	 (packets gated from the Internet by other stations are excluded from this test)."
	 *
	 * This is the part I'm not so sure about.
	 * I guess the intention is that if the sender can be heard over RF, then the addressee
	 * might hear the sender without the help of Igate stations.
	 * Suppose the sender was 1 digipeater hop to the west and the addressee was 1 digipeater hop to the east.
	 * I can communicate with each of them with 1 digipeater hop but for them to reach each other, they
	 * might need 3 hops and using that many is generally frowned upon and rare.
	 *
	 * Maybe we could compromise here and say the sender must have been heard directly.
	 * It sent the message currently being processed so we must have heard it very recently, i.e. in
	 * the past minute, rather than the usual 180 minutes for the addressee.
	 */

	was_heard = mheardDB.WasRecentlyNearby("source", pf.decoded.g_src, 1, 0, G_UNKNOWN, G_UNKNOWN, G_UNKNOWN)

	if was_heard {
		return 0, nil
	}

	return 1, nil

	// #endif
} /* end filt_i */

/*-------------------------------------------------------------------
 *
 * Name:   	print_error
 *
 * Purpose:     Build an error with context so someone can figure out what caused it.
 *
 * Inputs:	pf	- Pointer to current state information.
 *
 *		msg	- Specific error message.
 *
 *--------------------------------------------------------------------*/

func newFilterError(pf *pfstate_t, msg string) error {
	var intro string

	if pf.from_chan == MAX_TOTAL_CHANS {
		if pf.to_chan == MAX_TOTAL_CHANS {
			intro = "filter[IG,IG]: "
		} else {
			intro = fmt.Sprintf("filter[IG,%d]: ", pf.to_chan)
		}
	} else {
		if pf.to_chan == MAX_TOTAL_CHANS {
			intro = fmt.Sprintf("filter[%d,IG]: ", pf.from_chan)
		} else {
			intro = fmt.Sprintf("filter[%d,%d]: ", pf.from_chan, pf.to_chan)
		}
	}

	return fmt.Errorf("%s%s\n%*s\n%s", intro, pf.filter_str, len(intro)+pf.tokeni+1, "^", msg)
}

// pfilterDummyMonitorLine is a synthetic packet with a known position, symbol
// and addressee used to exercise as much of the filter grammar as possible
// during config-time syntax validation.
//
// This lives here, not in a _test.go file, because pfilter_validate() is a
// runtime dependency of config.go (handleFILTER/handleCFILTER), not just
// something exercised by tests: it needs a real *packet_t to drive the
// parser/evaluator against when checking FILTER/CFILTER syntax at config
// load time, and this constant is that fixture.
const pfilterDummyMonitorLine = "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA"

/*-------------------------------------------------------------------
 *
 * Name:   	pfilter_validate
 *
 * Purpose:     Check a filter expression for syntax errors without needing
 *		a real packet, so config file problems are caught at load
 *		time rather than the first time a matching packet flows.
 *
 * Inputs:	filter	- Filter specification/expression from the config file.
 *
 *		is_aprs	- True for APRS digipeater/IGate filters, false for
 *			  connected mode digipeater filters.
 *
 * Returns:	nil if the filter is syntactically valid, otherwise an error
 *		describing the problem.
 *
 *--------------------------------------------------------------------*/

func pfilter_validate(filter string, is_aprs bool) error {
	var pp = AX25FromText(pfilterDummyMonitorLine, true)
	if pp == nil {
		return nil
	}
	defer AX25Delete(pp)

	var saved_pftest_running = pftest_running
	pftest_running = true
	defer func() { pftest_running = saved_pftest_running }()

	var _, err = pfilter(0, 0, filter, pp, is_aprs)

	return err
}
