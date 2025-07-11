package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <netdb.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <arpa/inet.h>
// #include <netinet/in.h>
// #include <netinet/tcp.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include <time.h>
// #include "direwolf.h"
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "version.h"
// #include "digipeater.h"
// #include "tq.h"
// #include "dlq.h"
// #include "igate.h"
// #include "latlong.h"
// #include "pfilter.h"
// #include "dtime_now.h"
// #include "mheard.h"
// extern volatile int igate_sock;
import "C"

import (
	"testing"
)

func igate_test_main(t *testing.T) {
	t.Helper()

	var audio_config C.struct_audio_s
	audio_config.adev[0].num_channels = 2
	C.strcpy(&audio_config.mycall[0][0], C.CString("WB2OSZ-1"))
	C.strcpy(&audio_config.mycall[1][0], C.CString("WB2OSZ-2"))

	var igate_config C.struct_igate_config_s
	C.strcpy(&igate_config.t2_server_name[0], C.CString("localhost"))
	igate_config.t2_server_port = 14580
	C.strcpy(&igate_config.t2_login[0], C.CString("WB2OSZ-JL"))
	C.strcpy(&igate_config.t2_passcode[0], C.CString("-1"))
	igate_config.t2_filter = C.CString("r/1/2/3")

	igate_config.tx_chan = 0
	C.strcpy(&igate_config.tx_via[0], C.CString(",WIDE2-1"))
	igate_config.tx_limit_1 = 3
	igate_config.tx_limit_5 = 5

	var digi_config C.struct_digi_config_s

	C.igate_init(&audio_config, &igate_config, &digi_config, 0)

	var tries = 0
	var timeout = 10
	for C.igate_sock == -1 {
		if tries == timeout {
			t.Fatal("Timeout waiting for igate_init to provide a valid igate_sock")
		}
		SLEEP_SEC(1)
		tries += 1
	}

	var pp C.packet_t

	SLEEP_SEC(2)
	pp = C.ax25_from_text(C.CString("A>B,C,D:Ztest message 1"), 0)
	C.igate_send_rec_packet(0, pp)
	C.ax25_delete(pp)

	SLEEP_SEC(2)
	pp = C.ax25_from_text(C.CString("A>B,C,D:Ztest message 2"), 0)
	C.igate_send_rec_packet(0, pp)
	C.ax25_delete(pp)

	SLEEP_SEC(2)
	pp = C.ax25_from_text(C.CString("A>B,C,D:Ztest message 2"), 0) /* Should suppress duplicate. */
	C.igate_send_rec_packet(0, pp)
	C.ax25_delete(pp)

	SLEEP_SEC(2)
	pp = C.ax25_from_text(C.CString("A>B,TCPIP,D:ZShould drop this due to path"), 0)
	C.igate_send_rec_packet(0, pp)
	C.ax25_delete(pp)

	SLEEP_SEC(2)
	pp = C.ax25_from_text(C.CString("A>B,C,D:?Should drop query"), 0)
	C.igate_send_rec_packet(0, pp)
	C.ax25_delete(pp)

	SLEEP_SEC(5)
	pp = C.ax25_from_text(C.CString("A>B,C,D:}E>F,G*,H:Zthird party stuff"), 0)
	C.igate_send_rec_packet(0, pp)
	C.ax25_delete(pp)

	/*
		for {
		  SLEEP_SEC (20);
		  text_color_set(DW_COLOR_INFO);
		  dw_printf ("Send received packet\n");
		  send_msg_to_server ("W1ABC>APRS:?", strlen("W1ABC>APRS:?"));
		}
	*/
}
