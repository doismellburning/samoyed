// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodedPacket(t *testing.T, monitor string) Packet {
	t.Helper()

	var pp = ax25.FromText(monitor, true)
	require.NotNil(t, pp)

	var A = aprs.NewDecoderFromDataFiles().Decode(pp, true)

	return NewPacket(pp, A, Received)
}

func TestNewPacketPosition(t *testing.T) {
	var p = decodedPacket(t, "Q1TEST-9>APRS,WIDE1-1:!5130.00N/00007.50W>Mobile")

	assert.Equal(t, Received, p.Direction)
	assert.Equal(t, "Q1TEST-9", p.Station)
	assert.Equal(t, "Q1TEST-9>APRS,WIDE1-1:!5130.00N/00007.50W>Mobile", p.Monitor)
	assert.Equal(t, "/>", p.Symbol)
	assert.Equal(t, "Mobile", p.Comment)
	assert.NotEmpty(t, p.Description)

	var pos, ok = p.Position.Get()
	require.True(t, ok)
	assert.InDelta(t, 51.5, pos.Lat, 1e-6)
	assert.InDelta(t, -0.125, pos.Lon, 1e-6)
}

func TestNewPacketObjectIsFiledUnderItsName(t *testing.T) {
	// The object's position mustn't move the station that sent it.
	var p = decodedPacket(t, "Q1TEST>APRS:;EVENT    *111111z5130.00N/00007.50W-Fete")

	assert.Equal(t, "EVENT", p.Station)
	assert.True(t, p.Position.IsJust())
}

func TestNewPacketStatusHasNoPosition(t *testing.T) {
	var p = decodedPacket(t, "Q1TEST>APRS:>On the air")

	assert.Equal(t, "Q1TEST", p.Station)
	assert.Equal(t, maybe.Nothing[Position](), p.Position)
}

func TestNewPacketNotAPRS(t *testing.T) {
	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var p = NewPacket(pp, nil, Transmitted)

	assert.Equal(t, Transmitted, p.Direction)
	assert.Equal(t, "Q1TEST", p.Station)
	assert.Equal(t, "Q1TEST>Q2TEST:hello", p.Monitor)
	assert.Empty(t, p.Description)
	assert.True(t, p.Position.IsNothing())
}
