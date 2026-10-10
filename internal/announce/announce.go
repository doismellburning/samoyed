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

const DNS_SD_SERVICE = "_kiss-tnc._tcp"

// KISS announces the KISS TCP service listening on port, as name,
// or as dns_sd_default_service_name if name is empty, until ctx is cancelled.
func KISS(ctx context.Context, name string, port int) {
	if name == "" {
		name = dns_sd_default_service_name()
	}

	var cfg = dnssd.Config{ //nolint:exhaustruct_v5
		Name: name,
		Type: DNS_SD_SERVICE,
		Port: port,
	}

	var sv, svErr = dnssd.NewService(cfg)
	if svErr != nil {
		logrus.WithError(svErr).Error("DNS-SD: Failed to create service")

		return
	}

	var rp, rpErr = dnssd.NewResponder()
	if rpErr != nil {
		logrus.WithError(rpErr).Error("DNS-SD: Failed to create responder")

		return
	}

	var _, addErr = rp.Add(sv)
	if addErr != nil {
		logrus.WithError(addErr).Error("DNS-SD: Failed to add service")

		return
	}

	// Info rather than Debug: the name is what an operator looks for in a
	// client's list of discovered TNCs, and when it defaults to one made up
	// from the hostname this is the only place they are told it.
	logrus.WithFields(logrus.Fields{
		"name": name,
		"port": port,
	}).Info("DNS-SD: Announcing KISS TCP")

	go func() {
		// Respond runs until its context is cancelled, so this is what
		// stops announcing when we are shutting down.
		var respondErr = rp.Respond(ctx)
		if respondErr != nil && ctx.Err() == nil {
			logrus.WithError(respondErr).Error("DNS-SD: Responder error")
		}
	}()
}
