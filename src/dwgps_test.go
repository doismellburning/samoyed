// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// With no GPS receiver to read, dwgps_read says so, rather than that one is
// running but has yet to be heard from - TBEACON's config check, for one,
// relies on telling the two apart.
func TestDWGPSReadWithoutAReceiver(t *testing.T) {
	var testCases = map[string]string{
		"none configured": "",
		"can't be opened": filepath.Join(t.TempDir(), "missing"),
	}

	for name, port := range testCases {
		t.Run(name, func(t *testing.T) {
			var config = new(misc_config_s)
			config.gpsnmea_port = port

			dwgps_init(context.Background(), config, 0)

			var info dwgps_info_t

			assert.Equal(t, DWFIX_NOT_INIT, dwgps_read(&info))
		})
	}
}
