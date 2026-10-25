// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sdr turns the IQ samples from a software defined radio into audio
// the demodulators can work on, one stream for each frequency asked for.
//
// RTLTCP talks to rtl_tcp, which serves an RTL-SDR dongle's samples over TCP.
// Tuning works out where to tune the dongle so that every frequency fits in
// what it sends, and Channelizer picks each frequency out of those samples
// and FM demodulates it.
package sdr
