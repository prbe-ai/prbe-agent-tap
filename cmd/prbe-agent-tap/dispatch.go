package main

import (
	"context"
	"fmt"
	"io"
	"runtime"

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

func runPair(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "pair: not yet implemented (Task 12)")
	return 2
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
