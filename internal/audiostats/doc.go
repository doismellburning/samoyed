// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package audiostats keeps count of what each audio device delivers - how
// many samples, and how many reads that came back with none - and
// periodically reports the sample rate, error count and receive audio levels
// that works out to, as a troubleshooting aid for an audio input that isn't
// decoding anything.  The -a option asks for the reports.
package audiostats
