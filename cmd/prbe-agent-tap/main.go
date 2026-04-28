package main

import (
	"context"
	"os"

	"github.com/prbe-ai/prbe-agent-tap/internal/logging"
)

func main() {
	closer, err := logging.New()
	if err == nil && closer != nil {
		defer closer.Close()
	}
	os.Exit(dispatch(context.Background(), os.Args, os.Stdout, os.Stderr))
}
