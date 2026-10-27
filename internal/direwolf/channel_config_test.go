// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/nodemap"
	"github.com/stretchr/testify/assert"
)

// Each channel is receive only exactly when canTransmit says that channel -
// not its device, nor any other channel - has nowhere to send audio.
func TestPttChannelsReceiveOnly(t *testing.T) {
	t.Parallel()

	var c = new(RadioConfig)
	c.chan_medium[0] = MEDIUM_RADIO
	c.chan_medium[1] = MEDIUM_RADIO
	c.chan_medium[2] = MEDIUM_RADIO

	var asked []int

	var channels = c.pttChannels(func(channel int) bool {
		asked = append(asked, channel)

		return channel == 1
	})

	assert.True(t, channels[0].ReceiveOnly)
	assert.False(t, channels[1].ReceiveOnly)
	assert.True(t, channels[2].ReceiveOnly)

	for ch := range MAX_RADIO_CHANS {
		assert.Contains(t, asked, ch, "every channel is asked about")
	}
}

// mapPortFor is a port for the node map with only id and linkType known.
func mapPortFor(id string, linkType nodemap.LinkType) nodemap.Port {
	return nodemap.Port{
		ID:         id,
		LinkType:   linkType,
		Freq:       maybe.Nothing[int64](),
		Mode:       "",
		Modulation: "",
		Baud:       maybe.Nothing[int32](),
		Bitrate:    maybe.Nothing[int32](),
		Usage:      "",
		Comment:    "",
	}
}

func TestMapPort(t *testing.T) {
	t.Parallel()

	t.Run("a radio channel is RF, with its modem's details", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name       string
			modem      modem_t
			bitrate    int32
			modulation string
			baud       int32
		}{
			{"300 AFSK", MODEM_AFSK, 300, "AFSK", 300},
			{"1200 AFSK", MODEM_AFSK, 1200, "AFSK", 1200},
			{"G3RUH", MODEM_SCRAMBLE, 9600, "FSK", 9600},
			{"baseband", MODEM_BASEBAND, 9600, "FSK", 9600},
			{"BPSK", MODEM_BPSK, 300, "BPSK", 300},
			{"QPSK", MODEM_QPSK, 2400, "QPSK", 1200},
			{"8PSK", MODEM_8PSK, 4800, "8PSK", 1600},
			{"16QAM", MODEM_16_QAM, 9600, "16QAM", 2400},
			{"64QAM", MODEM_64_QAM, 9600, "64QAM", 1600},
		} {
			var c = new(RadioConfig)
			c.chan_medium[1] = MEDIUM_RADIO
			c.achan[1].ModemType = tc.modem
			c.achan[1].Baud = int(tc.bitrate)

			var want = mapPortFor("1", nodemap.LinkRF)
			want.Mode = "AX.25"
			want.Modulation = tc.modulation
			want.Baud = maybe.Just(tc.baud)
			want.Bitrate = maybe.Just(tc.bitrate)

			assert.Equal(t, want, c.mapPort(1), tc.name)
		}
	})

	t.Run("a radio channel transmitting IL2P is IL2P", func(t *testing.T) {
		t.Parallel()

		var c = new(RadioConfig)
		c.chan_medium[0] = MEDIUM_RADIO
		c.achan[0].ModemType = MODEM_AFSK
		c.achan[0].Baud = 1200
		c.achan[0].layer2_xmit = LAYER2_IL2P

		assert.Equal(t, "IL2P", c.mapPort(0).Mode)
	})

	t.Run("a radio channel transmitting FX.25 is FX.25", func(t *testing.T) {
		t.Parallel()

		var c = new(RadioConfig)
		c.chan_medium[0] = MEDIUM_RADIO
		c.achan[0].ModemType = MODEM_AFSK
		c.achan[0].Baud = 1200
		c.achan[0].layer2_xmit = LAYER2_FX25

		assert.Equal(t, "FX.25", c.mapPort(0).Mode)
	})

	t.Run("a radio channel with no modem for linking says nothing of one", func(t *testing.T) {
		t.Parallel()

		for _, modem := range []modem_t{MODEM_AIS, MODEM_EAS, MODEM_OFF} {
			var c = new(RadioConfig)
			c.chan_medium[0] = MEDIUM_RADIO
			c.achan[0].ModemType = modem
			c.achan[0].Baud = 9600

			var want = mapPortFor("0", nodemap.LinkRF)
			want.Mode = "AX.25"

			assert.Equal(t, want, c.mapPort(0), modem.String())
		}
	})

	t.Run("a TNC is RF, and nothing more is known", func(t *testing.T) {
		t.Parallel()

		var c = new(RadioConfig)
		c.chan_medium[8] = MEDIUM_NETTNC
		c.chan_medium[9] = MediumSerialTNC

		assert.Equal(t, mapPortFor("8", nodemap.LinkRF), c.mapPort(8))
		assert.Equal(t, mapPortFor("9", nodemap.LinkRF), c.mapPort(9))
	})

	t.Run("an AXUDP port is Internet, and nothing more is known", func(t *testing.T) {
		t.Parallel()

		var c = new(RadioConfig)
		c.chan_medium[10] = MEDIUM_AXUDP

		assert.Equal(t, mapPortFor("10", nodemap.LinkInternet), c.mapPort(10))
	})
}

// myCall is a channel's callsign, and none for a channel there can't be.
func TestMyCall(t *testing.T) {
	t.Parallel()

	var c = new(RadioConfig)
	c.mycall[1] = "Q1TEST-15"

	assert.Equal(t, "Q1TEST-15", c.myCall(1))
	assert.Empty(t, c.myCall(0))
	assert.Empty(t, c.myCall(-1))
	assert.Empty(t, c.myCall(MAX_TOTAL_CHANS))
}
