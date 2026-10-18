// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hamlib drives a rig's PTT through Hamlib (https://hamlib.github.io/).
//
// Hamlib is loaded at run time with purego rather than linked with cgo, so it
// is an optional dependency: [Load] says whether it is there, and everything
// else in Samoyed works without it.
//
// Only the part of Hamlib's API that PTT needs is bound.  Port settings go
// through rig_set_conf rather than into the RIG structure, whose layout moves
// between Hamlib versions.
package hamlib

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

// Constants from hamlib/rig.h.
const (
	rigConfEnd = 0       // RIG_CONF_END, what rig_token_lookup returns for an unknown name
	rigVFOCurr = 1 << 29 // RIG_VFO_CURR
	rigPTTOff  = 0       // RIG_PTT_OFF
	rigPTTOn   = 1       // RIG_PTT_ON

	maxDebugLevel = 6 // RIG_DEBUG_CACHE, the most verbose
)

// library is the bound part of a loaded Hamlib.
//
// The C types map as: RIG * to uintptr, rig_model_t and vfo_t to uint32, token_t
// (a long) to int, and the int-sized enums and return codes to int32.
//
// Hamlib isn't thread-safe - two rig_init calls at once can corrupt its backend
// registry - so every call goes through call, which holds mu.
type library struct {
	mu sync.Mutex

	rigInit        func(model uint32) uintptr
	rigTokenLookup func(rig uintptr, name string) int
	rigSetConf     func(rig uintptr, token int, val string) int32
	rigOpen        func(rig uintptr) int32
	rigSetPTT      func(rig uintptr, vfo uint32, ptt int32) int32
	rigClose       func(rig uintptr) int32
	rigCleanup     func(rig uintptr) int32
	rigSetDebug    func(level int32)
}

// libraryNames lists the names Hamlib 4 is tried under.  The soname comes
// first, as only the -dev package provides the unversioned name; trying the
// soname rather than a package name also sidesteps Debian's
// libhamlib4/libhamlib4t64 split.
func libraryNames() []string {
	switch runtime.GOOS {
	case "darwin":
		// A Go binary doesn't search Homebrew's prefix, so name it too.
		return []string{
			"libhamlib.4.dylib",
			"/opt/homebrew/lib/libhamlib.4.dylib",
			"/usr/local/lib/libhamlib.4.dylib",
			"libhamlib.dylib",
		}
	default:
		return []string{"libhamlib.so.4", "libhamlib.so"}
	}
}

// load opens the first of names that it can and binds the functions this
// package uses.
func load(names []string) (*library, error) {
	var handle uintptr
	var openErrs []string

	for _, name := range names {
		var h, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil {
			handle = h

			break
		}

		openErrs = append(openErrs, err.Error())
	}

	// One error, not an errors.Join of the dlopen ones, so that it reads as a
	// whole wherever it is reported.
	if handle == 0 {
		return nil, fmt.Errorf("hamlib library not found - install Hamlib 4 (libhamlib4 or libhamlib4t64, or brew install hamlib) (%s)",
			strings.Join(openErrs, "; "))
	}

	var lib = new(library)
	var symbols = []struct {
		name string
		fptr any
	}{
		{"rig_init", &lib.rigInit},
		{"rig_token_lookup", &lib.rigTokenLookup},
		{"rig_set_conf", &lib.rigSetConf},
		{"rig_open", &lib.rigOpen},
		{"rig_set_ptt", &lib.rigSetPTT},
		{"rig_close", &lib.rigClose},
		{"rig_cleanup", &lib.rigCleanup},
		{"rig_set_debug", &lib.rigSetDebug},
	}

	for _, s := range symbols {
		// RegisterLibFunc would panic on a missing symbol; look it up first.
		var sym, err = purego.Dlsym(handle, s.name)
		if err != nil {
			return nil, fmt.Errorf("hamlib library lacks %s: %w", s.name, err)
		}

		purego.RegisterFunc(s.fptr, sym)
	}

	return lib, nil
}

// call runs f, which calls into Hamlib, with no other call in progress.
func call[T any](lib *library, f func() T) T {
	lib.mu.Lock()
	defer lib.mu.Unlock()

	return f()
}

// debugLevel is the Hamlib debug level asked for, applied whenever the library
// is loaded - Hamlib's own level is process-wide too.
var debugLevel struct { //nolint:gochecknoglobals // Hamlib's debug level is process-wide
	mu     sync.Mutex
	level  int32
	loaded *library // the loaded library, once there is one
}

// loadOnce gives the library [Load] found, or the error it got trying.
var loadOnce = sync.OnceValues(func() (*library, error) { //nolint:gochecknoglobals // a library is loaded once per process
	var lib, err = load(libraryNames())
	if err != nil {
		return nil, err
	}

	debugLevel.mu.Lock()
	defer debugLevel.mu.Unlock()

	setDebug(lib, debugLevel.level)
	debugLevel.loaded = lib

	return lib, nil
})

// Load loads Hamlib, if it hasn't been already, and says why not if it can't.
func Load() error {
	var _, err = loadOnce()

	return err
}

// SetDebugLevel sets Hamlib's debug level, from 0 (none) to 6 (everything).
// It doesn't load Hamlib itself: the level is applied once something does.
func SetDebugLevel(level int) {
	debugLevel.mu.Lock()
	defer debugLevel.mu.Unlock()

	debugLevel.level = int32(max(0, min(level, maxDebugLevel)))

	if debugLevel.loaded != nil {
		setDebug(debugLevel.loaded, debugLevel.level)
	}
}

func setDebug(lib *library, level int32) {
	call(lib, func() struct{} {
		lib.rigSetDebug(level)

		return struct{}{}
	})
}

// Rig is one rig opened through Hamlib.
type Rig struct {
	lib    *library
	handle uintptr // RIG *, zero once cleaned up; guarded by lib.mu
}

// NewRig initialises a rig of the given Hamlib model number - see
// "rigctl --list".  It loads Hamlib if need be.
func NewRig(model int) (*Rig, error) {
	var lib, err = loadOnce()
	if err != nil {
		return nil, err
	}

	return newRig(lib, model)
}

func newRig(lib *library, model int) (*Rig, error) {
	if model <= 0 || model > math.MaxUint32 {
		return nil, fmt.Errorf("rig_init: invalid rig model %d", model)
	}

	var rigModel = uint32(model)

	var handle = call(lib, func() uintptr { return lib.rigInit(rigModel) })
	if handle == 0 {
		return nil, fmt.Errorf("rig_init: unknown rig model %d", model)
	}

	return &Rig{lib: lib, handle: handle}, nil
}

// errCleanedUp is what a call on a rig after Cleanup gets.
var errCleanedUp = errors.New("rig has been cleaned up")

// do runs f, a call of the Hamlib function op, on the rig's handle.
func (r *Rig) do(op string, f func(handle uintptr) int32) error {
	r.lib.mu.Lock()
	defer r.lib.mu.Unlock()

	if r.handle == 0 {
		return errCleanedUp
	}

	return check(op, f(r.handle))
}

// SetConf sets one of the rig's configuration parameters by name, as
// "rigctl --set-conf" would - "rig_pathname" or "serial_speed", say.  Call it
// before Open.
func (r *Rig) SetConf(name string, value string) error {
	var unknown = false

	var err = r.do("rig_set_conf "+name, func(handle uintptr) int32 {
		var token = r.lib.rigTokenLookup(handle, name)
		if token == rigConfEnd {
			unknown = true

			return 0
		}

		return r.lib.rigSetConf(handle, token, value)
	})
	if unknown {
		return fmt.Errorf("rig_set_conf: unknown configuration parameter %q", name)
	}

	return err
}

// Open opens the port to the rig.
func (r *Rig) Open() error {
	return r.do("rig_open", r.lib.rigOpen)
}

// SetPTT keys or unkeys the rig's current VFO.
func (r *Rig) SetPTT(on bool) error {
	var ptt int32 = rigPTTOff
	if on {
		ptt = rigPTTOn
	}

	return r.do("rig_set_ptt", func(handle uintptr) int32 {
		return r.lib.rigSetPTT(handle, rigVFOCurr, ptt)
	})
}

// Close closes the port to the rig.  The rig can be opened again.
func (r *Rig) Close() error {
	return r.do("rig_close", r.lib.rigClose)
}

// Cleanup releases the rig.  Close it first if it is open.
func (r *Rig) Cleanup() error {
	return r.do("rig_cleanup", func(handle uintptr) int32 {
		r.handle = 0

		return r.lib.rigCleanup(handle)
	})
}

// Error is a Hamlib call that failed.
type Error struct {
	Op   string // the Hamlib function, e.g. "rig_open"
	Code int    // the RIG_E* code, as a positive number
}

func (e *Error) Error() string {
	var desc = "unknown error"
	if e.Code >= 0 && e.Code < len(errorDescriptions) {
		desc = errorDescriptions[e.Code]
	}

	return fmt.Sprintf("%s: %s (hamlib error %d)", e.Op, desc, e.Code)
}

// errorDescriptions are hamlib/rig.h's descriptions of the RIG_E* codes,
// indexed by code.
var errorDescriptions = []string{ //nolint:gochecknoglobals // I yearn for const arrays
	"no error",
	"invalid parameter",
	"invalid configuration (serial, ...)",
	"memory shortage",
	"function not implemented",
	"communication timed out",
	"I/O error, including open failed",
	"internal Hamlib error",
	"protocol error",
	"command rejected by the rig",
	"command performed, but argument truncated",
	"function not available",
	"VFO not targetable",
	"error talking on the bus",
	"collision on the bus",
	"NULL RIG handle or invalid pointer parameter",
	"invalid VFO",
	"argument out of domain of function",
	"function deprecated",
	"security error",
	"rig not powered on",
}

// check turns a Hamlib return code - zero, or a negated RIG_E* code - into an
// error.
func check(op string, ret int32) error {
	if ret == 0 {
		return nil
	}

	return &Error{Op: op, Code: int(max(ret, -ret))}
}
