package direwolf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

// pflag (not unreasonably) assumes it only ever gets called once. But lots of
// test infrastructure was built around "call this command then this command".
// Running it in Go tests (for coverage analysis and convenience etc.) means
// doing some slight bodges.
func setupPflag(args []string){
	os.Args = args
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
}

func Test_Modem1200I(t *testing.T) {
	var tmpdir = t.TempDir()
	var file = filepath.Join(tmpdir, "test12-il2p.wav")

	setupPflag([]string{"gen_packets", "-I1", "-n", "100", "-o", file})
	GenPacketsMain()

	setupPflag([]string{"atest", "-P+", "-D1", "-L92", "-G95", file})
	AtestMain()
}

func Test_FX25_9600_F0(t*testing.T) {
	var tmpdir = t.TempDir()
	var file = filepath.Join(tmpdir, "test96f0.wav")

	setupPflag([]string{"gen_packets", "-B9600", "-n", "100", "-X", "0", "-o", file})
	GenPacketsMain()

	setupPflag([]string{"atest", "-B9600", "-F0", "-L60", "-G64", file})
	AtestMain()
}
