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

// Each KISS port is announced, the second and later carrying their port number
// so that each instance name stays unique.  The first, which once was all we
// announced, keeps the plain name; an unused slot before it doesn't change that.
func TestServicesEveryKISSPort(t *testing.T) {
	t.Parallel()

	var ports = new(Ports)
	ports.KISS = []int{0, 8001, 0, 8002, 8003}

	assert.Equal(t,
		[]Service{
			{Name: "Q1TEST TNC", Type: KISSType, Port: 8001},
			{Name: "Q1TEST TNC (port 8002)", Type: KISSType, Port: 8002},
			{Name: "Q1TEST TNC (port 8003)", Type: KISSType, Port: 8003},
		},
		Services("Q1TEST TNC", ports))
}

func TestServicesAGW(t *testing.T) {
	t.Parallel()

	var ports = new(Ports)
	ports.AGW = 8000

	assert.Equal(t,
		[]Service{{Name: "Q1TEST TNC", Type: AGWType, Port: 8000}},
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
	ports.AGW = 0

	assert.Empty(t, Services("Q1TEST TNC", ports))
	assert.Empty(t, Services("Q1TEST TNC", new(Ports)))
}
