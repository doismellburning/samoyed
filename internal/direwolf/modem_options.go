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
// to one channel, saying what it changed: the modem's own, and that EAS and
// AIS take a frame only as it came.
func (achan *achan_param_s) settleModemOptions(channel int) {
	switch achan.ModemType {
	case MODEM_EAS, MODEM_AIS:
		// For AIS we will accept only a good CRC without any fixup attempts.
		// Even with that, there are still a lot of CRC false matches with random noise.
		var name = dwutil.IfThenElse(achan.ModemType == MODEM_EAS, "EAS", "AIS")

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

	default:
	}

	var settled = achan.Settle()

	if settled.V26Defaulted {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"hint": "For compatibility with direwolf <= 1.5, use 'V26A' modem option in config file. " +
				"For compatibility MFJ-2400 use 'V26B' modem option in config file. " +
				"Command line options -j and -J can be used for channel 0. " +
				"For more information, read the Dire Wolf User Guide and 2400-4800-PSK-for-APRS-Packet-Radio.pdf.",
		}).Warn("Two incompatible versions of 2400 bps QPSK are now available. The default is now MFJ-2400 compatibility mode.")
	}

	if settled.DecimationDropped > 1 {
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"decimate": settled.DecimationDropped,
		}).Warn("Decimation is not supported for PSK - ignoring")
	}
}
