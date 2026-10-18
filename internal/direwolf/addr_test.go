// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddrPort(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 8001, addrPort(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8001, Zone: ""}))
	assert.Equal(t, 0, addrPort(&net.UnixAddr{Name: "/nowhere", Net: "unix"}))
	assert.Equal(t, 0, addrPort(nil))
}
