// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config is samoyed-bbs's configuration file: YAML, read and checked
// in full before anything starts, so every problem in it can be reported at
// once.
//
// Everything sits under a top-level "bbs" key, so that the file can be the
// one samoyed-direwolf reads its settings from, under "samoyed", too.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/bbs/forward/script"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/routing"
	"github.com/doismellburning/samoyed/internal/maybe"
	"go.yaml.in/yaml/v3"
)

// file is the whole of a configuration file.
type file struct {
	// BBS is samoyed-bbs's own settings.
	BBS *Config `yaml:"bbs"`

	// Samoyed is samoyed-direwolf's settings, for a file the two share.
	// They are its to check, so they are taken as they are and left alone.
	Samoyed yaml.Node `yaml:"samoyed"`
}

// Config is samoyed-bbs's configuration: what is under a configuration file's
// "bbs" key.
type Config struct {
	// Call is the BBS's callsign, with any SSID.
	Call string `yaml:"call"`

	// HRoute is the rest of the BBS's hierarchical address, after its
	// callsign: where it is, most specific first, such as "#77.GBR.EURO".
	// Optional; the R: line it puts on messages says it.
	HRoute string `yaml:"hroute"`

	// Store is the path of the message database, which is created if it
	// isn't there.
	Store string `yaml:"store"`

	// Banner is what a station is greeted with.  Left out, it says which
	// BBS this is, and that it is experimental.
	Banner string `yaml:"banner"`

	// Prompt is what a user is prompted with for each command.  Left out,
	// it is "de CALL>".
	Prompt string `yaml:"prompt"`

	// IdleTimeout is how long a station may say nothing before it is
	// disconnected.  Left out, ten minutes.
	IdleTimeout time.Duration `yaml:"idleTimeout"`

	// FBB is whether to forward in FBB's protocol with partners that can.
	// Left out, it doesn't: LinBPQ, for one, takes FBB's protocol only
	// compressed, which this BBS can't do yet, and turns away a BBS
	// offering it uncompressed.
	FBB bool `yaml:"fbb"`

	// AGW, if given, is the AGW server stations reach the BBS through.
	AGW *AGW `yaml:"agw"`

	// Partners are the BBSes this one forwards mail to.
	Partners []Partner `yaml:"partners"`
}

// Partner is a BBS this one forwards mail to, and what for.
type Partner struct {
	// Call is the partner's callsign.
	Call string `yaml:"call"`

	// Covers are where personal mail and traffic is sent to this partner
	// for, as hierarchical addresses such as "GBR.EURO", or "*" for
	// anywhere.  Mail for the partner itself always goes to it.
	Covers []string `yaml:"covers"`

	// Floods are the bulletin distributions passed on to this partner,
	// such as "GBR" or "WW", or "*" for all of them.
	Floods []string `yaml:"floods"`

	// Connect, if given, is how this BBS calls the partner.  Left out, the
	// partner calls this BBS.
	Connect *Connect `yaml:"connect"`
}

// Connect is how this BBS calls a partner over AGW.
type Connect struct {
	// Port is the AGW server port to call on, counting from 0.
	Port int `yaml:"port"`

	// To is who to connect to, if not the partner itself: a node to go
	// through, say, whose script then connects on.
	To string `yaml:"to"`

	// Via are digipeaters to connect through.
	Via []string `yaml:"via"`

	// Script is what to send, and wait for, once connected to reach the
	// partner itself.
	Script []script.Step `yaml:"script"`

	// Every is how often to call, mail queued or not.  Left out,
	// DefaultCallEvery.
	Every time.Duration `yaml:"every"`

	// Timeout is how long each of the script's steps may wait.  Left out,
	// DefaultScriptTimeout.
	Timeout time.Duration `yaml:"timeout"`
}

const (
	// DefaultCallEvery is how often a partner is called, if its connect
	// block doesn't say.
	DefaultCallEvery = time.Hour

	// MinCallEvery is the least a partner may be called every, so as not
	// to keep the channel busy.
	MinCallEvery = time.Minute

	// DefaultScriptTimeout is how long a connect script's step waits, if
	// the connect block doesn't say.
	DefaultScriptTimeout = 2 * time.Minute
)

// Target is who to connect to for this partner: its To, or the partner
// itself.
func (c Connect) Target(partner string) string {
	if c.To != "" {
		return c.To
	}

	return strings.ToUpper(partner)
}

// AGW is the AGW server stations reach the BBS through, such as
// samoyed-direwolf's AGWPORT.
type AGW struct {
	// Addr is the server's host:port.
	Addr string `yaml:"addr"`

	// User, if given, is the user name to log in with, for a server that
	// wants a login (samoyed-direwolf's AGWLOGIN).
	User string `yaml:"user"`

	// PasswordEnv names the environment variable holding the password, so
	// that the password itself is never in the file.
	PasswordEnv string `yaml:"passwordEnv"`

	// Ports are the server ports to take connections on, counting from 0.
	// Left out, every port the server has.
	Ports []int `yaml:"ports"`

	// PacLen is the most data sent to the server at a time.  Left out,
	// agwclient's default.
	PacLen int `yaml:"paclen"`

	// HighWater is how many frames may be unacknowledged before writing
	// waits.  Left out, agwclient's default.
	HighWater uint32 `yaml:"highWater"`

	// password is what PasswordEnv held when the file was read.
	password string
}

// MaxPacLen is the most data an AX.25 frame can carry.
const MaxPacLen = 2048

// DefaultIdleTimeout is how long a station may stay idle unless the
// configuration says otherwise.
const DefaultIdleTimeout = 10 * time.Minute

// callPattern is a callsign, with an optional SSID from 0 to 15.
var callPattern = regexp.MustCompile(`^[A-Z0-9]{1,6}(-([0-9]|1[0-5]))?$`)

// ValidCall says whether call is a callsign, with an optional SSID.
func ValidCall(call string) bool {
	return callPattern.MatchString(call)
}

// Load reads and checks the configuration file at path.  Anything wrong with
// it is reported together, one problem per line of the error.
func Load(path string) (Config, error) {
	var data, readErr = os.ReadFile(path) //nolint:gosec // G304: the file the sysop named
	if readErr != nil {
		return Config{}, readErr
	}

	return Parse(data)
}

// Parse reads and checks a configuration, taking any password from the
// environment.
func Parse(data []byte) (Config, error) {
	return ParseWithEnv(data, os.LookupEnv)
}

// ParseWithEnv is Parse, looking environment variables up with lookupEnv.
func ParseWithEnv(data []byte, lookupEnv func(name string) (string, bool)) (Config, error) {
	var doc = new(file)

	var decoder = yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var decodeErr = decoder.Decode(doc)
	if decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		if !hasBBSKey(data) {
			return Config{}, errors.Join(
				fmt.Errorf("reading YAML: %w", decodeErr),
				errors.New(`settings go under a top-level "bbs" key`),
			)
		}

		return Config{}, fmt.Errorf("reading YAML: %w", decodeErr)
	}

	var cfg = doc.BBS
	if cfg == nil {
		return Config{}, errors.New(`settings go under a top-level "bbs" key, and there is none`)
	}

	cfg.Call = strings.ToUpper(strings.TrimSpace(cfg.Call))
	cfg.HRoute = strings.ToUpper(strings.TrimSpace(cfg.HRoute))

	var validErr = cfg.validate(lookupEnv)
	if validErr != nil {
		return Config{}, validErr
	}

	if cfg.Banner == "" {
		cfg.Banner = "Welcome to " + cfg.Call + ", a Samoyed BBS (experimental)."
	}

	if cfg.Prompt == "" {
		cfg.Prompt = "de " + cfg.Call + ">"
	}

	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}

	for _, p := range cfg.Partners {
		if p.Connect == nil {
			continue
		}

		p.Connect.To = strings.ToUpper(p.Connect.To)
		p.Connect.Via = upper(p.Connect.Via)

		if p.Connect.Every == 0 {
			p.Connect.Every = DefaultCallEvery
		}

		if p.Connect.Timeout == 0 {
			p.Connect.Timeout = DefaultScriptTimeout
		}
	}

	return *cfg, nil
}

// hasBBSKey says whether data, as YAML, has a "bbs" key at its top level -
// one without is most likely laid out as it was before there was one.
func hasBBSKey(data []byte) bool {
	var doc map[string]yaml.Node

	if yaml.Unmarshal(data, &doc) != nil {
		return true // Not a mapping at all, which is complaint enough.
	}

	var _, ok = doc["bbs"]

	return ok
}

// validate checks everything that must be right, returning every problem,
// each named by where it is under the file's "bbs" key.  It also takes the
// AGW password from the environment.
func (c Config) validate(lookupEnv func(name string) (string, bool)) error {
	var problems = c.problems(lookupEnv)

	for i, problem := range problems {
		problems[i] = fmt.Errorf("bbs.%w", problem)
	}

	return errors.Join(problems...)
}

// problems is everything wrong with the configuration.
func (c Config) problems(lookupEnv func(name string) (string, bool)) []error {
	var problems []error

	if !ValidCall(c.Call) {
		problems = append(problems, fmt.Errorf("call: %q is not a callsign, with an optional SSID from 0 to 15", c.Call))
	}

	if c.HRoute != "" && !mail.ValidHRoute(c.HRoute) {
		problems = append(problems, fmt.Errorf("hroute: %q is not a hierarchical address, such as #77.GBR.EURO", c.HRoute))
	}

	if strings.TrimSpace(c.Store) == "" {
		problems = append(problems, errors.New("store: the path of the message database is needed"))
	}

	if c.IdleTimeout < 0 {
		problems = append(problems, fmt.Errorf("idleTimeout: %v is negative", c.IdleTimeout))
	}

	if c.AGW != nil {
		problems = append(problems, c.AGW.validate(lookupEnv)...)
	}

	problems = append(problems, c.validatePartners()...)
	problems = append(problems, c.validateConnects(lookupEnv)...)

	return problems
}

// validateConnects checks how partners are called: over the AGW server, so
// there must be one, and with secrets the environment has.
func (c Config) validateConnects(lookupEnv func(name string) (string, bool)) []error {
	var problems []error

	for i, p := range c.Partners {
		if p.Connect == nil {
			continue
		}

		var where = fmt.Sprintf("partners[%d].connect", i)
		var conn = p.Connect

		if c.AGW == nil {
			problems = append(problems, fmt.Errorf("%s: calling a partner needs an agw block to call through", where))
		}

		if conn.Port < 0 || conn.Port > 255 {
			problems = append(problems, fmt.Errorf("%s.port: %d is not a port from 0 to 255", where, conn.Port))
		}

		for _, call := range append([]string{conn.To}, conn.Via...) {
			if call != "" && !ValidCall(strings.ToUpper(call)) {
				problems = append(problems, fmt.Errorf("%s: %q is not a callsign", where, call))
			}
		}

		if conn.Every != 0 && conn.Every < MinCallEvery {
			problems = append(problems, fmt.Errorf("%s.every: %v is less than %v", where, conn.Every, MinCallEvery))
		}

		if conn.Timeout < 0 {
			problems = append(problems, fmt.Errorf("%s.timeout: %v is negative", where, conn.Timeout))
		}

		for j, step := range conn.Script {
			var stepErr = step.Validate()
			if stepErr != nil {
				problems = append(problems, fmt.Errorf("%s.script[%d]: %w", where, j, stepErr))
			}

			if step.SendEnv != "" {
				var _, ok = lookupEnv(step.SendEnv)
				if !ok {
					problems = append(problems, fmt.Errorf("%s.script[%d].sendEnv: %s is not set", where, j, step.SendEnv))
				}
			}
		}
	}

	return problems
}

// validate checks the AGW settings, and takes the password from the
// environment.
func (a *AGW) validate(lookupEnv func(name string) (string, bool)) []error {
	var problems []error

	var _, _, addrErr = net.SplitHostPort(a.Addr)
	if addrErr != nil {
		problems = append(problems, fmt.Errorf("agw.addr: %q is not a host:port", a.Addr))
	}

	if a.User != "" && a.PasswordEnv == "" {
		problems = append(problems, errors.New("agw.passwordEnv: a user name needs the variable its password is in"))
	}

	if a.PasswordEnv != "" {
		var password, ok = lookupEnv(a.PasswordEnv)
		if !ok {
			problems = append(problems, fmt.Errorf("agw.passwordEnv: %s is not set", a.PasswordEnv))
		}

		a.password = password
	}

	for _, port := range a.Ports {
		if port < 0 || port > 255 {
			problems = append(problems, fmt.Errorf("agw.ports: %d is not a port from 0 to 255", port))
		}
	}

	if a.PacLen < 0 || a.PacLen > MaxPacLen {
		problems = append(problems, fmt.Errorf("agw.paclen: %d is not from 1 to %d", a.PacLen, MaxPacLen))
	}

	return problems
}

// AGWClient is the agwclient configuration for the BBS, if it has an AGW
// server.
func (c Config) AGWClient() maybe.Maybe[agwclient.Config] {
	if c.AGW == nil {
		return maybe.Nothing[agwclient.Config]()
	}

	var cfg = new(agwclient.Config)

	cfg.Addr = c.AGW.Addr
	cfg.Call = c.Call
	cfg.Ports = c.AGW.Ports
	cfg.PacLen = c.AGW.PacLen
	cfg.HighWater = c.AGW.HighWater

	if c.AGW.User != "" {
		cfg.Login = maybe.Just(agwclient.Credentials{User: c.AGW.User, Password: c.AGW.password})
	}

	return maybe.Just(*cfg)
}

// BannerLines is the banner a line at a time.
func (c Config) BannerLines() []string {
	return strings.Split(strings.TrimRight(c.Banner, "\n"), "\n")
}

// UseFBB is whether to forward in FBB's protocol with partners that can.
func (c Config) UseFBB() bool {
	return c.FBB
}

// HRouteMaybe is HRoute, or Nothing if it isn't given.
func (c Config) HRouteMaybe() maybe.Maybe[string] {
	if c.HRoute == "" {
		return maybe.Nothing[string]()
	}

	return maybe.Just(c.HRoute)
}

// validatePartners checks the partners list.
func (c Config) validatePartners() []error {
	var problems []error
	var seen = make(map[string]bool)

	for i, p := range c.Partners {
		var where = fmt.Sprintf("partners[%d]", i)
		var call = strings.ToUpper(strings.TrimSpace(p.Call))

		switch {
		case !ValidCall(call):
			problems = append(problems, fmt.Errorf("%s.call: %q is not a callsign", where, p.Call))
		case baseCall(call) == baseCall(c.Call):
			problems = append(problems, fmt.Errorf("%s.call: %s is this BBS", where, call))
		case seen[baseCall(call)]:
			problems = append(problems, fmt.Errorf("%s.call: %s is already a partner", where, call))
		}

		seen[baseCall(call)] = true

		for _, cover := range p.Covers {
			if cover != routing.Wildcard && !mail.ValidHRoute(strings.ToUpper(cover)) {
				problems = append(problems, fmt.Errorf("%s.covers: %q is not a hierarchical address, such as GBR.EURO, or *", where, cover))
			}
		}

		for _, flood := range p.Floods {
			if flood != routing.Wildcard && !mail.ValidHRoute(strings.ToUpper(flood)) {
				problems = append(problems, fmt.Errorf("%s.floods: %q is not a distribution, such as GBR or WW, or *", where, flood))
			}
		}
	}

	return problems
}

// RoutingSelf is this BBS, as routing needs it.
func (c Config) RoutingSelf() routing.Self {
	return routing.Self{Call: baseCall(c.Call)}
}

// RoutingPartners are the partners, as routing needs them.
func (c Config) RoutingPartners() []routing.Partner {
	var partners = make([]routing.Partner, 0, len(c.Partners))

	for _, p := range c.Partners {
		var partner = new(routing.Partner)

		partner.Call = baseCall(p.Call)
		partner.Covers = upper(p.Covers)
		partner.Floods = upper(p.Floods)

		partners = append(partners, *partner)
	}

	return partners
}

// baseCall is a callsign without its SSID, upper-cased.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(strings.TrimSpace(call)), "-")

	return base
}

func upper(values []string) []string {
	var out = make([]string, 0, len(values))

	for _, v := range values {
		out = append(out, strings.ToUpper(strings.TrimSpace(v)))
	}

	return out
}
