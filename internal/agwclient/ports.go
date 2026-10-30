// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"fmt"
	"strconv"
	"strings"
)

// PortInfo is one of the server's radio ports, as its port information reply
// describes it.
type PortInfo struct {
	// Number is the port number frames carry, counting from 0.  The server
	// names it one higher: "Port1" is port 0.
	Number int

	// Description is whatever the server says about the port.
	Description string
}

// parsePorts reads a port information reply: a count, then a description of
// each port, all separated by semicolons - "2;Port1 first soundcard
// left;Port2 first soundcard right;".  Gaps in the numbering are allowed, and
// so is a count that doesn't match, since only the ports themselves matter.
func parsePorts(data []byte) ([]PortInfo, error) {
	var text, _, _ = strings.Cut(string(data), "\x00")

	var fields = strings.Split(text, ";")

	var _, countErr = strconv.Atoi(strings.TrimSpace(fields[0]))
	if countErr != nil {
		return nil, fmt.Errorf("port information %q does not start with a count: %w", text, countErr)
	}

	var ports []PortInfo

	for _, field := range fields[1:] {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		var port, err = parsePort(field)
		if err != nil {
			return nil, err
		}

		ports = append(ports, port)
	}

	return ports, nil
}

// parsePort reads one port's entry from a port information reply, such as
// "Port1 first soundcard mono".
func parsePort(field string) (PortInfo, error) {
	var name, description, _ = strings.Cut(field, " ")

	var digits, ok = strings.CutPrefix(name, "Port")
	if !ok {
		return PortInfo{}, fmt.Errorf("port entry %q does not start with PortN", field)
	}

	var number, err = strconv.Atoi(digits)
	if err != nil || number < 1 || number > 256 {
		return PortInfo{}, fmt.Errorf("port entry %q has no port number from 1 to 256", field)
	}

	var port = new(PortInfo)

	port.Number = number - 1
	port.Description = strings.TrimSpace(description)

	return *port, nil
}
