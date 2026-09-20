package direwolf

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDNSSDDefaultServiceName(t *testing.T) {
	var hostname, hostnameErr = os.Hostname()
	require.NoError(t, hostnameErr)

	var shortHostname, _, _ = strings.Cut(hostname, ".")

	assert.Equal(t, "Samoyed on "+shortHostname, dns_sd_default_service_name())
}

// An FQDN is cut down to the first label, so the announced name doesn't carry
// the domain around with it.
func TestDNSSDDefaultServiceNameDropsDomain(t *testing.T) {
	var name = dns_sd_default_service_name()

	assert.NotContains(t, strings.TrimPrefix(name, "Samoyed on "), ".")
}
