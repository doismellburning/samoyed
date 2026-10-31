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
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const AX25_MAX_INFO_LEN = 2048 // Duplicated from C to avoid cgo

// maxCallLen is the longest callsign an AGW frame can carry.
const maxCallLen = 9

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
	var tncHostname = pflag.StringP("hostname", "h", "localhost", "TNC Hostname.")

	var tncPort = pflag.StringP("port", "p", "8000", "TNC Port.")

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

	if len(pflag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Exactly one argument required (MYCALL) - got %s\n", pflag.Args())
		os.Exit(1)
	}

	var mycall = strings.ToUpper(pflag.Arg(0))
	if len(mycall) > maxCallLen {
		fmt.Fprintf(os.Stderr, "Callsign %s too long (maximum length %d)\n", mycall, maxCallLen)
		os.Exit(1)
	}

	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	var runErr = run(ctx, net.JoinHostPort(*tncHostname, *tncPort), mycall)

	stop()

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "%s\n", runErr)
		os.Exit(1)
	}
} /* end main */

// run attaches to the TNC at addr, registers mycall on every port it has, and
// serves the stations that connect until ctx is cancelled.  A TNC that isn't
// there yet, or goes away, is waited for.
func run(ctx context.Context, addr string, mycall string) error {
	var cfg = new(agwclient.Config)

	cfg.Addr = addr
	cfg.Call = mycall
	cfg.Ports = nil // Every port the TNC has.

	// One reply line, or one timing test frame, is one frame to the TNC;
	// the TNC splits anything longer than its own PACLEN.
	cfg.PacLen = AX25_MAX_INFO_LEN
	cfg.HighWater = 16

	var client = agwclient.New(*cfg)
	var srv = newAppServer()

	logrus.WithFields(logrus.Fields{"addr": addr, "call": mycall}).Info("Attaching to network TNC")

	var group, groupCtx = errgroup.WithContext(ctx)

	group.Go(func() error { return client.Run(groupCtx) })
	group.Go(func() error { return srv.serve(groupCtx, client.Accept) })

	var err = group.Wait()
	if err != nil {
		return fmt.Errorf("serving connections: %w", err)
	}

	return nil
}

// commandHandler implements one connected-mode user command (e.g. "who",
// "bye").  It reports whether the session carries on afterwards.
type commandHandler func(ctx context.Context, s *session, rest string) bool

// command is one user command, along with what "?" and "HELP" say about it.
type command struct {
	name    string   // What the user types.  Lower case; input is folded to match.
	aliases []string // Other names for the same command.
	usage   string   // Name and arguments, for the command list.
	summary string   // One line, for the command list.
	detail  []string // What "HELP <command>" prints.
	handler commandHandler
}

// userCommands returns the commands, in the order "?" lists them.  It is a
// function rather than a package variable so that cmd_help, which is one of the
// handlers here, can read the table it appears in.
func userCommands() []command {
	return []command{
		{
			name:    "bye",
			aliases: nil,
			usage:   "BYE",
			summary: "Disconnect.",
			detail: []string{
				"BYE",
				"Say goodbye and hang up.  The link stays up until everything queued",
				"for you has been acknowledged, so nothing gets cut off.",
			},
			handler: cmd_bye,
		},
		{
			name:    "help",
			aliases: []string{"?"},
			usage:   "HELP <command>",
			summary: "Describe one command.",
			detail: []string{
				"HELP <command>",
				"Describe one command.  With nothing after it, or as ?, list them all.",
			},
			handler: cmd_help,
		},
		{
			name:    "test",
			aliases: nil,
			usage:   "TEST [count [length]]",
			summary: "Measure throughput.",
			detail: []string{
				"TEST [count [length]]",
				"Send count frames of length bytes each, then report how long it took",
				"and how close that came to the channel's bit rate.",
				fmt.Sprintf("count defaults to 1 and may be up to %d; length defaults to 256", maxTestCount),
				fmt.Sprintf("and is clamped to between 16 and %d bytes.", AX25_MAX_INFO_LEN),
			},
			handler: cmd_test,
		},
		{
			name:    "who",
			aliases: nil,
			usage:   "WHO",
			summary: "List the stations connected now.",
			detail: []string{
				"WHO",
				"List the stations connected to this server, with the channel each one",
				"came in on and when it connected.",
			},
			handler: cmd_who,
		},
	}
}

// lookupCommand finds the command answering to name, which must be lower case,
// and returns nil if there is not one.
func lookupCommand(name string) *command {
	var cmds = userCommands()

	for n := range cmds {
		if cmds[n].name == name || slices.Contains(cmds[n].aliases, name) {
			return &cmds[n]
		}
	}

	return nil
}

// loginTimeFormat renders a login time for "who".  UTC, because the stations
// listed are not necessarily in the same time zone as the one reading it, and
// packet operators think in UTC anyway.
const loginTimeFormat = "2006-01-02 15:04:05Z"

// cmd_who lists people currently logged in.
func cmd_who(_ context.Context, s *session, _ string) bool {
	var lines = []string{fmt.Sprintf("%-7s %-7s %-9s %s", "Session", "Channel", "User", "Since")}

	for n, other := range s.srv.sortedSessions() {
		lines = append(lines, fmt.Sprintf("%-7d %-7d %-9s %s",
			n, other.link.Port(), other.link.Remote(), other.loginTime.UTC().Format(loginTimeFormat)))
	}

	return s.send(lines...)
}

// maxTestCount caps the frames one TEST may ask for.  The test transmits for as
// long as it takes and the channel is shared, so an unbounded count would let
// one station hold the air for as long as it cared to stay connected.  A
// thousand frames is several minutes at 9600 baud, and TEST can be run again.
const maxTestCount = 1000

// cmd_test runs a timing test: send the specified number of frames with optional length.
// The station that asked waits for it to finish; nobody else does.
func cmd_test(ctx context.Context, s *session, rest string) bool {
	var fields = strings.Fields(rest)

	var count = 1

	if len(fields) > 0 {
		var countErr error

		count, countErr = strconv.Atoi(fields[0])
		if countErr != nil || count < 1 || count > maxTestCount {
			return s.send(fmt.Sprintf("TEST: %s is not a frame count between 1 and %d.  Type HELP TEST.", fields[0], maxTestCount))
		}
	}

	var length = 256

	if len(fields) > 1 {
		var lengthErr error

		length, lengthErr = strconv.Atoi(fields[1])
		if lengthErr != nil {
			return s.send(fmt.Sprintf("TEST: %s is not a frame length.  Type HELP TEST.", fields[1]))
		}

		length = min(max(length, 16), AX25_MAX_INFO_LEN)
	}

	if !s.send(fmt.Sprintf("Sending %d frame(s) of %d bytes; summary to follow.", count, length)) {
		return false
	}

	var start = time.Now()

	for n := 1; n <= count; n++ {
		var _, err = s.link.Write(timingTestFrame(n, length))
		if err != nil {
			return false
		}
	}

	// Wait until they have all been sent and ack'ed by other end.
	var drainErr = s.link.Drain(ctx)
	if drainErr != nil {
		return false
	}

	var elapsed = time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001 // avoid divide by 0
	}

	var byte_count = count * length

	return s.send(fmt.Sprintf("%d bytes in %.1f seconds, %.0f bytes/sec, efficiency %.0f%% at 1200, %.0f%% at 9600.",
		byte_count, elapsed, float64(byte_count)/elapsed,
		float64(byte_count)*8*100/elapsed/1200,
		float64(byte_count)*8*100/elapsed/9600))
}

// timingTestFrame builds one frame of the timing test: the sequence number
// followed by enough repeating alphanumeric filler to reach length bytes.
func timingTestFrame(seq int, length int) []byte {
	var c = 'a'

	var stuff = fmt.Sprintf("%06d ", seq)
	for k := len(stuff); k < length-1; k++ {
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

	return []byte(stuff)
}

// cmd_bye says farewell, waits for it to be acknowledged - or for
// byeDrainTimeout, whichever comes first - so it isn't cut off, and hangs up.
func cmd_bye(ctx context.Context, s *session, _ string) bool {
	if !s.send("Thank you folks for kindly droppin' in.  Y'all come on back now, ya hear?") {
		return false
	}

	var drainCtx, cancel = context.WithTimeout(ctx, s.srv.byeDrainTimeout)
	defer cancel()

	var drainErr = s.link.Drain(drainCtx)
	if drainErr != nil {
		logrus.WithError(drainErr).WithField("station", s.link.Remote()).Info("Farewell not acknowledged; disconnecting anyway")
	}

	var closeErr = s.link.Close()
	if closeErr != nil {
		logrus.WithError(closeErr).WithField("station", s.link.Remote()).Info("Disconnecting")
	}

	return false
}

// cmd_help lists the commands, or describes the one named.
func cmd_help(_ context.Context, s *session, rest string) bool {
	var topic, _, _ = strings.Cut(strings.TrimSpace(rest), " ")

	topic = strings.ToLower(topic)

	var topicCommand = lookupCommand(topic)

	var lines []string

	switch {
	case topic == "":
		lines = append(lines, "Commands:")

		for _, c := range userCommands() {
			lines = append(lines, fmt.Sprintf("  %-21s %s", c.usage, c.summary))
		}

		lines = append(lines, "Type HELP <command> for details.")
	case topicCommand != nil:
		lines = topicCommand.detail
	default:
		lines = []string{fmt.Sprintf("No such command: %s.  Type ? for the list.", topic)}
	}

	return s.send(lines...)
}
