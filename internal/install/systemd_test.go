package install

import (
	"strings"
	"testing"
)

func TestRenderSystemdService(t *testing.T) {
	out := RenderSystemdService("/usr/local/bin/prbe-agent-tap")
	for _, want := range []string{
		"[Unit]",
		"prbe-agent-tap watcher",
		"[Service]",
		"ExecStart=/usr/local/bin/prbe-agent-tap watch",
		"Restart=always",
		"RestartSec=10",
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("service missing %q", want)
		}
	}
}

func TestRenderSystemdHeartbeatService(t *testing.T) {
	out := RenderSystemdHeartbeatService("/usr/local/bin/prbe-agent-tap")
	if !strings.Contains(out, "ExecStart=/usr/local/bin/prbe-agent-tap heartbeat") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "Type=oneshot") {
		t.Fatalf("missing Type=oneshot")
	}
}

func TestRenderSystemdHeartbeatTimer(t *testing.T) {
	out := RenderSystemdHeartbeatTimer()
	for _, want := range []string{
		"OnBootSec=1min",
		"OnUnitActiveSec=5min",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("timer missing %q", want)
		}
	}
}
