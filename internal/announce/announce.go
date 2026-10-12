// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package announce

/*------------------------------------------------------------------
 *
 * Purpose:   	Announce the KISS over TCP service using DNS-SD
 *
 * Description:
 *
 *     Most people have typed in enough IP addresses and ports by now, and
 *     would rather just select an available TNC that is automatically
 *     discovered on the local network.  Even more so on a mobile device
 *     such an Android or iOS phone or tablet.
 *
 *     This uses the pure-Go github.com/brutella/dnssd package for
 *     cross-platform mDNS/DNS-SD service announcement without requiring
 *     any system daemon or C library dependencies.
 */

import (
	"context"

	"github.com/brutella/dnssd"
	"github.com/sirupsen/logrus"
)

// KISSType is the DNS-SD service type for KISS over TCP, as Dire Wolf
// announces it and clients such as APRSdroid look for it.
const KISSType = "_kiss-tnc._tcp"

// Service is one service to announce: an instance Name, unique among those of
// its Type, and the Port it listens on.
type Service struct {
	Name string
	Type string
	Port int
}

// Announce announces services, until ctx is cancelled.  One that cannot be
// announced is logged and left out, without holding up the rest.
func Announce(ctx context.Context, services []Service) {
	type pending struct {
		service dnssd.Service
		fields  logrus.Fields
	}

	var valid []pending

	for _, service := range services {
		var fields = logrus.Fields{
			"name": service.Name,
			"type": service.Type,
			"port": service.Port,
		}

		var cfg = dnssd.Config{ //nolint:exhaustruct_v5
			Name: service.Name,
			Type: service.Type,
			Port: service.Port,
		}

		var sv, svErr = dnssd.NewService(cfg)
		if svErr != nil {
			logrus.WithError(svErr).WithFields(fields).Error("DNS-SD: Failed to create service")

			continue
		}

		valid = append(valid, pending{service: sv, fields: fields})
	}

	// Checked only now, so that a service we could never have announced is
	// reported as such even where the mDNS sockets are unavailable.
	if len(valid) == 0 {
		return
	}

	var rp, rpErr = dnssd.NewResponder()
	if rpErr != nil {
		logrus.WithError(rpErr).Error("DNS-SD: Failed to create responder")

		return
	}

	for _, p := range valid {
		var _, addErr = rp.Add(p.service)
		if addErr != nil {
			logrus.WithError(addErr).WithFields(p.fields).Error("DNS-SD: Failed to add service")

			continue
		}

		// Info rather than Debug: the name is what an operator looks for in a
		// client's list of discovered services, and when it defaults to one
		// made up from the hostname this is the only place they are told it.
		logrus.WithFields(p.fields).Info("DNS-SD: Announcing")
	}

	go func() {
		// Respond runs until its context is cancelled, so this is what
		// stops announcing when we are shutting down.
		var respondErr = rp.Respond(ctx)
		if respondErr != nil && ctx.Err() == nil {
			logrus.WithError(respondErr).Error("DNS-SD: Responder error")
		}
	}()
}
