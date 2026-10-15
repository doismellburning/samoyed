// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import "net"

// addrPort is the port of a TCP address, such as a listener's, or 0 for any
// other sort, nil included.
func addrPort(addr net.Addr) int {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return tcp.Port
	}

	return 0
}
