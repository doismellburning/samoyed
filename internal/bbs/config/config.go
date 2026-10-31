// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config is samoyed-bbs's configuration file: YAML, read and checked
// in full before anything starts, so every problem in it can be reported at
// once.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config is samoyed-bbs's configuration.
type Config struct {
	// Call is the BBS's callsign, with any SSID.
	Call string `yaml:"call"`

	// Store is the path of the message database, which is created if it
	// isn't there.
	Store string `yaml:"store"`

	// Banner is what a station is greeted with.  Left out, it says which
	// BBS this is.
	Banner string `yaml:"banner"`

	// Prompt is what a user is prompted with for each command.  Left out,
	// it is "de CALL>".
	Prompt string `yaml:"prompt"`

	// IdleTimeout is how long a station may say nothing before it is
	// disconnected.  Left out, ten minutes.
	IdleTimeout time.Duration `yaml:"idleTimeout"`
}

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

// Parse reads and checks a configuration.
func Parse(data []byte) (Config, error) {
	var cfg = new(Config)

	var decoder = yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var decodeErr = decoder.Decode(cfg)
	if decodeErr != nil {
		return Config{}, fmt.Errorf("reading YAML: %w", decodeErr)
	}

	cfg.Call = strings.ToUpper(strings.TrimSpace(cfg.Call))

	var validErr = cfg.validate()
	if validErr != nil {
		return Config{}, validErr
	}

	if cfg.Banner == "" {
		cfg.Banner = "Welcome to " + cfg.Call + ", a Samoyed BBS."
	}

	if cfg.Prompt == "" {
		cfg.Prompt = "de " + cfg.Call + ">"
	}

	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}

	return *cfg, nil
}

// validate checks everything that must be right, returning every problem.
func (c Config) validate() error {
	var problems []error

	if !ValidCall(c.Call) {
		problems = append(problems, fmt.Errorf("call: %q is not a callsign, with an optional SSID from 0 to 15", c.Call))
	}

	if strings.TrimSpace(c.Store) == "" {
		problems = append(problems, errors.New("store: the path of the message database is needed"))
	}

	if c.IdleTimeout < 0 {
		problems = append(problems, fmt.Errorf("idleTimeout: %v is negative", c.IdleTimeout))
	}

	return errors.Join(problems...)
}

// BannerLines is the banner a line at a time.
func (c Config) BannerLines() []string {
	return strings.Split(strings.TrimRight(c.Banner, "\n"), "\n")
}
