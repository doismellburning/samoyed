// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package aprstt is the APRStt gateway: it turns touch tone sequences from
// users without a packet radio into APRS object reports, keeping a table of
// the users it has heard and sending their reports on to the radio, the client
// applications and the IGate.  internal/touchtone translates the button
// sequences it parses.
package aprstt
