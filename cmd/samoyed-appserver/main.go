/*------------------------------------------------------------------
 *
 * Purpose:   	Simple application server for connected mode AX.25.
 *
 *		This demonstrates how you can write a application that will wait for
 *		a connection from another station and respond to commands.
 *		It can be used as a starting point for developing your own applications.
 *
 * Description:	This attaches to an instance of Dire Wolf via the AGW network interface.
 *		It processes commands from other radio stations and responds.
 *
 *---------------------------------------------------------------*/
//nolint:gochecknoglobals
package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

var mycall Callsign /* Callsign, with SSID, for the application. */
/* Future?  Could have multiple applications, on the same */
/* radio channel, each with its own SSID. */

var tnc_hostname = "localhost" /* DNS host name or IPv4 address. */
/* Some of the code is there for IPv6 but */
/* needs more work. */

var tnc_port = "8000" /* a TCP port number  */

/*
 * Maintain information about connections from users which we will call "sessions."
 * It should be possible to have multiple users connected at the same time.
 *
 * This allows a "who" command to see who is currently connected and a place to keep
 * possible state information for each user.
 *
 * Each combination of channel & callsign is a separate session.
 * The same user (callsign), on a different channel, is a different session.
 */

type sessionKey struct {
	channel byte
	addr    Callsign
}

type session struct {
	channel    byte     // Radio channel.
	clientAddr Callsign // Callsign of other station.

	loginTime time.Time // Time when connection established.

	// For the timing test.
	// Send specified number of frames, optional length.
	// When finished summarize with statistics.

	ttStartTime time.Time
	ttCount     int // Number to send.
	ttLength    int // Bytes in info part.
	ttNext      int // Next sequence to send.

	txQueueLen int // Number in transmit queue.  For flow control.
}

type appServer struct {
	sessions map[sessionKey]*session
}

func newAppServer() *appServer {
	var srv = new(appServer)

	srv.sessions = make(map[sessionKey]*session)

	return srv
}

var srv = newAppServer()

// findSession looks up an existing session, returning nil if there isn't one.
func (srv *appServer) findSession(channel byte, addr Callsign) *session {
	return srv.sessions[sessionKey{channel: channel, addr: addr}]
}

// createSession returns the existing session for channel/addr, creating one if necessary.
func (srv *appServer) createSession(channel byte, addr Callsign) *session {
	var key = sessionKey{channel: channel, addr: addr}

	if s, ok := srv.sessions[key]; ok {
		return s
	}

	var s = new(session)

	s.channel = channel
	s.clientAddr = addr
	s.loginTime = time.Now()

	srv.sessions[key] = s

	return s
}

func (srv *appServer) removeSession(channel byte, addr Callsign) {
	delete(srv.sessions, sessionKey{channel: channel, addr: addr})
}

// sortedSessions returns the current sessions in a stable order, for display purposes.
func (srv *appServer) sortedSessions() []*session {
	var sessions = make([]*session, 0, len(srv.sessions))

	for _, s := range srv.sessions {
		sessions = append(sessions, s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].channel != sessions[j].channel {
			return sessions[i].channel < sessions[j].channel
		}

		return string(sessions[i].clientAddr[:]) < string(sessions[j].clientAddr[:])
	})

	return sessions
}

/*------------------------------------------------------------------
 *
 * Name: 	main
 *
 * Purpose:   	Attach to Dire Wolf TNC, wait for requests from users.
 *
 * Usage:	Described above.
 *
 *---------------------------------------------------------------*/

func main() {
	/*
	 * Extract command line args.
	 */
	var _tnc_hostname = pflag.StringP("hostname", "h", "localhost", "TNC Hostname.")

	var _tnc_port = pflag.StringP("port", "p", "8000", "TNC Port.")

	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - Simple application server for connected mode AX.25\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] MYCALL\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "MYCALL is the callsign for which the TNC will accept connections.\n")
		pflag.PrintDefaults()
	}

	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	tnc_hostname = *_tnc_hostname
	tnc_port = *_tnc_port

	if len(pflag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Exactly one argument required (MYCALL) - got %s\n", pflag.Args())
		os.Exit(1)
	}

	var _mycall = pflag.Arg(0)
	if len(_mycall) > len(mycall) {
		fmt.Fprintf(os.Stderr, "Callsign %s too long (maximum length %d)\n", _mycall, len(mycall))
		os.Exit(1)
	}

	copy(mycall[:], []byte(strings.ToUpper(pflag.Arg(0))))

	/*
	 * Establish a TCP socket to the network TNC.
	 * It starts up a thread, which listens for messages from the TNC,
	 * and calls the corresponding agw_cb_... callback functions.
	 *
	 * After attaching to the TNC, the specified init function is called.
	 * We pass it to the library, rather than doing it here, so it can
	 * repeated automatically if the TNC goes away and comes back again.
	 * We need to reestablish what it knows about the application.
	 */
	var initErr = agwlib_init(tnc_hostname, tnc_port, agwlib_G_ask_port_information)
	if initErr != nil {
		fmt.Printf("Could not attach to network TNC %s:%s: %s.\n", tnc_hostname, tnc_port, initErr)
		os.Exit(1)
	}
	/*
	 * Send command to ask what channels are available.
	 * The response will be handled by agw_cb_G_port_information.
	 */
	// FIXME:  Need to do this again if we lose TNC and reattach to it.
	///   should happen automatically now.   agwlib_G_ask_port_information ();
	for {
		direwolf.SLEEP_SEC(1) // other places based on 1 second assumption.
		srv.pollTimingTest()
	}
} /* end main */

func (srv *appServer) pollTimingTest() {
	for _, s := range srv.sessions {
		s.pollTimingTest()
	}
}

func (s *session) pollTimingTest() {
	if s.ttCount == 0 {
		return // nothing to do
	}

	if s.ttNext <= s.ttCount {
		var rem = s.ttCount - s.ttNext + 1 // remaining to send.

		agwlib_Y_outstanding_frames_for_station(s.channel, mycall, s.clientAddr)
		direwolf.SLEEP_MS(10)

		if s.txQueueLen > 128 {
			return // enough queued up for now.
		}

		if rem > 64 {
			rem = 64 // add no more than 64 at a time.
		}

		for range rem {
			var c = 'a'

			var stuff = fmt.Sprintf("%06d ", s.ttNext)
			for k := len(stuff); k < s.ttLength-1; k++ {
				stuff += string(c)

				c++
				if c == 'z'+1 {
					c = 'A'
				}

				if c == 'Z'+1 {
					c = '0'
				}

				if c == '9'+1 {
					c = 'a'
				}
			}

			stuff += "\r"
			agwlib_D_send_connected_data(s.channel, 0xF0, mycall, s.clientAddr, []byte(stuff))

			s.ttNext++
		}
	} else {
		// All done queuing up the packets.
		// Wait until they have all been sent and ack'ed by other end.
		agwlib_Y_outstanding_frames_for_station(s.channel, mycall, s.clientAddr)
		direwolf.SLEEP_MS(10)

		if s.txQueueLen > 0 {
			return // not done yet.
		}

		var elapsed = time.Since(s.ttStartTime)
		if elapsed <= 0 {
			elapsed = 1 // avoid divide by 0
		}

		var byte_count = s.ttCount * s.ttLength

		var summary = fmt.Sprintf("%d bytes in %d seconds, %d bytes/sec, efficiency %d%% at 1200, %d%% at 9600.\r",
			byte_count, elapsed, int(float64(byte_count)/elapsed.Seconds()),
			int(float64(byte_count)*8*100/elapsed.Seconds()/1200),
			int(float64(byte_count)*8*100/elapsed.Seconds()/9600))

		agwlib_D_send_connected_data(s.channel, 0xF0, mycall, s.clientAddr, []byte(summary))
		s.ttCount = 0 // all done.
	}
} // end session.pollTimingTest

/*-------------------------------------------------------------------
 *
 * Name:        agw_cb_C_connection_received
 *
 * Purpose:     Callback for the "connection received" command from the TNC.
 *
 * Inputs:	chan		- Radio channel, first is 0.
 *
 *		call_from	- Address of other station.
 *
 *		call_to		- Callsign I responded to.  (could be an alias.)
 *
 *		data_len	- Length of data field.
 *
 *		data		- Should look something like this for incoming:
 *					*** CONNECTED to Station xxx\r
 *
 * Description:	Add to the sessions table.
 *
 *--------------------------------------------------------------------*/

/*-------------------------------------------------------------------
 *
 * Name:        on_C_connection_received
 *
 * Purpose:     Callback for the "connection received" command from the TNC.
 *
 * Inputs:	chan		- Radio channel, first is 0.
 *
 *		call_from	- Address of other station.
 *
 *		call_to		- My call.
 *				  In the case of an incoming connect request (i.e. to
 *				  a server) this is the callsign I responded to.
 *				  It is possible to define additional aliases and respond
 *				  to any one of them.  It would be possible to have a server
 *				  that responds to multiple names and behaves differently
 *				  depending on the name.
 *
 *		incoming	- true(1) if other station made connect request.
 *				  false(0) if I made request and other statio accepted.
 *
 *		data		- Should look something like this for incoming:
 *					*** CONNECTED to Station xxx\r
 *				  and this for my request being accepted:
 *					*** CONNECTED With Station xxx\r
 *
 *		session_id	- Session id to be used in data transfer and
 *				  other control functions related to this connection.
 *				  Think of it like a file handle.  Once it is open
 *				  we usually don't care about the name anymore and
 *				  and just refer to the handle.  This is used to
 *				  keep track of multiple connections at the same
 *				  time.  e.g. a server could be handling multiple
 *				  clients at once on the same or different channels.
 *
 * Description:	Add to the table of clients.
 *
 *--------------------------------------------------------------------*/

// old void agw_cb_C_connection_received (int chan, char *call_from, char *call_to, int data_len, char *data)
func on_C_connection_received(channel byte, call_from Callsign, call_to Callsign, incoming bool, data []byte) { //nolint:unparam
	srv.createSession(channel, call_from)

	fmt.Printf("Begin session %d,%s: %s\n", channel, call_from, data)

	// Send greeting.

	var greeting = "Welcome!  Type ? for list of commands or HELP <command> for details.\r"

	agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(greeting))
} /* end agw_cb_C_connection_received */

/*-------------------------------------------------------------------
 *
 * Name:        agw_cb_d_disconnected
 *
 * Purpose:     Process the "disconnected" command from the TNC.
 *
 * Inputs:	chan		- Radio channel.
 *
 *		call_from	- Address of other station.
 *
 *		call_to		- Callsign I responded to.  (could be aliases.)
 *
 *		data_len	- Length of data field.
 *
 *		data		- Should look something like one of these:
 *					*** DISCONNECTED RETRYOUT With xxx\r
 *					*** DISCONNECTED From Station xxx\r
 *
 * Description:	Remove from the sessions table.
 *
 *--------------------------------------------------------------------*/

func agw_cb_d_disconnected(channel byte, call_from Callsign, call_to Callsign, data []byte) { //nolint:unparam
	var dataStr = strings.TrimSpace(string(data))

	fmt.Printf("End session %d,%s: %s\n", channel, call_from, dataStr)

	srv.removeSession(channel, call_from)
} /* end agw_cb_d_disconnected */

/*-------------------------------------------------------------------
 *
 * Name:        agw_cb_D_connected_data
 *
 * Purpose:     Process "connected ax.25 data" from the TNC.
 *
 * Inputs:	chan		- Radio channel.
 *
 *		addr		- Address of other station.
 *
 *		msg		- What the user sent us.  Probably a command.
 *
 * Global In:	tnc_sock	- Socket for TNC.
 *
 * Description:	Remove from the session table.
 *
 *--------------------------------------------------------------------*/

// commandHandler implements one connected-mode user command (e.g. "who", "bye").
type commandHandler func(s *session, channel byte, call_to Callsign, call_from Callsign, rest []byte)

var commandTable = map[string]commandHandler{
	"who":  cmd_who,
	"test": cmd_test,
	"bye":  cmd_bye,
	"help": cmd_help,
	"?":    cmd_help,
}

func agw_cb_D_connected_data(channel byte, call_from Callsign, call_to Callsign, data []byte) {
	var s = srv.findSession(channel, call_from)

	var dataStr = strings.TrimSpace(string(data))

	// TODO: Should timestamp to all output.

	if s == nil {
		// Uh oh. Data from some station when not connected.
		fmt.Printf("Internal error.  Incoming data, no corresponding session: %d,%s: %s\n", channel, call_from, dataStr)
		return
	}

	fmt.Printf("%d,%s: %s\n", channel, call_from, dataStr)

	// Process the command from user.

	var _pcmd, rest, _ = BytesCut(data, ' ')

	var pcmd = string(_pcmd)
	if pcmd == "" {
		var greeting = "Type ? for list of commands or HELP <command> for details.\r"

		agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(greeting))

		return
	}

	var handler, ok = commandTable[strings.ToLower(pcmd)]
	if !ok {
		// command not recognized.
		var greeting = "Invalid command. Type ? for list of commands or HELP <command> for details.\r"

		agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(greeting))

		return
	}

	handler(s, channel, call_to, call_from, rest)
} /* end agw_cb_D_connected_data */

// cmd_who lists people currently logged in.
func cmd_who(s *session, channel byte, call_to Callsign, call_from Callsign, rest []byte) {
	var greeting = "Session Channel User   Since\r"

	agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(greeting))

	for n, other := range srv.sortedSessions() {
		var line = fmt.Sprintf("  %2d       %d    %-9s [time later]\r", n, other.channel, other.clientAddr)

		agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(line))
	}
}

// cmd_test runs a timing test: send the specified number of frames with optional length.
func cmd_test(s *session, channel byte, call_to Callsign, call_from Callsign, rest []byte) {
	var _pcount, rest2, _ = BytesCut(rest, ' ')

	var pcount = string(_pcount)

	var _plength, _, _ = BytesCut(rest2, ' ')

	var plength = string(_plength)

	s.ttStartTime = time.Now()
	s.ttNext = 1
	s.ttLength = 256
	s.ttCount = 1

	if plength != "" {
		s.ttLength, _ = strconv.Atoi(plength)
		if s.ttLength < 16 {
			s.ttLength = 16
		}

		if s.ttLength > AX25_MAX_INFO_LEN {
			s.ttLength = AX25_MAX_INFO_LEN
		}
	}

	if pcount != "" {
		s.ttCount, _ = strconv.Atoi(pcount)
	}
}

// cmd_bye disconnects the user.
func cmd_bye(s *session, channel byte, call_to Callsign, call_from Callsign, rest []byte) {
	var greeting = "Thank you folks for kindly droppin' in.  Y'all come on back now, ya hear?\r"

	agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(greeting))
	// Ideally we'd want to wait until nothing in the outgoing queue
	// to that station so we know the message was received.
	direwolf.SLEEP_SEC(10)
	agwlib_d_disconnect(channel, call_to, call_from)
}

// cmd_help prints help text.
func cmd_help(s *session, channel byte, call_to Callsign, call_from Callsign, rest []byte) {
	var greeting = "Help not yet available.\r"

	agwlib_D_send_connected_data(channel, 0xF0, call_to, call_from, []byte(greeting))
}

/*-------------------------------------------------------------------
 *
 * Name:        agw_cb_G_port_information
 *
 * Purpose:     Process the port information "radio channels available" response from the TNC.
 *
 *
 * Inputs:	num_chan_avail		- Number of radio channels available.
 *
 *		chan_descriptions	- Array of string pointers to form "Port99 description".
 *					  Port1 is channel 0.
 *
 *--------------------------------------------------------------------*/

func agw_cb_G_port_information(num_chan_avail int, chan_descriptions []string) {
	fmt.Printf("TNC has %d radio channel(s) available:\n", num_chan_avail)

	for n := range num_chan_avail {
		var p = chan_descriptions[n]

		// Expecting something like this:  "Port1 first soundcard mono"

		if strings.EqualFold(p[:4], "Port") && unicode.IsDigit(rune(p[4])) {
			var port, desc, _ = strings.Cut(p, " ")

			var _channel, _ = strconv.Atoi(port[4:])

			if _channel >= 0 && _channel < MAX_TOTAL_CHANS {
				var channel = byte(_channel)

				channel -= 1 // "Port1" is our channel 0.

				fmt.Printf("  Channel %d: %s\n", channel, desc)

				// Later? Use 'g' to get speed and maybe other properties?
				// Though I'm not sure why we would care here.

				/*
				 * Send command to register my callsign for incoming connect requests.
				 */

				agwlib_X_register_callsign(channel, mycall)
			} else {
				fmt.Printf("Radio channel number is out of bounds: %s\n", p)
			}
		} else {
			fmt.Printf("Radio channel description not in expected format: %s\n", p)
		}
	}
} /* end agw_cb_G_port_information */

/*-------------------------------------------------------------------
 *
 * Name:        agw_cb_Y_outstanding_frames_for_station
 *
 * Purpose:     Process the "disconnected" command from the TNC.
 *
 * Inputs:	chan		- Radio channel.
 *
 *		call_from	- Should be my call.
 *
 *		call_to		- Callsign of other station.
 *
 *		frame_count
 *
 * Description:	Remove from the sessions table.
 *
 *--------------------------------------------------------------------*/

func agw_cb_Y_outstanding_frames_for_station(channel byte, call_from Callsign, call_to Callsign, frame_count int) { //nolint:unparam
	var s = srv.findSession(channel, call_to)

	if s == nil {
		fmt.Printf("Oops!  Did not expect to be here.\n")
		return
	}

	fmt.Printf("debug ----------------------> session %d,%s, callback Y outstanding frame_count %d\n", channel, call_to, frame_count)

	// Update the transmit queue length

	s.txQueueLen = frame_count
} /* end agw_cb_Y_outstanding_frames_for_station */

// strings.Cut for []bytes
func BytesCut(s []byte, b byte) ([]byte, []byte, bool) { //nolint:unparam
	var i = bytes.Index(s, []byte{b})

	if i >= 0 {
		return s[:i], s[i+1:], true
	}

	return s, nil, false
}
