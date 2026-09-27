package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Bits of the CM108 GPIO PTT support that are not
 *		platform-specific, so that callers on any platform can
 *		interpret the errors returned by the CM108 code.
 *
 *---------------------------------------------------------------*/

import "errors"

// ErrUnknownCM108Device reports that a HID is not one of the USB audio adapters
// known to work for GPIO PTT.  It is advisory rather than fatal - an
// unrecognised device may well work - so callers should warn and carry on.
var ErrUnknownCM108Device = errors.New("not a device known to work with GPIO PTT")

// CM108PermissionAdvice returns advice, one line per element, for a user who
// cannot open a CM108 HID because of the file permissions on it.  Callers
// print it however suits them, for errors that wrap fs.ErrPermission.
func CM108PermissionAdvice(name string) []string {
	return []string{
		"Type \"ls -l " + name + "\" and verify that it has audio group rw similar to this:",
		"    crw-rw---- 1 root audio 247, 0 Oct  6 19:24 " + name,
		"rather than root-only access like this:",
		"    crw------- 1 root root 247, 0 Sep 24 09:40 " + name,
		"This permission should be set by one of:",
		"/etc/udev/rules.d/99-direwolf-cmedia.rules",
		"/usr/lib/udev/rules.d/99-direwolf-cmedia.rules",
		"which should be created by the installation process.",
		"Your account must be in the 'audio' group.",
	}
}
