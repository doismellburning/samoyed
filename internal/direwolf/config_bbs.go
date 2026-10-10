// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/netrom"
)

// BBSSettings describes the node's BBS.  It has no line-format directive: it
// is written in YAML, under "bbs".
type BBSSettings struct {
	// Call is the BBS's own callsign, which stations can connect to directly
	// over AX.25 and NET/ROM; left out, the BBS is reached only from the
	// node's shell, and goes by the node's callsign.
	Call string `yaml:"call"`

	// Alias is advertised for Call in NODES broadcasts, with Quality.
	Alias   string `yaml:"alias"`
	Quality *int   `yaml:"quality"`

	// HRoute is where the BBS is, hierarchically: "#HANTS.GBR.EURO", say.
	HRoute string `yaml:"hroute"`

	Partners []BBSPartnerSettings `yaml:"partners"`
}

// BBSPartnerSettings is a BBS the node's forwards with.
type BBSPartnerSettings struct {
	Call string `yaml:"call"`

	// Node is the NET/ROM node or application to connect to; left out, the
	// partner is reached over AX.25 at Call on Channel, by way of Via.
	Node    string   `yaml:"node"`
	Channel int      `yaml:"channel"`
	Via     []string `yaml:"via"`

	Script    []string       `yaml:"script"`
	Routes    []string       `yaml:"routes"`
	Bulletins bool           `yaml:"bulletins"`
	Interval  *time.Duration `yaml:"interval"`
}

// defaultBBSQuality is the quality the BBS's NODES entry is given, unless the
// configuration says otherwise.
const defaultBBSQuality = 200

// applyBBS checks the BBS's settings and keeps them for startup.
func (ps *parseState) applyBBS(settings BBSSettings) error {
	if settings.Call != "" {
		var call, err = netrom.NormaliseCall(settings.Call)
		if err != nil {
			return fmt.Errorf("line %d: BBS: %w", ps.line, err)
		}

		settings.Call = call
	}

	var alias, aerr = netrom.NormaliseAlias(settings.Alias)
	if aerr != nil {
		return fmt.Errorf("line %d: BBS: %w", ps.line, aerr)
	}

	settings.Alias = alias

	if settings.Quality == nil {
		var q = defaultBBSQuality
		settings.Quality = &q
	}

	if *settings.Quality < 0 || *settings.Quality > 255 {
		return fmt.Errorf("line %d: BBS quality %d not in 0 to 255", ps.line, *settings.Quality)
	}

	if strings.ContainsAny(settings.HRoute, " @") {
		return fmt.Errorf("line %d: BBS hroute %q must be a dotted route such as #HANTS.GBR.EURO", ps.line, settings.HRoute)
	}

	var seen = make(map[string]bool)

	for i := range settings.Partners {
		var p = &settings.Partners[i]

		var call, err = netrom.NormaliseCall(p.Call)
		if err != nil {
			return fmt.Errorf("line %d: BBS partner: %w", ps.line, err)
		}

		if seen[call] {
			return fmt.Errorf("line %d: BBS partner %s given twice", ps.line, call)
		}

		seen[call] = true
		p.Call = call

		if p.Channel < 0 || p.Channel >= MAX_TOTAL_CHANS {
			return fmt.Errorf("line %d: BBS partner %s channel %d must be in range 0 to %d", ps.line, call, p.Channel, MAX_TOTAL_CHANS-1)
		}

		if len(p.Routes) == 0 {
			return fmt.Errorf("line %d: BBS partner %s has no routes: \"*\" sends it everything", ps.line, call)
		}

		if p.Interval != nil && *p.Interval < time.Minute && *p.Interval != 0 {
			return fmt.Errorf("line %d: BBS partner %s interval must be at least a minute, or 0", ps.line, call)
		}
	}

	ps.misc.bbs = &settings

	return nil
}
