// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package phy

import (
	"fmt"
)

// Modem is the kind of modem a channel uses.
type Modem int

const (
	AFSK Modem = iota
	Baseband
	Scramble
	QPSK
	PSK8
	Off
	QAM16
	QAM64
	AIS
	EAS
	BPSK
)

func (m Modem) String() string {
	switch m {
	case AFSK:
		return "AFSK"
	case Baseband:
		return "BASEBAND"
	case Scramble:
		return "SCRAMBLE"
	case QPSK:
		return "QPSK"
	case PSK8:
		return "8PSK"
	case Off:
		return "OFF"
	case QAM16:
		return "16QAM"
	case QAM64:
		return "64QAM"
	case AIS:
		return "AIS"
	case EAS:
		return "EAS"
	case BPSK:
		return "BPSK"
	default:
		return fmt.Sprintf("modem_t(%d)", int(m))
	}
}
