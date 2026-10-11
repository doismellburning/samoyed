// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dwcore holds the small types the receive path's stages share -
// where a frame was heard, how it was heard, and the callbacks that carry it
// on - so that the layer 2 receivers and the rest of the TNC can agree on them
// without depending on each other.
package dwcore
