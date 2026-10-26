// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hamlib

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dummyModel is Hamlib's dummy rig, which needs no port.  It has no PTT
// unless told to key itself with ptt_type RIG.
const dummyModel = 1

func TestDummyRigPTT(t *testing.T) {
	t.Parallel()

	var r, err = NewRig(dummyModel)
	require.NoError(t, err)

	require.NoError(t, r.SetConf("ptt_type", "RIG"))
	require.NoError(t, r.Open())
	require.NoError(t, r.SetPTT(true))
	require.NoError(t, r.SetPTT(false))
	require.NoError(t, r.Close())
	require.NoError(t, r.Cleanup())
}

func TestSetConf(t *testing.T) {
	t.Parallel()

	var r, err = NewRig(dummyModel)
	require.NoError(t, err)

	t.Cleanup(func() { r.Cleanup() }) //nolint:errcheck

	require.NoError(t, r.SetConf("rig_pathname", "/dev/null"))

	err = r.SetConf("no_such_parameter", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_parameter")
}

func TestNewRigRejectsBadModels(t *testing.T) {
	t.Parallel()

	for _, model := range []int{0, -1, 999999} {
		var _, err = NewRig(model)
		assert.Error(t, err, "model %d", model)
	}
}

func TestCleanedUpRigRefusesCalls(t *testing.T) {
	t.Parallel()

	var r, err = NewRig(dummyModel)
	require.NoError(t, err)
	require.NoError(t, r.Cleanup())

	require.ErrorIs(t, r.Open(), errCleanedUp)
	require.ErrorIs(t, r.SetPTT(true), errCleanedUp)
	require.ErrorIs(t, r.SetConf("rig_pathname", "/dev/null"), errCleanedUp)
	require.ErrorIs(t, r.Close(), errCleanedUp)
	require.ErrorIs(t, r.Cleanup(), errCleanedUp)
}

func TestLoadMissingLibrary(t *testing.T) {
	t.Parallel()

	var _, err = load([]string{"libsamoyed-no-such-hamlib.so.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "libsamoyed-no-such-hamlib.so.0")
	assert.Contains(t, err.Error(), "install Hamlib")
	assert.NotContains(t, err.Error(), "\n", "one line, so it reads as one complaint")
}

// TestLoadMissingLibraryConcurrently runs load from many goroutines at once, so
// that the scheduler is likely to move one to another OS thread between
// purego's dlopen and dlerror calls.  dlerror's state is per thread, so a
// moved goroutine used to get an empty error message.
func TestLoadMissingLibraryConcurrently(t *testing.T) {
	t.Parallel()

	const goroutines = 64

	const iterations = 50

	var wg sync.WaitGroup

	var errs = make(chan string, goroutines*iterations)

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				var _, err = load([]string{"libsamoyed-no-such-hamlib.so.0"})
				if err == nil || !strings.Contains(err.Error(), "libsamoyed-no-such-hamlib.so.0") {
					errs <- fmt.Sprint(err)
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	if len(errs) > 0 {
		assert.Fail(t, "dlopen error lost", "%d of %d loads, e.g. %s", len(errs), goroutines*iterations, <-errs)
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	require.NoError(t, check("rig_open", 0))
	require.EqualError(t, check("rig_open", -6), "rig_open: I/O error, including open failed (hamlib error 6)")
	require.EqualError(t, check("rig_open", -99), "rig_open: unknown error (hamlib error 99)")
	require.EqualError(t, check("rig_open", math.MinInt32), "rig_open: unknown error (hamlib error 2147483648)")
	assert.NoError(t, check("rig_open", 1), "only a negative return is an error")
}
