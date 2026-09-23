// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
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

// modemFlags are the command line options that choose a channel's modem:
// -B, -g, -k, -j and -J, and for a program that receives, the demodulator's
// -P, -D and -U too.
type modemFlags struct {
	fs               *pflag.FlagSet
	bitrate          *string
	g3ruh            *bool
	bpsk             *bool
	direwolf15compat *bool
	mfj2400compat    *bool

	// Only for a program that receives.
	receive  bool
	profile  *string
	decimate *int
	upsample *int
}

func addModemFlags(fs *pflag.FlagSet, receive bool) *modemFlags {
	var f = new(modemFlags)
	f.fs = fs
	f.bitrate = fs.StringP("bitrate", "B", strconv.Itoa(DEFAULT_BAUD), `Bits/second for data.  Proper modem automatically selected for speed.
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`)
	f.g3ruh = fs.BoolP("g3ruh", "g", false, "Use G3RUH modem rather than default for data rate.")
	f.bpsk = fs.BoolP("bpsk", "k", false, "Use BPSK modem rather than default for data rate.")
	f.direwolf15compat = fs.BoolP("direwolf-15-compat", "j", false, "2400 bps QPSK compatible with direwolf <= 1.5.")
	f.mfj2400compat = fs.BoolP("mfj-2400-compat", "J", false, "2400 bps QPSK compatible with MFJ-2400.")

	f.receive = receive
	if receive {
		f.profile = fs.StringP("modem-profile", "P", "", "Demodulator profile: letters, then optionally + or -, such as E+ for AFSK or PQRS for 2400 bps.")
		f.decimate = fs.IntP("decimate", "D", 0, "Divide audio sample rate by n, 1 to 8. 0 is auto-select.")
		f.upsample = fs.IntP("upsample", "U", 0, "Upsample for G3RUH by n, 1 to 4, to improve performance when the sample rate to baud ratio is low. 0 is auto-select.")
	}

	return f
}

// apply sets up achan from the options, once they have been parsed.
func (f *modemFlags) apply(achan *achan_param_s) error {
	if f.fs.Changed("bitrate") {
		var err = achan.setModem(*f.bitrate)
		if err != nil {
			return err
		}
	}

	if *f.g3ruh {
		// Force G3RUH mode, overriding default for speed.
		//   Example:   -B 2400 -g
		achan.modem_type = MODEM_SCRAMBLE
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.profiles = ""
	}

	if *f.bpsk {
		// Force BPSK mode, overriding default for speed.
		//   Example:   -B 300 -k
		achan.modem_type = MODEM_BPSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.profiles = ""
	}

	// We have two different incompatible flavors of V.26.
	if *f.direwolf15compat {
		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j
		achan.v26_alternative = V26_A
		achan.modem_type = MODEM_QPSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.baud = 2400
		achan.profiles = ""
	}

	if *f.mfj2400compat {
		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J
		achan.v26_alternative = V26_B
		achan.modem_type = MODEM_QPSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.baud = 2400
		achan.profiles = ""
	}

	// After -j and -J, which bring a rate of their own.
	if f.fs.Changed("bitrate") {
		if achan.modem_type == MODEM_QPSK && achan.baud != 2400 {
			logrus.WithField("bitrate", achan.baud).Warn("Bit rate should be the standard 2400 for QPSK")
		}

		if achan.modem_type == MODEM_8PSK && achan.baud != 4800 {
			logrus.WithField("bitrate", achan.baud).Warn("Bit rate should be the standard 4800 for 8PSK")
		}
	}

	if f.receive {
		// Needs to be after -B, -j, -J.
		if *f.profile != "" {
			achan.profiles = *f.profile
		}

		if f.fs.Changed("decimate") {
			if *f.decimate < 0 || *f.decimate > 8 {
				return fmt.Errorf("decimate should be between 0 and 8 inclusive, not %d", *f.decimate)
			}

			// Reduce audio sampling rate to reduce CPU requirements.
			achan.decimate = *f.decimate
		}

		if f.fs.Changed("upsample") {
			if *f.upsample < 0 || *f.upsample > 4 {
				return fmt.Errorf("upsample should be between 0 and 4 inclusive, not %d", *f.upsample)
			}

			// Increase G3RUH audio sampling rate to improve performance.
			// The value is normally determined automatically based on audio
			// sample rate and baud.  This allows override for experimentation.
			achan.upsample = *f.upsample
		}
	}

	if f.anyChanged() {
		logrus.WithFields(logrus.Fields{
			"bitrate":  achan.baud,
			"modem":    achan.modem_type,
			"mark":     achan.mark_freq,
			"space":    achan.space_freq,
			"profiles": achan.profiles,
		}).Info("Modem set from the command line")
	}

	return nil
}

// anyChanged says whether any of the options were given.
func (f *modemFlags) anyChanged() bool {
	return slices.ContainsFunc([]string{"bitrate", "g3ruh", "bpsk", "direwolf-15-compat", "mfj-2400-compat", "modem-profile", "decimate", "upsample"}, f.fs.Changed)
}

// layer2TxFlags are the command line options that choose how a channel
// frames what it transmits: -X for FX.25, or -I or -i for IL2P.
type layer2TxFlags struct {
	fx25CheckBytes *int
	il2pNormal     *int
	il2pInverted   *int
}

// addLayer2TxFlags adds the options to fs.  il2pVersionFrom says where the
// IL2P version is chosen, which the help for -I and -i points to.
func addLayer2TxFlags(fs *pflag.FlagSet, il2pVersionFrom string) *layer2TxFlags {
	var f = new(layer2TxFlags)
	f.fx25CheckBytes = fs.IntP("fx25-check-bytes", "X", 0, "1 to enable FX.25 transmit.  16, 32, 64 for specific number of check bytes.")
	f.il2pNormal = fs.IntP("il2p", "I", -1, "Enable IL2P transmit.  n=1 is recommended.  0 asks for weaker FEC, which only v0.4 has (see "+il2pVersionFrom+").")
	f.il2pInverted = fs.IntP("il2p-inverted", "i", -1, "Enable IL2P transmit, inverted polarity.  n=1 is recommended.  0 asks for weaker FEC, which only v0.4 has (see "+il2pVersionFrom+").")

	return f
}

// apply sets up achan from the options, once they have been parsed.  It
// goes after the modem's options, whose bit rate one of its warnings is about.
func (f *layer2TxFlags) apply(achan *achan_param_s) error {
	if *f.fx25CheckBytes > 0 {
		if *f.il2pNormal >= 0 || *f.il2pInverted >= 0 {
			return errors.New("can't mix -X with -I or -i")
		}

		achan.fx25_strength = *f.fx25CheckBytes
		achan.layer2_xmit = LAYER2_FX25
	}

	if *f.il2pNormal >= 0 && *f.il2pInverted >= 0 {
		return errors.New("can't use both -I and -i at the same time")
	}

	if *f.il2pNormal >= 0 {
		achan.layer2_xmit = LAYER2_IL2P
		achan.il2p_max_fec = IfThenElse(*f.il2pNormal > 0, 1, 0)
		achan.il2p_invert_polarity = 0 // normal

		if achan.il2p_max_fec == 0 {
			logrus.Warn("It is highly recommended that 1, rather than 0, is used with -I for best results")
		}
	}

	if *f.il2pInverted >= 0 {
		achan.layer2_xmit = LAYER2_IL2P
		achan.il2p_max_fec = IfThenElse(*f.il2pInverted > 0, 1, 0)
		achan.il2p_invert_polarity = 1 // invert for transmit

		if achan.il2p_max_fec == 0 {
			logrus.Warn("It is highly recommended that 1, rather than 0, is used with -i for best results")
		}

		if achan.baud == 1200 {
			logrus.Warn("Using -i with 1200 bps is a bad idea.  Use -I instead")
		}
	}

	if *f.fx25CheckBytes > 0 || *f.il2pNormal >= 0 || *f.il2pInverted >= 0 {
		logrus.WithFields(logrus.Fields{
			"fx25_check_bytes":     achan.fx25_strength,
			"il2p_max_fec":         achan.il2p_max_fec,
			"il2p_invert_polarity": achan.il2p_invert_polarity,
		}).Info(IfThenElse(achan.layer2_xmit == LAYER2_FX25, "Transmitting FX.25", "Transmitting IL2P"))
	}

	return nil
}
