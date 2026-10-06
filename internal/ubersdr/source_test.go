// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ubersdr

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasPrefix(t *testing.T) {
	assert.True(t, HasPrefix("ubersdr:https://sdr.example.org/"))
	assert.True(t, HasPrefix("UberSDR:https://sdr.example.org/"))
	assert.False(t, HasPrefix("ubersdr"))
	assert.False(t, HasPrefix("udp:7355"))
	assert.False(t, HasPrefix("default"))
}

func TestParseSource(t *testing.T) {
	var src, err = ParseSource("ubersdr:https://sdr.example.org/?frequency=10147600")
	require.NoError(t, err)

	assert.Equal(t, uint64(10147600), src.Frequency)
	assert.Equal(t, "usb", src.Mode, "mode defaults to USB")
	assert.Equal(t, 12000, src.SampleRate)
	assert.Equal(t, 1, src.Channels)
	assert.Equal(t, "https://sdr.example.org/connection", src.connectionURL())

	var ws, wsErr = url.Parse(src.webSocketURL("Q1TEST-session"))
	require.NoError(t, wsErr)
	assert.Equal(t, "wss", ws.Scheme)
	assert.Equal(t, "sdr.example.org", ws.Host)
	assert.Equal(t, "/ws", ws.Path)
	assert.Equal(t, url.Values{
		"frequency":       {"10147600"},
		"mode":            {"usb"},
		"format":          {"pcm-zstd"},
		"version":         {"4"},
		"user_session_id": {"Q1TEST-session"},
	}, ws.Query())
}

func TestParseSource_everything(t *testing.T) {
	var src, err = ParseSource("ubersdr:http://sdr.example.org:8080/radio/?frequency=7000000&mode=NFM&bandwidthLow=-5000&bandwidthHigh=5000&password=Q1TEST-secret")
	require.NoError(t, err)

	assert.Equal(t, "nfm", src.Mode)
	assert.Equal(t, 24000, src.SampleRate)
	assert.Equal(t, "http://sdr.example.org:8080/radio/connection", src.connectionURL())

	var ws, wsErr = url.Parse(src.webSocketURL("Q1TEST-session"))
	require.NoError(t, wsErr)
	assert.Equal(t, "ws", ws.Scheme)
	assert.Equal(t, "sdr.example.org:8080", ws.Host)
	assert.Equal(t, "/radio/ws", ws.Path)
	assert.Equal(t, "-5000", ws.Query().Get("bandwidthLow"))
	assert.Equal(t, "5000", ws.Query().Get("bandwidthHigh"))
	assert.Equal(t, "Q1TEST-secret", ws.Query().Get("password"))

	assert.NotContains(t, src.String(), "Q1TEST-secret", "the password stays out of the logs")
	assert.Contains(t, src.String(), "sdr.example.org:8080")
}

func TestParseSource_modeSampleRates(t *testing.T) {
	for mode, rate := range map[string]int{
		"usb": 12000, "lsb": 12000, "cwu": 12000, "cwl": 12000,
		"am": 24000, "sam": 24000, "fm": 24000, "nfm": 24000,
	} {
		var src, err = ParseSource("ubersdr:https://sdr.example.org/?frequency=10147600&mode=" + mode)
		require.NoError(t, err, mode)
		assert.Equal(t, rate, src.SampleRate, mode)
	}
}

func TestParseSource_errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"not ubersdr", "udp:7355", "does not start with"},
		{"no frequency", "ubersdr:https://sdr.example.org/", "frequency=<Hz> is required"},
		{"bad frequency", "ubersdr:https://sdr.example.org/?frequency=10.1476", "whole number of Hz"},
		{"zero frequency", "ubersdr:https://sdr.example.org/?frequency=0", "whole number of Hz"},
		{"unknown mode", "ubersdr:https://sdr.example.org/?frequency=10147600&mode=dsb", "mode must be"},
		{"IQ mode", "ubersdr:https://sdr.example.org/?frequency=10147600&mode=iq", "mode must be"},
		{"wide IQ mode", "ubersdr:https://sdr.example.org/?frequency=10147600&mode=iq96", "mode must be"},
		{"misspelt parameter", "ubersdr:https://sdr.example.org/?frequency=10147600&mdoe=lsb", `unknown parameter "mdoe"`},
		{"repeated parameter", "ubersdr:https://sdr.example.org/?frequency=10147600&frequency=7000000", "given 2 times"},
		{"bad bandwidth", "ubersdr:https://sdr.example.org/?frequency=10147600&bandwidthLow=wide", "bandwidthLow must be"},
		{"WebSocket URL", "ubersdr:wss://sdr.example.org/?frequency=10147600", "http:// or https://"},
		{"no host", "ubersdr:https:///?frequency=10147600", "no host"},
		{"credentials", "ubersdr:https://Q1TEST:secret@sdr.example.org/?frequency=10147600", "password="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var _, err = ParseSource(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestNewSessionID(t *testing.T) {
	var a, errA = newSessionID()
	require.NoError(t, errA)

	var b, errB = newSessionID()
	require.NoError(t, errB)

	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, a)
	assert.NotEqual(t, a, b)
}
