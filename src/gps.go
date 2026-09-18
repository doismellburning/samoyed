package direwolf

// Glue between the configuration file and internal/dwgps.

import (
	"github.com/doismellburning/samoyed/internal/dwgps"
)

// dwgpsConfig is the GPS part of the configuration, in the shape the dwgps
// package wants it.  The serial port is opened by way of SerialPortOpen rather
// than by that package, which has no business knowing how we talk to one.
func dwgpsConfig(pconfig *misc_config_s) *dwgps.Config {
	return &dwgps.Config{
		NMEAPort:       pconfig.gpsnmea_port,
		NMEASpeed:      pconfig.gpsnmea_speed,
		OpenSerialPort: SerialPortOpen,
		GPSDHost:       pconfig.gpsd_host,
		GPSDPort:       pconfig.gpsd_port,
	}
}
