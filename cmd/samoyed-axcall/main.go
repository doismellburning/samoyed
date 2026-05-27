//nolint:gochecknoglobals
package main

/*------------------------------------------------------------------
 *
 * Purpose:     Interactive outbound AX.25 connect client.
 *
 * Description: Connects to a remote station via a samoyed-direwolf TNC
 *              (using the AGW TCP/IP API) and provides a line-mode
 *              terminal session.  Replaces the ax25-apps axcall / call
 *              utility for setups that no longer have the Linux kernel
 *              AX.25 stack available.
 *
 * Usage:       samoyed-axcall [options] MYCALL TARGETCALL [via DIGI...]
 *
 *---------------------------------------------------------------*/

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	agwlib "github.com/doismellburning/samoyed/internal/agwlib"
	"github.com/spf13/pflag"
)

var (
	flagHostname = pflag.StringP("hostname", "h", "localhost", "TNC hostname.")
	flagPort     = pflag.StringP("port", "p", "8000", "TNC AGW port.")
	flagChannel  = pflag.IntP("channel", "c", 0, "Radio channel number (0-based).")
	flagIdle     = pflag.IntP("idle", "T", 0, "Idle timeout in seconds; 0 to disable.")
	flagHelp     = pflag.Bool("help", false, "Show this help message.")
)

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "samoyed-axcall - Interactive outbound AX.25 connect client\n\n")
		fmt.Fprintf(os.Stderr, "Usage: samoyed-axcall [options] MYCALL TARGETCALL [via DIGI...]\n\n")
		fmt.Fprintf(os.Stderr, "  MYCALL      Your callsign (registered with the TNC).\n")
		fmt.Fprintf(os.Stderr, "  TARGETCALL  Remote station to connect to.\n")
		fmt.Fprintf(os.Stderr, "  via DIGI... Optional list of digipeater callsigns.\n\n")
		fmt.Fprintf(os.Stderr, "Tilde escapes (at the start of an input line):\n")
		fmt.Fprintf(os.Stderr, "  ~.  or  ~q  Disconnect and exit.\n")
		fmt.Fprintf(os.Stderr, "  ~?          Show this tilde-escape help.\n")
		fmt.Fprintf(os.Stderr, "  ~r          (Not yet implemented.)\n\n")
		pflag.PrintDefaults()
	}

	pflag.Parse()

	if *flagHelp {
		pflag.Usage()
		os.Exit(0)
	}

	var args = pflag.Args()
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: MYCALL and TARGETCALL are required.\n")
		pflag.Usage()
		os.Exit(1)
	}

	var mycall, myErr = callsignFromString(args[0])
	if myErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: invalid MYCALL %q: %s\n", args[0], myErr)
		os.Exit(1)
	}

	var targetcall, targetErr = callsignFromString(args[1])
	if targetErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: invalid TARGETCALL %q: %s\n", args[1], targetErr)
		os.Exit(1)
	}

	var viaDigis []agwlib.Callsign

	if len(args) > 2 {
		// Optional: "via DIGI1 DIGI2 ..." — "via" keyword is optional.
		var start = 2
		if strings.EqualFold(args[2], "via") {
			start = 3
		}

		for _, raw := range args[start:] {
			var digi, digiErr = callsignFromString(raw)
			if digiErr != nil {
				fmt.Fprintf(os.Stderr, "samoyed-axcall: error: invalid digipeater callsign %q: %s\n", raw, digiErr)
				os.Exit(1)
			}

			viaDigis = append(viaDigis, digi)
		}
	}

	var channel = byte(*flagChannel)

	// Channels used to communicate between the AGW receive goroutine and
	// the interactive loop running on the main goroutine.
	var connectedCh = make(chan struct{})
	var disconnectedCh = make(chan struct{})
	var rxCh = make(chan []byte, 16)

	var handlers = agwlib.Handlers{
		OnConnectionReceived: func(_ byte, callFrom, _ agwlib.Callsign, incoming bool, _ []byte) {
			if incoming {
				fmt.Fprintf(os.Stderr, "*** CONNECT FROM %s\n", callsignString(callFrom))
				return
			}

			select {
			case <-connectedCh:
				// already signalled
			default:
				close(connectedCh)
			}
		},
		OnConnectedData: func(_ byte, _, _ agwlib.Callsign, data []byte) {
			select {
			case rxCh <- data:
			default:
				// Receive buffer full; drop to avoid blocking the listen goroutine.
			}
		},
		OnDisconnected: func(_ byte, _, _ agwlib.Callsign, _ []byte) {
			select {
			case <-disconnectedCh:
				// already signalled
			default:
				close(disconnectedCh)
			}
		},
		OnPortInformation:   nil,
		OnOutstandingFrames: nil,
	}

	var tnc, newErr = agwlib.New(*flagHostname, *flagPort, nil, handlers)
	if newErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: cannot connect to TNC at %s:%s: %s\n", *flagHostname, *flagPort, newErr)
		os.Exit(1)
	}

	var regErr = tnc.RegisterCallsign(channel, mycall)
	if regErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: cannot register callsign: %s\n", regErr)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Connecting to %s...\n", callsignString(targetcall))

	var connectErr error
	if len(viaDigis) > 0 {
		connectErr = tnc.ConnectVia(channel, mycall, targetcall, viaDigis)
	} else {
		connectErr = tnc.Connect(channel, mycall, targetcall)
	}

	if connectErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: cannot send connect request: %s\n", connectErr)
		os.Exit(1)
	}

	// Wait for the connection to be established (or time out).
	var connectTimer = time.NewTimer(30 * time.Second)

	select {
	case <-connectedCh:
		connectTimer.Stop()
		fmt.Printf("*** CONNECTED With Station %s\n", callsignString(targetcall))
	case <-connectTimer.C:
		connectTimer.Stop()
		fmt.Fprintf(os.Stderr, "samoyed-axcall: error: timed out waiting for connection.\n")
		os.Exit(1)
	}

	// --- interactive loop ---
	var stdinCh = make(chan string)

	go func() {
		var scanner = bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			stdinCh <- scanner.Text()
		}
		close(stdinCh)
	}()

	var sigCh = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var idleTimer *time.Timer

	if *flagIdle > 0 {
		idleTimer = time.NewTimer(time.Duration(*flagIdle) * time.Second)
	}

	var resetIdle = func() {
		if idleTimer == nil {
			return
		}

		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}

		idleTimer.Reset(time.Duration(*flagIdle) * time.Second)
	}

	var doDisconnect = func() {
		tnc.Disconnect(channel, mycall, targetcall) //nolint:errcheck

		var dt = time.NewTimer(10 * time.Second)
		defer dt.Stop()

		select {
		case <-disconnectedCh:
		case <-dt.C:
		}

		fmt.Printf("\n*** DISCONNECTED\n")
	}

	for {
		var idleC <-chan time.Time
		if idleTimer != nil {
			idleC = idleTimer.C
		}

		select {
		case line, ok := <-stdinCh:
			if !ok {
				// EOF on stdin — disconnect and exit.
				doDisconnect()
				os.Exit(0)
			}

			if strings.HasPrefix(line, "~") {
				handleTildeEscape(line, doDisconnect)
				continue
			}

			// Append CR per AX.25 convention, then send.
			resetIdle()
			tnc.SendConnectedData(channel, 0xF0, mycall, targetcall, []byte(line+"\r")) //nolint:errcheck

		case data := <-rxCh:
			resetIdle()
			fmt.Printf("%s", data)

		case <-disconnectedCh:
			fmt.Printf("\n*** DISCONNECTED\n")
			os.Exit(0)

		case <-idleC:
			fmt.Fprintf(os.Stderr, "samoyed-axcall: idle timeout.\n")
			doDisconnect()
			os.Exit(0)

		case <-sigCh:
			doDisconnect()
			os.Exit(0)
		}
	}
}

// handleTildeEscape processes a tilde-prefixed input line.
func handleTildeEscape(line string, doDisconnect func()) {
	var esc = strings.ToLower(strings.TrimSpace(line))

	switch esc {
	case "~.", "~q":
		doDisconnect()
		os.Exit(0)

	case "~?":
		fmt.Fprintf(os.Stderr, "Tilde escapes:\n")
		fmt.Fprintf(os.Stderr, "  ~.  or  ~q  Disconnect and exit.\n")
		fmt.Fprintf(os.Stderr, "  ~?          Show this help.\n")
		fmt.Fprintf(os.Stderr, "  ~r          Reconnect (not yet implemented).\n")

	case "~r":
		fmt.Fprintf(os.Stderr, "~r: reconnect not yet implemented.\n")

	default:
		fmt.Fprintf(os.Stderr, "Unknown tilde escape: %s\n", line)
	}
}

// callsignFromString converts a string into a zero-padded Callsign.
// Returns an error if the string is longer than the 9-character limit.
func callsignFromString(s string) (agwlib.Callsign, error) {
	var cs agwlib.Callsign
	var upper = strings.ToUpper(s)

	if len(upper) > len(cs)-1 {
		return cs, fmt.Errorf("callsign %q too long (maximum %d characters)", s, len(cs)-1)
	}

	copy(cs[:], upper)
	return cs, nil
}

// callsignString converts a Callsign to its string representation,
// stripping NUL padding.
func callsignString(cs agwlib.Callsign) string {
	return strings.TrimRight(string(cs[:]), "\x00")
}
