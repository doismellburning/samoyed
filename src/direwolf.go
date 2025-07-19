// Package direwolf is a cgo wrapper for the Dire Wolf C source, eventually leading to a full port.
package direwolf

// #cgo CFLAGS: -I../external/geotranz -I../external/misc -DMAJOR_VERSION=0 -DMINOR_VERSION=0 -DUSE_CM108 -DUSE_AVAHI_CLIENT -DUSE_HAMLIB -DUSE_ALSA
// #cgo LDFLAGS: -lm -ludev -lavahi-common -lavahi-client -lhamlib -lasound
import "C"

import (
	_ "github.com/doismellburning/samoyed/external/geotranz" // Pulls this in for cgo
	_ "github.com/doismellburning/samoyed/external/misc"     // Pulls this in for cgo
)
