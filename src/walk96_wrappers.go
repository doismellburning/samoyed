package direwolf

// Lightweight wrappers exporting internal symbols needed by cmd/samoyed-walk96,
// which was moved out of this package but still needs access to a few
// unexported GPS internals.

import (
	"context"

	"github.com/doismellburning/samoyed/internal/maybe"
)

// DWGPSInit is a wrapper around dwgps_init, without exposing misc_config_s.
func DWGPSInit(ctx context.Context, gpsnmeaPort string, debug int) {
	var config misc_config_s
	config.gpsnmea_port = gpsnmeaPort

	dwgps_init(ctx, &config, debug)
}

// DWGPSRead is a wrapper around dwgps_read, without exposing dwgps_info_t.
func DWGPSRead() (fix int, lat maybe.Maybe[float64], lon maybe.Maybe[float64],
	speedKnots maybe.Maybe[float64], track maybe.Maybe[float64], altitude maybe.Maybe[float64],
) {
	var info dwgps_info_t
	var f = dwgps_read(&info)

	return int(f), info.dlat, info.dlon, info.speed_knots, info.track, info.altitude
}
