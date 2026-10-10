// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package aprs

// For user-defined data format.
// APRS protocol spec Chapter 18 and http://www.aprs.org/aprs11/expfmts.txt

// UserDefUserID : KG 2026-01-19: Dire Wolf has D reserved per
// https://www.aprs.org/aprs11/expfmts.txt and there seems a lot less space for
// me to comfortably just DIY like with version.Tocall (and S is already assigned).
// So I'll stick with D for now as it seems the least-worst option.
const UserDefUserID = 'D'

const UserDefTypeAIS = 'A' // data type A for AIS NMEA sentence
const UserDefTypeEAS = 'E' // data type E for EAS broadcasts
