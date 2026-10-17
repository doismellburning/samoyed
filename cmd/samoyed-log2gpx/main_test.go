package main

import (
	"os"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// TODO Break down log2gpx into something easier to test...!
// TODO Use a much less brittle test than "compare with exact Dire Wolf output"
func Test_log2gpx(t *testing.T) {
	t.Parallel()

	var input = strings.TrimSpace(`
chan,utime,isotime,source,heard,level,error,dti,name,symbol,latitude,longitude,speed,course,altitude,frequency,offset,tone,system,status,telemetry,comment
0,1752183342,2025-07-10T21:35:42Z,XXXXX-12,XXXXX-12,198(105/99),0,0,XXXXX-12,/j,1,1,1,,,,,,"DireWolf, WB2OSZ",,,
0,1752183353,2025-07-10T21:35:53Z,XXXXX-12,XXXXX-12,199(100/104),0,0,XXXXX-12,/j,1,1,1,,,,,,"DireWolf, WB2OSZ",,,
0,1752183362,2025-07-10T21:36:02Z,XXXXX-12,XXXXX-12,198(105/99),0,0,XXXXX-12,/j,2,2,2,,,,,,"DireWolf, WB2OSZ",,,
0,1752183372,2025-07-10T21:36:12Z,XXXXX-12,XXXXX-12,198(100/104),0,0,XXXXX-12,/j,3,3,3,,,,,,"DireWolf, WB2OSZ",,,
`)

	var expected = strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<gpx version="1.1" creator="Dire Wolf">
  <trk>
    <name>XXXXX-12</name>
    <trkseg>
      <trkpt lat="1.000000" lon="1.000000">
        <speed>0.5</speed>
        <time>2025-07-10T21:35:42Z</time>
      </trkpt>
      <trkpt lat="1.000000" lon="1.000000">
        <speed>0.5</speed>
        <time>2025-07-10T21:35:53Z</time>
      </trkpt>
      <trkpt lat="2.000000" lon="2.000000">
        <speed>1.0</speed>
        <time>2025-07-10T21:36:02Z</time>
      </trkpt>
      <trkpt lat="3.000000" lon="3.000000">
        <speed>1.5</speed>
        <time>2025-07-10T21:36:12Z</time>
      </trkpt>
    </trkseg>
  </trk>
  <wpt lat="3.000000" lon="3.000000">
    <name>XXXXX-12</name>
  </wpt>
</gpx>
`)

	var result = testutils.RunMain(t, input)

	assert.Equal(t, 0, result.Status)
	assert.Equal(t, expected, strings.TrimSpace(result.Output()))
}
