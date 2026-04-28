package install

import "fmt"

func RenderSystemdService(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=prbe-agent-tap watcher
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s watch
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, binPath)
}

func RenderSystemdHeartbeatService(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=prbe-agent-tap heartbeat (one-shot)

[Service]
Type=oneshot
ExecStart=%s heartbeat
`, binPath)
}

func RenderSystemdHeartbeatTimer() string {
	return `[Unit]
Description=prbe-agent-tap heartbeat timer

[Timer]
OnBootSec=1min
OnUnitActiveSec=5min
Unit=prbe-agent-tap-heartbeat.service

[Install]
WantedBy=timers.target
`
}
