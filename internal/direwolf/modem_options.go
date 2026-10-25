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

	default:
	}

	var settled = achan.Settle()

	if settled.V26Defaulted {
		logrus.WithField("channel", channel).Warn("Two incompatible versions of 2400 bps QPSK are available; " +
			"use the 'V26A' modem option for compatibility with direwolf <= 1.5, or 'V26B' for MFJ-2400 " +
			"(-j and -J on the command line for channel 0; see the Dire Wolf User Guide and 2400-4800-PSK-for-APRS-Packet-Radio.pdf). " +
			"Defaulting to MFJ-2400 compatibility mode")
	}

	if settled.DecimationDropped > 1 {
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"decimate": settled.DecimationDropped,
		}).Warn("Decimation is not supported for PSK, ignoring")
	}
}
