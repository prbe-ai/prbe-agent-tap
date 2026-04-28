package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDispatchUnknownExitCode(t *testing.T) {
	var stderr bytes.Buffer
	code := dispatch(context.Background(), []string{"prbe-agent-tap", "nope"}, nil, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDispatchHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := dispatch(context.Background(), []string{"prbe-agent-tap", "help"}, &stdout, nil)
	if code != 0 {
		t.Fatalf("exit = %d want 0", code)
	}
	for _, want := range []string{"pair", "watch", "heartbeat", "status", "backfill", "revoke", "install", "uninstall"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestDispatchVersion(t *testing.T) {
	var stdout bytes.Buffer
	code := dispatch(context.Background(), []string{"prbe-agent-tap", "version"}, &stdout, nil)
	if code != 0 {
		t.Fatalf("exit = %d want 0", code)
	}
	if !strings.Contains(stdout.String(), "prbe-agent-tap") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
