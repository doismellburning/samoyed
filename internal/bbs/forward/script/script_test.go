// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package script

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		var v, ok = vars[name]

		return v, ok
	}
}

// node plays a node at the far end of a pipe: it answers each line it is
// sent with answer's lines, and records what it was sent.
func node(t *testing.T, answer func(line string) []string) (net.Conn, <-chan []string) {
	t.Helper()

	var bbs, far = net.Pipe()

	t.Cleanup(func() { _ = bbs.Close(); _ = far.Close() })

	var heard = make(chan []string, 1)

	go func() {
		var got []string
		var r = lines.NewReader(far, 256)

		for {
			var line, err = r.ReadLine(context.Background())
			if err != nil {
				heard <- got

				return
			}

			got = append(got, line)

			if lines.Write(far, answer(line)...) != nil {
				heard <- got

				return
			}
		}
	}()

	return bbs, heard
}

func TestRunReachesAPartnerThroughANode(t *testing.T) {
	t.Parallel()

	var conn, heard = node(t, func(line string) []string {
		switch line {
		case "C Q3BPQ":
			return []string{"QNOD:Q3NOD} Trying Q3BPQ", "QNOD:Q3NOD} Connected to Q3BPQ"}
		case "sekrit":
			return []string{"Welcome"}
		default:
			return []string{"?"}
		}
	})

	var r = lines.NewReader(conn, 256)
	var steps = []Step{SendLine("C Q3BPQ"), ExpectText("connected TO"), SendVar("PASS"), ExpectText("welcome")}

	require.NoError(t, Run(t.Context(), conn, r, steps, env(map[string]string{"PASS": "sekrit"}), time.Second))
	require.NoError(t, conn.Close())
	assert.Equal(t, []string{"C Q3BPQ", "sekrit"}, <-heard)
}

func TestRunSaysWhichStepFailed(t *testing.T) {
	t.Parallel()

	var conn, _ = node(t, func(string) []string { return []string{"Busy, try later"} })

	var r = lines.NewReader(conn, 256)
	var err = Run(t.Context(), conn, r, []Step{SendLine("C Q3BPQ"), ExpectText("Connected to")}, env(nil), 100*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), `step 2 (expect "Connected to")`)
	assert.Contains(t, err.Error(), `last heard "Busy, try later"`)
}

func TestRunKeepsSecretsOutOfErrors(t *testing.T) {
	t.Parallel()

	var conn, _ = node(t, func(string) []string { return nil })

	require.NoError(t, conn.Close())

	var r = lines.NewReader(conn, 256)
	var err = Run(t.Context(), conn, r, []Step{SendVar("PASS")}, env(map[string]string{"PASS": "sekrit"}), time.Second)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sekrit")
	assert.Contains(t, err.Error(), "send $PASS")

	var unset = Run(t.Context(), io.Discard, r, []Step{SendVar("NOPE")}, env(nil), time.Second)
	require.ErrorIs(t, unset, ErrUnset)
}

func TestRunWithTheFarEndGone(t *testing.T) {
	t.Parallel()

	var r = lines.NewReader(strings.NewReader("Trying\r"), 256)

	var err = Run(t.Context(), io.Discard, r, []Step{ExpectText("Connected")}, env(nil), time.Second)
	require.ErrorIs(t, err, io.EOF)
}

func TestValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, SendLine("C Q3BPQ").Validate())
	require.NoError(t, SendVar("PASS").Validate())
	require.NoError(t, ExpectText("Connected").Validate())
	require.Error(t, new(Step).Validate())
	require.Error(t, Step{Send: "C Q3BPQ", SendEnv: "", Expect: "Connected"}.Validate())
}

func FuzzRun(f *testing.F) {
	f.Add("Connected", "QNOD:Q3NOD} Connected to Q3BPQ\r")
	f.Add("", "\r\n\x00")
	f.Add("x", "")

	f.Fuzz(func(t *testing.T, want string, heard string) {
		var r = lines.NewReader(strings.NewReader(heard), 256)

		var err = Run(t.Context(), io.Discard, r, []Step{ExpectText(want)}, env(nil), time.Second)
		if err == nil {
			assert.Contains(t, strings.ToLower(heard), strings.ToLower(want))
		}
	})
}
