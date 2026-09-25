// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package testutils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// raceWarning is how the race detector starts its report of a data race.  A
// command run from a test binary built with -race has the detector too, but
// one that goes on running, or that the test kills, never gets as far as the
// exit status that would give it away - so its output is watched for this.
const raceWarning = "WARNING: DATA RACE"

// ProcessTimeout is how long a command run by these helpers is given before it
// is killed, so that one which hangs fails its test rather than stalling it.
//
//nolint:gochecknoglobals // A variable so that these helpers' own tests can see one time out.
var ProcessTimeout = 30 * time.Second

// BuildCommand builds the command whose tests are running - the package in the
// current directory - and returns the path to the binary.  Tests that need the
// real thing, such as what a signal does to the process, run that.
func BuildCommand(t *testing.T) string {
	t.Helper()

	var wd, wdErr = os.Getwd()
	require.NoError(t, wdErr)

	var binary = filepath.Join(t.TempDir(), filepath.Base(wd))

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec // Building ourselves.

	var out, err = build.CombinedOutput()
	require.NoError(t, err, "Building the command failed: %s", out)

	return binary
}

// runMainEnv, when set, has a test binary that calls RunMainIfAsked run main
// with the arguments it holds, JSON-encoded, instead of its tests.
const runMainEnv = "SAMOYED_TESTUTILS_RUN_MAIN"

// RunMainIfAsked runs main in place of the tests, and exits, when RunMain has
// started this test binary to stand in for the command.  Call it first thing
// in TestMain.
func RunMainIfAsked(main func()) {
	var args, ok = os.LookupEnv(runMainEnv)
	if !ok {
		return
	}

	var argv []string

	var decodeErr = json.Unmarshal([]byte(args), &argv)
	if decodeErr != nil {
		panic(decodeErr)
	}

	var wd, wdErr = os.Getwd()
	if wdErr != nil {
		panic(wdErr)
	}

	os.Args = append([]string{filepath.Base(wd)}, argv...)

	main()
	os.Exit(0)
}

// encodeArgs puts args in a form that survives the trip through the
// environment intact, spaces and all.
func encodeArgs(t *testing.T, args []string) string {
	t.Helper()

	// errchkjson only recognises the error being checked from := assignment.
	encoded, err := json.Marshal(args)
	require.NoError(t, err)

	return string(encoded)
}

// Result is what a command did, as RunMain saw it.
type Result struct {
	Stdout string
	Stderr string
	Status int
}

// Output is everything the command printed, stdout then stderr.
func (r Result) Output() string {
	return r.Stdout + r.Stderr
}

// RunMain runs the command's main with args, and stdin as its standard input,
// in a process of its own and waits for it to finish.  That process is this
// test binary, which RunMainIfAsked turns into the command, so a test can see
// main exit - and the coverage it earns still counts.
func RunMain(t *testing.T, stdin string, args ...string) Result {
	t.Helper()

	var ctx, cancel = context.WithTimeout(t.Context(), ProcessTimeout)
	defer cancel()

	var cmd = exec.CommandContext(ctx, os.Args[0]) //nolint:gosec // Running ourselves.
	cmd.Env = append(os.Environ(), runMainEnv+"="+encodeArgs(t, args))
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	var runErr = cmd.Run()

	// Killed for taking too long, it has an exit status, but not one that
	// says anything about what it was doing.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		require.FailNow(t, fmt.Sprintf("The command didn't finish within %s", ProcessTimeout), "Its output:\n%s%s", stdout.String(), stderr.String())
	}

	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		require.NoError(t, runErr)
	}

	if strings.Contains(stderr.String(), raceWarning) {
		t.Errorf("The command hit a data race:\n%s", stderr.String())
	}

	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Status: cmd.ProcessState.ExitCode()}
}

// Process is a command started by Start, which carries on running while the
// test talks to it.
type Process struct {
	// Stdin is the command's standard input.  The command sees end of file
	// once it is closed.
	Stdin *os.File

	lines <-chan string

	// output is everything the command has printed, for reporting a data
	// race, which it may do in the middle of things the test is reading.
	outputMu sync.Mutex
	output   strings.Builder

	ctx      context.Context //nolint:containedctx // The command's own, to tell a timeout from a kill.
	cmd      *exec.Cmd
	once     sync.Once
	status   int
	timedOut bool
}

// Start runs binary with args, and hands back the running process.  It is
// killed when the test ends, if it hasn't exited by then, and in any case
// after ProcessTimeout.
func Start(t *testing.T, binary string, args ...string) *Process {
	t.Helper()

	return start(t, nil, binary, args...)
}

// StartMain is Start for the command's main, run as RunMain runs it, for a
// test that talks to the command while it is running.
func StartMain(t *testing.T, args ...string) *Process {
	t.Helper()

	return start(t, []string{runMainEnv + "=" + encodeArgs(t, args)}, os.Args[0])
}

func start(t *testing.T, env []string, binary string, args ...string) *Process {
	t.Helper()

	var ctx, cancel = context.WithTimeout(context.Background(), ProcessTimeout)

	var cmd = exec.CommandContext(ctx, binary, args...) //nolint:gosec // The command under test.
	cmd.Env = append(os.Environ(), env...)

	var stdinReader, stdinWriter, stdinErr = os.Pipe()
	require.NoError(t, stdinErr)

	// Both stdout and stderr, in the order the command wrote them.
	var outputReader, outputWriter, outputErr = os.Pipe()
	require.NoError(t, outputErr)

	cmd.Stdin = stdinReader
	cmd.Stdout = outputWriter
	cmd.Stderr = outputWriter

	require.NoError(t, cmd.Start())

	// The command has its own copies now.  Letting go of ours means the
	// command's exit is the end of its output.
	stdinReader.Close()
	outputWriter.Close()

	var lines = make(chan string, 1000)

	var p = new(Process)
	p.Stdin = stdinWriter
	p.lines = lines
	p.ctx = ctx
	p.cmd = cmd

	go func() {
		defer close(lines)
		defer outputReader.Close()

		var scanner = bufio.NewScanner(outputReader)
		for scanner.Scan() {
			p.outputMu.Lock()
			p.output.WriteString(scanner.Text() + "\n")
			p.outputMu.Unlock()

			lines <- scanner.Text()
		}
	}()

	t.Cleanup(func() {
		cancel()
		p.Wait()
		stdinWriter.Close()

		p.outputMu.Lock()
		defer p.outputMu.Unlock()

		// Killed for taking too long, whatever it has been saying or doing
		// can't be relied on, so the test can't have passed.
		if p.timedOut {
			t.Errorf("The command didn't finish within %s.  Its output:\n%s", ProcessTimeout, p.output.String())
		}

		if strings.Contains(p.output.String(), raceWarning) {
			t.Errorf("The command hit a data race:\n%s", p.output.String())
		}
	})

	return p
}

// WaitFor reads what the command prints until a line contains want, failing
// the test if none does before the command exits or in good time.  It hands
// back every line it read.
func (p *Process) WaitFor(t *testing.T, want string) []string {
	t.Helper()

	var timeout = time.After(ProcessTimeout)

	var seen []string

	for {
		select {
		case line, ok := <-p.lines:
			require.True(t, ok, "output ended without %q:\n%s", want, strings.Join(seen, "\n"))

			seen = append(seen, line)

			if strings.Contains(line, want) {
				return seen
			}
		case <-timeout:
			require.FailNow(t, "timed out waiting for output", "wanted %q, got:\n%s", want, strings.Join(seen, "\n"))
		}
	}
}

// Signal sends the command sig, as a user interrupting it would.
func (p *Process) Signal(t *testing.T, sig os.Signal) {
	t.Helper()

	require.NoError(t, p.cmd.Process.Signal(sig))
}

// Wait waits for the command to exit, and returns its exit status.  Whatever
// it printed that WaitFor hadn't read is discarded.  A command killed for
// taking longer than ProcessTimeout fails the test once it is over.
func (p *Process) Wait() int {
	p.once.Do(func() {
		for range p.lines { // Drain what's left so Wait can finish.
		}

		_ = p.cmd.Wait()
		p.status = p.cmd.ProcessState.ExitCode()
		p.timedOut = errors.Is(p.ctx.Err(), context.DeadlineExceeded)
	})

	return p.status
}
