// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package announce

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServicesKISS(t *testing.T) {
	t.Parallel()

	var ports = new(Ports)
	ports.KISS = []int{8001}

	assert.Equal(t,
		[]Service{{Name: "Q1TEST TNC", Type: KISSType, Port: 8001}},
		Services("Q1TEST TNC", ports))
}

// With no name configured we fall back to the hostname-derived default.
func TestServicesDefaultsName(t *testing.T) {
	t.Parallel()

	var ports = new(Ports)
	ports.KISS = []int{8001}

	var services = Services("", ports)

	assert.Len(t, services, 1)
	assert.Equal(t, dns_sd_default_service_name(), services[0].Name)
}

// A port of 0 isn't listening, so there is nothing there to announce.
func TestServicesSkipsUnusedPorts(t *testing.T) {
	t.Parallel()

	var ports = new(Ports)
	ports.KISS = []int{0}

	assert.Empty(t, Services("Q1TEST TNC", ports))
	assert.Empty(t, Services("Q1TEST TNC", new(Ports)))
}
