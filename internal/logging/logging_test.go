package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

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
