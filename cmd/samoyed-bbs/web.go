// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/webui"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/version"
)

// webInfo is what the web interface says about this BBS, started at started.
func webInfo(cfg config.Config, started time.Time) webui.Info {
	var info = new(webui.Info)

	info.Call = cfg.Call
	info.HRoute = cfg.HRouteMaybe()
	info.SID = ourSID(cfg).String()
	info.Version = version.Version
	info.Started = started
	info.FBB = cfg.UseFBB()

	if cfg.AGW != nil {
		info.AGW = cfg.AGW.Addr
	}

	var routed = cfg.RoutingPartners()

	info.Partners = make([]webui.Partner, 0, len(cfg.Partners))

	for i, p := range cfg.Partners {
		var partner = new(webui.Partner)

		// The same calls the store queues mail under.
		partner.Call = routed[i].Call
		partner.Covers = routed[i].Covers
		partner.Floods = routed[i].Floods

		if p.Connect != nil {
			var every = p.Connect.Every
			if every == 0 {
				every = config.DefaultCallEvery
			}

			partner.Connect = maybe.Just(webui.Connect{
				Port:         p.Connect.Port,
				To:           p.Connect.Target(partner.Call),
				Via:          p.Connect.Via,
				EverySeconds: int64(every.Seconds()),
			})
		}

		info.Partners = append(info.Partners, *partner)
	}

	return *info
}
