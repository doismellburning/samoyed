// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pcmv4 decodes UberSDR's version 4 lossless audio packets: a
// change-tracked header ahead of a predictively coded, Rice-coded body.
//
// The other files here are UberSDR's own receive-side decoder, taken verbatim
// from clients/hpsdr-go/internal/pcmv4 in https://github.com/madpsy/ka9q_ubersdr
// at commit c13b7e70f3800dd9d4d83104e76eaef98e6015b9, along with its tests and
// conformance fixtures. They are kept as they were upstream - and so excluded
// from linting in .golangci.yml - so that a later upstream change can be
// diffed in and carried across.
package pcmv4
