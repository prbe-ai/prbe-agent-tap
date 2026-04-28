package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoadDeleteFileFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", dir)

	if err := Store("device-token", "tok-abc"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := Load("device-token")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "tok-abc" {
		t.Fatalf("got %q want %q", got, "tok-abc")
	}

	info, err := os.Stat(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("got mode %v want 0600", info.Mode().Perm())
	}

	if err := Delete("device-token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = Load("device-token")
	if got != "" {
		t.Fatalf("after delete got %q want empty", got)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", dir)

	got, err := Load("missing")
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q want empty", got)
	}
}
