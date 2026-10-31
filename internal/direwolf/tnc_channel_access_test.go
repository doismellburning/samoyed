// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"net"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tncChannelAccessTestChannel is a virtual channel, as an NCHANNEL's is,
// so that a KISS client's settings for it are the TNC's rather than a radio's.
const tncChannelAccessTestChannel = 10

// testChannelAccess is every setting given, as a configuration might give
// them.
func testChannelAccess() TNCChannelAccess {
	return TNCChannelAccess{
		TXDelay:  maybe.Just[byte](30),
		Persist:  maybe.Just[byte](63),
		SlotTime: maybe.Just[byte](10),
		TXTail:   maybe.Just[byte](5),
	}
}

// noChannelAccess is no settings at all, leaving the TNC to its own.
func noChannelAccess() TNCChannelAccess {
	var none TNCChannelAccess

	return none
}

// kissCommandFrame is a KISS command frame for KISS port 0, as a TNC would
// get it on the wire.
func kissCommandFrame(command byte, value byte) []byte {
	return kiss.Encapsulate([]byte{command, value})
}

// readTestNetTNCBytes reads exactly len(want) bytes from what we sent the
// fake TNC, and checks they are want.
func readTestNetTNCBytes(t *testing.T, tnc net.Conn, want []byte) {
	t.Helper()

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(10*time.Second)))

	var got = make([]byte, len(want))
	require.NoError(t, readFullFrom(tnc, got))
	assert.Equal(t, want, got)
}

// Each setting given becomes a KISS command for port 0 - the TNC's only
// radio, as for the data frames - and one not given is not sent at all.
func TestTNCChannelAccessKISSFrames(t *testing.T) {
	t.Parallel()

	var all = testChannelAccess()

	assert.Equal(t, concatBytes(
		kissCommandFrame(kiss.CmdTxDelay, 30),
		kissCommandFrame(kiss.CmdPersistence, 63),
		kissCommandFrame(kiss.CmdSlotTime, 10),
		kissCommandFrame(kiss.CmdTxTail, 5),
	), all.kissFrames())

	var some TNCChannelAccess
	some.Persist = maybe.Just[byte](0xc0) // FEND, which has to be escaped.

	assert.Equal(t, []byte{kiss.FEND, kiss.CmdPersistence, kiss.FESC, kiss.TFEND, kiss.FEND}, some.kissFrames())

	var none TNCChannelAccess

	assert.Empty(t, none.kissFrames())
}

// concatBytes joins its arguments end to end.
func concatBytes(parts ...[]byte) []byte {
	var joined []byte

	for _, part := range parts {
		joined = append(joined, part...)
	}

	return joined
}

// A change replaces only the settings it gives.
func TestTNCChannelAccessUpdatedBy(t *testing.T) {
	t.Parallel()

	var update TNCChannelAccess
	update.TXDelay = maybe.Just[byte](50)

	var want = testChannelAccess()
	want.TXDelay = maybe.Just[byte](50)

	assert.Equal(t, want, testChannelAccess().updatedBy(update))
}

// A setting has to fit its KISS command's one byte.
func TestTNCChannelAccessSettingsRange(t *testing.T) {
	t.Parallel()

	for _, value := range []int{-1, 256} {
		var settings TNCChannelAccessSettings
		settings.SlotTime = &value

		var _, err = settings.channelAccess(1, "NCHANNEL")
		require.Error(t, err, value)
		assert.Contains(t, err.Error(), "SLOTTIME for NCHANNEL command must be in range 0 to 255")
	}

	var zero, top = 0, 255

	var settings TNCChannelAccessSettings
	settings.TXDelay = &zero
	settings.TXTail = &top

	var access, err = settings.channelAccess(1, "NCHANNEL")
	require.NoError(t, err)

	var want TNCChannelAccess
	want.TXDelay = maybe.Just[byte](0)
	want.TXTail = maybe.Just[byte](255)

	assert.Equal(t, want, access)
}

// attachTestNetTNCWith is attachTestNetTNC for a TNC with channel access
// settings to send it.
func attachTestNetTNCWith(t *testing.T, access TNCChannelAccess) (*NetTNC, net.Conn, <-chan net.Conn) {
	t.Helper()

	var port, conns = newTestNetTNC(t.Context(), t)

	var nt, err = NewNetTNC(t.Context(), tncChannelAccessTestChannel, "127.0.0.1", port, access, nil, 0)
	require.NoError(t, err)

	nt.reattachDelay = 10 * time.Millisecond

	nt.Start(t.Context())

	return nt, nextTestNetTNCConn(t, conns), conns
}

// The configured settings are the first thing a network TNC hears on being
// attached, and it hears them again when it is reattached - a TNC that has
// restarted has forgotten them.  A KISS client's change since then is part of
// what it hears.
func TestNetTNCSendsChannelAccessOnAttachAndReattach(t *testing.T) {
	t.Parallel()

	var nt, tnc, conns = attachTestNetTNCWith(t, testChannelAccess())

	readTestNetTNCBytes(t, tnc, testChannelAccess().kissFrames())

	var update TNCChannelAccess
	update.Persist = maybe.Just[byte](128)

	nt.SetChannelAccess(update)

	// Only the change goes now: the rest the TNC already has.
	readTestNetTNCBytes(t, tnc, kissCommandFrame(kiss.CmdPersistence, 128))

	require.NoError(t, tnc.Close())

	var reattached = nextTestNetTNCConn(t, conns)

	var want = testChannelAccess()
	want.Persist = maybe.Just[byte](128)

	readTestNetTNCBytes(t, reattached, want.kissFrames())
}

// With no settings configured, nothing is sent ahead of the first frame.
func TestNetTNCSendsNoChannelAccessWhenNoneIsGiven(t *testing.T) {
	t.Parallel()

	var nt, tnc, _ = attachTestNetTNCWith(t, noChannelAccess())

	var pp = newTestPacket(t)

	require.True(t, nt.sendPacket(tncChannelAccessTestChannel, pp))

	readTestNetTNCBytes(t, tnc, kissFrameFor(pp))
}

// A change made while the TNC is away is kept for when it is reattached.
func TestNetTNCChannelAccessWhileNotAttached(t *testing.T) {
	t.Parallel()

	var nt = new(NetTNC)
	nt.channel = tncChannelAccessTestChannel

	var update TNCChannelAccess
	update.SlotTime = maybe.Just[byte](20)

	nt.SetChannelAccess(update)

	assert.Equal(t, update, nt.access)

	// A channel with no TNC has nothing to keep it in, and doesn't fall over.
	var none *NetTNC

	none.SetChannelAccess(update)
}

// A KISS client's channel access settings for a network TNC's channel are
// passed on to that TNC, on KISS port 0, whatever channel the client named -
// rather than logged and dropped, as a radio channel's setter would for a
// channel past the radio ones.
func TestKissClientChannelAccessReachesTheNetTNC(t *testing.T) {
	t.Parallel()

	var nt, tnc, _ = attachTestNetTNCWith(t, noChannelAccess())

	var audioConfig = kissTestRadioConfig()
	audioConfig.chan_medium[tncChannelAccessTestChannel] = MEDIUM_NETTNC

	var tncs [MAX_TOTAL_CHANS]*NetTNC
	tncs[tncChannelAccessTestChannel] = nt

	var tq = NewTransmitQueue(audioConfig, tncs, [MAX_TOTAL_CHANS]*SerialTNC{}, [MAX_TOTAL_CHANS]*AXUDPChannel{}, nil)

	var h = NewKissHandler(t.Context(), audioConfig, new(XmitService), tq, tq.SetTNCChannelAccess, nil)

	var _, from = recordingKissClient()

	var port = byte(tncChannelAccessTestChannel << 4)

	h.processMsg([]byte{port | kiss.CmdTxDelay, 40}, from)
	h.processMsg([]byte{port | kiss.CmdPersistence, 63}, from)
	h.processMsg([]byte{port | kiss.CmdSlotTime, 8}, from)
	h.processMsg([]byte{port | kiss.CmdTxTail, 4}, from)

	readTestNetTNCBytes(t, tnc, concatBytes(
		kissCommandFrame(kiss.CmdTxDelay, 40),
		kissCommandFrame(kiss.CmdPersistence, 63),
		kissCommandFrame(kiss.CmdSlotTime, 8),
		kissCommandFrame(kiss.CmdTxTail, 4),
	))
}

// A radio channel's settings are the transmit thread's, and a channel with no
// external TNC has nobody to pass them to, so the transmit queue passes
// nothing on for either, nor for one out of range.
func TestTransmitQueueSetTNCChannelAccessIgnoresOtherChannels(t *testing.T) {
	t.Parallel()

	var nt, tnc, _ = attachTestNetTNCWith(t, noChannelAccess())

	var audioConfig = kissTestRadioConfig()
	audioConfig.chan_medium[tncChannelAccessTestChannel] = MEDIUM_NETTNC
	audioConfig.chan_medium[tncChannelAccessTestChannel+1] = MEDIUM_AXUDP

	var tncs [MAX_TOTAL_CHANS]*NetTNC
	tncs[tncChannelAccessTestChannel] = nt
	tncs[0] = nt                             // Never consulted: channel 0 is a radio.
	tncs[tncChannelAccessTestChannel+1] = nt // Nor this: it is an AXUDP channel.

	var tq = NewTransmitQueue(audioConfig, tncs, [MAX_TOTAL_CHANS]*SerialTNC{}, [MAX_TOTAL_CHANS]*AXUDPChannel{}, nil)

	for _, channel := range []int{-1, 0, tncChannelAccessTestChannel + 1, MAX_TOTAL_CHANS} {
		tq.SetTNCChannelAccess(channel, testChannelAccess())
	}

	// The only thing the TNC hears is what is for its own channel.
	var update TNCChannelAccess
	update.TXTail = maybe.Just[byte](7)

	tq.SetTNCChannelAccess(tncChannelAccessTestChannel, update)

	readTestNetTNCBytes(t, tnc, kissCommandFrame(kiss.CmdTxTail, 7))
}

// The KISS handler passes each timing command on as just the one setting,
// for the channel the client named.
func TestKissHandlerForwardsChannelAccess(t *testing.T) {
	t.Parallel()

	type forwarded struct {
		channel int
		access  TNCChannelAccess
	}

	var got []forwarded

	var record = func(channel int, access TNCChannelAccess) {
		got = append(got, forwarded{channel: channel, access: access})
	}

	var h = NewKissHandler(t.Context(), kissTestRadioConfig(), new(XmitService), nil, record, nil)

	var _, from = recordingKissClient()

	h.processMsg([]byte{0x70 | kiss.CmdTxDelay, 30}, from)
	h.processMsg([]byte{0x70 | kiss.CmdFullDuplex, 1}, from) // Not a TNC channel access setting.

	var want TNCChannelAccess
	want.TXDelay = maybe.Just[byte](30)

	assert.Equal(t, []forwarded{{channel: 7, access: want}}, got)
}
