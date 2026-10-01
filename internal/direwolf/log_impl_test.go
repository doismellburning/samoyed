// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression test for a bug where heard[:4] and heard[4] were indexed
// before checking len(heard) == 5, causing a panic whenever the heard
// station's callsign was shorter than 4 characters.
func TestLogRRBitsShortHeardDoesNotPanic(t *testing.T) {
	t.Parallel()

	var pp = ax25.FromText("Q1TEST>APRS,Q2TEST*,AB*:test", true)
	if pp == nil {
		t.Fatal("failed to parse test packet")
	}

	if pp.Heard() < ax25.Repeater2 {
		t.Fatal("test packet did not set up heard station at or beyond AX25_REPEATER_2")
	}

	var A Decoded

	var pl = NewPacketLogger(false, "")
	pl.RRBits(&A, pp)
}

const logHeader = "chan,utime,isotime,source,heard,level,error,dti,name,symbol,latitude,longitude,speed,course,altitude,frequency,offset,tone,system,status,telemetry,comment"

func logNoLevel() ax25.ALevel { return ax25.ALevel{Rec: -1, Mark: 0, Space: 0} }

func readLogRecords(t *testing.T, path string) [][]string {
	t.Helper()

	var f, err = os.Open(path) //nolint:gosec // Test-controlled path
	require.NoError(t, err)

	defer f.Close()

	var records, readErr = csv.NewReader(f).ReadAll()
	require.NoError(t, readErr)

	return records
}

func fullLogAprs() *Decoded {
	var A = new(Decoded)
	A.Src = "Q1TEST"
	A.Name = "OBJNAME"
	A.SymbolTable = '/'
	A.SymbolCode = '>'
	A.Lat = maybe.Just(51.5)
	A.Lon = maybe.Just(-0.125)
	A.SpeedMPH = maybe.Just(11.5078)
	A.Course = maybe.Just(90.0)
	A.AltitudeFt = maybe.Just(1000.0)
	A.Freq = maybe.Just(146.52)
	A.Offset = maybe.Just(-600)
	A.Tone = maybe.Just(100.0)
	A.Mfr = "Maker, Inc"
	A.MicEStatus = "En Route"
	A.Telemetry = "T#001"
	A.Comment = "hello, \"world\""

	return A
}

func TestLogNewPacketLoggerEmptyPathDisabled(t *testing.T) {
	t.Parallel()

	var pl = NewPacketLogger(true, "")
	assert.Empty(t, pl.logPath)

	// Nothing should happen, and nothing should panic.
	pl.Write(0, new(Decoded), nil, logNoLevel(), 0)
	assert.Nil(t, pl.logFp)
	pl.Close()
}

func TestLogNewPacketLoggerDailyExistingDir(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	var pl = NewPacketLogger(true, dir)
	assert.Equal(t, dir, pl.logPath)
	assert.True(t, pl.dailyNames)
}

func TestLogNewPacketLoggerDailyCreatesDir(t *testing.T) {
	t.Parallel()

	var dir = filepath.Join(t.TempDir(), "logs")
	var pl = NewPacketLogger(true, dir)
	assert.Equal(t, dir, pl.logPath)

	var stat, err = os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, stat.IsDir())
}

func TestLogNewPacketLoggerDailyNotADirFallsBack(t *testing.T) {
	t.Parallel()

	var file = filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0600))

	var pl = NewPacketLogger(true, file)
	assert.Equal(t, ".", pl.logPath)
}

func TestLogNewPacketLoggerDailyMkdirFailsFallsBack(t *testing.T) {
	t.Parallel()

	// Parent doesn't exist, and we don't mkdir -p.
	var dir = filepath.Join(t.TempDir(), "missing", "logs")
	var pl = NewPacketLogger(true, dir)
	assert.Equal(t, ".", pl.logPath)
}

func TestLogWriteSingleFileAllFields(t *testing.T) {
	t.Parallel()

	var path = filepath.Join(t.TempDir(), "packets.log")
	var pl = NewPacketLogger(false, path)
	assert.Equal(t, path, pl.logPath)

	var pp = ax25.FromText("Q1TEST>APRS,Q2TEST*,WIDE2*:!5130.00N/00007.50W>hello", true)
	require.NotNil(t, pp)

	var before = time.Now().Unix()

	pl.Write(3, fullLogAprs(), pp, ax25.ALevel{Rec: 50, Mark: 1, Space: 2}, 2)

	var after = time.Now().Unix()

	pl.Close()
	assert.Nil(t, pl.logFp)

	var records = readLogRecords(t, path)
	require.Len(t, records, 2)
	assert.Equal(t, strings.Split(logHeader, ","), records[0])

	var r = records[1]
	require.Len(t, r, 22)
	assert.Equal(t, "3", r[0])

	var utime, err = strconv.ParseInt(r[1], 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, utime, before)
	assert.LessOrEqual(t, utime, after)

	var itime, parseErr = time.Parse("2006-01-02T15:04:05Z", r[2])
	require.NoError(t, parseErr)
	assert.Equal(t, utime, itime.Unix())

	assert.Equal(t, []string{
		"Q1TEST",
		"Q2TEST?", // WIDE2 heard, so guess the previous digipeater
		"50(1/2)",
		"2",
		"!",
		"OBJNAME",
		"/>",
		"51.500000", "-0.125000",
		"10.0",  // knots
		"90.0",  // course
		"304.8", // metres
		"146.520", "-600", "100.0",
		"Maker, Inc", "En Route", "T#001", "hello, \"world\"",
	}, r[3:])
}

func TestLogWriteSingleFileAppendsWithoutSecondHeader(t *testing.T) {
	t.Parallel()

	var path = filepath.Join(t.TempDir(), "packets.log")

	var A = new(Decoded)
	A.Src = "Q1TEST"

	var pl = NewPacketLogger(false, path)
	pl.Write(0, A, nil, logNoLevel(), 0)
	pl.Write(1, A, nil, logNoLevel(), 0)
	pl.Close()

	// A new logger on an existing file must not write the header again.
	var pl2 = NewPacketLogger(false, path)
	pl2.Write(2, A, nil, logNoLevel(), 0)
	pl2.Close()

	var records = readLogRecords(t, path)
	require.Len(t, records, 4)
	assert.Equal(t, "chan", records[0][0])

	for i, r := range records[1:] {
		assert.Equal(t, strconv.Itoa(i), r[0])
		assert.Equal(t, "Q1TEST", r[3])
		assert.Empty(t, r[4], "no packet, so nothing heard")
		assert.Empty(t, r[5], "negative level is blank")
		assert.Empty(t, r[7], "no packet, so no DTI")
		assert.Equal(t, "Q1TEST", r[8], "name falls back to source")

		for j := 10; j <= 17; j++ {
			assert.Empty(t, r[j], "column %d", j)
		}
	}
}

func TestLogWriteDCSOverridesTone(t *testing.T) {
	t.Parallel()

	var path = filepath.Join(t.TempDir(), "packets.log")

	var A = new(Decoded)
	A.Src = "Q1TEST"
	A.Tone = maybe.Just(100.0)
	A.DCS = maybe.Just(0o23)

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)
	require.NotNil(t, pp)

	var pl = NewPacketLogger(false, path)
	pl.Write(0, A, pp, ax25.ALevel{Rec: 10, Mark: -1, Space: -1}, 0)
	pl.Close()

	var records = readLogRecords(t, path)
	require.Len(t, records, 2)
	assert.Equal(t, "Q1TEST", records[1][4], "heard directly")
	assert.Equal(t, "10", records[1][5])
	assert.Equal(t, ">", records[1][7])
	assert.Equal(t, "D023", records[1][17])
}

func TestLogWriteSingleFileOpenFails(t *testing.T) {
	t.Parallel()

	var path = filepath.Join(t.TempDir(), "missing", "packets.log")
	var pl = NewPacketLogger(false, path)

	pl.Write(0, new(Decoded), nil, logNoLevel(), 0)

	assert.Nil(t, pl.logFp)
	assert.Empty(t, pl.logPath, "logging is disabled after a failed open")
	assert.NoFileExists(t, path)

	// Subsequent writes are no-ops.
	pl.Write(0, new(Decoded), nil, logNoLevel(), 0)
	assert.Nil(t, pl.logFp)
}

func TestLogWriteDailyNames(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	var pl = NewPacketLogger(true, dir)

	var A = new(Decoded)
	A.Src = "Q1TEST"

	pl.Write(0, A, nil, logNoLevel(), 0)
	require.NotNil(t, pl.logFp)
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}\.log$`, pl.openFname)

	// Pretend the date has changed since the file was opened: the current
	// file should be closed and today's reopened (for append, no new header).
	var firstFp = pl.logFp
	pl.openFname = "1999-12-31.log"

	pl.Write(1, A, nil, logNoLevel(), 0)
	require.NotNil(t, pl.logFp)
	assert.NotSame(t, firstFp, pl.logFp)

	pl.Close()
	assert.Nil(t, pl.logFp)
	assert.Empty(t, pl.openFname)

	var entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	// Normally one file, but allow for the test straddling midnight UTC.
	var total = 0

	for _, e := range entries {
		var records = readLogRecords(t, filepath.Join(dir, e.Name()))
		require.NotEmpty(t, records)
		assert.Equal(t, "chan", records[0][0])

		total += len(records) - 1
	}

	assert.Equal(t, 2, total)
}

func TestLogWriteDailyOpenFails(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()

	// Put directories where today's (and, in case we straddle midnight UTC,
	// tomorrow's) log files would go, so opening them for write fails.
	var now = time.Now().UTC()
	for _, d := range []time.Time{now, now.Add(time.Minute)} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d.Format("2006-01-02.log")), 0750))
	}

	var pl = NewPacketLogger(true, dir)
	pl.Write(0, new(Decoded), nil, logNoLevel(), 0)

	assert.Nil(t, pl.logFp)
	assert.Empty(t, pl.openFname)
	assert.Equal(t, dir, pl.logPath, "daily logging stays enabled to retry later")
}

func TestLogRRBits(t *testing.T) {
	t.Parallel()

	var pl = NewPacketLogger(false, "")

	var A = new(Decoded)
	A.Src = "Q1TEST"
	A.Mfr = "Maker, Inc"

	for _, text := range []string{
		"Q1TEST>APRS:>status",
		"Q1TEST>APRS,Q2TEST*,WIDE2*:>status",
	} {
		var pp = ax25.FromText(text, true)
		require.NotNil(t, pp, text)
		pl.RRBits(A, pp)
	}

	// No packet: nothing printed, no panic.
	pl.RRBits(A, nil)
}
