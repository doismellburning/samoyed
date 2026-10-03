// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwgps

// A browser has no serial ports to read a GPS receiver from, and the serial
// port library doesn't build there, so on js there is only this stand-in for
// dwgpsnmea.go.  The NMEA parsing in nmea.go is unaffected.

import (
	"context"

	"github.com/sirupsen/logrus"
)

// gpsnmeaPort is always the zero value: no port is ever opened.
type gpsnmeaPort struct{}

// dwgpsnmea_init reports, as dwgpsnmea.go's does, 0 when no port is
// configured and -1 when the configured one could not be opened - which here
// is always.
func dwgpsnmea_init(_ context.Context, _ *GPS, pconfig *Config, _ int) int {
	if pconfig.NMEAPort == "" {
		return 0
	}

	logrus.WithField("port", pconfig.NMEAPort).Error("Serial port GPS receivers are not supported on this platform")

	return -1
}

func dwgpsnmea_term() {}
