package status

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestRenderUnpaired(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()

	var buf bytes.Buffer
	code := Render(s, &buf, false)
	if code == 0 {
		t.Fatal("expected non-zero exit when unpaired")
	}
	if !strings.Contains(buf.String(), "not paired") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderHalted(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	now := strconv.FormatInt(time.Now().Unix(), 10)
	_ = s.SetMeta("device_id", "dev-1")
	_ = s.SetMeta("last_401_at", now)

	var buf bytes.Buffer
	code := Render(s, &buf, false)
	if code == 0 {
		t.Fatal("expected non-zero exit when halted")
	}
	if !strings.Contains(buf.String(), "halted") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderPaired(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	now := strconv.FormatInt(time.Now().Unix(), 10)
	_ = s.SetMeta("device_id", "abcdef0123456789")
	_ = s.SetMeta("customer_id", "cust-1")
	_ = s.SetMeta("paired_at", now)
	_ = s.SetMeta("last_successful_post_at", now)
	_ = s.SetMeta("last_heartbeat_at", now)

	var buf bytes.Buffer
	code := Render(s, &buf, false)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	out := buf.String()
	if !strings.Contains(out, "paired") || !strings.Contains(out, "device:") {
		t.Fatalf("output = %q", out)
	}
}
