// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

import (
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/direwolf"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/serialport"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTNC points the global tnc at a pseudo terminal, standing in for the
// serial port to a KISS TNC, and hands back the far end of it.
func fakeTNC(t *testing.T) *os.File {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	var oldTNC = tnc

	tnc = serialport.SerialPortOpen(slave.Name(), 9600)
	require.NotNil(t, tnc)

	// serialport.SerialPortOpen opens the device by name, so this handle is surplus.
	require.NoError(t, slave.Close())

	t.Cleanup(func() {
		tnc.Close()
		tnc = oldTNC
		master.Close()
	})

	return master
}

// readN reads exactly n bytes from f, failing the test rather than hanging
// if they don't turn up.
func readN(t *testing.T, f *os.File, n int) []byte {
	t.Helper()

	var got = make(chan []byte, 1)

	go func() {
		var buf = make([]byte, n)

		var _, err = io.ReadFull(f, buf)
		if err != nil {
			buf = nil
		}

		got <- buf
	}()

	select {
	case buf := <-got:
		require.NotNil(t, buf, "short read from the TNC")

		return buf
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out reading from the TNC")

		return nil
	}
}

func Test_walk96(t *testing.T) {
	var master = fakeTNC(t)

	var oldMYCALL, oldSequence = MYCALL, sequence

	t.Cleanup(func() { MYCALL, sequence = oldMYCALL, oldSequence })

	// The reports are numbered from wherever the count stands, so start it
	// afresh, as main would.
	MYCALL, sequence = "Q1TEST-9", 0

	var output = testutils.CaptureOutput(t, func() {
		walk96(42.61875, -71.347212,
			maybe.Just(5.07), maybe.Just(291.42), maybe.Just(33.5))
	})

	var report = strings.TrimSpace(output)

	assert.Equal(t, "Q1TEST-9>WALK96:!4237.12N/07120.83W=291/005445.925MHz /A=000109Sequence number 0001", report)

	// What went to the TNC is that same report as a KISS data frame for
	// channel 0.
	var pp = ax25.FromText(report, true)
	require.NotNil(t, pp)

	var want = direwolf.KissEncapsulate(append([]byte{0}, pp.Pack()...))

	assert.Equal(t, want, readN(t, master, len(want)))

	// The next report carries the next sequence number.
	output = testutils.CaptureOutput(t, func() {
		walk96(42.61875, -71.347212,
			maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Nothing[float64]())
	})

	assert.Contains(t, output, "Sequence number 0002")
}

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// fakePort is a pseudo terminal standing in for a serial port that main opens
// by name in a process of its own.  Everything written to the port is
// collected, so the test can look at it once main has finished.
type fakePort struct {
	name   string
	master *os.File

	mu       sync.Mutex
	received []byte
}

func newFakePort(t *testing.T) *fakePort {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	var p = new(fakePort)
	p.name = slave.Name()
	p.master = master

	// main opens the device by name, so this handle is surplus - but holding
	// it until the test is over keeps the far end from seeing a hangup when
	// main closes its own.
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})

	go func() {
		var buf = make([]byte, 256)

		for {
			var n, err = master.Read(buf)

			p.mu.Lock()
			p.received = append(p.received, buf[:n]...)
			p.mu.Unlock()

			if err != nil {
				return
			}
		}
	}()

	return p
}

// sawBytes reports whether want has turned up among what was written to the
// port.
func (p *fakePort) sawBytes(want string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return strings.Contains(string(p.received), want)
}

func Test_main_wrong_arguments(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "Q1TEST")

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "Syntax: samoyed-walk96 <CALLSIGN> <TNC Serial Port> <GPS Serial Port>")
}

func Test_main_no_TNC(t *testing.T) {
	t.Parallel()

	var missing = t.TempDir() + "/no-such-tnc"

	var result = testutils.RunMain(t, "", "Q1TEST", missing, missing)

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "Can't open serial port to KISS TNC.")
}

// With no GPS receiver to read, the fix is never anything but "not
// initialised", which main takes as the receiver being unreachable.
func Test_main_no_GPS(t *testing.T) {
	t.Parallel()

	var tncPort = newFakePort(t)

	var result = testutils.RunMain(t, "", "Q1TEST", tncPort.name, t.TempDir()+"/no-such-gps")

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "Can't communicate with GPS receiver.")

	// The TNC was put into KISS mode before the GPS was looked at.
	require.Eventually(t, func() bool { return tncPort.sawBytes("kiss on\rrestart\r") },
		5*time.Second, 10*time.Millisecond)
}

// A GPS receiver that is there but has nothing to say yet gives no fix, which
// main reports and carries on waiting for, until it is interrupted - and then
// takes the TNC back out of KISS mode.
func Test_main_no_fix(t *testing.T) {
	t.Parallel()

	var tncPort = newFakePort(t)
	var gpsPort = newFakePort(t)

	var p = testutils.StartMain(t, "Q1TEST", tncPort.name, gpsPort.name)

	// Printed from inside the loop, so the interrupt handler is in place.
	p.WaitFor(t, "GPS fix not available.")
	p.Signal(t, syscall.SIGINT)

	assert.Equal(t, 0, p.Wait())

	assert.True(t, tncPort.sawBytes("\r\rhbaud 9600\rkiss on\rrestart\r"))
	require.Eventually(t, func() bool { return tncPort.sawBytes("\xc0\xff\xc0") },
		5*time.Second, 10*time.Millisecond)
}

// With a 3D fix from the GPS receiver, main sends a position report to the
// TNC each time round, until it is interrupted.
func Test_main_fix(t *testing.T) {
	t.Parallel()

	var tncPort = newFakePort(t)
	var gpsPort = newFakePort(t)

	var p = testutils.StartMain(t, "Q1TEST-9", tncPort.name, gpsPort.name)

	// The receiver sends before main reads, so the fix is there the first
	// time main looks.  Written again until it is seen, in case the first
	// lot arrives before the port is opened on the far side.
	var done = make(chan struct{})
	var fed sync.WaitGroup

	fed.Go(func() {
		var ticker = time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			_, _ = gpsPort.master.WriteString(
				"$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F\r\n" +
					"$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B\r\n")

			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	})

	var lines = p.WaitFor(t, "Q1TEST-9>WALK96:")

	close(done)
	fed.Wait()

	p.Signal(t, syscall.SIGINT)

	assert.Equal(t, 0, p.Wait())

	var report = lines[len(lines)-1]

	assert.Equal(t, "Q1TEST-9>WALK96:!4237.12N/07120.83W=291/005445.925MHz /A=000109Sequence number 0001", report)

	// The report went to the TNC as a KISS frame for channel 0, and the TNC
	// was then taken out of KISS mode.
	var pp = ax25.FromText(report, true)
	require.NotNil(t, pp)

	var frame = string(direwolf.KissEncapsulate(append([]byte{0}, pp.Pack()...)))

	require.Eventually(t, func() bool { return tncPort.sawBytes("\xc0\xff\xc0") },
		5*time.Second, 10*time.Millisecond)
	assert.True(t, tncPort.sawBytes(frame))
}
