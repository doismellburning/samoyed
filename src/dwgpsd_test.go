package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parse_gpsd_tpv(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantErr   bool
		wantMode  int
		checkLat  bool
		wantLat   float64
		checkAlt  bool
		wantAlt   float64
		checkSpd  bool
		wantSpeed float64
	}{
		{
			name:      "non-TPV class is ignored",
			line:      `{"class":"SKY","device":"/dev/ttyACM0"}`,
			wantErr:   true,
			wantMode:  0,
			checkLat:  false,
			wantLat:   0,
			checkAlt:  false,
			wantAlt:   0,
			checkSpd:  false,
			wantSpeed: 0,
		},
		{
			name:      "malformed JSON",
			line:      `{"class":"TPV"`,
			wantErr:   true,
			wantMode:  0,
			checkLat:  false,
			wantLat:   0,
			checkAlt:  false,
			wantAlt:   0,
			checkSpd:  false,
			wantSpeed: 0,
		},
		{
			name:      "3D fix with altMSL",
			line:      `{"class":"TPV","mode":3,"lat":42.61857,"lon":-71.34817,"altMSL":41.4,"track":180.0,"speed":1.5}`,
			wantErr:   false,
			wantMode:  3,
			checkLat:  true,
			wantLat:   42.61857,
			checkAlt:  true,
			wantAlt:   41.4,
			checkSpd:  true,
			wantSpeed: 1.5 * MPS_TO_KNOTS,
		},
		{
			name:      "3D fix falls back to legacy alt field",
			line:      `{"class":"TPV","mode":3,"lat":42.0,"lon":-71.0,"alt":100.0}`,
			wantErr:   false,
			wantMode:  3,
			checkLat:  false,
			wantLat:   0,
			checkAlt:  true,
			wantAlt:   100.0,
			checkSpd:  false,
			wantSpeed: 0,
		},
		{
			name:      "no fix",
			line:      `{"class":"TPV","mode":1}`,
			wantErr:   false,
			wantMode:  1,
			checkLat:  false,
			wantLat:   0,
			checkAlt:  false,
			wantAlt:   0,
			checkSpd:  false,
			wantSpeed: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var report, err = parse_gpsd_tpv([]byte(tt.line))

			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, report)
			assert.Equal(t, tt.wantMode, report.Mode)

			if tt.checkLat {
				require.NotNil(t, report.Lat)
				assert.InDelta(t, tt.wantLat, *report.Lat, 0.00001)
			}

			var info = new(dwgps_info_t)
			apply_gpsd_tpv(info, report)

			if tt.checkAlt {
				assert.InDelta(t, tt.wantAlt, maybe.FromJust(info.altitude), 0.001)
			}

			if tt.checkSpd {
				assert.InDelta(t, tt.wantSpeed, maybe.FromJust(info.speed_knots), 0.001)
			}
		})
	}
}

func Test_apply_gpsd_tpv_no_fix_keeps_last_location(t *testing.T) {
	var info = new(dwgps_info_t)
	info.fix = DWFIX_3D
	info.dlat = maybe.Just(42.0)
	info.dlon = maybe.Just(-71.0)
	info.altitude = maybe.Just(10.0)

	var report, err = parse_gpsd_tpv([]byte(`{"class":"TPV","mode":1}`))
	require.NoError(t, err)
	require.NotNil(t, report)

	apply_gpsd_tpv(info, report)

	assert.Equal(t, DWFIX_NO_FIX, info.fix)
	assert.InDelta(t, 42.0, maybe.FromJust(info.dlat), 0.00001)
	assert.InDelta(t, -71.0, maybe.FromJust(info.dlon), 0.00001)
	assert.InDelta(t, 10.0, maybe.FromJust(info.altitude), 0.00001)
}

func Test_apply_gpsd_tpv_2d_keeps_last_altitude(t *testing.T) {
	var info = new(dwgps_info_t)
	info.fix = DWFIX_3D
	info.altitude = maybe.Just(123.0)

	var report, err = parse_gpsd_tpv([]byte(`{"class":"TPV","mode":2,"lat":1.0,"lon":2.0}`))
	require.NoError(t, err)
	require.NotNil(t, report)

	apply_gpsd_tpv(info, report)

	assert.Equal(t, DWFIX_2D, info.fix)
	assert.InDelta(t, 123.0, maybe.FromJust(info.altitude), 0.00001)
}

func Test_dwgps_info_zero_value_is_nothing_known(t *testing.T) {
	var info dwgps_info_t

	assert.Equal(t, DWFIX_NOT_SEEN, info.fix)
	assert.Equal(t, maybe.Nothing[float64](), info.dlat)
	assert.Equal(t, maybe.Nothing[float64](), info.dlon)
	assert.Equal(t, maybe.Nothing[float64](), info.speed_knots)
	assert.Equal(t, maybe.Nothing[float64](), info.track)
	assert.Equal(t, maybe.Nothing[float64](), info.altitude)
}

func Test_apply_gpsd_tpv_absent_fields_are_nothing(t *testing.T) {
	var info = new(dwgps_info_t)

	// A 2D report from $GPRMC alone carries neither altitude nor, when
	// stationary, a track.
	var report, err = parse_gpsd_tpv([]byte(`{"class":"TPV","mode":2,"lat":42.0,"lon":-71.0}`))
	require.NoError(t, err)

	apply_gpsd_tpv(info, report)

	assert.Equal(t, maybe.Just(42.0), info.dlat)
	assert.Equal(t, maybe.Nothing[float64](), info.track)
	assert.Equal(t, maybe.Nothing[float64](), info.speed_knots)
	assert.Equal(t, maybe.Nothing[float64](), info.altitude)
}
