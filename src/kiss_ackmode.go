//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	KISS ACKMODE (G8BPQ extended KISS) transmit-acknowledgement support.
 *
 * Description: Ordinary KISS gives the host no indication of *when* a frame
 *		actually left the TNC.  On HF, where a frame can sit in the
 *		transmit queue for a long time, the host needs to start its
 *		own timers (FRACK, etc.) from the moment the frame is really
 *		on the air, not from the moment it was handed to the TNC.
 *
 *		ACKMODE (G8BPQ multi-drop / extended KISS) solves this.  The
 *		host sends a data frame with the command nibble 0x0C instead
 *		of 0x00 and inserts two opaque "id" bytes between the command
 *		byte and the AX.25 frame:
 *
 *			C0 xC aa bb <ax25 frame> C0      (host -> TNC)
 *
 *		The two id bytes (aa bb) are NOT transmitted over the air.
 *		Once the frame has actually been keyed out, the TNC echoes the
 *		command byte and the same two id bytes back to the host that
 *		sent it (and only that host):
 *
 *			C0 xC aa bb C0                   (TNC -> host)
 *
 *		Reference: Karl Medcalf WK5M, "Multi-Drop KISS Operation" (1991)
 *		https://github.com/packethacking/ax25spec/blob/main/doc/multi-drop-kiss-operation.md
 *		and the G8BPQ/LinBPQ kiss.c reference implementation, which uses
 *		command nibble 0x0C (not 0x0E - that is the separate poll frame)
 *		for the acknowledgement.
 *
 *		The two id bytes are treated as completely opaque and echoed
 *		back verbatim, in the same order, so there is no byte-order
 *		convention to get wrong.
 *
 * Threading:	A KISS data frame flows
 *			kiss_process_msg  ->  tq_append  ->  xmit_thread
 *		across two goroutines.  Rather than thread the id bytes and the
 *		originating client through every layer of the transmit-queue
 *		API, we keep a small side table keyed on the packet pointer.
 *
 *		The entry is registered BEFORE the packet is handed to
 *		tq_append, so it is already present if xmit_thread picks the
 *		packet up immediately, and so a drop inside tq_append can
 *		discard it cleanly.  It is removed exactly once, when the packet
 *		reaches a terminal disposition:
 *
 *			transmitted -> ackmode_notify_sent (sends the ack)
 *			dropped      -> ackmode_discard     (no ack)
 *
 *		The packet pointer is a stable, unique key for the lifetime of
 *		the packet, and the entry is always removed before ax25_delete,
 *		so a later packet that happens to reuse the address cannot match
 *		a stale entry.
 *
 *---------------------------------------------------------------*/

import "sync"

// ackPending carries everything needed to send an ACKMODE acknowledgement back
// to the originating client once the frame has been transmitted.
type ackPending struct {
	id      [2]byte            // opaque id bytes from the host, echoed back verbatim
	sendfun kiss_sendfun       // how to send the ack to the client
	channel int                // radio channel the frame was queued on
	kps     *kissport_status_s // KISS TCP port (nil for serial port / pseudo terminal)
	client  int                // client index within the port (-1 for serial port / pty)
}

var ackmodeMu sync.Mutex
var ackmodePending = map[*packet_t]*ackPending{}

// ackmode_register records that frame pp was sent with ACKMODE and that an
// acknowledgement carrying id should be returned to (kps, client) via sendfun
// once pp has actually been transmitted.  Call this BEFORE handing pp to
// tq_append so the entry exists before xmit_thread can pick the packet up.
func ackmode_register(pp *packet_t, id [2]byte, channel int, sendfun kiss_sendfun, kps *kissport_status_s, client int) {
	ackmodeMu.Lock()
	ackmodePending[pp] = &ackPending{id: id, sendfun: sendfun, channel: channel, kps: kps, client: client}
	ackmodeMu.Unlock()
}

// ackmode_notify_sent sends the ACKMODE acknowledgement for pp, if it was an
// ACKMODE frame, and removes the pending entry.  Call this immediately after
// the frame has been keyed out over the air.  It is a harmless no-op for
// packets that were not registered (i.e. ordinary, non-ACKMODE frames).
func ackmode_notify_sent(pp *packet_t) {
	ackmodeMu.Lock()
	var entry, ok = ackmodePending[pp]
	delete(ackmodePending, pp)
	ackmodeMu.Unlock()

	if !ok {
		return
	}

	// Echo the two opaque id bytes back to the originating client.  The
	// command byte is XKISS_CMD_DATA (0x0C); sendfun (SendRecPacket) adds the
	// channel number in the high nibble.
	var ack = []byte{entry.id[0], entry.id[1]}
	entry.sendfun(entry.channel, XKISS_CMD_DATA, ack, len(ack), entry.kps, entry.client)
}

// ackmode_discard removes the pending ACKMODE entry for pp without sending an
// acknowledgement.  Call this when a registered packet is dropped instead of
// transmitted (invalid channel, queue overflow, clear-channel timeout, ...) so
// the host's timer is not started for a frame that never went out.  It is a
// harmless no-op for packets that were not registered.
func ackmode_discard(pp *packet_t) {
	ackmodeMu.Lock()
	delete(ackmodePending, pp)
	ackmodeMu.Unlock()
}
