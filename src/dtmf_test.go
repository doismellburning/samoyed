// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A channel with no tone generator still says how long the transmission would
// have been, as xmit_thread holds the PTT for that long.
func TestDTMFSendWithoutToneGenerator(t *testing.T) {
	var origGenerators = toneGenerators

	t.Cleanup(func() { toneGenerators = origGenerators })

	toneGenerators = [MAX_RADIO_CHANS]*ToneGenerator{}

	assert.Equal(t, 300+400+250, dtmf_send(0, "1234", 10, 300, 250))
}
