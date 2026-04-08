package direwolf

import (
	"strconv"
	"unicode"
)

// handleADEVICE handles the ADEVICE[n] keyword.
func handleADEVICE(ps *parseState) bool {
	/*
	 * ADEVICE[n] 		- Name of input sound device, and optionally output, if different.
	 *
	 *			ADEVICE    plughw:1,0			-- same for in and out.
	 *			ADEVICE	   plughw:2,0  plughw:3,0	-- different in/out for a channel or channel pair.
	 *			ADEVICE1   udp:7355  default		-- from Software defined radio (SDR) via UDP.
	 *
	 *	New in 1.8: Ability to map to another audio device.
	 *	This allows multiple modems (i.e. data speeds) on the same audio interface.
	 *
	 *			ADEVICEn   = n				-- Copy from different already defined channel.
	 */
	/* Note that ALSA name can contain comma such as hw:1,0 */
	/* "ADEVICE" is equivalent to "ADEVICE0". */
	ps.adevice = 0

	// ps.keyword holds the original token e.g. "ADEVICE" or "ADEVICE1".
	if len(ps.keyword) >= 8 {
		var i, iErr = strconv.Atoi(string(ps.keyword[7]))
		if iErr != nil {
			dw_printf("Config file: Could not parse ADEVICE number on line %d: %s.\n", ps.line, iErr)
			return true
		}

		if i < 0 || i >= MAX_ADEVS {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Device number %d out of range for ADEVICE command on line %d.\n", ps.adevice, ps.line)
			dw_printf("If you really need more than %d audio devices, increase MAX_ADEVS and recompile.\n", MAX_ADEVS)

			ps.adevice = 0

			return true
		}

		ps.adevice = i
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing name of audio device for ADEVICE command on line %d.\n", ps.line)
		rtfm()
		exit(1)
	}

	// Do not allow same adevice to be defined more than once.
	// Overriding the default for adevice 0 is ok.
	// In that case defined was 2.  That's why we check for 1, not just non-zero.

	if ps.audio.adev[ps.adevice].defined == 1 { // 1 means defined by user.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: ADEVICE%d can't be defined more than once. Line %d.\n", ps.adevice, ps.line)

		return true
	}

	ps.audio.adev[ps.adevice].defined = 1

	// New case for release 1.8.

	if t == "=" {
		t = split("", false)
		if t != "" { //nolint:staticcheck
		}

		/////////  to be continued....  FIXME
	} else {
		/* First channel of device is valid. */
		// This might be changed to UDP or STDIN when the device name is examined.
		ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

		ps.audio.adev[ps.adevice].adevice_in = t
		ps.audio.adev[ps.adevice].adevice_out = t

		t = split("", false)
		if t != "" {
			// Different audio devices for receive and transmit.
			ps.audio.adev[ps.adevice].adevice_out = t
		}
	}
	return false
}

// handlePAIDEVICE handles PAIDEVICE[n].
func handlePAIDEVICE(ps *parseState) bool {
	// ps.keyword holds the original token e.g. "PAIDEVICE" or "PAIDEVICE1".
	ps.adevice = 0
	if len(ps.keyword) > 9 && unicode.IsDigit(rune(ps.keyword[9])) {
		ps.adevice = int(ps.keyword[9] - '0')
	}

	if ps.adevice < 0 || ps.adevice >= MAX_ADEVS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Device number %d out of range for PADEVICE command on line %d.\n", ps.adevice, ps.line)
		ps.adevice = 0

		return true
	}

	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing name of audio device for PADEVICE command on line %d.\n", ps.line)

		return true
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_in = t
	return false
}

// handlePAODEVICE handles PAODEVICE[n].
func handlePAODEVICE(ps *parseState) bool {
	// ps.keyword holds the original token e.g. "PAODEVICE" or "PAODEVICE1".
	ps.adevice = 0
	if len(ps.keyword) > 9 && unicode.IsDigit(rune(ps.keyword[9])) {
		ps.adevice = int(ps.keyword[9] - '0')
	}

	if ps.adevice < 0 || ps.adevice >= MAX_ADEVS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Device number %d out of range for PADEVICE command on line %d.\n", ps.adevice, ps.line)
		ps.adevice = 0

		return true
	}

	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing name of audio device for PADEVICE command on line %d.\n", ps.line)

		return true
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_out = t
	return false
}

// handleARATE handles the ARATE keyword.
func handleARATE(ps *parseState) bool {
	/*
	 * ARATE 		- Audio samples per second, 11025, 22050, 44100, etc.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing audio sample rate for ARATE command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= MIN_SAMPLES_PER_SEC && n <= MAX_SAMPLES_PER_SEC {
		ps.audio.adev[ps.adevice].samples_per_sec = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Use a more reasonable audio sample rate in range of %d - %d.\n",
			ps.line, MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC)
	}
	return false
}

// handleACHANNELS handles the ACHANNELS keyword.
func handleACHANNELS(ps *parseState) bool {
	/*
	 * ACHANNELS 		- Number of audio channels for current device: 1 or 2
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing number of audio channels for ACHANNELS command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n == 1 || n == 2 {
		ps.audio.adev[ps.adevice].num_channels = n

		/* Set valid channels depending on mono or stereo. */

		ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO
		if n == 2 {
			ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)+1] = MEDIUM_RADIO
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Number of audio channels must be 1 or 2.\n", ps.line)
	}
	return false
}
