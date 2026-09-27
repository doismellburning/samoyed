// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import "github.com/doismellburning/samoyed/internal/dwutil"

// settleModemOptions applies, to each radio channel, the rules about which
// modem options go together, so that the configuration is final before any
// modem - receive or transmit - is set up from it.
//
// It is for once the configuration file and the command line have both had
// their say.
func settleModemOptions(pa *AudioConfig) {
	for channel := range MAX_RADIO_CHANS {
		if pa.chan_medium[channel] == MEDIUM_RADIO {
			pa.achan[channel].settleModemOptions(channel)
		}
	}
}

// settleModemOptions applies the rules about which modem options go together
// to one channel, saying what it changed.
func (achan *achan_param_s) settleModemOptions(channel int) {
	switch achan.modem_type {
	case MODEM_EAS, MODEM_AIS:
		// For AIS we will accept only a good CRC without any fixup attempts.
		// Even with that, there are still a lot of CRC false matches with random noise.
		var name = dwutil.IfThenElse(achan.modem_type == MODEM_EAS, "EAS", "AIS")

		if achan.fix_bits != RETRY_NONE {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Channel %d: FIX_BITS option has been turned off for %s.\n", channel, name)

			achan.fix_bits = RETRY_NONE
		}

		if achan.passall {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Channel %d: PASSALL option has been turned off for %s.\n", channel, name)

			achan.passall = false
		}

	case MODEM_QPSK:
		// In versions 1.4 and 1.5, V.26 "Alternative A" was used.
		// years later, I discover that the MFJ-2400 used "Alternative B."
		// It looks like the other two manufacturers use the same but we
		// can't be sure until we find one for compatibility testing.
		// In version 1.6 we add a choice for the user.
		// If neither one was explicitly specified, print a message and take
		// a default.  My current thinking is that we default to direwolf <= 1.5
		// compatible for version 1.6 and MFJ compatible after that.
		//
		// The transmitter reads this too, so it has to be settled before
		// either end is set up.
		if achan.v26_alternative == V26_UNSPECIFIED {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Two incompatible versions of 2400 bps QPSK are now available.\n")
			dw_printf("For compatibility with direwolf <= 1.5, use 'V26A' modem option in config file.\n")
			dw_printf("For compatibility MFJ-2400 use 'V26B' modem option in config file.\n")
			dw_printf("Command line options -j and -J can be used for channel 0.\n")
			dw_printf("For more information, read the Dire Wolf User Guide and\n")
			dw_printf("2400-4800-PSK-for-APRS-Packet-Radio.pdf.\n")
			dw_printf("The default is now MFJ-2400 compatibility mode.\n")

			achan.v26_alternative = V26_DEFAULT
		}

		achan.forbidPSKDecimation(channel)

	case MODEM_8PSK, MODEM_BPSK:
		achan.forbidPSKDecimation(channel)

	default:
	}
}

// forbidPSKDecimation turns decimation off for a PSK channel.  PSK is always
// demodulated at the full sample rate; the decimating path was never
// implemented for it.  Complain, rather than silently ignoring, when the
// configuration asked for decimation.
func (achan *achan_param_s) forbidPSKDecimation(channel int) {
	if achan.decimate > 1 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Channel %d: Decimation is not supported for PSK - ignoring.\n", channel)
	}

	achan.decimate = 1
}
