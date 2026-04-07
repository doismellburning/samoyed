//nolint:gochecknoglobals
package direwolf

import "sync"

// ackPending holds enough state to send an ACKMODE reply after a frame has been transmitted.
type ackPending struct {
	seqno   uint16
	sendfun kiss_sendfun
	channel int
	kps     *kissport_status_s
	client  int
}

var ackmodeMu sync.Mutex
var ackmodeMap = map[*packet_t]*ackPending{}

func ackmode_register(pp *packet_t, seqno uint16, channel int,
	sendfun kiss_sendfun, kps *kissport_status_s, client int) {
	ackmodeMu.Lock()
	ackmodeMap[pp] = &ackPending{seqno: seqno, sendfun: sendfun,
		channel: channel, kps: kps, client: client}
	ackmodeMu.Unlock()
}

// ackmode_notify_sent sends an ACKMODE ACK (0x0E + seqno) back to the client
// after the frame has been transmitted over the air.  Safe to call for every
// packet; it is a no-op for non-ACKMODE packets.
func ackmode_notify_sent(pp *packet_t) {
	ackmodeMu.Lock()
	var entry, ok = ackmodeMap[pp]
	delete(ackmodeMap, pp)
	ackmodeMu.Unlock()
	if !ok {
		return
	}
	var seqBytes = []byte{byte(entry.seqno >> 8), byte(entry.seqno & 0xFF)}
	entry.sendfun(entry.channel, XKISS_CMD_POLL, seqBytes, len(seqBytes), entry.kps, entry.client)
}

// ackmode_discard removes a pending ACK without sending it.
// Used when a packet is discarded due to channel timeout.
func ackmode_discard(pp *packet_t) {
	ackmodeMu.Lock()
	delete(ackmodeMap, pp)
	ackmodeMu.Unlock()
}
