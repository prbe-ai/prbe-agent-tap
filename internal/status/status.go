package status

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

// Render writes the status summary and returns an exit code.
// 0 = healthy paired; 1 = unpaired or halted.
func Render(s *storage.Storage, w io.Writer, verbose bool) int {
	deviceID, _ := s.GetMeta("device_id")
	if deviceID == "" {
		fmt.Fprintln(w, "prbe-agent-tap: not paired — run `prbe-agent-tap pair <token>`")
		return 1
	}
	if v, _ := s.GetMeta("last_401_at"); v != "" {
		fmt.Fprintf(w, "prbe-agent-tap: halted (token revoked at %s)\n", relative(v))
		fmt.Fprintln(w, "  Run `prbe-agent-tap pair <token>` with a fresh token from the dashboard to resume.")
		return 1
	}

	customer, _ := s.GetMeta("customer_id")
	pairedAt, _ := s.GetMeta("paired_at")
	lastShipped, _ := s.GetMeta("last_successful_post_at")
	lastHB, _ := s.GetMeta("last_heartbeat_at")

	outboxBytes, _ := s.OutboxByteSize()
	dropped, _ := s.GetMeta("outbox_dropped_count")

	fmt.Fprintln(w, "prbe-agent-tap: paired")
	fmt.Fprintf(w, "  device:        %s\n", shortID(deviceID, verbose))
	fmt.Fprintf(w, "  customer:      %s\n", customer)
	fmt.Fprintf(w, "  paired:        %s\n", relative(pairedAt))
	fmt.Fprintf(w, "  last shipped:  %s\n", relative(lastShipped))
	fmt.Fprintf(w, "  outbox:        %d bytes queued, %s dropped lifetime\n", outboxBytes, fallback(dropped, "0"))
	fmt.Fprintf(w, "  last heartbeat: %s\n", relative(lastHB))
	return 0
}

func shortID(id string, verbose bool) string {
	if verbose || len(id) <= 12 {
		return id
	}
	return id[:8] + "..." + id[len(id)-4:]
}

func relative(unixSec string) string {
	if unixSec == "" {
		return "never"
	}
	n, err := strconv.ParseInt(unixSec, 10, 64)
	if err != nil {
		return unixSec
	}
	d := time.Since(time.Unix(n, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
