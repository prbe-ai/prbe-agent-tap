package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/pair"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
	"github.com/prbe-ai/prbe-agent-tap/internal/version"
)

type subcommand struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, stdout, stderr io.Writer) int
}

func subcommands() []subcommand {
	return []subcommand{
		{name: "pair", summary: "exchange pairing token for a device token", run: runPair},
		{name: "watch", summary: "tail Claude Code transcripts and ship batches", run: runWatch},
		{name: "heartbeat", summary: "one-shot liveness ping (called by launchd/systemd timer)", run: runHeartbeat},
		{name: "revoke", summary: "revoke this device and wipe local credentials", run: runRevoke},
		{name: "status", summary: "print local daemon state", run: runStatus},
		{name: "backfill", summary: "ship historical Claude Code sessions", run: runBackfill},
		{name: "install", summary: "register launchd/systemd units and start the watcher", run: runInstall},
		{name: "uninstall", summary: "stop the daemon and remove local state", run: runUninstall},
	}
}

func dispatch(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(argv) < 2 {
		fmt.Fprintln(stderr, "no subcommand given; try `prbe-agent-tap help`")
		return 2
	}
	cmd := argv[1]
	args := argv[2:]
	switch cmd {
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "prbe-agent-tap %s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
		return 0
	}
	for _, s := range subcommands() {
		if s.name == cmd {
			return s.run(ctx, args, stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "unknown subcommand %q; try `prbe-agent-tap help`\n", cmd)
	return 2
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: prbe-agent-tap <subcommand> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	for _, s := range subcommands() {
		fmt.Fprintf(w, "  %-12s %s\n", s.name, s.summary)
	}
}

// Stub implementations; each is replaced in later tasks.

func runPair(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: prbe-agent-tap pair <pairing-token>")
		return 2
	}
	token := fs.Arg(0)

	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()

	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	if err := pair.Run(ctx, pair.Args{
		PairingToken: token, Storage: s, Client: hc, Stdout: stdout,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
func runWatch(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "watch: not yet implemented (Task 20)")
	return 2
}
func runHeartbeat(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "heartbeat: not yet implemented (Task 13)")
	return 2
}
func runRevoke(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "revoke: not yet implemented (Task 14)")
	return 2
}
func runStatus(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "status: not yet implemented (Task 21)")
	return 2
}
func runBackfill(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "backfill: not yet implemented (Task 22)")
	return 2
}
func runInstall(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "install: not yet implemented (Task 25)")
	return 2
}
func runUninstall(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "uninstall: not yet implemented (Task 25)")
	return 2
}

func apiBaseURL() string {
	if u := os.Getenv("PRBE_API_BASE_URL"); u != "" {
		return u
	}
	return "https://api.prbe.ai"
}

func stateDBPath() (string, error) {
	dir, err := creds.StateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}
