package direwolf

import (
	"context"
	"testing"

	"github.com/brutella/dnssd"
	"github.com/stretchr/testify/assert"
)

// cancelledContext is what the announcement tests hand dns_sd_announce: the
// message we care about is printed before the responder goroutine starts, and
// an already-cancelled context stops that goroutine putting mDNS traffic on
// the network, or printing, after the test has finished with stdout.
func cancelledContext(t *testing.T) context.Context {
	t.Helper()

	var ctx, cancel = context.WithCancel(context.Background())
	cancel()

	return ctx
}

// requireMDNSSockets skips a test on a machine where we cannot bind the mDNS
// sockets at all - a container with no multicast, say - rather than reporting
// the environment as a failure.  This is the same connection dnssd.NewResponder
// makes, opened directly because a Responder holds its sockets for good and
// offers no way to hand them back.
func requireMDNSSockets(t *testing.T) {
	t.Helper()

	var conn, err = dnssd.NewMDNSConn()
	if err != nil {
		t.Skipf("mDNS sockets unavailable here: %v", err)
	}

	conn.Close()
}

func TestDNSSDAnnounceUsesConfiguredName(t *testing.T) {
	requireMDNSSockets(t)

	var mc = new(misc_config_s)
	mc.kiss_port[0] = 8001
	mc.dns_sd_name = "Q1TEST TNC"

	var output = CaptureOutput(t, func() {
		dns_sd_announce(cancelledContext(t), mc)
	})

	assert.Contains(t, output, "Announcing KISS TCP on port 8001 as 'Q1TEST TNC'")
}

// With no name configured we fall back to the hostname-derived default.
func TestDNSSDAnnounceDefaultsName(t *testing.T) {
	requireMDNSSockets(t)

	var mc = new(misc_config_s)
	mc.kiss_port[0] = 8002

	var output = CaptureOutput(t, func() {
		dns_sd_announce(cancelledContext(t), mc)
	})

	assert.Contains(t, output, "Announcing KISS TCP on port 8002 as '"+dns_sd_default_service_name()+"'")
}

// Nothing is listening on port 0, so there is nothing to announce - and saying
// so beats advertising a service nobody can connect to.
func TestDNSSDAnnounceRejectsPortZero(t *testing.T) {
	var mc = new(misc_config_s)

	var output = CaptureOutput(t, func() {
		dns_sd_announce(cancelledContext(t), mc)
	})

	assert.Contains(t, output, "DNS-SD: Failed to create service")
	assert.NotContains(t, output, "Announcing")
}
