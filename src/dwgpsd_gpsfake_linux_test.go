package direwolf

// Integration test against a real gpsd, driven by gpsfake (from the gpsd-clients
// package) replaying a canned NMEA log. Linux only, since gpsfake feeds gpsd
// via a pty and that combination isn't available on macOS.

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gpsfakeFixtureNMEA = "$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F\n" +
	"$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B\n"

// freeTCPPort grabs a free port by briefly binding to port 0 and reading back
// what the kernel assigned. There's a small window where something else could
// grab it before gpsfake does, but that's an acceptable risk for a test.
func freeTCPPort(t *testing.T) int {
	t.Helper()

	var lc = new(net.ListenConfig)

	var l, err = lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var tcpAddr, ok = l.Addr().(*net.TCPAddr)
	require.True(t, ok)

	var port = tcpAddr.Port

	require.NoError(t, l.Close())

	return port
}

// startGpsfake launches gpsfake against a fixture NMEA log, on its own process
// group so the child gpsd it spawns can be killed alongside it in cleanup, and
// waits for it to start accepting connections.
func startGpsfake(t *testing.T, port int) {
	t.Helper()

	var fixture = filepath.Join(t.TempDir(), "gpsfake.log")
	require.NoError(t, os.WriteFile(fixture, []byte(gpsfakeFixtureNMEA), 0o600))

	var cmd = exec.CommandContext(context.Background(), "gpsfake", "-n", "-P", strconv.Itoa(port), "-c", "0.1", fixture) //nolint:gosec

	// Only Setpgid is relevant here; the rest are fine at their zero values.
	cmd.SysProcAttr = &syscall.SysProcAttr{ //nolint:exhaustruct
		Setpgid: true,
	}

	var output, outputErr = os.CreateTemp(t.TempDir(), "gpsfake-output-*.log")
	require.NoError(t, outputErr)

	cmd.Stdout = output
	cmd.Stderr = output

	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()

		if t.Failed() {
			var contents, readErr = os.ReadFile(output.Name())
			if readErr == nil {
				t.Logf("gpsfake output:\n%s", contents)
			}
		}
	})

	var addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	// Only Timeout is relevant here; the rest are fine at their zero values.
	var dialer = net.Dialer{Timeout: 200 * time.Millisecond} //nolint:exhaustruct

	var deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var conn, dialErr = dialer.DialContext(context.Background(), "tcp", addr)
		if dialErr == nil {
			conn.Close()

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("gpsd never started listening on %s", addr)
}

func Test_dwgpsd_against_real_gpsfake(t *testing.T) {
	var port = freeTCPPort(t)

	startGpsfake(t, port)

	var config = new(misc_config_s)
	config.gpsd_host = "127.0.0.1"
	config.gpsd_port = port

	require.Equal(t, 1, dwgpsd_init(config, 3))

	t.Cleanup(dwgpsd_term)

	var info = new(dwgps_info_t)

	var fix dwfix_t

	// gpsd emits several TPV reports per cycle as each NMEA sentence arrives:
	// a 2D-only one from $GPRMC, then a 3D one still without altitude, then
	// finally the fuller one derived from $GPGGA that carries altitude. Wait
	// for that last one rather than just the first 3D report.
	var deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		fix = dwgps_read(info)
		if fix >= DWFIX_3D && info.altitude != G_UNKNOWN {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	require.GreaterOrEqual(t, fix, DWFIX_3D, "never got a 3D location fix from gpsd")
	require.NotEqual(t, G_UNKNOWN, info.altitude, "never got an altitude from gpsd")

	assert.InDelta(t, 42.6187, info.dlat, 0.001)
	assert.InDelta(t, -71.3472, info.dlon, 0.001)
	assert.InDelta(t, 33.5, info.altitude, 0.001)
}
