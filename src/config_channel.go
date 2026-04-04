package direwolf

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// handleCHANNEL handles the CHANNEL keyword.
func handleCHANNEL(ps *parseState) bool {
	/*
	 * ==================== Radio channel parameters ====================
	 */

	/*
	 * CHANNEL n		- Set channel for channel-specific commands.  Only for modem/radio channels.
	 */

	// TODO: allow full range so mycall can be set for network channels.
	// Watch out for achan[] out of bounds.
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing channel number for CHANNEL command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < MAX_RADIO_CHANS {
		ps.channel = n

		if ps.audio.chan_medium[n] != MEDIUM_RADIO {
			if ps.audio.adev[ACHAN2ADEV(n)].defined == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Channel number %d is not valid because audio device %d is not defined.\n",
					ps.line, n, ACHAN2ADEV(n))
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Channel number %d is not valid because audio device %d is not in stereo.\n",
					ps.line, n, ACHAN2ADEV(n))
			}
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Channel number must in range of 0 to %d.\n", ps.line, MAX_RADIO_CHANS-1)
	}
	return false
}

// handleICHANNEL handles the ICHANNEL keyword.
func handleICHANNEL(ps *parseState) bool {
	/*
	 * ICHANNEL n			- Define IGate virtual channel.
	 *
	 *	This allows a client application to talk to to APRS-IS
	 *	by using a channel number outside the normal range for modems.
	 *	In the future there might be other typs of virtual channels.
	 *	This does not change the current channel number used by MODEM, PTT, etc.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing virtual channel number for ICHANNEL command.\n", ps.line)

		return true
	}

	var ichan, _ = strconv.Atoi(t)
	if ichan >= MAX_RADIO_CHANS && ichan < MAX_TOTAL_CHANS {
		if ps.audio.chan_medium[ichan] == MEDIUM_NONE {
			ps.audio.chan_medium[ichan] = MEDIUM_IGATE

			// This is redundant but saves the time of searching through all
			// the channels for each packet.
			ps.audio.igate_vchannel = ichan
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: ICHANNEL can't use channel %d because it is already in use.\n", ps.line, ichan)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: ICHANNEL number must in range of %d to %d.\n", ps.line, MAX_RADIO_CHANS, MAX_TOTAL_CHANS-1)
	}
	return false
}

// handleNCHANNEL handles the NCHANNEL keyword.
func handleNCHANNEL(ps *parseState) bool {
	/*
	 * NCHANNEL chan addr port			- Define Network TNC virtual channel.
	 *
	 *	This allows a client application to talk to to an external TNC over TCP KISS
	 *	by using a channel number outside the normal range for modems.
	 *	This does not change the current channel number used by MODEM, PTT, etc.
	 *
	 *	chan = direwolf channel.
	 *	addr = hostname or IP address of network TNC.
	 *	port = KISS TCP port on network TNC.
	 *
	 *	Future: Might allow selection of channel on the network TNC.
	 *	For now, ignore incoming and set to 0 for outgoing.
	 *
	 * FIXME: Can't set mycall for nchannel.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing virtual channel number for NCHANNEL command.\n", ps.line)

		return true
	}

	var nchan, _ = strconv.Atoi(t)
	if nchan >= MAX_RADIO_CHANS && nchan < MAX_TOTAL_CHANS {
		if ps.audio.chan_medium[nchan] == MEDIUM_NONE {
			ps.audio.chan_medium[nchan] = MEDIUM_NETTNC
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: NCHANNEL can't use channel %d because it is already in use.\n", ps.line, nchan)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: NCHANNEL number must in range of %d to %d.\n", ps.line, MAX_RADIO_CHANS, MAX_TOTAL_CHANS-1)
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing network TNC address for NCHANNEL command.\n", ps.line)

		return true
	}

	ps.audio.nettnc_addr[nchan] = t

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing network TNC TCP port for NCHANNEL command.\n", ps.line)

		return true
	}
	var n, _ = strconv.Atoi(t)
	ps.audio.nettnc_port[nchan] = n
	return false
}

// handleMYCALL handles the MYCALL keyword.
func handleMYCALL(ps *parseState) bool {
	/*
	 * MYCALL station
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing value for MYCALL command on line %d.\n", ps.line)

		return true
	} else {
		var strictness = 2

		/* Silently force upper case. */
		/* Might change to warning someday. */
		t = strings.ToUpper(t)

		var _, _, _, ok = ax25_parse_addr(-1, t, strictness)

		if !ok {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Invalid value for MYCALL command on line %d.\n", ps.line)

			return true
		}

		// Definitely set for current channel.
		// Set for other channels which have not been set yet.

		for c := 0; c < MAX_TOTAL_CHANS; c++ {
			if c == ps.channel || IsNoCall(ps.audio.mycall[c]) {
				ps.audio.mycall[c] = t
			}
		}
	}
	return false
}

// handleMODEM handles the MODEM keyword.
func handleMODEM(ps *parseState) bool {
	/*
	 * MODEM	- Set modem properties for current channel.
	 *
	 *
	 * Old style:
	 * 	MODEM  baud [ mark  space  [A][B][C][+]  [  num-decoders spacing ] ]
	 *
	 * New style, version 1.2:
	 *	MODEM  speed [ option ] ...
	 *
	 * Options:
	 *	mark:space	- AFSK tones.  Defaults based on speed.
	 *	num@offset	- Multiple decoders on different frequencies.
	 *	/9		- Divide sample rate by specified number.
	 *	*9		- Upsample ratio for G3RUH.
	 *	[A-Z+-]+	- Letters, plus, minus for the demodulator "profile."
	 *	g3ruh		- This modem type regardless of default for speed.
	 *	v26a or v26b	- V.26 alternative.  a=original, b=MFJ compatible
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: MODEM can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing data transmission speed for MODEM command.\n", ps.line)

		return true
	}

	var n int
	if strings.EqualFold(t, "AIS") {
		n = MAX_BAUD - 1 // Hack - See special case later.
	} else if strings.EqualFold(t, "EAS") {
		n = MAX_BAUD - 2 // Hack - See special case later.
	} else {
		n, _ = strconv.Atoi(t)
	}

	if n >= MIN_BAUD && n <= MAX_BAUD {
		ps.audio.achan[ps.channel].baud = n
		if n != 300 && n != 1200 && n != 2400 && n != 4800 && n != 9600 && n != 19200 && n != MAX_BAUD-1 && n != MAX_BAUD-2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Warning: Non-standard data rate of %d bits per second.  Are you sure?\n", ps.line, n)
		}
	} else {
		ps.audio.achan[ps.channel].baud = DEFAULT_BAUD

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable data rate. Using %d bits per second.\n",
			ps.line, ps.audio.achan[ps.channel].baud)
	}

	/* Set defaults based on speed. */
	/* Should be same as -B command line option in direwolf.c. */

	/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
	/* that need to be kept in sync.  Maybe it could be a common function someday. */

	if ps.audio.achan[ps.channel].baud < 600 {
		ps.audio.achan[ps.channel].modem_type = MODEM_AFSK
		ps.audio.achan[ps.channel].mark_freq = 1600
		ps.audio.achan[ps.channel].space_freq = 1800
	} else if ps.audio.achan[ps.channel].baud < 1800 {
		ps.audio.achan[ps.channel].modem_type = MODEM_AFSK
		ps.audio.achan[ps.channel].mark_freq = DEFAULT_MARK_FREQ
		ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ
	} else if ps.audio.achan[ps.channel].baud < 3600 {
		ps.audio.achan[ps.channel].modem_type = MODEM_QPSK
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	} else if ps.audio.achan[ps.channel].baud < 7200 {
		ps.audio.achan[ps.channel].modem_type = MODEM_8PSK
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	} else if ps.audio.achan[ps.channel].baud == MAX_BAUD-1 {
		ps.audio.achan[ps.channel].modem_type = MODEM_AIS
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	} else if ps.audio.achan[ps.channel].baud == MAX_BAUD-2 {
		ps.audio.achan[ps.channel].modem_type = MODEM_EAS
		ps.audio.achan[ps.channel].baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		ps.audio.achan[ps.channel].mark_freq = 2083  // Actually 2083.3 - logic 1.
		ps.audio.achan[ps.channel].space_freq = 1563 // Actually 1562.5 - logic 0.
		// ? strlcpy (p_audio_config.achan[channel].profiles, "A", sizeof(p_audio_config.achan[channel].profiles));
	} else {
		ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	}

	/* Get any options. */

	t = split("", false)
	if t == "" {
		/* all done. */
		return true
	}

	if alldigits(t) {
		/* old style */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Old style (pre version 1.2) format will no longer be supported in next version.\n", ps.line)

		n, _ = strconv.Atoi(t)
		/* Originally the upper limit was 3000. */
		/* Version 1.0 increased to 5000 because someone */
		/* wanted to use 2400/4800 Hz AFSK. */
		/* Of course the MIC and SPKR connections won't */
		/* have enough bandwidth so radios must be modified. */
		if n >= 300 && n <= 5000 {
			ps.audio.achan[ps.channel].mark_freq = n
		} else {
			ps.audio.achan[ps.channel].mark_freq = DEFAULT_MARK_FREQ

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable mark tone frequency. Using %d.\n",
				ps.line, ps.audio.achan[ps.channel].mark_freq)
		}

		/* Get space frequency */

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Missing tone frequency for space.\n", ps.line)

			return true
		}

		n, _ = strconv.Atoi(t)
		if n >= 300 && n <= 5000 {
			ps.audio.achan[ps.channel].space_freq = n
		} else {
			ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable space tone frequency. Using %d.\n",
				ps.line, ps.audio.achan[ps.channel].space_freq)
		}

		/* Gently guide users toward new format. */

		if ps.audio.achan[ps.channel].baud == 1200 &&
			ps.audio.achan[ps.channel].mark_freq == 1200 &&
			ps.audio.achan[ps.channel].space_freq == 2200 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: The AFSK frequencies can be omitted when using the 1200 baud default 1200:2200.\n", ps.line)
		}

		if ps.audio.achan[ps.channel].baud == 300 &&
			ps.audio.achan[ps.channel].mark_freq == 1600 &&
			ps.audio.achan[ps.channel].space_freq == 1800 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: The AFSK frequencies can be omitted when using the 300 baud default 1600:1800.\n", ps.line)
		}

		/* New feature in 0.9 - Optional filter profile(s). */

		t = split("", false)
		if t != "" {
			/* Look for some combination of letter(s) and + */
			if unicode.IsLetter(rune(t[0])) || t[0] == '+' {
				/* Here we only catch something other than letters and + mixed in. */
				/* Later, we check for valid letters and no more than one letter if + specified. */
				if strings.ContainsFunc(t, func(r rune) bool {
					return !(unicode.IsLetter(r) || r == '+' || r == '-')
				}) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Demodulator type can only contain letters and + character.\n", ps.line)
				}

				ps.audio.achan[ps.channel].profiles = t

				t = split("", false)
				if len(ps.audio.achan[ps.channel].profiles) > 1 && t != "" {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Can't combine multiple demodulator types and multiple frequencies.\n", ps.line)

					return true
				}
			}
		}

		/* New feature in 0.9 - optional number of decoders and frequency offset between. */

		if t != "" {
			n, _ = strconv.Atoi(t)
			if n < 1 || n > MAX_SUBCHANS {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Number of demodulators is out of range. Using 3.\n", ps.line)

				n = 3
			}

			ps.audio.achan[ps.channel].num_freq = n

			t = split("", false)
			if t != "" {
				n, _ = strconv.Atoi(t)
				if n < 5 || n > int(math.Abs(float64(ps.audio.achan[ps.channel].mark_freq-ps.audio.achan[ps.channel].space_freq))/2) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Unreasonable value for offset between modems.  Using 50 Hz.\n", ps.line)

					n = 50
				}

				ps.audio.achan[ps.channel].offset = n

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: New style for multiple demodulators is %d@%d\n", ps.line,
					ps.audio.achan[ps.channel].num_freq, ps.audio.achan[ps.channel].offset)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Missing frequency offset between modems.  Using 50 Hz.\n", ps.line)

				ps.audio.achan[ps.channel].offset = 50
			}
		}
	} else {
		/* New style in version 1.2. */
		for t != "" {
			if strings.Contains(t, ":") { /* mark:space */
				var markStr, spaceStr, _ = strings.Cut(t, ":")
				var mark, _ = strconv.Atoi(markStr)
				var space, _ = strconv.Atoi(spaceStr)

				ps.audio.achan[ps.channel].mark_freq = mark
				ps.audio.achan[ps.channel].space_freq = space

				if ps.audio.achan[ps.channel].mark_freq == 0 && ps.audio.achan[ps.channel].space_freq == 0 {
					ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
				} else {
					ps.audio.achan[ps.channel].modem_type = MODEM_AFSK

					if ps.audio.achan[ps.channel].mark_freq < 300 || ps.audio.achan[ps.channel].mark_freq > 5000 {
						ps.audio.achan[ps.channel].mark_freq = DEFAULT_MARK_FREQ

						text_color_set(DW_COLOR_ERROR)
						dw_printf("Line %d: Unreasonable mark tone frequency. Using %d instead.\n",
							ps.line, ps.audio.achan[ps.channel].mark_freq)
					}

					if ps.audio.achan[ps.channel].space_freq < 300 || ps.audio.achan[ps.channel].space_freq > 5000 {
						ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ

						text_color_set(DW_COLOR_ERROR)
						dw_printf("Line %d: Unreasonable space tone frequency. Using %d instead.\n",
							ps.line, ps.audio.achan[ps.channel].space_freq)
					}
				}
			} else if strings.Contains(t, "@") { /* num@offset */
				var numStr, offsetStr, _ = strings.Cut(t, "@")
				var num, _ = strconv.Atoi(numStr)
				var offset, _ = strconv.Atoi(offsetStr)

				ps.audio.achan[ps.channel].num_freq = num
				ps.audio.achan[ps.channel].offset = offset

				if ps.audio.achan[ps.channel].num_freq < 1 || ps.audio.achan[ps.channel].num_freq > MAX_SUBCHANS {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Number of demodulators is out of range. Using 3.\n", ps.line)

					ps.audio.achan[ps.channel].num_freq = 3
				}

				if ps.audio.achan[ps.channel].offset < 5 ||
					float64(ps.audio.achan[ps.channel].offset) > math.Abs(float64(ps.audio.achan[ps.channel].mark_freq-ps.audio.achan[ps.channel].space_freq))/2 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Offset between demodulators is unreasonable. Using 50 Hz.\n", ps.line)

					ps.audio.achan[ps.channel].offset = 50
				}
			} else if alllettersorpm(t) { /* profile of letter(s) + - */
				// Will be validated later.
				ps.audio.achan[ps.channel].profiles = t
			} else if t[0] == '/' { /* /div */
				var n, _ = strconv.Atoi(t[1:])

				if n >= 1 && n <= 8 {
					ps.audio.achan[ps.channel].decimate = n
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Ignoring unreasonable sample rate division factor of %d.\n", ps.line, n)
				}
			} else if t[0] == '*' { /* *upsample */
				var n, _ = strconv.Atoi(t[1:])

				if n >= 1 && n <= 4 {
					ps.audio.achan[ps.channel].upsample = n
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Ignoring unreasonable upsample ratio of %d.\n", ps.line, n)
				}
			} else if strings.EqualFold(t, "G3RUH") { /* Force G3RUH modem regardless of default for speed. New in 1.6. */
				ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
				ps.audio.achan[ps.channel].mark_freq = 0
				ps.audio.achan[ps.channel].space_freq = 0
			} else if strings.EqualFold(t, "V26A") || /* Compatible with direwolf versions <= 1.5.  New in 1.6. */
				strings.EqualFold(t, "V26B") { /* Compatible with MFJ-2400.  New in 1.6. */
				if ps.audio.achan[ps.channel].modem_type != MODEM_QPSK ||
					ps.audio.achan[ps.channel].baud != 2400 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: %s option can only be used with 2400 bps PSK.\n", ps.line, t)

					return true
				}

				ps.audio.achan[ps.channel].v26_alternative = IfThenElse((strings.EqualFold(t, "V26A")), V26_A, V26_B)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Unrecognized option for MODEM: %s\n", ps.line, t)
			}

			t = split("", false)
		}

		/* A later place catches disallowed combination of + and @. */
		/* A later place sets /n for 300 baud if not specified by user. */

		//dw_printf ("debug: div = %d\n", p_audio_config.achan[channel].decimate);
	}
	return false
}

// handleDTMF handles the DTMF keyword.
func handleDTMF(ps *parseState) bool {
	/*
	 * DTMF  		- Enable DTMF decoder.
	 *
	 * Future possibilities:
	 *	Option to determine if it goes to APRStt gateway and/or application.
	 *	Disable normal demodulator to reduce CPU requirements.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: DTMF can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	ps.audio.achan[ps.channel].dtmf_decode = DTMF_DECODE_ON
	return false
}

// handleFIX_BITS handles the FIX_BITS keyword.
func handleFIX_BITS(ps *parseState) bool {
	/*
	 * FIX_BITS  n  [ APRS | AX25 | NONE ] [ PASSALL ]
	 *
	 *	- Attempt to fix frames with bad FCS.
	 *	- n is maximum number of bits to attempt fixing.
	 *	- Optional sanity check & allow everything even with bad FCS.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FIX_BITS can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for FIX_BITS command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if BitFixLevel(n) >= RETRY_NONE && BitFixLevel(n) < RETRY_MAX { // MAX is actually last valid +1
		ps.audio.achan[ps.channel].fix_bits = BitFixLevel(n)
	} else {
		ps.audio.achan[ps.channel].fix_bits = DEFAULT_FIX_BITS

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid value %d for FIX_BITS. Using default of %d.\n",
			ps.line, n, ps.audio.achan[ps.channel].fix_bits)
	}

	if ps.audio.achan[ps.channel].fix_bits > DEFAULT_FIX_BITS {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Line %d: Using a FIX_BITS value greater than %d is not recommended for normal operation.\n",
			ps.line, DEFAULT_FIX_BITS)
		dw_printf("FIX_BITS > 1 was an interesting experiment but turned out to be a bad idea.\n")
		dw_printf("Don't be surprised if it takes 100%% CPU, direwolf can't keep up with the audio stream,\n")
		dw_printf("and you see messages like \"Audio input device 0 error code -32: Broken pipe\"\n")
	}

	t = split("", false)
	for t != "" {
		// If more than one sanity test, we silently take the last one.
		if strings.EqualFold(t, "APRS") {
			ps.audio.achan[ps.channel].sanity_test = SANITY_APRS
		} else if strings.EqualFold(t, "AX25") || strings.EqualFold(t, "AX.25") {
			ps.audio.achan[ps.channel].sanity_test = SANITY_AX25
		} else if strings.EqualFold(t, "NONE") {
			ps.audio.achan[ps.channel].sanity_test = SANITY_NONE
		} else if strings.EqualFold(t, "PASSALL") {
			ps.audio.achan[ps.channel].passall = true

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: There is an old saying, \"Be careful what you ask for because you might get it.\"\n", ps.line)
			dw_printf("The PASSALL option means allow all frames even when they are invalid.\n")
			dw_printf("You are asking to receive random trash and you WILL get your wish.\n")
			dw_printf("Don't complain when you see all sorts of random garbage.  That's what you asked for.\n")
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid option '%s' for FIX_BITS.\n", ps.line, t)
		}

		t = split("", false)
	}
	return false
}

// handlePTTDCDCON handles the PTTDCDCON keyword.
func handlePTTDCDCON(ps *parseState) bool {
	/*
	 * PTT 		- Push To Talk signal line.
	 * DCD		- Data Carrier Detect indicator.
	 * CON		- Connected to another station indicator.
	 *
	 * xxx  serial-port [-]rts-or-dtr [ [-]rts-or-dtr ]
	 * xxx  GPIO  [-]gpio-num
	 * xxx  LPT  [-]bit-num
	 * PTT  RIG  model  port [ rate ]
	 * PTT  RIG  AUTO  port [ rate ]
	 * PTT  CM108 [ [-]bit-num ] [ hid-device ]
	 *
	 * 		When model is 2, port would host:port like 127.0.0.1:4532
	 *		Otherwise, port would be a serial port like /dev/ttyS0
	 *
	 *
	 * Applies to most recent CHANNEL command.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: PTT can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}
	var ot int
	var otname string

	if strings.EqualFold(ps.keyword, "PTT") {
		ot = OCTYPE_PTT
		otname = "PTT"
	} else if strings.EqualFold(ps.keyword, "DCD") {
		ot = OCTYPE_DCD
		otname = "DCD"
	} else {
		ot = OCTYPE_CON
		otname = "CON"
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file line %d: Missing output control device for %s command.\n",
			ps.line, otname)

		return true
	}

	if strings.EqualFold(t, "GPIO") {
		/* GPIO case, Linux only. */

		/* TODO KG
		   #if __WIN32__
		   	      text_color_set(DW_COLOR_ERROR);
		   	      dw_printf ("Config file line %d: %s with GPIO is only available on Linux.\n", ps.line, otname);
		   #else
		*/
		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO number for %s.\n", ps.line, otname)

			return true
		}

		var gpio, _ = strconv.Atoi(t)
		if gpio < 0 {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_GPIO
		// #endif
	} else if strings.EqualFold(t, "GPIOD") {
		/*
			#if __WIN32__
				      text_color_set(DW_COLOR_ERROR);
				      dw_printf ("Config file line %d: %s with GPIOD is only available on Linux.\n", ps.line, otname);
			#else
		*/
		// #if defined(USE_GPIOD)
		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO chip name for %s.\n", ps.line, otname)
			dw_printf("Use the \"gpioinfo\" command to get a list of gpio chip names and corresponding I/O lines.\n")

			return true
		}

		// Issue 590.  Originally we used the chip name, like gpiochip3, and fed it into
		// gpiod_chip_open_by_name.   This function has disappeared in Debian 13 Trixie.
		// We must now specify the full device path, like /dev/gpiochip3, for the only
		// remaining open function gpiod_chip_open.
		// We will allow the user to specify either the name or full device path.
		// While we are here, also allow only the number as used by the gpiod utilities.

		if t[0] == '/' { // Looks like device path.  Use as given.
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_name = t
		} else if unicode.IsDigit(rune(t[0])) { // or if digit, prepend "/dev/gpiochip"
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_name = "/dev/gpiochip" + t
		} else { // otherwise, prepend "/dev/" to the name
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_name = "/dev/" + t
		}

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO number for %s.\n", ps.line, otname)

			return true
		}

		var gpio, _ = strconv.Atoi(t)
		if gpio < 0 {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_GPIOD
		/* TODO KG
		#else
			      text_color_set(DW_COLOR_ERROR);
			      dw_printf ("Application was not built with optional support for GPIOD.\n");
			      dw_printf ("Install packages gpiod and libgpiod-dev, remove 'build' subdirectory, then rebuild.\n");
		#endif // USE_GPIOD
		*/
		//#endif /* __WIN32__ */
	} else if strings.EqualFold(t, "LPT") {
		/* Parallel printer case, x86 Linux only. */

		//#if  ( defined(__i386__) || defined(__x86_64__) ) && ( defined(__linux__) || defined(__unix__) )
		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing LPT bit number for %s.\n", ps.line, otname)

			return true
		}

		var lpt, _ = strconv.Atoi(t)
		if lpt < 0 {
			ps.audio.achan[ps.channel].octrl[ot].ptt_lpt_bit = -1 * lpt
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			ps.audio.achan[ps.channel].octrl[ot].ptt_lpt_bit = lpt
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_LPT
		/*
			#else
				      text_color_set(DW_COLOR_ERROR);
				      dw_printf ("Config file line %d: %s with LPT is only available on x86 Linux.\n", ps.line, otname);
			#endif
		*/
	} else if strings.EqualFold(t, "RIG") {
		// TODO KG #ifdef USE_HAMLIB
		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing model number for hamlib.\n", ps.line)

			return true
		}

		if strings.EqualFold(t, "AUTO") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_model = -1
		} else {
			if !alldigits(t) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: A rig number, not a name, is required here.\n", ps.line)
				dw_printf("For example, if you have a Yaesu FT-847, specify 101.\n")
				dw_printf("See https://github.com/Hamlib/Hamlib/wiki/Supported-Radios for more details.\n")

				return true
			}

			var n, _ = strconv.Atoi(t)
			if n < 1 || n > 9999 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: Unreasonable model number %d for hamlib.\n", ps.line, n)

				return true
			}

			ps.audio.achan[ps.channel].octrl[ot].ptt_model = n
		}

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing port for hamlib.\n", ps.line)

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_device = t

		// Optional serial port rate for CAT control PTT.

		t = split("", false)
		if t != "" {
			if !alldigits(t) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: An optional number is required here for CAT serial port speed: %s\n", ps.line, t)

				return true
			}
			var n, _ = strconv.Atoi(t)
			ps.audio.achan[ps.channel].octrl[ot].ptt_rate = n
		}

		t = split("", false)
		if t != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: %s was not expected after model & port for hamlib.\n", ps.line, t)
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_HAMLIB

		// #else
		/* TODO KG
		   #if __WIN32__
		   	      text_color_set(DW_COLOR_ERROR);
		   	      dw_printf ("Config file line %d: Windows version of direwolf does not support HAMLIB.\n", ps.line);
		   	      exit (EXIT_FAILURE);
		   #else
		*/
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file line %d: %s with RIG is only available when hamlib support is enabled.\n", ps.line, otname)
		dw_printf("You must rebuild direwolf with hamlib support.\n")
		dw_printf("See User Guide for details.\n")
		// #endif

		//#endif
	} else if strings.EqualFold(t, "CM108") {
		/* CM108 - GPIO of USB sound card. case, Linux and Windows only. */

		// TODO KG #if USE_CM108
		if ot != OCTYPE_PTT {
			// Future project:  Allow DCD and CON via the same device.
			// This gets more complicated because we can't selectively change a single GPIO bit.
			// We would need to keep track of what is currently there, change one bit, in our local
			// copy of the status and then write out the byte for all of the pins.
			// Let's keep it simple with just PTT for the first stab at this.
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: PTT CM108 option is only valid for PTT, not %s.\n", ps.line, otname)

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = 3 // All known designs use GPIO 3.
		// User can override for special cases.
		ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false // High for transmit.
		ps.audio.achan[ps.channel].octrl[ot].ptt_device = ""

		// Try to find PTT device for audio output device.
		// Simplifiying assumption is that we have one radio per USB Audio Adapter.
		// Failure at this point is not an error.
		// See if config file sets it explicitly before complaining.

		ps.audio.achan[ps.channel].octrl[ot].ptt_device = cm108_find_ptt(ps.audio.adev[ACHAN2ADEV(ps.channel)].adevice_out)

		for {
			t = split("", false)
			if t == "" {
				break
			}

			if t[0] == '-' {
				var gpio, _ = strconv.Atoi(t[1:])
				ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = -1 * gpio
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
			} else if unicode.IsDigit(rune(t[0])) {
				var gpio, _ = strconv.Atoi(t)
				ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = gpio
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
			} else if t[0] == '/' {
				ps.audio.achan[ps.channel].octrl[ot].ptt_device = t
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: Found \"%s\" when expecting GPIO number or device name like /dev/hidraw1.\n", ps.line, t)

				return true
			}
		}

		if ps.audio.achan[ps.channel].octrl[ot].out_gpio_num < 1 || ps.audio.achan[ps.channel].octrl[ot].out_gpio_num > 8 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: CM108 GPIO number %d is not in range of 1 thru 8.\n", ps.line,
				ps.audio.achan[ps.channel].octrl[ot].out_gpio_num)

			return true
		}

		if ps.audio.achan[ps.channel].octrl[ot].ptt_device == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Could not determine USB Audio GPIO PTT device for audio output %s.\n", ps.line,
				ps.audio.adev[ACHAN2ADEV(ps.channel)].adevice_out)
			/* TODO KG
			#if __WIN32__
				        dw_printf ("You must explicitly mention a HID path.\n");
			#else
			*/
			dw_printf("You must explicitly mention a device name such as /dev/hidraw1.\n")
			dw_printf("Run \"cm108\" utility to get a list.\n")
			dw_printf("See Interface Guide for details.\n")

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_CM108

		/* TODO KG
		#else
			      text_color_set(DW_COLOR_ERROR);
			      dw_printf ("Config file line %d: %s with CM108 is only available when USB Audio GPIO support is enabled.\n", ps.line, otname);
			      dw_printf ("You must rebuild direwolf with CM108 Audio Adapter GPIO PTT support.\n");
			      dw_printf ("See Interface Guide for details.\n");
			      rtfm();
			      exit (EXIT_FAILURE);
		#endif
		*/
	} else {
		/* serial port case. */
		ps.audio.achan[ps.channel].octrl[ot].ptt_device = t

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing RTS or DTR after %s device name.\n",
				ps.line, otname)

			return true
		}

		if strings.EqualFold(t, "rts") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_RTS
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		} else if strings.EqualFold(t, "dtr") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_DTR
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		} else if strings.EqualFold(t, "-rts") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_RTS
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else if strings.EqualFold(t, "-dtr") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_DTR
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Expected RTS or DTR after %s device name.\n",
				ps.line, otname)

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_SERIAL

		/* In version 1.2, we allow a second one for same serial port. */
		/* Some interfaces want the two control lines driven with opposite polarity. */
		/* e.g.   PTT COM1 RTS -DTR  */

		t = split("", false)
		if t != "" {
			if strings.EqualFold(t, "rts") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_RTS
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = false
			} else if strings.EqualFold(t, "dtr") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_DTR
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = false
			} else if strings.EqualFold(t, "-rts") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_RTS
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = true
			} else if strings.EqualFold(t, "-dtr") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_DTR
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = true
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: Expected RTS or DTR after first RTS or DTR.\n",
					ps.line)

				return true
			}

			/* Would not make sense to specify the same one twice. */

			if ps.audio.achan[ps.channel].octrl[ot].ptt_line == ps.audio.achan[ps.channel].octrl[ot].ptt_line2 {
				dw_printf("Config file line %d: Doesn't make sense to specify the some control line twice.\n",
					ps.line)
			}
		} /* end of second serial port control ps.line. */
	} /* end of serial port case. */
	/* end of PTT, DCD, CON */
	return false
}

// handleTXINH handles the TXINH keyword.
func handleTXINH(ps *parseState) bool {
	/*
	 * INPUTS
	 *
	 * TXINH - TX holdoff input
	 *
	 * TXINH GPIO [-]gpio-num (only type supported so far)
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TXINH can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}
	var itname = "TXINH"

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file line %d: Missing input type name for %s command.\n", ps.line, itname)

		return true
	}

	if strings.EqualFold(t, "GPIO") {
		/* TODO KG
		#if __WIN32__
			      text_color_set(DW_COLOR_ERROR);
			      dw_printf ("Config file line %d: %s with GPIO is only available on Linux.\n", ps.line, itname);
		#else
		*/
		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO number for %s.\n", ps.line, itname)

			return true
		}

		var gpio, _ = strconv.Atoi(t)
		if gpio < 0 {
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].in_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].invert = true
		} else {
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].in_gpio_num = gpio
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].invert = false
		}

		ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
		// #endif
	}
	return false
}

// handleDWAIT handles the DWAIT keyword.
func handleDWAIT(ps *parseState) bool {
	/*
	 * DWAIT n		- Extra delay for receiver squelch. n = 10 mS units.
	 *
	 * Why did I do this?  Just add more to TXDELAY.
	 * Now undocumented in User Guide.  Might disappear someday.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: DWAIT can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing delay time for DWAIT command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= 255 {
		ps.audio.achan[ps.channel].dwait = n
	} else {
		ps.audio.achan[ps.channel].dwait = DEFAULT_DWAIT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid delay time for DWAIT. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].dwait)
	}
	return false
}

// handleSLOTTIME handles the SLOTTIME keyword.
func handleSLOTTIME(ps *parseState) bool {
	/*
	 * SLOTTIME n		- For non-digipeat transmit delay timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: SLOTTIME can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing delay time for SLOTTIME command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 5 && n < 50 {
		// 0 = User has no clue.  This would be no delay.
		// 10 = Default.
		// 50 = Half second.  User might think it is mSec and use 100.
		ps.audio.achan[ps.channel].slottime = n
	} else {
		ps.audio.achan[ps.channel].slottime = DEFAULT_SLOTTIME

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid delay time for persist algorithm. Using default %d.\n",
			ps.line, ps.audio.achan[ps.channel].slottime)
		dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
		dw_printf("section, to understand what this means.\n")
		dw_printf("Why don't you just use the default?\n")
	}
	return false
}

// handlePERSIST handles the PERSIST keyword.
func handlePERSIST(ps *parseState) bool {
	/*
	 * PERSIST 		- For non-digipeat transmit delay timing.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: PERSIST can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing probability for PERSIST command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 5 && n <= 250 {
		ps.audio.achan[ps.channel].persist = n
	} else {
		ps.audio.achan[ps.channel].persist = DEFAULT_PERSIST

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid probability for persist algorithm. Using default %d.\n",
			ps.line, ps.audio.achan[ps.channel].persist)
		dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
		dw_printf("section, to understand what this means.\n")
		dw_printf("Why don't you just use the default?\n")
	}
	return false
}

// handleTXDELAY handles the TXDELAY keyword.
func handleTXDELAY(ps *parseState) bool {
	/*
	 * TXDELAY n		- For transmit delay timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TXDELAY can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing time for TXDELAY command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= 255 {
		text_color_set(DW_COLOR_ERROR)

		if n < 10 {
			dw_printf("Line %d: Setting TXDELAY this small is a REALLY BAD idea if you want other stations to hear you.\n",
				ps.line)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default rather than reducing reliability?\n")
		} else if n >= 100 {
			dw_printf("Line %d: Keeping with tradition, going back to the 1980s, TXDELAY is in 10 millisecond units.\n",
				ps.line)
			dw_printf("Line %d: The value %d would be %.3f seconds which seems rather excessive.  Are you sure you want that?\n",
				ps.line, n, float64(n)*10./1000.)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default?\n")
		}

		ps.audio.achan[ps.channel].txdelay = n
	} else {
		ps.audio.achan[ps.channel].txdelay = DEFAULT_TXDELAY

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid time for transmit delay. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].txdelay)
	}
	return false
}

// handleTXTAIL handles the TXTAIL keyword.
func handleTXTAIL(ps *parseState) bool {
	/*
	 * TXTAIL n		- For transmit timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TXTAIL can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing time for TXTAIL command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= 255 {
		if n < 5 {
			dw_printf("Line %d: Setting TXTAIL that small is a REALLY BAD idea if you want other stations to hear you.\n",
				ps.line)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default rather than reducing reliability?\n")
		} else if n >= 50 {
			dw_printf("Line %d: Keeping with tradition, going back to the 1980s, TXTAIL is in 10 millisecond units.\n",
				ps.line)
			dw_printf("Line %d: The value %d would be %.3f seconds which seems rather excessive.  Are you sure you want that?\n",
				ps.line, n, float64(n)*10./1000.)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default?\n")
		}

		ps.audio.achan[ps.channel].txtail = n
	} else {
		ps.audio.achan[ps.channel].txtail = DEFAULT_TXTAIL

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid time for transmit timing. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].txtail)
	}
	return false
}

// handleFULLDUP handles the FULLDUP keyword.
func handleFULLDUP(ps *parseState) bool {
	/*
	 * FULLDUP  {on|off} 		- Full Duplex
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FULLDUP can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing parameter for FULLDUP command.  Expecting ON or OFF.\n", ps.line)

		return true
	}

	if strings.EqualFold(t, "ON") {
		ps.audio.achan[ps.channel].fulldup = true
	} else if strings.EqualFold(t, "OFF") {
		ps.audio.achan[ps.channel].fulldup = false
	} else {
		ps.audio.achan[ps.channel].fulldup = false

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Expected ON or OFF for FULLDUP.\n", ps.line)
	}
	return false
}

// handleSPEECH handles the SPEECH keyword.
func handleSPEECH(ps *parseState) bool {
	/*
	 * SPEECH  script
	 *
	 * Specify script for text-to-speech function.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: SPEECH can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing script for Text-to-Speech function.\n", ps.line)

		return true
	}

	/* See if we can run it. */

	/*
	   TODO KG Do we *actually* want to do this...? If so, let's do it when we've ported this to Go...

	   	 if (xmit_speak_it(t, -1, " ") == 0) {
	   	   if (strlcpy (ps.audio.tts_script, t, sizeof(ps.audio.tts_script)) >= sizeof(ps.audio.tts_script)) {
	   	     text_color_set(DW_COLOR_ERROR);
	   	     dw_printf ("Line %d: Script for text-to-speech function is too long.\n", ps.line);
	   	   }
	   	 } else {
	   	   text_color_set(DW_COLOR_ERROR);
	   	   dw_printf ("Line %d: Error trying to run Text-to-Speech function.\n", ps.line);
	   	   continue;
	   	}
	*/
	return false
}

// handleFX25TX handles the FX25TX keyword.
func handleFX25TX(ps *parseState) bool {
	/*
	 * FX25TX n		- Enable FX.25 transmission.  Default off.
	 *				0 = off, 1 = auto mode, others are suggestions for testing
	 *				or special cases.  16, 32, 64 is number of parity bytes to add.
	 *				Also set by "-X n" command line option.
	 *				V1.7 changed from global to per-channel setting.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FX25TX can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing FEC mode for FX25TX command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < 200 {
		ps.audio.achan[ps.channel].fx25_strength = n
		ps.audio.achan[ps.channel].layer2_xmit = LAYER2_FX25
	} else {
		ps.audio.achan[ps.channel].fx25_strength = 1
		ps.audio.achan[ps.channel].layer2_xmit = LAYER2_FX25

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable value for FX.25 transmission mode. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].fx25_strength)
	}
	return false
}

// handleFX25AUTO handles the FX25AUTO keyword.
func handleFX25AUTO(ps *parseState) bool {
	/*
	 * FX25AUTO n		- Enable Automatic use of FX.25 for connected mode.  *** Not Implemented ***
	 *				Automatically enable, for that session only, when an identical
	 *				frame is sent more than this number of times.
	 *				Default 5 based on half of default RETRY.
	 *				0 to disable feature.
	 *				Current a global setting.  Could be per channel someday.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FX25AUTO can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing count for FX25AUTO command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < 20 {
		ps.audio.fx25_auto_enable = n
	} else {
		ps.audio.fx25_auto_enable = AX25_N2_RETRY_DEFAULT / 2

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable count for connected mode automatic FX.25. Using %d.\n",
			ps.line, ps.audio.fx25_auto_enable)
	}
	return false
}

// handleIL2PTX handles the IL2PTX keyword.
func handleIL2PTX(ps *parseState) bool {
	/*
	 * IL2PTX  [ + - ] [ 0 1 ]	- Enable IL2P transmission.  Default off.
	 *				"+" means normal polarity. Redundant since it is the default.
	 *					(command line -I for first channel)
	 *				"-" means inverted polarity. Do not use for 1200 bps.
	 *					(command line -i for first channel)
	 *				"0" means weak FEC.  Not recommended.
	 *				"1" means stronger FEC.  "Max FEC."  Default if not specified.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: IL2PTX can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	ps.audio.achan[ps.channel].layer2_xmit = LAYER2_IL2P
	ps.audio.achan[ps.channel].il2p_max_fec = 1
	ps.audio.achan[ps.channel].il2p_invert_polarity = 0

	for {
		var t = split("", false)
		if t == "" {
			break
		}

		for _, c := range t {
			switch c {
			case '+':
				ps.audio.achan[ps.channel].il2p_invert_polarity = 0
			case '-':
				ps.audio.achan[ps.channel].il2p_invert_polarity = 1
			case '0':
				ps.audio.achan[ps.channel].il2p_max_fec = 0
			case '1':
				ps.audio.achan[ps.channel].il2p_max_fec = 1
			default:
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Invalid parameter '%c' for IL2PTX command.\n", ps.line, c)

				continue
			}
		}
	}
	return false
}
