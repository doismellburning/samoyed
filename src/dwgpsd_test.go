package direwolf

import (
	"bufio"
	"net"
	"testing"
	"time"

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

// fakeGpsd listens as a gpsd would, returning a configuration pointing at it
// and a channel that hands over the connection once the client has asked to
// WATCH, so the test can send it reports.
func fakeGpsd(t *testing.T) (*misc_config_s, <-chan net.Conn) {
	t.Helper()

	var listener, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { listener.Close() })

	var conns = make(chan net.Conn, 1)

	go func() {
		var conn, acceptErr = listener.Accept()
		if acceptErr != nil {
			return
		}

		t.Cleanup(func() { conn.Close() })

		_, _ = bufio.NewReader(conn).ReadString('\n') // ?WATCH=...

		conns <- conn
	}()

	var config = new(misc_config_s)
	config.gpsd_host = "127.0.0.1"
	config.gpsd_port = listener.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // A TCP listener has a TCP address.

	return config, conns
}

// Every GPS used to share one gpsd connection handle, so Term on one closed
// whichever connection had been made last - another GPS's.
func TestGPSTermLeavesAnotherGPSsGpsdConnectionAlone(t *testing.T) {
	var config1, conns1 = fakeGpsd(t)
	var config2, conns2 = fakeGpsd(t)

	var gps1, gps2 = new(GPS), new(GPS)

	require.Equal(t, 1, dwgpsd_init(t.Context(), gps1, config1, 0))
	require.Equal(t, 1, dwgpsd_init(t.Context(), gps2, config2, 0))

	<-conns1
	var server2 = <-conns2

	gps1.Term()

	// Let gps1's reader finish reporting its lost connection before carrying
	// on, so it isn't still printing once the test is over.
	require.Eventually(t, func() bool { return gps1.Read(new(dwgps_info_t)) == DWFIX_ERROR },
		5*time.Second, 10*time.Millisecond, "gps1's reader never noticed it had been shut down")

	var _, writeErr = server2.Write([]byte(`{"class":"TPV","mode":3,"lat":42.6,"lon":-71.3,"altMSL":33.5}` + "\n"))
	require.NoError(t, writeErr)

	var info = new(dwgps_info_t)

	var fix dwfix_t

	var deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fix = gps2.Read(info)
		if fix == DWFIX_3D || fix == DWFIX_ERROR {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	assert.Equal(t, DWFIX_3D, fix, "the other GPS lost its connection to gpsd")
}
