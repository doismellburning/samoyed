package direwolf

import (
	"github.com/doismellburning/samoyed/internal/ax25"
)

// il2p_deliver_packet is the il2p.PacketSink used in normal operation,
// passing each packet the IL2P receiver decodes on to the rest of the receive
// path.
func il2p_deliver_packet(channel int, subchannel int, slice int, pp *ax25.Packet, corrected int) {
	var alevel = demod_get_audio_level(channel, subchannel)

	// TODO: Could we put last 3 arguments in packet object rather than passing around separately?

	multi_modem_process_rec_packet(channel, subchannel, slice, pp, alevel, BitFixLevel(corrected), fec_type_il2p)
}
