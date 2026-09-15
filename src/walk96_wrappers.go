package direwolf

// Lightweight wrappers exporting internal symbols needed by cmd/samoyed-walk96,
// which was moved out of this package but still needs access to a few
// unexported GPS internals.

// DWGPSInit is a wrapper around dwgps_init, without exposing misc_config_s.
func DWGPSInit(gpsnmeaPort string, debug int) {
	var config misc_config_s
	config.gpsnmea_port = gpsnmeaPort

	dwgps_init(&config, debug)
}

// DWGPSRead is a wrapper around dwgps_read, without exposing dwgps_info_t.
// Unknown values come back as the G_UNKNOWN sentinel, which is what the
// callers' eventual destination, EncodePosition, still speaks (see issue #619).
func DWGPSRead() (fix int, lat float64, lon float64, speedKnots float64, track float64, altitude float64) {
	var info dwgps_info_t
	var f = dwgps_read(&info)

	return int(f), orUnknown(info.dlat), orUnknown(info.dlon),
		orUnknown(info.speed_knots), orUnknown(info.track), orUnknown(info.altitude)
}
