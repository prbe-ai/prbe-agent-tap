package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("prbe-agent-tap %s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "no subcommand given (subcommand dispatch added in Task 11)")
	os.Exit(2)
}
