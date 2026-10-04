// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package phy

import (
	"fmt"
)

// Layer2 is the layer 2 protocol a channel transmits.
type Layer2 int

const (
	Layer2AX25 Layer2 = iota
	Layer2FX25
	Layer2IL2P
)

// String names the layer 2 protocol as the channel summary at startup shows it.
func (l Layer2) String() string {
	switch l {
	case Layer2AX25:
		return "AX.25"
	case Layer2FX25:
		return "FX.25"
	case Layer2IL2P:
		return "IL2P"
	default:
		return fmt.Sprintf("layer2_t(%d)", int(l))
	}
}
