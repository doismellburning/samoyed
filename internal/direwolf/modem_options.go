// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

// settleModemOptions applies, to each radio channel, the rules about which
// modem options go together, so that the configuration is final before any
// modem - receive or transmit - is set up from it.
//
// It is for once the configuration file and the command line have both had
// their say.
func settleModemOptions(pa *RadioConfig) {
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
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"modem":   name,
			}).Info("FIX_BITS option has been turned off")

			achan.fix_bits = RETRY_NONE
		}

		if achan.passall {
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"modem":   name,
			}).Info("PASSALL option has been turned off")

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
			logrus.WithField("channel", channel).Warn("Two incompatible versions of 2400 bps QPSK are available; " +
				"use the 'V26A' modem option for compatibility with direwolf <= 1.5, or 'V26B' for MFJ-2400 " +
				"(-j and -J on the command line for channel 0; see the Dire Wolf User Guide and 2400-4800-PSK-for-APRS-Packet-Radio.pdf). " +
				"Defaulting to MFJ-2400 compatibility mode")

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
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"decimate": achan.decimate,
		}).Warn("Decimation is not supported for PSK, ignoring")
	}

	achan.decimate = 1
}
