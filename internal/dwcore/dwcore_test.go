// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwcore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The names are the ones the web interface has always shown, and which its
// JavaScript expects.
func TestSourceKindString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "radio", SourceRadio.String())
	assert.Equal(t, "dtmf", SourceDTMF.String())
	assert.Equal(t, "aprs-is", SourceAPRSIS.String())
	assert.Equal(t, "network", SourceNetTNC.String())
	assert.Equal(t, "axudp", SourceAXUDP.String())
	assert.Equal(t, "serial", SourceSerialTNC.String())
	assert.Equal(t, "SourceKind(99)", SourceKind(99).String())
}

func TestRadioSourceKeepsTheSlicer(t *testing.T) {
	t.Parallel()

	var from = SlicerID{Channel: 1, Subchannel: 2, Slice: 3}

	assert.Equal(t, Source{Kind: SourceRadio, Slicer: from}, RadioSource(from))
}

func TestChannelSourceHasOnlyAChannel(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		Source{Kind: SourceAXUDP, Slicer: SlicerID{Channel: 4, Subchannel: 0, Slice: 0}},
		ChannelSource(SourceAXUDP, 4))
}
