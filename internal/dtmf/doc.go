// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dtmf decodes DTMF, "touch tones", from audio samples with the
// Goertzel algorithm, and generates the audio to send a string of them to a
// SampleSink, such as a channel's tone generator.
package dtmf
