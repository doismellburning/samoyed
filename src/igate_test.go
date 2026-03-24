package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_is_message_message(t *testing.T) {
	tests := []struct {
		name  string
		infop string
		want  bool
	}{
		{
			name:  "no colon prefix",
			infop: "W1AW>APRS:Hello",
			want:  false,
		},
		{
			name:  "too short",
			infop: ":ABCDE",
			want:  false,
		},
		{
			name:  "exactly 10 chars (too short for addressee delimiter)",
			infop: ":123456789",
			want:  false,
		},
		{
			name:  "telemetry PARM keyword",
			infop: ":ABCDEFGHI:PARM.something",
			want:  false,
		},
		{
			name:  "telemetry UNIT keyword",
			infop: ":ABCDEFGHI:UNIT.something",
			want:  false,
		},
		{
			name:  "telemetry EQNS keyword",
			infop: ":ABCDEFGHI:EQNS.something",
			want:  false,
		},
		{
			name:  "telemetry BITS keyword",
			infop: ":ABCDEFGHI:BITS.something",
			want:  false,
		},
		{
			name:  "bulletin BLN prefix",
			infop: ":BLN_someXX:this is a bulletin",
			want:  false,
		},
		{
			name:  "weather NWS prefix",
			infop: ":NWS_someXX:weather alert",
			want:  false,
		},
		{
			name:  "weather SKY prefix",
			infop: ":SKY_someXX:sky forecast",
			want:  false,
		},
		{
			name:  "weather CWA prefix",
			infop: ":CWA_someXX:watch area",
			want:  false,
		},
		{
			name:  "weather BOM prefix",
			infop: ":BOM_someXX:bureau message",
			want:  false,
		},
		{
			name:  "valid message",
			infop: ":W1AW     :Hello there!",
			want:  true,
		},
		{
			name:  "valid ack",
			infop: ":W1AW     :ack42",
			want:  true,
		},
		{
			name:  "valid rej",
			infop: ":W1AW     :rej99",
			want:  true,
		},
		{
			name:  "exactly 11 chars, valid",
			infop: ":W1AW     :",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, is_message_message(tt.infop))
		})
	}
}
