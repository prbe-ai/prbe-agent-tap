package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

// Install registers the platform-appropriate user-level units and starts them.
// Idempotent: re-running rewrites units.
func Install(stdout io.Writer) error {
	binPath, err := os.Executable()
	if err != nil {
		return err
	}
	binPath, _ = filepath.EvalSymlinks(binPath)
	stateDir, err := creds.StateDir()
	if err != nil {
		return err
	}
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "agent-tap.log")

	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(binPath, logPath, stdout)
	case "linux":
		return installSystemd(binPath, stdout)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func installLaunchd(binPath, logPath string, stdout io.Writer) error {
	home, _ := os.UserHomeDir()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return err
	}
	watchPath := filepath.Join(plistDir, "ai.prbe.agent-tap.watch.plist")
	beatPath := filepath.Join(plistDir, "ai.prbe.agent-tap.heartbeat.plist")
	if err := os.WriteFile(watchPath, []byte(RenderLaunchdWatch(binPath, logPath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(beatPath, []byte(RenderLaunchdHeartbeat(binPath, logPath)), 0o644); err != nil {
		return err
	}
	uid := fmt.Sprint(os.Getuid())
	for _, plist := range []string{watchPath, beatPath} {
		_ = exec.Command("launchctl", "bootout", "gui/"+uid, plist).Run()
		if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).Run(); err != nil {
			return fmt.Errorf("launchctl bootstrap %s: %w", plist, err)
		}
	}
	fmt.Fprintln(stdout, "Installed launchd agents (watch + heartbeat).")
	return nil
}

func installSystemd(binPath string, stdout io.Writer) error {
	home, _ := os.UserHomeDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "prbe-agent-tap.service"),
		[]byte(RenderSystemdService(binPath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "prbe-agent-tap-heartbeat.service"),
		[]byte(RenderSystemdHeartbeatService(binPath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "prbe-agent-tap-heartbeat.timer"),
		[]byte(RenderSystemdHeartbeatTimer()), 0o644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now",
		"prbe-agent-tap.service", "prbe-agent-tap-heartbeat.timer").Run(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Installed systemd user service + timer.")
	return nil
}

// Uninstall removes platform units and (always) wipes local state.
// If purgeBinary is true, also removes the binary.
func Uninstall(s *storage.Storage, purgeBinary bool, stdout io.Writer) error {
	switch runtime.GOOS {
	case "darwin":
		_ = uninstallLaunchd()
	case "linux":
		_ = uninstallSystemd()
	}
	stateDir, _ := creds.StateDir()
	_ = os.RemoveAll(stateDir)
	_ = creds.Delete("device-token")
	if purgeBinary {
		bin, err := os.Executable()
		if err == nil {
			_ = os.Remove(bin)
		}
	}
	fmt.Fprintln(stdout, "Uninstalled.")
	return nil
}

func uninstallLaunchd() error {
	home, _ := os.UserHomeDir()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	uid := fmt.Sprint(os.Getuid())
	for _, name := range []string{"ai.prbe.agent-tap.watch.plist", "ai.prbe.agent-tap.heartbeat.plist"} {
		plist := filepath.Join(plistDir, name)
		_ = exec.Command("launchctl", "bootout", "gui/"+uid, plist).Run()
		_ = os.Remove(plist)
	}
	return nil
}

func uninstallSystemd() error {
	home, _ := os.UserHomeDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	_ = exec.Command("systemctl", "--user", "disable", "--now",
		"prbe-agent-tap.service", "prbe-agent-tap-heartbeat.timer").Run()
	for _, name := range []string{
		"prbe-agent-tap.service",
		"prbe-agent-tap-heartbeat.service",
		"prbe-agent-tap-heartbeat.timer",
	} {
		_ = os.Remove(filepath.Join(unitDir, name))
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}
