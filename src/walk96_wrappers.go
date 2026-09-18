package direwolf

// Lightweight wrappers exporting internal symbols needed by cmd/samoyed-walk96,
// which was moved out of this package but still needs access to a few
// unexported GPS internals.

import (
	"github.com/doismellburning/samoyed/internal/dwgps"
)

// DWGPSInit is a wrapper around dwgps.Init, without exposing misc_config_s.
func DWGPSInit(gpsnmeaPort string, debug int) {
	var config misc_config_s
	config.gpsnmea_port = gpsnmeaPort

	dwgps.Init(dwgpsConfig(&config), debug)
}

// DWGPSRead is a wrapper around dwgps.Read, without exposing dwgps.Info.
// Unknown values come back as the G_UNKNOWN sentinel, which is what the
// callers' eventual destination, EncodePosition, still speaks (see issue #619).
func DWGPSRead() (fix dwgps.Fix, lat float64, lon float64, speedKnots float64, track float64, altitude float64) {
	var info dwgps.Info
	var f = dwgps.Read(&info)

	return f, orUnknown(info.Lat), orUnknown(info.Lon),
		orUnknown(info.SpeedKnots), orUnknown(info.Track), orUnknown(info.Altitude)
}
