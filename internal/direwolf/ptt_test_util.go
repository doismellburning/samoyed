// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package direwolf

// serialControlCapture, when set, is given each change to a serial port's
// control lines instead of the port itself.  Neither a pseudo terminal nor a
// plain file has modem control lines - the ioctls simply fail, which is why
// their result is ignored - so this is the only way a test can see which line
// was driven and at what level.
//
// The same shape as toneGenCapture, and for the same reason: a test needs to
// watch something that would otherwise go to hardware.
var serialControlCapture func(bit int, on bool)

// _TIOCM sets or clears one of a serial port's control lines.
func _TIOCM(fd int, value int, on bool) {
	if serialControlCapture != nil {
		serialControlCapture(value, on)

		return
	}

	_TIOCM_real(fd, value, on)
}
