package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <ctype.h>
// #include <math.h>
// #include "ax25_pad.h"
// #include "ax25_pad2.h"
// #include "xid.h"
// #include "textcolor.h"
// #include "dlq.h"
// #include "tq.h"
// #include "ax25_link.h"
// #include "dtime_now.h"
// #include "server.h"
// #include "ptt.h"
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ax25_link_test_main(t *testing.T) {
	t.Helper()

	TestAX25LinkConnectedBasic(t)
}

// Pokes at some of the state machine API in the style of recv_process from recv.go
func TestAX25LinkConnectedBasic(t *testing.T) {
	t.Helper()

	// Setup

	var MY_CALL = C.CString("M6KGG")
	var THEIR_CALL = C.CString("2E0KGG")
	const CHANNEL C.int = 1

	var audioConfig = new(C.struct_audio_s)
	C.ptt_init(audioConfig)
	tq_init(audioConfig)

	var miscConfig = new(C.struct_misc_config_s)
	ax25_link_init(miscConfig, 1)

	list_head = nil

	var E *C.dlq_item_t
	var pp C.packet_t
	var addrs [C.AX25_MAX_ADDRS][C.AX25_MAX_ADDR_LEN]C.char

	// Setup done, let's do stuff!

	// Connect request

	E = new(C.dlq_item_t)
	E._type = C.DLQ_CONNECT_REQUEST
	E._chan = CHANNEL
	C.strcpy(&E.addrs[OWNCALL][0], MY_CALL)
	C.strcpy(&E.addrs[PEERCALL][0], THEIR_CALL)
	E.num_addr = 2

	dl_connect_request(E)

	// Now acknowledge

	C.strcpy(&addrs[OWNCALL][0], THEIR_CALL)
	C.strcpy(&addrs[PEERCALL][0], MY_CALL)
	pp = C.ax25_u_frame(&addrs[0], 2, cr_cmd, frame_type_U_UA, 1, 1, nil, 0)
	assert.NotNil(t, pp)

	E = new(C.dlq_item_t)
	E._chan = CHANNEL
	E.pp = pp

	lm_data_indication(E)

	// And now we should be connected!

	assert.NotNil(t, list_head)
	assert.Equal(t, state_3_connected, list_head.state, "%+v", list_head)
}
