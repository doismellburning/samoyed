package direwolf

// The TNC side of the KISS protocol - collecting frames from a client
// application and acting on them.  The framing itself is in internal/kiss.

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/sirupsen/logrus"
)

/*-------------------------------------------------------------------
 *
 * Name:        kiss_debug_print
 *
 * Purpose:     Print message to/from client for debugging.
 *
 * Inputs:	fromto		- Direction of message.
 *		special		- Comment if not a KISS frame.
 *		pmsg		- Address of the message block.
 *		msg_len		- Length of the message.
 *
 *--------------------------------------------------------------------*/

func kiss_debug_print(fromto fromto_t, special string, pmsg []byte) {
	logKissMessage(logrus.DebugLevel, fromto, special, pmsg)
}

// logKissMessage is kiss_debug_print at level: Error when the message comes
// with a complaint about it, so the dump shows without -d k.
func logKissMessage(level logrus.Level, fromto fromto_t, special string, pmsg []byte) {
	var direction = []string{"from", "to"}
	var function = []string{
		"Data frame", "TXDELAY", "P", "SlotTime",
		"TXtail", "FullDuplex", "SetHardware", "Invalid 7",
		"Invalid 8", "Invalid 9", "Invalid 10", "Invalid 11",
		"ACKMODE", "Invalid 13", "Invalid 14", "Return"}

	var entry = logrus.WithField("direction", direction[fromto])

	if special == "" {
		if len(pmsg) > 0 && pmsg[0] == kiss.FEND {
			/* Skip over FEND if present. */
			pmsg = pmsg[1:]
		}

		// What is left can be nothing at all - a message that was only an
		// escaped FEND, say - and then there is no command byte to describe.
		if len(pmsg) == 0 {
			entry.Warn("Empty message for KISS client application")

			return
		}

		entry = entry.WithFields(logrus.Fields{
			"function": function[pmsg[0]&0xf],
			"channel":  int(pmsg[0]>>4) & 0xf,
			"length":   len(pmsg),
		})
	} else {
		entry = entry.WithFields(logrus.Fields{
			"function": special,
			"length":   len(pmsg),
		})
	}

	entry.Log(level, "KISS client application message")
	dwutil.LogHexDump(entry, level, pmsg)
}

// kissClientFrame is what goes to a client application for frame, heard on
// channel or answering one of the client's commands: the type byte, made of
// channel and cmd, then the frame, with the KISS framing and escapes added.
// transport names the kind of connection, for the complaint about a frame too
// long to send whole.
func kissClientFrame(channel int, cmd int, frame []byte, debug int, transport string) []byte {
	if len(frame) > ax25.MaxPacketLen {
		logrus.WithFields(logrus.Fields{
			"transport": transport,
			"length":    len(frame),
		}).Error("KISS buffer too small, frame truncated")

		frame = frame[:ax25.MaxPacketLen]
	}

	var stemp = append([]byte{byte(channel&0xf)<<4 | byte(cmd&0xf)}, frame...)

	if debug >= 2 {
		/* AX.25 frame with the CRC removed. */
		var entry = logrus.WithField("length", len(frame))
		entry.Debug("Packet content before adding KISS framing and any escapes")
		dwutil.LogHexDump(entry, logrus.DebugLevel, frame)
	}

	var buf = kiss.Encapsulate(stemp)

	/* This has KISS framing and escapes for sending to client app. */

	if debug > 0 {
		kiss_debug_print(TO_CLIENT, "", buf)
	}

	return buf
}

// kissHandlerTNC is the TNC a KISS client application drives: where its data frames
// go, and the transmit timing its commands change.
type kissHandlerTNC interface {
	// Transmit queues pp to go out on channel, ahead of the originals waiting
	// if it is urgent.
	Transmit(channel int, urgent bool, pp *ax25.Packet)

	// QueuedBytes is how many bytes of frames are waiting to go out on
	// channel, for the TXBUF query.
	QueuedBytes(channel int) int

	// ChannelValid reports whether channel is one a client may transmit on.
	ChannelValid(channel int) bool

	SetTxdelay(channel, value int)
	SetPersist(channel, value int)
	SetSlottime(channel, value int)
	SetTxtail(channel, value int)
	SetFulldup(channel int, value bool)
}

// kissCopier shows msg, a data frame from a client for channel with command
// cmd, to the other clients that want a copy - KISSCOPY - but not to from, the
// client it came from.
type kissCopier func(msg []byte, channel int, cmd int, from kissClient)

// KissHandler acts on what a KISS client application sends the TNC, whichever
// transport it arrives by - TCP, the serial port or the pseudo terminal - so
// each of them hands its client's bytes to the same one.
type KissHandler struct {
	tnc         kissHandlerTNC // Where a client's frames and timing commands go.
	copyToPeers kissCopier     // Where KISSCOPY copies a data frame, or nil.
	acks        *kissAcks      // Sends the ACKMODE acknowledgements for a client's frames.
}

// NewKissHandler builds a KissHandler that hands a client's data frames and
// timing commands to tnc, and copies its data frames with copyToPeers, which
// may be nil.  The goroutines that send a client its ACKMODE
// acknowledgements stop when ctx is cancelled.
func NewKissHandler(ctx context.Context, tnc kissHandlerTNC, copyToPeers kissCopier) *KissHandler {
	var h = new(KissHandler)
	h.tnc = tnc
	h.copyToPeers = copyToPeers
	h.acks = newKissAcks(ctx)

	return h
}

/*-------------------------------------------------------------------
 *
 * Name:        RecByte
 *
 * Purpose:     Process one byte from a KISS client app.
 *
 * Inputs:	kf	- Current state of building a frame.
 *		ch	- A byte from the input stream.
 *		debug	- Activates debug output.
 *		from	- The client application it came from.
 *
 * Outputs:	kf	- Current state is updated.
 *
 * Returns:	none.
 *
 *-----------------------------------------------------------------*/

/*
 * Application might send some commands to put TNC into KISS mode.
 * For example, APRSIS32 sends something like:
 *
 *	<0x0d>
 *	<0x0d>
 *	XFLOW OFF<0x0d>
 *	FULLDUP OFF<0x0d>
 *	KISS ON<0x0d>
 *	RESTART<0x0d>
 *	<0x03><0x03><0x03>
 *	TC 1<0x0d>
 *	TN 2,0<0x0d><0x0d><0x0d>
 *	XFLOW OFF<0x0d>
 *	FULLDUP OFF<0x0d>
 *	KISS ON<0x0d>
 *	RESTART<0x0d>
 *
 * This keeps repeating over and over and over and over again if
 * it doesn't get any sort of response.
 *
 * Let's try to keep it happy by sending back a command prompt.
 */

// kissClient is the client application a message came from, by whichever
// transport, and where any answer to it goes.
type kissClient interface {
	// Reply sends frame, of type cmd for radio channel, to this client alone.
	Reply(channel int, cmd int, frame []byte)

	// Prompt sends text as it is, not as a KISS frame, to appease a client
	// that thinks it is driving a traditional TNC in command mode.
	Prompt(text []byte)

	// RadioChannel is the radio channel a message from this client is for,
	// given the one its KISS type byte names.
	RadioChannel(frameChannel int) int
}

func (h *KissHandler) RecByte(kf *kiss.Collector, ch byte, debug int,
	from kissClient) {
	var chunk = kf.Add(ch)

	switch {
	case chunk.Noise != nil:
		if debug > 0 {
			kiss_debug_print(FROM_CLIENT, "Rejected Noise", chunk.Noise)
		}

		/* Try to appease client app by sending something back. */
		if chunk.EndOfLine {
			if strings.EqualFold("restart\r", string(chunk.Noise)) ||
				strings.EqualFold("reset\r", string(chunk.Noise)) {
				from.Prompt([]byte("\xc0\xc0"))
			} else {
				from.Prompt([]byte("\r\ncmd:"))
			}
		}

	case chunk.Err != nil:
		logrus.Error("KISS message exceeded maximum length, discarded")

	case chunk.Frame != nil:
		if debug > 0 {
			/* As received over the wire from client app. */
			kiss_debug_print(FROM_CLIENT, "", chunk.Frame)
		}

		var unwrapped = kiss.Unwrap(chunk.Frame)

		if len(unwrapped) == 0 {
			// FEND FESC FEND, say: no type byte, so nothing to act on.
			logrus.Error("KISS frame from client application empty once unescaped, ignored")

			return
		}

		if debug >= 2 {
			/* Append CRC to this and it goes out over the radio. */
			/* Don't include the "type" indicator. */
			/* It contains the radio channel and type should always be 0 here. */
			var entry = logrus.WithField("length", len(unwrapped)-1)
			entry.Debug("Packet content after removing KISS framing and any escapes")
			dwutil.LogHexDump(entry, logrus.DebugLevel, unwrapped[1:])
		}

		h.processMsg(unwrapped, from)
	}
} /* end RecByte */

/*-------------------------------------------------------------------
 *
 * Name:        processMsg
 *
 * Purpose:     Process a message from the KISS client.
 *
 * Inputs:	kiss_msg	- Kiss frame with FEND and escapes removed.
 *				  The first byte contains channel and command.
 *
 *		kiss_len	- Number of bytes including the command.
 *
 *		from		- The client application it came from.
 *				  "Set Hardware" can send it a response, and
 *				  an ACKMODE data frame an acknowledgement.
 *
 *-----------------------------------------------------------------*/

// This is used only by the TNC side.

func (h *KissHandler) processMsg(kiss_msg []byte, from kissClient) {
	// New in 1.7:
	// We can have KISS TCP ports which convey only a single radio channel.
	// This is to allow operation by applications which only know how to talk to single radio TNCs.
	// Normally, though, the radio channel is the one the KISS frame names.
	var channel = from.RadioChannel(int(kiss_msg[0]>>4) & 0xf)

	var alevel ax25.ALevel
	var cmd = kiss_msg[0] & 0xf

	// Called once a data frame has been transmitted, for a client that asked
	// to be told.
	var onTransmitted func()

	// What the client sent, for showing it as it came, before an ACKMODE
	// frame is turned into the data frame it carries.
	var received = kiss_msg

	if cmd == kiss.CmdXKissData {
		// ACKMODE: an ordinary data frame, but with a two byte ID in front of
		// it, which goes back to the client, as a frame of its own with the
		// same command, once the frame has gone out over the air.  That lets
		// the client time its retries from when the frame was sent rather
		// than from when it handed it over.
		if len(kiss_msg) < 3 {
			logrus.WithField("channel", channel).Error("KISS ACKMODE data frame is missing its frame ID.")

			return
		}

		var id = bytes.Clone(kiss_msg[1:3])

		onTransmitted = func() {
			h.acks.send(from, channel, id)
		}

		// From here on it is the data frame it carries - which is also what
		// a KISSCOPY client gets, as it didn't ask for an acknowledgement,
		// and may not know what to do with one.
		cmd = kiss.CmdDataFrame
		kiss_msg = append([]byte{kiss_msg[0]&0xf0 | cmd}, kiss_msg[3:]...)
	}

	switch cmd {
	case kiss.CmdDataFrame: /* 0 = Data Frame */
		// The copies carry the channel and command we have already
		// determined, rather than the first byte as it came.
		if h.copyToPeers != nil {
			h.copyToPeers(kiss_msg, channel, int(cmd), from)
		}

		/* Note July 2017: There is a variant of of KISS, called SMACK, that assumes */
		/* a TNC can never have more than 8 channels.  http://symek.de/g/smack.html */
		/* It uses the MSB to indicate that a checksum is added.  I wonder if this */
		/* is why we sometimes hear about a request to transmit on channel 8.  */
		/* Should we have a message that asks the user if SMACK is being used, */
		/* and if so, turn it off in the application configuration? */
		/* Our current default is a maximum of 6 channels but it is easily */
		/* increased by changing one number and recompiling. */

		// Additional information, from Mike Playle, December 2018, for Issue #42
		//
		//	I came across this the other day with Xastir, and took a quick look.
		//	The problem is fixable without the kiss_frame.c hack, which doesn't help with Xastir anyway.
		//
		//	Workaround
		//
		//	After the kissattach command, put the interface into CRC mode "none" with a command like this:
		//
		//	# kissparms -c 1 -p radio
		//
		//	Analysis
		//
		//	The source of this behaviour is the kernel's KISS implementation:
		//
		//	https://elixir.bootlin.com/linux/v4.9/source/drivers/net/hamradio/mkiss.c#L489
		//
		//	It defaults to starting in state CRC_MODE_SMACK_TEST and ending up in mode CRC_NONE
		//	after the first two packets, which have their framing byte modified by this code in the process.
		//	It looks to me like deliberate behaviour on the kernel's part.
		//
		//	Setting the CRC mode explicitly before sending any packets stops this state machine from running.
		//
		//	Is this a bug? I don't know - that's up to you! Maybe it would make sense for Direwolf to set
		//	the CRC mode itself, or to expect this behaviour and ignore these flags on the first packets
		//	received from the Linux pty.
		//
		//	This workaround seems sound to me, though, so perhaps this is just a documentation issue.

		// Would it make sense to implement SMACK?  I don't think so.
		// Adding a checksum to the KISS data offers no benefit because it is very reliable.
		// It violates the original protocol specification which states that 16 radio channels are possible.
		// (Some times the term 'port' is used but I try to use 'channel' all the time because 'port'
		// has too many other meanings. Serial port, TCP port, ...)
		// SMACK imposes a limit of 8.  That limit might have been OK back in 1991 but not now.
		// There are people using more than 8 radio channels (using SDR not traditional radios) with direwolf.

		/* Verify that the radio channel number is valid. */
		/* Any sort of medium should be OK here. */
		if !h.tnc.ChannelValid(channel) {
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"hint": "Are you using AX.25 for Linux?  It might be trying to use a modified " +
					"version of KISS which uses the channel field differently than the " +
					"original KISS protocol specification.  The solution might be to use " +
					"a command like 'kissparms -c 1 -p radio' to set CRC none mode.  " +
					"Another way of doing this is pre-loading the kiss kernel module with CRC disabled: " +
					"sudo /sbin/modprobe -q mkiss crc_force=1",
			}).Error("Invalid transmit channel from KISS client application")

			logKissMessage(logrus.ErrorLevel, FROM_CLIENT, "", received)

			return
		}

		alevel = ax25.ALevel{} //nolint:exhaustruct_v5

		var pp = ax25.FromFrame(kiss_msg[1:], alevel)
		if pp == nil {
			logrus.WithField("channel", channel).Error("Invalid KISS data frame from client application")
		} else {
			pp.SetOnTransmitted(onTransmitted)

			/* How can we determine if it is an original or repeated message? */
			/* If there is at least one digipeater in the frame, AND */
			/* that digipeater has been used, it should go out quickly thru */
			/* the high priority queue. */
			/* Otherwise, it is an original for the low priority queue. */
			h.tnc.Transmit(channel, pp.NumRepeaters() >= 1 && pp.H(ax25.Repeater1) > 0, pp)
		}

	case kiss.CmdTxDelay: /* 1 = TXDELAY */
		if len(kiss_msg) < 2 {
			logMissingKissValue(channel, "TXDELAY")

			return
		}

		var entry = logrus.WithFields(logrus.Fields{
			"channel": channel,
			"txdelay": kiss_msg[1],
			"ms":      int(kiss_msg[1]) * 10,
		})
		entry.Info("KISS protocol set TXDELAY")

		if kiss_msg[1] < 10 || kiss_msg[1] >= 100 {
			entry.WithField("hint", "Read the Dire Wolf User Guide, 'Radio Channel - Transmit Timing' section, to understand what this means").Warn("Extreme value for TXDELAY")
		}

		h.tnc.SetTxdelay(channel, int(kiss_msg[1]))

	case kiss.CmdPersistence: /* 2 = Persistence */
		if len(kiss_msg) < 2 {
			logMissingKissValue(channel, "PERSISTENCE")

			return
		}

		var entry = logrus.WithFields(logrus.Fields{
			"channel": channel,
			"persist": kiss_msg[1],
		})
		entry.Info("KISS protocol set Persistence")

		if kiss_msg[1] < 5 || kiss_msg[1] > 250 {
			entry.WithField("hint", "Read the Dire Wolf User Guide, 'Radio Channel - Transmit Timing' section, to understand what this means").Warn("Extreme value for PERSIST")
		}

		h.tnc.SetPersist(channel, int(kiss_msg[1]))

	case kiss.CmdSlotTime: /* 3 = SlotTime */
		if len(kiss_msg) < 2 {
			logMissingKissValue(channel, "SLOTTIME")

			return
		}

		var entry = logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"slottime": kiss_msg[1],
			"ms":       int(kiss_msg[1]) * 10,
		})
		entry.Info("KISS protocol set SlotTime")

		if kiss_msg[1] < 2 || kiss_msg[1] > 50 {
			entry.WithField("hint", "Read the Dire Wolf User Guide, 'Radio Channel - Transmit Timing' section, to understand what this means").Warn("Extreme value for SLOTTIME")
		}

		h.tnc.SetSlottime(channel, int(kiss_msg[1]))

	case kiss.CmdTxTail: /* 4 = TXtail */
		if len(kiss_msg) < 2 {
			logMissingKissValue(channel, "TXTAIL")

			return
		}

		var entry = logrus.WithFields(logrus.Fields{
			"channel": channel,
			"txtail":  kiss_msg[1],
			"ms":      int(kiss_msg[1]) * 10,
		})
		entry.Info("KISS protocol set TXtail")

		if kiss_msg[1] < 5 {
			entry.WithField("hint", "Read the Dire Wolf User Guide, 'Radio Channel - Transmit Timing' section, to understand what this means").Warn("Setting TXTAIL so low is asking for trouble")
		}

		h.tnc.SetTxtail(channel, int(kiss_msg[1]))

	case kiss.CmdFullDuplex: /* 5 = FullDuplex */
		if len(kiss_msg) < 2 {
			logMissingKissValue(channel, "FULLDUPLEX")

			return
		}
		var val = kiss_msg[1] != 0

		logrus.WithFields(logrus.Fields{
			"channel":     channel,
			"full_duplex": val,
		}).Info("KISS protocol set FullDuplex")
		h.tnc.SetFulldup(channel, val)

	case kiss.CmdSetHardware: /* 6 = TNC specific */
		if len(kiss_msg) < 2 {
			logMissingKissValue(channel, "SET HARDWARE")

			return
		}

		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"command": string(kiss_msg[1:]),
		}).Info("KISS protocol set hardware")
		kiss_set_hardware(channel, kiss_msg[1:], from, h.tnc)

	case kiss.CmdEndKiss: /* 15 = End KISS mode, channel should be 15. */
		/* Ignore it. */
		logrus.WithField("channel", channel).Info("KISS protocol end KISS mode ignored")

	default:
		var hint = "Use the '-d kn' option on the direwolf command line to observe " +
			"all communication with the client application"

		if cmd == kiss.CmdXKissPoll {
			hint += ".  It looks like you are trying to use the polled mode of the XKISS protocol, which is not supported.  " +
				"Change your application settings to use standard KISS, with or without ACKMODE"
		}

		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"command": cmd,
			"hint":    hint,
		}).Error("Invalid KISS command")
		logKissMessage(logrus.ErrorLevel, FROM_CLIENT, "", kiss_msg)
	}
} /* end processMsg */

// logMissingKissValue complains about a command from a KISS client, for
// channel, that should have carried a value and didn't.
func logMissingKissValue(channel int, command string) {
	logrus.WithFields(logrus.Fields{
		"channel": channel,
		"command": command,
	}).Error("Missing value for KISS command")
}

/*-------------------------------------------------------------------
 *
 * Name:        kiss_set_hardware
 *
 * Purpose:     Process the "set hardware" command.
 *
 * Inputs:	channel		- channel, 0 - 15.
 *
 *		command		- All but the first byte.  e.g.  "TXBUF:99"
 *				  Case sensitive.
 *				  Will be modified so be sure caller doesn't care.
 *
 *		from		- The client application it came from.
 *
 *				  This is the tricky part.  We can have any combination of
 *				  serial port, pseudo terminal, and multiple TCP clients.
 *				  We need to send the response to same place where query came
 *				  from, which is what from's Reply does.
 *
 *		tnc		- The TNC whose transmit queue TXBUF reports on.
 *
 *
 * Description:	This is new in version 1.5.  "Set hardware" was previously ignored.
 *
 *		There are times when the client app might want to send configuration
 *		commands, such as modem speed, to the KISS TNC or inquire about its
 *		current state.
 *
 *		The immediate motivation for adding this is that one application wants
 *		to know how many frames are currently in the transmit queue.  This can
 *		be used for throttling of large transmissions and performing some action
 *		after the last frame has been sent.
 *
 *		The original KISS protocol spec offers no guidance on what "Set Hardware" might look
 *		like.  I'm aware of only two, drastically different, implementations:
 *
 *		fldigi - http://www.w1hkj.com/FldigiHelp-3.22/kiss_command_page.html
 *
 *			Everything is in human readable in both directions:
 *
 *			COMMAND: [ parameter [ , parameter ... ] ]
 *
 *			Lack of a parameter, in the client to TNC direction, is a query
 *			which should generate a response in the same format.
 *
 *		    Used by applications, http://www.w1hkj.com/FldigiHelp/kiss_host_prgs_page.html
 *			- BPQ32
 *			- UIChar
 *			- YAAC
 *
 *		mobilinkd - https://raw.githubusercontent.com/mobilinkd/tnc1/tnc2/bertos/net/kiss.c
 *
 *			Single byte with the command / response code, followed by
 *			zero or more value bytes.
 *
 *		    Used by applications:
 *			- APRSdroid
 *
 *		It would be beneficial to adopt one of them rather than doing something
 *		completely different.  It might even be possible to recognize both.
 *		This might allow leveraging of other existing applications.
 *
 *		Let's start with the easy to understand human readable format.
 *
 * Commands:	(Client to TNC, with parameter(s) to set something.)
 *
 *			none yet
 *
 * Queries:	(Client to TNC, no parameters, generate a response.)
 *
 *			Query		Response		Comment
 *			-----		--------		-------
 *
 *			TNC:		TNC:DIREWOLF 9.9	9.9 represents current version.
 *
 *			TXBUF:		TXBUF:999		Number of bytes (not frames) in transmit queue.
 *
 *--------------------------------------------------------------------*/

func kiss_set_hardware(channel int, command []byte, from kissClient, tnc kissHandlerTNC) {
	var cmd, value, found = bytes.Cut(command, []byte{':'})

	if found {
		if bytes.Equal(cmd, []byte("TNC")) { /* TNC - Identify software version. */
			if len(value) > 0 {
				logrus.WithFields(logrus.Fields{
					"command":   "TNC",
					"parameter": string(value),
				}).Error("KISS Set Hardware query did not expect a parameter")
			}

			var response = fmt.Sprintf("DIREWOLF %d.%d", version.Major, version.Minor)
			from.Reply(channel, kiss.CmdSetHardware, []byte(response))
		} else if bytes.Equal(cmd, []byte("TXBUF")) { /* TXBUF - Number of bytes in transmit queue. */
			if len(value) > 0 {
				logrus.WithFields(logrus.Fields{
					"command":   "TXBUF",
					"parameter": string(value),
				}).Error("KISS Set Hardware query did not expect a parameter")
			}

			var response = fmt.Sprintf("TXBUF:%d", tnc.QueuedBytes(channel))
			from.Reply(channel, kiss.CmdSetHardware, []byte(response))
		} else {
			logrus.WithField("command", string(cmd)).Error("KISS Set Hardware unrecognized command")
		}
	} else {
		logrus.WithField("command", string(command)).Error("KISS Set Hardware expected the form COMMAND:[parameter[,parameter...]]")
	}
} /* end kiss_set_hardware */
