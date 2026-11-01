// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/routing"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	var cfg, err = Parse([]byte(`
call: q1bbs-3
hroute: "#77.gbr.euro"
store: /var/lib/samoyed-bbs/bbs.db
banner: |
  Welcome to the club BBS.
  Sysop: Q1TEST
prompt: "Q1BBS>"
idleTimeout: 5m
`))
	require.NoError(t, err)

	assert.Equal(t, "Q1BBS-3", cfg.Call)
	assert.Equal(t, maybe.Just("#77.GBR.EURO"), cfg.HRouteMaybe())
	assert.Equal(t, "/var/lib/samoyed-bbs/bbs.db", cfg.Store)
	assert.Equal(t, []string{"Welcome to the club BBS.", "Sysop: Q1TEST"}, cfg.BannerLines())
	assert.Equal(t, "Q1BBS>", cfg.Prompt)
	assert.Equal(t, 5*time.Minute, cfg.IdleTimeout)
}

func TestParseDefaults(t *testing.T) {
	t.Parallel()

	var cfg, err = Parse([]byte("call: Q1BBS\nstore: bbs.db\n"))
	require.NoError(t, err)

	assert.Equal(t, []string{"Welcome to Q1BBS, a Samoyed BBS (experimental)."}, cfg.BannerLines())
	assert.Equal(t, "de Q1BBS>", cfg.Prompt)
	assert.Equal(t, DefaultIdleTimeout, cfg.IdleTimeout)
	assert.True(t, cfg.HRouteMaybe().IsNothing())
}

func TestParseReportsEveryProblem(t *testing.T) {
	t.Parallel()

	var _, err = Parse([]byte("call: Q1BBS-16\nhroute: GBR..EURO\nidleTimeout: -1s\n"))
	require.Error(t, err)

	assert.Contains(t, err.Error(), `hroute: "GBR..EURO" is not a hierarchical address`)
	assert.Contains(t, err.Error(), `call: "Q1BBS-16" is not a callsign`)
	assert.Contains(t, err.Error(), "store: the path of the message database is needed")
	assert.Contains(t, err.Error(), "idleTimeout: -1s is negative")
}

func TestParseRefusesUnknownSettings(t *testing.T) {
	t.Parallel()

	var _, err = Parse([]byte("call: Q1BBS\nstore: bbs.db\nsysop: Q1TEST\n"))
	require.ErrorContains(t, err, "sysop")
}

func TestParseRefusesNonsense(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"call: [", "idleTimeout: soon\n", ""} {
		var _, err = Parse([]byte(data))
		require.Error(t, err, "%q", data)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	var file = filepath.Join(t.TempDir(), "samoyed-bbs.yaml")
	require.NoError(t, os.WriteFile(file, []byte("call: Q1BBS\nstore: bbs.db\n"), 0o600))

	var cfg, err = Load(file)
	require.NoError(t, err)
	assert.Equal(t, "Q1BBS", cfg.Call)

	var _, missingErr = Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorIs(t, missingErr, os.ErrNotExist)
}

func TestValidCall(t *testing.T) {
	t.Parallel()

	for _, call := range []string{"Q1TEST", "Q1TEST-0", "Q1TEST-15", "Q1"} {
		assert.True(t, ValidCall(call), call)
	}

	for _, call := range []string{"", "q1test", "Q1TEST-16", "Q1TESTX", "Q1TEST-", "Q1 TEST"} {
		assert.False(t, ValidCall(call), call)
	}
}

// env is an environment with just the variables given.
func env(vars map[string]string) func(name string) (string, bool) {
	return func(name string) (string, bool) {
		var value, ok = vars[name]

		return value, ok
	}
}

func TestParseAGW(t *testing.T) {
	t.Parallel()

	var cfg, err = ParseWithEnv([]byte(`
call: Q1BBS
store: bbs.db
agw:
  addr: localhost:8000
  user: Q1BBS
  passwordEnv: BBS_AGW_PASSWORD
  ports: [0, 2]
  paclen: 256
  highWater: 4
`), env(map[string]string{"BBS_AGW_PASSWORD": "correct horse"}))
	require.NoError(t, err)

	var client, ok = cfg.AGWClient().Get()
	require.True(t, ok)

	assert.Equal(t, "localhost:8000", client.Addr)
	assert.Equal(t, "Q1BBS", client.Call)
	assert.Equal(t, []int{0, 2}, client.Ports)
	assert.Equal(t, 256, client.PacLen)
	assert.Equal(t, uint32(4), client.HighWater)

	var login, hasLogin = client.Login.Get()
	require.True(t, hasLogin)
	assert.Equal(t, "Q1BBS", login.User)
	assert.Equal(t, "correct horse", login.Password, "the password comes from the environment")
}

func TestParseAGWDefaults(t *testing.T) {
	t.Parallel()

	var cfg, err = ParseWithEnv([]byte("call: Q1BBS\nstore: bbs.db\nagw:\n  addr: localhost:8000\n"), env(nil))
	require.NoError(t, err)

	var client, ok = cfg.AGWClient().Get()
	require.True(t, ok)

	assert.Empty(t, client.Ports, "every port the server has")
	assert.True(t, client.Login.IsNothing())
}

func TestParseWithoutAGW(t *testing.T) {
	t.Parallel()

	var cfg, err = ParseWithEnv([]byte("call: Q1BBS\nstore: bbs.db\n"), env(nil))
	require.NoError(t, err)
	assert.True(t, cfg.AGWClient().IsNothing())
}

func TestParseAGWProblems(t *testing.T) {
	t.Parallel()

	var _, err = ParseWithEnv([]byte(`
call: Q1BBS
store: bbs.db
agw:
  addr: localhost
  user: Q1BBS
  ports: [0, 256]
  paclen: 4096
`), env(nil))
	require.Error(t, err)

	for _, want := range []string{
		`agw.addr: "localhost" is not a host:port`,
		"agw.passwordEnv: a user name needs the variable its password is in",
		"agw.ports: 256 is not a port from 0 to 255",
		"agw.paclen: 4096 is not from 1 to 2048",
	} {
		assert.Contains(t, err.Error(), want)
	}

	var _, unsetErr = ParseWithEnv([]byte("call: Q1BBS\nstore: bbs.db\nagw:\n  addr: localhost:8000\n  passwordEnv: NOT_SET\n"), env(nil))
	require.ErrorContains(t, unsetErr, "agw.passwordEnv: NOT_SET is not set")

	var _, negativeErr = ParseWithEnv([]byte("call: Q1BBS\nstore: bbs.db\nagw:\n  addr: localhost:8000\n  highWater: -1\n"), env(nil))
	require.Error(t, negativeErr)
}

func TestParsePartners(t *testing.T) {
	t.Parallel()

	var cfg, err = Parse([]byte(`
call: Q1BBS-1
store: bbs.db
partners:
  - call: q2bbs
    covers: ["#77.gbr.euro"]
    floods: [gbr]
  - call: Q3BBS-2
    covers: ["*"]
    floods: ["*"]
`))
	require.NoError(t, err)

	assert.Equal(t, routing.Self{Call: "Q1BBS"}, cfg.RoutingSelf())
	assert.Equal(t, []routing.Partner{
		{Call: "Q2BBS", Covers: []string{"#77.GBR.EURO"}, Floods: []string{"GBR"}},
		{Call: "Q3BBS", Covers: []string{"*"}, Floods: []string{"*"}},
	}, cfg.RoutingPartners())

	var none, noneErr = Parse([]byte("call: Q1BBS\nstore: bbs.db\n"))
	require.NoError(t, noneErr)
	assert.Empty(t, none.RoutingPartners())
}

func TestParsePartnerProblems(t *testing.T) {
	t.Parallel()

	var _, err = Parse([]byte(`
call: Q1BBS
store: bbs.db
partners:
  - call: not a call
  - call: Q1BBS-2
  - call: Q2BBS
    covers: ["GBR..EURO"]
    floods: ["G R"]
  - call: Q2BBS-1
`))
	require.Error(t, err)

	for _, want := range []string{
		`partners[0].call: "not a call" is not a callsign`,
		"partners[1].call: Q1BBS-2 is this BBS",
		`partners[2].covers: "GBR..EURO" is not a hierarchical address`,
		`partners[2].floods: "G R" is not a distribution`,
		"partners[3].call: Q2BBS-1 is already a partner",
	} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestParseConnect(t *testing.T) {
	t.Parallel()

	var cfg, err = ParseWithEnv([]byte(`
call: Q1BBS
store: bbs.db
agw:
  addr: localhost:8000
partners:
  - call: Q3BPQ
    covers: ["*"]
    connect:
      port: 1
      to: q3nod
      via: [q5digi]
      script:
        - send: C Q3BPQ
        - expect: Connected to
        - sendEnv: BPQ_PASS
      every: 30m
      timeout: 1m
  - call: Q4BBS-1
    connect: {}
  - call: Q5BBS
`), env(map[string]string{"BPQ_PASS": "sekrit"}))
	require.NoError(t, err)

	var full = cfg.Partners[0].Connect

	require.NotNil(t, full)
	assert.Equal(t, 1, full.Port)
	assert.Equal(t, "Q3NOD", full.Target("Q3BPQ"))
	assert.Equal(t, []string{"Q5DIGI"}, full.Via)
	assert.Len(t, full.Script, 3)
	assert.Equal(t, "BPQ_PASS", full.Script[2].SendEnv)
	assert.Equal(t, 30*time.Minute, full.Every)
	assert.Equal(t, time.Minute, full.Timeout)

	var bare = cfg.Partners[1].Connect

	require.NotNil(t, bare)
	assert.Equal(t, "Q4BBS-1", bare.Target("Q4BBS-1"), "the partner itself")
	assert.Equal(t, DefaultCallEvery, bare.Every)
	assert.Equal(t, DefaultScriptTimeout, bare.Timeout)

	assert.Nil(t, cfg.Partners[2].Connect, "a partner that calls this BBS")
}

func TestParseConnectProblems(t *testing.T) {
	t.Parallel()

	var _, err = ParseWithEnv([]byte(`
call: Q1BBS
store: bbs.db
partners:
  - call: Q3BPQ
    connect:
      port: 300
      to: not a call
      via: [Q5-DIGI]
      script:
        - send: C Q3BPQ
          expect: Connected
        - sendEnv: NOT_SET
      every: 10s
      timeout: -1s
`), env(nil))
	require.Error(t, err)

	for _, want := range []string{
		"partners[0].connect: calling a partner needs an agw block",
		"partners[0].connect.port: 300 is not a port",
		`partners[0].connect: "not a call" is not a callsign`,
		`partners[0].connect: "Q5-DIGI" is not a callsign`,
		"partners[0].connect.every: 10s is less than 1m0s",
		"partners[0].connect.timeout: -1s is negative",
		"partners[0].connect.script[0]: a step needs exactly one of send, sendEnv and expect",
		"partners[0].connect.script[1].sendEnv: NOT_SET is not set",
	} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestParseFBB(t *testing.T) {
	t.Parallel()

	var byDefault, err = Parse([]byte("call: Q1BBS\nstore: bbs.db\n"))
	require.NoError(t, err)
	assert.False(t, byDefault.UseFBB(), "off unless turned on")

	var on, onErr = Parse([]byte("call: Q1BBS\nstore: bbs.db\nfbb: true\n"))
	require.NoError(t, onErr)
	assert.True(t, on.UseFBB())
}
