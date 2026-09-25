// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nmea is a position report from a receiver with a 3D fix: $GPRMC for the
// speed and course, $GPGGA for the altitude.
const nmea = "$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F\r\n" +
	"$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B\r\n"

// fakeGPS hands back the name of a pseudo terminal whose far end repeats a
// position report until the test is over, standing in for a GPS receiver.
func fakeGPS(t *testing.T) string {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	var name = slave.Name()

	// The port is opened again by name; the terminal lives on as long as the
	// master is open.
	require.NoError(t, slave.Close())

	var ctx, cancel = context.WithCancel(context.Background())

	var done = make(chan struct{})

	go func() {
		defer close(done)

		// Repeat it, since opening the port can discard whatever was
		// already waiting there.
		for ctx.Err() == nil {
			_, _ = master.WriteString(nmea)

			time.Sleep(50 * time.Millisecond)
		}
	}()

	t.Cleanup(func() {
		cancel()
		<-done
		master.Close()
	})

	return name
}

func Test_show(t *testing.T) {
	assert.Equal(t, "1.50", show("%.2f", maybe.Just(1.5)))
	assert.Equal(t, "unknown", show("%.2f", maybe.Nothing[float64]()))
}

func Test_run_reportsAFix(t *testing.T) {
	var port = fakeGPS(t)

	// The first reading comes shortly after starting up, and the next not for
	// another three seconds, by which time this has stopped it.
	var ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	var out strings.Builder

	var status = run(ctx, []string{port}, &out)

	assert.Equal(t, 0, status)
	assert.Contains(t, out.String(), "42.618750  -71.347212")
	assert.Contains(t, out.String(), "altitude = 33.5 meters")
}
