// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pfilter evaluates the packet filter expressions of the FILTER,
// CFILTER and IGFILTER configuration: filter specifications loosely modelled on
// the APRS-IS server-side filter commands, combined with AND, OR, NOT and
// parentheses, deciding whether a digipeater or the IGate passes a packet on.
package pfilter
