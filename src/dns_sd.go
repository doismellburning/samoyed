package direwolf

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

func dns_sd_announce(ctx context.Context, mc *misc_config_s) {
	var name = mc.dns_sd_name
	if name == "" {
		name = dns_sd_default_service_name()
	}

	var cfg = dnssd.Config{ //nolint:exhaustruct_v5
		Name: name,
		Type: DNS_SD_SERVICE,
		Port: mc.kiss_port[0],
	}

	var sv, svErr = dnssd.NewService(cfg)
	if svErr != nil {
		logrus.Errorf("DNS-SD: Failed to create service: %v", svErr)

		return
	}

	var rp, rpErr = dnssd.NewResponder()
	if rpErr != nil {
		logrus.Errorf("DNS-SD: Failed to create responder: %v", rpErr)

		return
	}

	var _, addErr = rp.Add(sv)
	if addErr != nil {
		logrus.Errorf("DNS-SD: Failed to add service: %v", addErr)

		return
	}

	text_color_set(DW_COLOR_INFO)
	dw_printf("DNS-SD: Announcing KISS TCP on port %d as '%s'\n", mc.kiss_port[0], name)

	go func() {
		// Respond runs until its context is cancelled, so this is what
		// stops announcing when we are shutting down.
		var respondErr = rp.Respond(ctx)
		if respondErr != nil && ctx.Err() == nil {
			logrus.Errorf("DNS-SD: Responder error: %v", respondErr)
		}
	}()
}
