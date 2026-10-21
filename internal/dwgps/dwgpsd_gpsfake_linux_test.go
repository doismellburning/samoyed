// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwgps

// Integration test against a real gpsd, driven by gpsfake (from the gpsd-clients
// package) replaying a canned NMEA log. Linux only, since gpsfake feeds gpsd
// via a pty and that combination isn't available on macOS.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gpsfakeFixtureNMEA is one fix, as an RMC and a GGA sentence, dated now.
//
// gpsd checks the date it is given against its own idea of the present: an
// old one, such as the 2014 these sentences once carried, it takes for a GPS
// week rollover bug and moves 1024 weeks on, into the future, where it then
// warns about it every cycle.  Dating the fix now keeps all of that out of
// the test.  Both sentences carry the same time, so gpsd sees one fix rather
// than two a minute apart with time running backwards between them.
func gpsfakeFixtureNMEA(now time.Time) string {
	var date = now.UTC().Format("020106")
	var clock = now.UTC().Format("150405") + ".710"

	return nmeaSentence("GPRMC,"+clock+",A,4237.1240,N,07120.8333,W,5.07,291.42,"+date+",,,A") +
		nmeaSentence("GPGGA,"+clock+",4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000")
}

// nmeaSentence wraps body, the part between the $ and the *, into a sentence
// with its checksum.
func nmeaSentence(body string) string {
	var sum byte

	for i := range len(body) {
		sum ^= body[i]
	}

	return fmt.Sprintf("$%s*%02X\n", body, sum)
}

// gpsfakeAttempts is how many times startGpsfake starts gpsfake before giving
// up on gpsd ever attaching its device.
const gpsfakeAttempts = 3

// startGpsfake starts gpsfake against a fixture NMEA log and waits until gpsd
// is serving gpsfake's device.
//
// gpsfake hands gpsd the device over gpsd's control socket, once the socket's
// file exists, and quietly drops the device if it can't connect - as it can't
// when gpsd has bound the socket but not yet listened on it.  gpsd then serves
// no device at all, which no amount of waiting fixes, so start over instead.
func startGpsfake(t *testing.T, port int) {
	t.Helper()

	var fixture = filepath.Join(t.TempDir(), "gpsfake.log")
	require.NoError(t, os.WriteFile(fixture, []byte(gpsfakeFixtureNMEA(time.Now())), 0o600))

	var addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	for attempt := 1; attempt <= gpsfakeAttempts; attempt++ {
		var stop = launchGpsfake(t, port, fixture)

		waitForGpsdListening(t, addr)

		if waitForGpsdDevice(addr, 5*time.Second) {
			return
		}

		t.Logf("gpsd never attached gpsfake's device (attempt %d of %d)", attempt, gpsfakeAttempts)

		// Before the next attempt, so its gpsd can have the port.
		stop()
	}

	t.Fatalf("gpsd never attached gpsfake's device in %d attempts", gpsfakeAttempts)
}

// launchGpsfake launches gpsfake on its own process group, so the child gpsd
// it spawns can be killed alongside it, and returns a func that does so.  The
// test's cleanup calls it too, and prints gpsfake's output if the test failed.
func launchGpsfake(t *testing.T, port int, fixture string) func() {
	t.Helper()

	// -D 2 has gpsd log its warnings, and what its clients ask of it,
	// alongside gpsfake's own output, which the test prints if it fails.
	var cmd = exec.CommandContext(context.Background(), "gpsfake", "-n", "-P", strconv.Itoa(port), "-c", "0.1", "-D", "2", fixture) //nolint:gosec

	// Only Setpgid is relevant here; the rest are fine at their zero values.
	cmd.SysProcAttr = &syscall.SysProcAttr{ //nolint:exhaustruct_v5
		Setpgid: true,
	}

	var output, outputErr = os.CreateTemp(t.TempDir(), "gpsfake-output-*.log")
	require.NoError(t, outputErr)

	cmd.Stdout = output
	cmd.Stderr = output

	require.NoError(t, cmd.Start())

	var stop = sync.OnceFunc(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()

		// Only once gpsfake is gone, so nothing is still writing to it.
		_ = output.Close()
	})

	t.Cleanup(func() {
		stop()

		if t.Failed() {
			var contents, readErr = os.ReadFile(output.Name())
			if readErr == nil {
				t.Logf("gpsfake output (%s):\n%s", output.Name(), contents)
			}
		}
	})

	return stop
}

// waitForGpsdListening waits for gpsd to accept connections on addr.
func waitForGpsdListening(t *testing.T, addr string) {
	t.Helper()

	// Only Timeout is relevant here; the rest are fine at their zero values.
	var dialer = net.Dialer{Timeout: 200 * time.Millisecond} //nolint:exhaustruct_v5

	var deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var conn, dialErr = dialer.DialContext(context.Background(), "tcp", addr)
		if dialErr == nil {
			conn.Close()

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	// The usual local cause is AppArmor stopping gpsd from opening gpsfake's pty.
	t.Fatalf("gpsd never started listening on %s (if AppArmor is enforcing, try ./dev-setup.sh gpsd-apparmor)", addr)
}

// waitForGpsdDevice reports whether the gpsd at addr lists a device within
// timeout.
func waitForGpsdDevice(addr string, timeout time.Duration) bool {
	var deadline = time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if gpsdHasDevice(addr, deadline) {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}

	return false
}

// gpsdHasDevice asks the gpsd at addr for its devices, and reports whether it
// lists any.  A gpsd it can't reach, or that doesn't answer by deadline, has
// none.
func gpsdHasDevice(addr string, deadline time.Time) bool {
	// Only Deadline is relevant here; the rest are fine at their zero values.
	var dialer = net.Dialer{Deadline: deadline} //nolint:exhaustruct_v5

	var conn, dialErr = dialer.DialContext(context.Background(), "tcp", addr)
	if dialErr != nil {
		return false
	}
	defer conn.Close()

	if conn.SetDeadline(deadline) != nil {
		return false
	}

	var _, writeErr = conn.Write([]byte("?DEVICES;\n"))
	if writeErr != nil {
		return false
	}

	// gpsd greets a client with a VERSION report first, so skip to the
	// DEVICES one.
	var scanner = bufio.NewScanner(conn)
	for scanner.Scan() {
		var report struct {
			Class   string            `json:"class"`
			Devices []json.RawMessage `json:"devices"`
		}

		if json.Unmarshal(scanner.Bytes(), &report) != nil || report.Class != "DEVICES" {
			continue
		}

		return len(report.Devices) > 0
	}

	return false
}

//nolint:paralleltest // Serial: relies on a port staying unused, which a parallel test could take.
func Test_dwgpsd_against_real_gpsfake(t *testing.T) {
	var port, portErr = strconv.Atoi(testutils.UnusedPort(t))
	require.NoError(t, portErr)

	startGpsfake(t, port)

	var config = new(Config)
	config.GPSDHost = "127.0.0.1"
	config.GPSDPort = port

	var gps = new(GPS)

	require.Equal(t, 1, dwgpsd_init(t.Context(), gps, config, 3))

	t.Cleanup(gps.Term)

	var info GPSInfo

	// gpsd emits several TPV reports per cycle as each NMEA sentence arrives:
	// a 2D-only one from $GPRMC, then a 3D one still without altitude, then
	// finally the fuller one derived from $GPGGA that carries altitude. Wait
	// for that last one rather than just the first 3D report.  It normally
	// comes within two seconds, but a busy CI runner can be much slower, and
	// waiting longer costs nothing when it does come.
	var deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		info = gps.Read()
		if info.Fix >= DWFIX_3D && info.Altitude.IsJust() {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	require.GreaterOrEqual(t, info.Fix, DWFIX_3D, "never got a 3D location fix from gpsd")
	require.True(t, info.Altitude.IsJust(), "never got an altitude from gpsd")

	assert.InDelta(t, 42.6187, maybe.FromJust(info.Lat), 0.001)
	assert.InDelta(t, -71.3472, maybe.FromJust(info.Lon), 0.001)
	assert.InDelta(t, 33.5, maybe.FromJust(info.Altitude), 0.001)
}

// The checksum is the one the sentence the fixture used to carry has.
func TestNMEASentenceChecksum(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B\n",
		nmeaSentence("GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000"))
}

// fakeGpsdDevices serves every client that connects to it a VERSION report and then,
// once asked for devices, devicesReport.  It returns the address it listens on.
func fakeGpsdDevices(t *testing.T, devicesReport string) string {
	t.Helper()

	var listener, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			var conn, acceptErr = listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				defer conn.Close()

				_, _ = conn.Write([]byte(`{"class":"VERSION","release":"3.25","proto_major":3,"proto_minor":15}` + "\n"))

				var scanner = bufio.NewScanner(conn)
				if scanner.Scan() && scanner.Text() == "?DEVICES;" {
					_, _ = conn.Write([]byte(devicesReport + "\n"))
				}
			}()
		}
	}()

	return listener.Addr().String()
}

// What gpsd answered when gpsfake's device went missing.
func TestGpsdHasDeviceNone(t *testing.T) {
	t.Parallel()

	var addr = fakeGpsdDevices(t, `{"class":"DEVICES","devices":[]}`)

	assert.False(t, gpsdHasDevice(addr, time.Now().Add(5*time.Second)))
	assert.False(t, waitForGpsdDevice(addr, 300*time.Millisecond))
}

func TestGpsdHasDeviceOne(t *testing.T) {
	t.Parallel()

	var addr = fakeGpsdDevices(t, `{"class":"DEVICES","devices":[{"class":"DEVICE","path":"/dev/pts/3","activated":"2026-10-21T00:00:00.000Z","bps":4800}]}`)

	assert.True(t, gpsdHasDevice(addr, time.Now().Add(5*time.Second)))
	assert.True(t, waitForGpsdDevice(addr, 5*time.Second))
}

func TestGpsdHasDeviceUnreachable(t *testing.T) {
	t.Parallel()

	var listener, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	var addr = listener.Addr().String()
	listener.Close()

	assert.False(t, gpsdHasDevice(addr, time.Now().Add(time.Second)))
}
