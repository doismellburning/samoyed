// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package announce

import "fmt"

// Ports are the ports of the services there are to announce, 0 for one that
// isn't listening.
type Ports struct {
	KISS    []int
	AGW     int
	Web     int
	Metrics int
}

// Services lists what there is to announce on ports, as name, or as
// dns_sd_default_service_name if name is empty.
//
// An instance name need only be unique among services of the same type, so
// each type is announced as name - bar the KISS ports after the first, which
// carry their port number to tell them apart.  The first keeps the plain
// name, which is all there was before any other was announced.
func Services(name string, ports *Ports) []Service {
	if name == "" {
		name = dns_sd_default_service_name()
	}

	var services []Service

	for _, port := range ports.KISS {
		if port == 0 {
			continue
		}

		var kissName = name
		if len(services) > 0 {
			kissName = fmt.Sprintf("%s (port %d)", name, port)
		}

		services = append(services, Service{Name: kissName, Type: KISSType, Port: port, Text: nil})
	}

	if ports.AGW != 0 {
		services = append(services, Service{Name: name, Type: AGWType, Port: ports.AGW, Text: nil})
	}

	if ports.Web != 0 {
		services = append(services, Service{Name: name, Type: HTTPType, Port: ports.Web, Text: map[string]string{"path": "/"}})
	}

	if ports.Metrics != 0 {
		services = append(services, Service{Name: name, Type: PrometheusType, Port: ports.Metrics, Text: map[string]string{"path": "/metrics"}})
	}

	return services
}
