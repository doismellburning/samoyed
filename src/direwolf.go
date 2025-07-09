// Package direwolf is a cgo wrapper for the Dire Wolf C source, eventually leading to a full port.
package direwolf

// int direwolf_main(int argc, char *argv[]);
// #cgo CFLAGS: -I../external/geotranz -I../external/misc -DMAJOR_VERSION=0 -DMINOR_VERSION=0 -DUSE_CM108
// #cgo LDFLAGS: -lm -ludev
import "C"

import (
	_ "github.com/doismellburning/samoyed/external/geotranz" // Pulls this in for cgo
	_ "github.com/doismellburning/samoyed/external/misc"     // Pulls this in for cgo
)

// Main just wraps the C Dire Wolf main.
func Main(args []string) {
	var argv []*C.char

	for _, arg := range args {
		argv = append(argv, C.CString(arg))
	}

	C.direwolf_main(C.int(len(args)), &argv[0])
}
