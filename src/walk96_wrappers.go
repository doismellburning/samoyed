package direwolf

// Lightweight wrappers exporting internal symbols needed by cmd/samoyed-walk96,
// which was moved out of this package but still needs access to a few
// unexported GPS and APRS encoding internals.

// DWGPSInit is a wrapper around dwgps_init, without exposing misc_config_s.
func DWGPSInit(gpsnmeaPort string, debug int) {
	var config misc_config_s
	config.gpsnmea_port = gpsnmeaPort

	dwgps_init(&config, debug)
}

// DWGPSRead is a wrapper around dwgps_read, without exposing dwgps_info_t.
func DWGPSRead() (fix int, lat float64, lon float64, speedKnots float64, track float64, altitude float64) {
	var info dwgps_info_t
	var f = dwgps_read(&info)

	return int(f), info.dlat, info.dlon, info.speed_knots, info.track, info.altitude
}

// EncodePosition is a wrapper around encode_position.
func EncodePosition(messaging bool, compressed bool, lat float64, lon float64, ambiguity int, altFt int,
	symtab byte, symbol byte,
	power int, height int, gain int, dir string,
	course int, speed int,
	freq float64, tone float64, offset float64,
	comment string) string {
	return encode_position(messaging, compressed, lat, lon, ambiguity, altFt,
		symtab, symbol,
		power, height, gain, dir,
		course, speed,
		freq, tone, offset,
		comment)
}
