package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	direwolf "github.com/doismellburning/samoyed/src"
)

func main() {
	// Everything long-lived that DirewolfMain starts hangs off this context,
	// so a stop asks all of it to stop rather than leaving the goroutines
	// running until the process goes away underneath them.
	//
	// SIGTERM is how a supervisor - systemd, a container runtime - stops a
	// long-running samoyed-direwolf, and its default disposition ends the
	// process on the spot, before anything has released the PTT.  CM108 and
	// hamlib PTT are device-side state that exiting does not undo, so taking
	// SIGTERM here is what keeps a supervised stop from leaving the rig keyed.
	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	direwolf.DirewolfMain(ctx)
}
