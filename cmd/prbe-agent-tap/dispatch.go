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
	"github.com/prbe-ai/prbe-agent-tap/internal/heartbeat"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/outbox"
	"github.com/prbe-ai/prbe-agent-tap/internal/pair"
	"github.com/prbe-ai/prbe-agent-tap/internal/revoke"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
	"github.com/prbe-ai/prbe-agent-tap/internal/version"
	"github.com/prbe-ai/prbe-agent-tap/internal/watch"
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
func runWatch(ctx context.Context, _ []string, _, stderr io.Writer) int {
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

	if v, _ := s.GetMeta("last_401_at"); v != "" {
		fmt.Fprintln(stderr, "halted: device token revoked at", v, "— run `prbe-agent-tap pair` to resume")
		return 1
	}

	deviceID, _ := s.GetMeta("device_id")
	if deviceID == "" {
		fmt.Fprintln(stderr, "not paired; run `prbe-agent-tap pair` first")
		return 1
	}

	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})

	w, err := watch.NewWatcher(watch.WatcherConfig{
		ProjectsRoot: claudeProjectsDir(),
		Storage:      s,
	})
	if err != nil {
		fmt.Fprintln(stderr, "watcher init:", err)
		return 1
	}

	d := outbox.New(outbox.Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return creds.Load("device-token") },
	})

	errCh := make(chan error, 2)
	go func() { errCh <- w.Run(ctx) }()
	go func() { errCh <- d.Run(ctx) }()

	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	return 0
}

func claudeProjectsDir() string {
	if d := os.Getenv("PRBE_CLAUDE_PROJECTS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}
func runHeartbeat(ctx context.Context, _ []string, _, stderr io.Writer) int {
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

	tok, err := creds.Load("device-token")
	if err != nil || tok == "" {
		fmt.Fprintln(stderr, "no device token; run `prbe-agent-tap pair` first")
		return 1
	}
	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	if err := heartbeat.Run(ctx, heartbeat.Args{DeviceToken: tok, Storage: s, Client: hc}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
func runRevoke(ctx context.Context, _ []string, stdout, stderr io.Writer) int {
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
	tok, _ := creds.Load("device-token")
	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	if err := revoke.Run(ctx, revoke.Args{DeviceToken: tok, Storage: s, Client: hc}); err != nil {
		fmt.Fprintln(stderr, "server-side revoke failed (local state still wiped):", err)
		return 0
	}
	fmt.Fprintln(stdout, "Revoked. Local credentials and state cleared.")
	return 0
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
