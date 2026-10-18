// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ascii classifies characters the way C's <ctype.h> does in the "C"
// locale, for code ported from Dire Wolf.
//
// The unicode package is no substitute.  unicode.IsUpper(rune(b)) reads a
// byte of 0x80 or above as a Latin-1 code point, so it accepts 0xC0 (À), and
// unicode.IsPunct rejects ASCII symbols such as '$', '+' and '|' that C's
// ispunct accepts.  Ranging over a string yields runes, so there
// unicode.IsDigit accepts the likes of '٣' (Arabic-Indic three), and
// unicode.ToUpper turns 'ı' (dotless i) into 'I'.
//
// Each function takes a byte or a rune; anything outside ASCII is in no class.
package ascii

// IsDigit reports whether c is an ASCII digit, like C's isdigit.
func IsDigit[T byte | rune](c T) bool {
	return c >= '0' && c <= '9'
}

// IsUpper reports whether c is an ASCII upper case letter, like C's isupper.
func IsUpper[T byte | rune](c T) bool {
	return c >= 'A' && c <= 'Z'
}

// IsLower reports whether c is an ASCII lower case letter, like C's islower.
func IsLower[T byte | rune](c T) bool {
	return c >= 'a' && c <= 'z'
}

// IsAlpha reports whether c is an ASCII letter, like C's isalpha.
func IsAlpha[T byte | rune](c T) bool {
	return IsUpper(c) || IsLower(c)
}

// IsAlnum reports whether c is an ASCII letter or digit, like C's isalnum.
func IsAlnum[T byte | rune](c T) bool {
	return IsAlpha(c) || IsDigit(c)
}

// IsPrint reports whether c is a printable ASCII character, space included,
// like C's isprint.
func IsPrint[T byte | rune](c T) bool {
	return c >= ' ' && c <= '~'
}

// IsPunct reports whether c is printable ASCII other than a space, letter or
// digit, like C's ispunct.
func IsPunct[T byte | rune](c T) bool {
	return IsPrint(c) && c != ' ' && !IsAlnum(c)
}

// ToUpper maps an ASCII lower case letter to upper case and leaves anything
// else as it is, like C's toupper.
func ToUpper[T byte | rune](c T) T {
	if IsLower(c) {
		return c - 'a' + 'A'
	}

	return c
}
