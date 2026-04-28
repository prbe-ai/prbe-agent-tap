package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLevelFilterDropsBelowMin(t *testing.T) {
	var buf bytes.Buffer
	lf := levelFilter{w: &buf, min: slog.LevelWarn}

	// INFO line: should be dropped.
	n, err := lf.Write([]byte(`{"time":"x","level":"INFO","msg":"hi"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no bytes forwarded, got %d", buf.Len())
	}
	if n != len(`{"time":"x","level":"INFO","msg":"hi"}`)+1 {
		t.Fatalf("Write must report full length (got %d)", n)
	}

	// WARN line: should be forwarded.
	want := []byte(`{"time":"x","level":"WARN","msg":"hi"}` + "\n")
	if _, err := lf.Write(want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("expected WARN to be forwarded; got %q", buf.String())
	}
}

func TestLevelFilterDropsMalformed(t *testing.T) {
	var buf bytes.Buffer
	lf := levelFilter{w: &buf, min: slog.LevelInfo}
	_, _ = lf.Write([]byte("not-json\n"))
	if buf.Len() != 0 {
		t.Fatalf("malformed line should be dropped; got %q", buf.String())
	}
}

func TestLoggerEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	l.Info("hello", "device_token", "secret-tok", "hostname", "mac1")

	out := buf.String()
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if got["msg"] != "hello" {
		t.Fatalf("msg = %v", got["msg"])
	}
	if got["device_token"] != "***" {
		t.Fatalf("device_token not redacted: %v", got["device_token"])
	}
	if got["hostname"] != "mac1" {
		t.Fatalf("hostname unexpectedly altered: %v", got["hostname"])
	}
}
