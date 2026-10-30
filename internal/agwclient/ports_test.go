// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePorts(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		name string
		data string
		want []PortInfo
	}{
		{
			name: "samoyed-direwolf with a stereo soundcard",
			data: "2;Port1 first soundcard left;Port2 first soundcard right;",
			want: []PortInfo{{Number: 0, Description: "first soundcard left"}, {Number: 1, Description: "first soundcard right"}},
		},
		{
			name: "gaps in the numbering",
			data: "2;Port1 first soundcard mono;Port11 Network TNC;",
			want: []PortInfo{{Number: 0, Description: "first soundcard mono"}, {Number: 10, Description: "Network TNC"}},
		},
		{
			name: "no trailing semicolon, and a NUL after",
			data: "1;Port1 first soundcard mono\x00junk",
			want: []PortInfo{{Number: 0, Description: "first soundcard mono"}},
		},
		{
			name: "no ports",
			data: "0;",
			want: nil,
		},
		{
			name: "no description",
			data: "1;Port3;",
			want: []PortInfo{{Number: 2, Description: ""}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got, err = parsePorts([]byte(tt.data))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsePortsRefusesNonsense(t *testing.T) {
	t.Parallel()

	for _, data := range []string{
		"",
		"two;Port1 x;",
		"1;Radio1 x;",
		"1;Port0 x;",
		"1;Port257 x;",
		"1;Portx y;",
	} {
		var _, err = parsePorts([]byte(data))
		require.Error(t, err, "%q", data)
	}
}

func FuzzParsePorts(f *testing.F) {
	f.Add([]byte("2;Port1 first soundcard left;Port2 first soundcard right;"))
	f.Add([]byte("1;Port11 Network TNC;"))
	f.Add([]byte("0;"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var ports, err = parsePorts(data)
		if err != nil {
			return
		}

		for _, port := range ports {
			if port.Number < 0 || port.Number > 255 {
				t.Fatalf("port number %d out of range", port.Number)
			}
		}
	})
}
