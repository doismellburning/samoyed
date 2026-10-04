// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"fmt"

	"github.com/doismellburning/samoyed/internal/ubersdr"
	"github.com/sirupsen/logrus"
)

// audioNameIsUberSDR reports whether an audio device name is an UberSDR
// instance to receive from, e.g. "ubersdr:https://sdr.example.org/?frequency=10147600".
func audioNameIsUberSDR(name string) bool {
	return ubersdr.HasPrefix(name)
}

// uberSDRUserAgent is how we introduce ourselves to an UberSDR server, whose
// operator sees it in their list of listeners.
func uberSDRUserAgent() string {
	var version = SAMOYED_VERSION
	if version == "" {
		version = "unknown"
	}

	return "Samoyed/" + version + " (+https://github.com/doismellburning/samoyed)"
}

// applyUberSDRFormat fits audio device a's settings to what the UberSDR
// source it names will stream, before anything is sized from them.
//
// The server streams each mode at a fixed rate, so the rate is the mode's,
// whatever ARATE says - there is nothing to resample with, and demodulating
// at the wrong rate would decode nothing.  This has to happen while opening
// the devices, before the demodulators are set up from the same settings.
func applyUberSDRFormat(a int, ad *adev_param_s, src *ubersdr.Source) error {
	if ad.num_channels != src.Channels {
		return fmt.Errorf("UberSDR audio is mono, so audio device %d needs ACHANNELS %d, not %d", a, src.Channels, ad.num_channels)
	}

	if ad.bits_per_sample != 16 {
		return fmt.Errorf("UberSDR audio is 16 bits per sample, so audio device %d can't use %d", a, ad.bits_per_sample)
	}

	if ad.samples_per_sec != src.SampleRate {
		logrus.WithFields(logrus.Fields{
			"device":  a,
			"mode":    src.Mode,
			"was":     ad.samples_per_sec,
			"now":     src.SampleRate,
			"ubersdr": src.String(),
		}).Info("Using the sample rate UberSDR streams this mode at")

		ad.samples_per_sec = src.SampleRate
	}

	return nil
}

// openUberSDRInput starts receiving audio from src into audio device a's
// input ring buffer, where GetByte reads it as it would a soundcard's.
//
// It doesn't wait for the server: one that is unreachable at startup is
// retried in the background, as it would be had it dropped later, so a
// station that also has other devices, or transmits, keeps working meanwhile.
func (d *AudioDevices) openUberSDRInput(ctx context.Context, a int, pa *RadioConfig, src *ubersdr.Source) {
	// About a second of audio, as for a soundcard: enough to ride out the
	// jitter of packets arriving over the Internet.
	var ringBufSize = pa.adev[a].samples_per_sec * pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
	var ring = newAudioRingBuffer(ringBufSize)

	d.dev[a].inputRingBuf = ring
	d.dev[a].inbufSizeInBytes = calcbufsize(pa.adev[a].samples_per_sec, pa.adev[a].num_channels, pa.adev[a].bits_per_sample)

	var runCtx, cancel = context.WithCancel(ctx)
	var done = make(chan struct{})

	d.dev[a].uberSDRCancel = cancel
	d.dev[a].uberSDRDoneCh = done

	var sampleRate = pa.adev[a].samples_per_sec
	var channels = pa.adev[a].num_channels

	go func() {
		defer close(done)

		ubersdr.Run(runCtx, src, uberSDRUserAgent(), func(pcmLE []byte, packetRate, packetChannels int) error {
			if packetRate != sampleRate || packetChannels != channels {
				return fmt.Errorf("UberSDR sent %d Hz, %d channel audio, where audio device %d expects %d Hz, %d channel", packetRate, packetChannels, a, sampleRate, channels)
			}

			ring.write(pcmLE)

			return nil
		})
	}()
}

// closeUberSDRInput stops audio device a receiving from UberSDR, and waits
// for it to have stopped, so that nothing writes to the ring buffer once
// Close has let go of it.
func (d *AudioDevices) closeUberSDRInput(a int) {
	if d.dev[a].uberSDRCancel == nil {
		return
	}

	d.dev[a].uberSDRCancel()
	<-d.dev[a].uberSDRDoneCh

	d.dev[a].uberSDRCancel = nil
	d.dev[a].uberSDRDoneCh = nil
}
