// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package announce

// Ports are the ports of the services there are to announce, 0 for one that
// isn't listening.
type Ports struct {
	KISS []int
}

// Services lists what there is to announce on ports, as name, or as
// dns_sd_default_service_name if name is empty.
func Services(name string, ports *Ports) []Service {
	if name == "" {
		name = dns_sd_default_service_name()
	}

	var services []Service

	if len(ports.KISS) > 0 && ports.KISS[0] != 0 {
		services = append(services, Service{Name: name, Type: KISSType, Port: ports.KISS[0]})
	}

	return services
}
