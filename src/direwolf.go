package direwolf

// int direwolf_main(int argc, char *argv[]);
// #cgo CFLAGS: -I../external/geotranz -I../external/misc -DMAJOR_VERSION=0 -DMINOR_VERSION=0
// #cgo LDFLAGS: -lm
import "C"
import _ "github.com/doismellburning/crocuta/external/misc"
import _ "github.com/doismellburning/crocuta/external/geotranz"

func Main() {
	C.direwolf_main(1, nil)
}
