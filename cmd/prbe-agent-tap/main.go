package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/prbe-ai/prbe-agent-tap/internal/logging"
)

func main() {
	closer, err := logging.New()
	if err == nil && closer != nil {
		defer closer.Close()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(dispatch(ctx, os.Args, os.Stdout, os.Stderr))
}
