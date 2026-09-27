//nolint:gochecknoglobals
package direwolf

import (
	"fmt"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/stretchr/testify/assert"
)

// The part of Dire Wolf's IL2P unit tests that goes through the HDLC sender
// and receiver; the rest live with the codec in internal/il2p.

func Test_IL2P_serdes(t *testing.T) {
	il2p.Init(0)

	test_serdes(t)
}

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test serialize / deserialize.
//
//	This uses same functions used on the air.
//
/////////////////////////////////////////////////////////////////////////////////////////////

var addrs2 = "AA1AAA-1>ZZ9ZZZ-9"
var addrs3 = "AA1AAA-1>ZZ9ZZZ-9,DIGI*"

func test_serdes(t *testing.T) {
	t.Helper()

	dw_printf("\nTest serialize / deserialize...\n")

	// Frames are sent as v0.4, so the receiver has to read the header FEC
	// Level bit rather than assume the v0.6 fixed size.
	var recorder = il2pLoopback(t, il2p.Version0_4)
	var recCount = 0

	// try combinations of header type, max_fec, polarity, errors.

	for hdr_type := range 1 {
		var packet string
		if hdr_type == 1 {
			packet = fmt.Sprintf("%s:%s", addrs2, il2pTestText)
		} else {
			packet = fmt.Sprintf("%s:%s", addrs3, il2pTestText)
		}
		var pp = ax25.FromText(packet, true)
		assert.NotNil(t, pp)

		var sender = NewHDLCSender(0, nil, 0)

		for max_fec := range 2 {
			for polarity := range 3 { // 2 means throw in some errors.
				var num_bits_sent = sender.sendIL2PFrame(pp, il2p.Version0_4, max_fec, true, polarity)
				dw_printf("%d bits sent.\n", num_bits_sent)

				// Need extra bit at end to flush out state machine.
				recorder.flush()

				// Whatever came back should be the frame that went out, with
				// the errors polarity 2 introduced all corrected.
				for _, frame := range recorder.take() {
					recCount++

					assert.Equal(t, il2pTestText, string(frame.info))

					if polarity == 2 {
						assert.Equal(t, BitFixLevel(10), frame.retries)
					} else {
						assert.Zero(t, frame.retries)
					}
				}
			}
		}
	}

	dw_printf("Serdes receive count = %d\n", recCount)
	assert.Equal(t, 6, recCount, "every frame sent should have been received")
}
