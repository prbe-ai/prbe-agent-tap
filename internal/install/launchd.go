package install

import "fmt"

// RenderLaunchdWatch returns the macOS LaunchAgent plist for the long-running watch service.
func RenderLaunchdWatch(binPath, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.prbe.agent-tap.watch</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>watch</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, binPath, logPath, logPath)
}

// RenderLaunchdHeartbeat returns the LaunchAgent plist for the 5-minute heartbeat timer.
func RenderLaunchdHeartbeat(binPath, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.prbe.agent-tap.heartbeat</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>heartbeat</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>300</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, binPath, logPath, logPath)
}
