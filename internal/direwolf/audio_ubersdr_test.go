// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ubersdr/ubersdrtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uberSDRConfig is a receive-only configuration naming server, as a single
// name ADEVICE leaves it.
func uberSDRConfig(server *ubersdrtest.Server, query string) *RadioConfig {
	var name = "ubersdr:" + server.URL + "/?" + query

	return makeRadioConfig(name, name)
}

func Test_audioOpen_uberSDR_receives(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var pa = uberSDRConfig(server, "frequency=10147600&mode=usb&password=Q1TEST-secret")

	var d = openAudio(t, pa)
	assert.Equal(t, AUDIO_IN_TYPE_UBERSDR, d.dev[0].g_audio_in_type)
	assert.False(t, d.transmitAvailable(0), "UberSDR is receive only")
	assert.Equal(t, 12000, pa.adev[0].samples_per_sec, "the rate is the one UberSDR streams USB at, not the default")
	assert.Equal(t, 12000, d.dev[0].sampleRate)

	var conn = server.Accept(t)
	assert.Equal(t, "Q1TEST-secret", conn.Query.Get("password"))

	conn.SendAudio(t, []int16{0x0201, -2}, 12000)
	conn.SendAudio(t, []int16{0x0403}, 12000)

	for _, want := range []int{0x01, 0x02, 0xfe, 0xff, 0x03, 0x04} {
		assert.Equal(t, want, d.GetByte(0))
	}

	d.Close()
	conn.WaitClosed(t)

	assert.Nil(t, d.dev[0].uberSDRCancel)
	assert.Nil(t, d.dev[0].inputRingBuf)
}

func Test_audioOpen_uberSDR_closeWakesGetByte(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var d = openAudio(t, uberSDRConfig(server, "frequency=10147600"))

	server.Accept(t)

	var got = make(chan int)

	go func() { got <- d.GetByte(0) }()

	time.Sleep(50 * time.Millisecond)
	d.Close()

	select {
	case b := <-got:
		assert.Equal(t, -1, b, "the stream has ended")
	case <-time.After(5 * time.Second):
		t.Fatal("GetByte still blocked after Close")
	}
}

// A server that can't be reached at startup is retried in the background,
// rather than stopping a station that may have other things to do.
func Test_audioOpen_uberSDR_unreachable(t *testing.T) {
	var pa = makeRadioConfig("ubersdr:http://127.0.0.1:1/?frequency=10147600", "ubersdr:http://127.0.0.1:1/?frequency=10147600")

	var d = openAudio(t, pa)
	assert.NotNil(t, d.dev[0].uberSDRCancel)

	var done = make(chan struct{})

	go func() {
		d.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return while retrying the connection")
	}
}

func Test_audioOpen_uberSDR_wrongRateDropsTheConnection(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	openAudio(t, uberSDRConfig(server, "frequency=10147600&mode=usb"))

	var conn = server.Accept(t)
	conn.SendAudio(t, []int16{1, 2, 3}, 24000)
	conn.WaitClosed(t)
}

func Test_audioOpen_uberSDR_keepsAMatchingARATE(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var pa = uberSDRConfig(server, "frequency=7000000&mode=nfm")
	pa.adev[0].samples_per_sec = 24000

	openAudio(t, pa)
	assert.Equal(t, 24000, pa.adev[0].samples_per_sec)
}

func Test_audioOpen_uberSDR_badConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		chans int
		bits  int
		want  string
	}{
		{"no frequency", "ubersdr:https://sdr.example.org/", 1, 16, "frequency=<Hz> is required"},
		{"IQ mode", "ubersdr:https://sdr.example.org/?frequency=10147600&mode=iq", 1, 16, "mode must be"},
		{"stereo", "ubersdr:https://sdr.example.org/?frequency=10147600", 2, 16, "ACHANNELS 1"},
		{"8 bit", "ubersdr:https://sdr.example.org/?frequency=10147600", 1, 8, "16 bits per sample"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pa = makeRadioConfig(tc.input, tc.input)
			pa.adev[0].num_channels = tc.chans
			pa.adev[0].bits_per_sample = tc.bits

			var d, err = AudioOpen(t.Context(), pa)
			if d != nil {
				t.Cleanup(d.Close)
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// Naming an UberSDR receiver for transmit is refused, without repeating a
// password its URL might hold.
func Test_audioOpen_uberSDR_cannotTransmit(t *testing.T) {
	var pa = makeRadioConfig("stdin", "ubersdr:https://sdr.example.org/?frequency=10147600&password=Q1TEST-secret")
	pa.adev[0].adevice_out_specified = true

	var d, err = AudioOpen(t.Context(), pa)
	if d != nil {
		t.Cleanup(d.Close)
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot transmit")
	assert.NotContains(t, err.Error(), "Q1TEST-secret")
}
