// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"strconv"
	"strings"
)

func (m modem_t) String() string {
	switch m {
	case MODEM_AFSK:
		return "AFSK"
	case MODEM_BASEBAND:
		return "BASEBAND"
	case MODEM_SCRAMBLE:
		return "SCRAMBLE"
	case MODEM_QPSK:
		return "QPSK"
	case MODEM_8PSK:
		return "8PSK"
	case MODEM_OFF:
		return "OFF"
	case MODEM_16_QAM:
		return "16QAM"
	case MODEM_64_QAM:
		return "64QAM"
	case MODEM_AIS:
		return "AIS"
	case MODEM_EAS:
		return "EAS"
	case MODEM_BPSK:
		return "BPSK"
	default:
		return fmt.Sprintf("modem_t(%d)", int(m))
	}
}

// setModem sets up achan for a -B option or a MODEM line's speed: a bit rate,
// which brings the usual modem for that rate, or AIS or EAS, which name one.
// The modem's tones go with it, and so does the demodulator profile, unless
// the channel was AFSK and stays AFSK.  achan is left alone if bitrate is no
// good.
//
//	300 implies 1600/1800 AFSK, for HF SSB.
//	1200 implies 1200/2200 AFSK.
//	2400 implies V.26 QPSK.
//	4800 implies V.27 8PSK.
//	9600 and up implies G3RUH baseband scrambled.
func (achan *achan_param_s) setModem(bitrate string) error {
	if strings.EqualFold(bitrate, "AIS") {
		achan.modem_type = MODEM_AIS
		achan.baud = 9600
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.profiles = ""

		return nil
	}

	if strings.EqualFold(bitrate, "EAS") {
		achan.modem_type = MODEM_EAS
		achan.baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		achan.mark_freq = 2083  // Actually 2083.3 - logic 1.
		achan.space_freq = 1563 // Actually 1562.5 - logic 0.
		achan.profiles = "A"

		return nil
	}

	var baud, err = strconv.Atoi(bitrate)
	if err != nil {
		return fmt.Errorf("invalid bitrate (should be an integer or 'AIS' or 'EAS'): %s", bitrate)
	}

	if baud < MIN_BAUD || baud > MAX_BAUD {
		return fmt.Errorf("use a more reasonable bit rate in range of %d - %d", MIN_BAUD, MAX_BAUD)
	}

	achan.baud = baud

	// The profile belongs to the modem it was chosen for.  AFSK keeps an
	// AFSK one, and every other modem starts afresh.
	if baud < 1800 && achan.modem_type != MODEM_AFSK {
		achan.profiles = ""
	}

	switch {
	case baud < 600:
		achan.modem_type = MODEM_AFSK
		achan.mark_freq = 1600
		achan.space_freq = 1800
	case baud < 1800:
		achan.modem_type = MODEM_AFSK
		achan.mark_freq = DEFAULT_MARK_FREQ
		achan.space_freq = DEFAULT_SPACE_FREQ
	case baud < 3600:
		achan.modem_type = MODEM_QPSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.profiles = ""
	case baud < 7200:
		achan.modem_type = MODEM_8PSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.profiles = ""
	default:
		achan.modem_type = MODEM_SCRAMBLE
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.profiles = ""
	}

	return nil
}
