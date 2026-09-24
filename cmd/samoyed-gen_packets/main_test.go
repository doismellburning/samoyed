// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// main parses its flags from the global pflag.CommandLine, which can only be
// done once per process, so this is the only test that can call it.
func Test_main_roundTrip(t *testing.T) {
	var dir = t.TempDir()

	var messages = filepath.Join(dir, "messages.txt")
	require.NoError(t, os.WriteFile(messages, []byte(
		"Q1TEST>APDW17:>First\n"+
			"Q2TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#Second\n",
	), 0o600))

	var wav = filepath.Join(dir, "out.wav")

	var oldArgs = os.Args

	defer func() { os.Args = oldArgs }()

	os.Args = []string{"gen_packets", "-o", wav, messages}

	main()

	var opts = new(direwolf.AtestOptions)
	opts.IL2PVersion = "0.6"

	var atest, atestErr = direwolf.NewAtest(opts)
	require.NoError(t, atestErr)

	var result, decodeErr = atest.DecodeFile(wav)
	require.NoError(t, decodeErr)

	assert.Equal(t, 2, result.PacketsDecoded)
}
