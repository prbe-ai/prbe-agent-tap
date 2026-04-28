package install

import (
	"strings"
	"testing"
)

func TestRenderLaunchdWatch(t *testing.T) {
	out := RenderLaunchdWatch("/usr/local/bin/prbe-agent-tap", "/Users/x/.prbe/logs/agent-tap.log")
	for _, want := range []string{
		"<key>Label</key>",
		"<string>ai.prbe.agent-tap.watch</string>",
		"<string>/usr/local/bin/prbe-agent-tap</string>",
		"<string>watch</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"<string>/Users/x/.prbe/logs/agent-tap.log</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRenderLaunchdHeartbeat(t *testing.T) {
	out := RenderLaunchdHeartbeat("/usr/local/bin/prbe-agent-tap", "/Users/x/.prbe/logs/agent-tap.log")
	for _, want := range []string{
		"<string>ai.prbe.agent-tap.heartbeat</string>",
		"<key>StartInterval</key>",
		"<integer>300</integer>",
		"<string>heartbeat</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}
