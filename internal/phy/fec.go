// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package phy

// FECType is the forward error correction, if any, a frame was received with.
type FECType int

const (
	FECNone FECType = 0
	FECFX25 FECType = 1
	FECIL2P FECType = 2
)

// Sanity is how closely a frame recovered by fixing bits must look like a
// valid one before it is accepted.
type Sanity int

const (
	SanityAPRS Sanity = iota
	SanityAX25
	SanityNone
)
