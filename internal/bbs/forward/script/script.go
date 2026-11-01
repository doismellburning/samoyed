// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package script is connect scripts: the lines a BBS sends, and the answers
// it waits for, to reach a partner it can't connect to directly - through a
// node, say, which has to be told where to connect on to.  Once the script
// has run, the session is with the partner itself.
package script

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/lines"
)

// ErrUnset says a step sends an environment variable that isn't set.
var ErrUnset = errors.New("environment variable not set")

// Step is one step of a script: exactly one of its fields is set.
type Step struct {
	// Send is a line to send.
	Send string `yaml:"send"`

	// SendEnv names an environment variable whose value is sent as a
	// line, for a password, so that it needn't be written in the
	// configuration.  The value is never logged or put in an error.
	SendEnv string `yaml:"sendEnv"`

	// Expect is text to wait for, case-insensitively, in a line from the
	// far end.  Lines without it are passed over.
	Expect string `yaml:"expect"`
}

// SendLine is a step sending line.
func SendLine(line string) Step { return Step{Send: line, SendEnv: "", Expect: ""} }

// SendVar is a step sending the value of the environment variable name.
func SendVar(name string) Step { return Step{Send: "", SendEnv: name, Expect: ""} }

// ExpectText is a step waiting for a line containing text.
func ExpectText(text string) Step { return Step{Send: "", SendEnv: "", Expect: text} }

// Validate checks the step has exactly one thing to do.
func (s Step) Validate() error {
	var set = 0

	for _, field := range []string{s.Send, s.SendEnv, s.Expect} {
		if field != "" {
			set++
		}
	}

	if set != 1 {
		return errors.New("a step needs exactly one of send, sendEnv and expect")
	}

	return nil
}

func (s Step) String() string {
	switch {
	case s.Expect != "":
		return fmt.Sprintf("expect %q", s.Expect)
	case s.SendEnv != "":
		return "send $" + s.SendEnv
	default:
		return fmt.Sprintf("send %q", s.Send)
	}
}

// Run runs steps over a session: w is where lines are sent, and r is where
// the far end's lines are read from, so that whatever follows the script
// carries on reading from r.  Each expect waits at most timeout.
func Run(ctx context.Context, w io.Writer, r *lines.Reader, steps []Step, lookupEnv func(name string) (string, bool), timeout time.Duration) error {
	var lastHeard = ""

	for i, step := range steps {
		var err error

		switch {
		case step.Expect != "":
			lastHeard, err = expect(ctx, r, step.Expect, timeout, lastHeard)
		case step.SendEnv != "":
			var value, ok = lookupEnv(step.SendEnv)
			if !ok {
				err = fmt.Errorf("%w: %s", ErrUnset, step.SendEnv)
			} else {
				err = lines.Write(w, value)
			}
		default:
			err = lines.Write(w, step.Send)
		}

		if err != nil {
			return fmt.Errorf("connect script step %d (%s): %w; last heard %q", i+1, step, err, lastHeard)
		}
	}

	return nil
}

// expect reads lines until one contains want, giving up after timeout, and
// returns the last line heard.
func expect(ctx context.Context, r *lines.Reader, want string, timeout time.Duration, lastHeard string) (string, error) {
	var stepCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	var lower = strings.ToLower(want)

	for {
		var line, err = r.ReadLine(stepCtx)
		if err != nil {
			return lastHeard, err
		}

		lastHeard = line

		if strings.Contains(strings.ToLower(line), lower) {
			return lastHeard, nil
		}
	}
}
