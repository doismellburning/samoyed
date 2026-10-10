// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package waypoint turns the positions and objects heard over the air into
// NMEA waypoint sentences - generic, Garmin, Magellan and Kenwood - and
// passes AIS sentences on, for a GPS display or mapping application on a
// serial port or at a UDP address.  The symbols each format wants come from
// internal/waypointsym.
package waypoint
