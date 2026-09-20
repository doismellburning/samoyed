package main

import (
	"context"
	"os"
	"os/signal"

	direwolf "github.com/doismellburning/samoyed/src"
)

func main() {
	// Everything long-lived that DirewolfMain starts hangs off this context,
	// so an interrupt asks all of it to stop rather than leaving the goroutines
	// running until the process goes away underneath them.
	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	direwolf.DirewolfMain(ctx)
}
