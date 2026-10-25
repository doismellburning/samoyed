// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/sdr"
	"github.com/sirupsen/logrus"
)

// rtlTCPPrefix starts the name of an audio device that is an RTL-SDR served
// by rtl_tcp: "rtl_tcp:host:port".
const rtlTCPPrefix = "rtl_tcp:"

// sdrParams is how to tune the SDR an audio device receives from.
type sdrParams struct {
	// frequencies has the frequency, in Hz, of each of the device's audio
	// channels.
	frequencies []float64

	// gain is the tuner gain in dB, or Nothing for its automatic gain
	// control.
	gain maybe.Maybe[float64]

	// ppm corrects the dongle's oscillator, in parts per million.
	ppm int
}

// sdrReceiver is an SDR that an audio device is receiving from.
type sdrReceiver struct {
	cancel context.CancelFunc

	// done is closed once receiveSDR has returned.
	done chan struct{}
}

// audioNameIsRTLTCP reports whether an audio device name is an rtl_tcp server.
func audioNameIsRTLTCP(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), rtlTCPPrefix)
}

// openRTLTCPInput connects audio device a to the rtl_tcp server name gives,
// tunes it, and starts turning its IQ samples into audio for the device's
// channels, a frequency each.
func (d *AudioDevices) openRTLTCPInput(ctx context.Context, a int, pa *RadioConfig, name string, bufSizeInBytes int) error {
	var params, ok = pa.adev[a].sdr.Get()
	if !ok {
		return fmt.Errorf("audio device %s has no SDR settings, which only a YAML configuration can give", name)
	}

	if len(params.frequencies) != pa.adev[a].num_channels {
		return fmt.Errorf("audio device %s has %d audio channels but %d SDR frequencies",
			name, pa.adev[a].num_channels, len(params.frequencies))
	}

	if pa.adev[a].bits_per_sample != 16 {
		return fmt.Errorf("audio device %s gives 16 bit audio, not %d", name, pa.adev[a].bits_per_sample)
	}

	var rate = pa.adev[a].samples_per_sec

	var tuning, tuningErr = sdr.NewTuning(params.frequencies, rate)
	if tuningErr != nil {
		return fmt.Errorf("audio device %s: %w", name, tuningErr)
	}

	var channelizer, channelizerErr = sdr.NewChannelizer(tuning.SampleRate, rate, tuning.Offsets)
	if channelizerErr != nil {
		return fmt.Errorf("audio device %s: %w", name, channelizerErr)
	}

	var log = logrus.WithField("device", name)

	var r, dialErr = sdr.DialRTLTCP(ctx, name[len(rtlTCPPrefix):])
	if dialErr != nil {
		log.WithError(dialErr).Error("Could not connect to rtl_tcp")

		return fmt.Errorf("audio device %s: %w", name, dialErr)
	}

	var err = errors.Join(
		r.SetSampleRate(tuning.SampleRate),
		r.SetFrequencyCorrection(params.ppm),
		r.SetFrequency(tuning.Centre),
		r.SetGain(params.gain),
	)
	if err != nil {
		_ = r.Close()

		return fmt.Errorf("audio device %s: %w", name, err)
	}

	log.WithFields(logrus.Fields{
		"tuner":      r.TunerType,
		"centre":     fmt.Sprintf("%.4f MHz", float64(tuning.Centre)/1e6),
		"sampleRate": tuning.SampleRate,
		"gain":       maybe.Format("%g dB", "auto", params.gain),
		"ppm":        params.ppm,
	}).Info("Receiving from rtl_tcp")

	// As for a soundcard, about a second's worth.
	d.dev[a].inputRingBuf = newAudioRingBuffer(rate * pa.adev[a].num_channels * 2)
	d.dev[a].inbufSizeInBytes = bufSizeInBytes

	var receiveCtx, cancel = context.WithCancel(ctx)

	var receiver = new(sdrReceiver)
	receiver.cancel = cancel
	receiver.done = make(chan struct{})
	d.dev[a].sdr = receiver

	go receiveSDR(receiveCtx, log, r, channelizer, d.dev[a].inputRingBuf, receiver.done)

	return nil
}

// receiveSDR reads IQ samples from r until ctx is cancelled or r fails,
// writing the audio channelizer makes of them to rb.  It closes rb when it
// stops, so that whoever reads it sees the device has gone, and says why on
// log.
func receiveSDR(ctx context.Context, log *logrus.Entry, r io.ReadCloser, channelizer *sdr.Channelizer, rb *audioRingBuffer, done chan<- struct{}) {
	defer close(done)
	defer rb.close()

	// CloseOnDone only closes r on cancellation, so close it on the way out
	// from a failed read too.
	defer func() { _ = r.Close() }()

	var release = dwutil.CloseOnDone(ctx, r)
	defer release()

	// A few milliseconds of samples at a time.
	var iq = make([]byte, 16384)
	var audio []int16
	var pcm []byte

	for ctx.Err() == nil {
		var n, err = r.Read(iq)

		if n > 0 {
			audio = channelizer.Process(iq[:n], audio[:0])

			pcm = pcm[:0]
			for _, sample := range audio {
				// Little-endian, lower byte first.
				pcm = append(pcm, byte(sample&0xff), byte((sample>>8)&0xff))
			}

			rb.write(pcm)
		}

		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				log.WithError(err).Error("Lost rtl_tcp")
			}

			return
		}
	}
}

// stop stops the receiver, and waits for it to.
func (s *sdrReceiver) stop() {
	s.cancel()
	<-s.done
}
