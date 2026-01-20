package direwolf

import "C"
import "unsafe"

/*
Work around the problems of C that expects you to define functions with the same name,
and avoid collisions through selective linking or #ifdefs.

Here the C `kiss_process_msg` can call `kiss_process_msg_override`, and *that* can call a function defined in (e.g.) `cmd/kissutil/main.go` via the variable.

If kiss_process_msg tried to call something defined in cmd/kissutil directly, it was fine for kissutil, but would fail for e.g. cmd/atest

Only use Go types for the variable because C types end up different (C.int vs direwolf.C.int)
*/

var KISS_PROCESS_MSG_OVERRIDE func(unsafe.Pointer, int)

func kiss_process_msg_override(_kiss_msg *C.uchar, kiss_len C.int) {
	if KISS_PROCESS_MSG_OVERRIDE == nil {
		panic("kiss_process_msg_override called but KISS_PROCESS_MSG_OVERRIDE not set!")
	}

	KISS_PROCESS_MSG_OVERRIDE(unsafe.Pointer(_kiss_msg), int(kiss_len))
}
