//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	backend   = "http://localhost:8081"
	knowledge = "http://localhost:8080"
	customer  = "smoke-cust"
	employee  = "smoke-emp"
)

func TestEndToEnd(t *testing.T) {
	signKey := os.Getenv("PAIRING_TOKEN_SIGNING_KEY")
	intKey := os.Getenv("INTERNAL_KNOWLEDGE_API_KEY")
	if signKey == "" || intKey == "" {
		t.Skip("PAIRING_TOKEN_SIGNING_KEY / INTERNAL_KNOWLEDGE_API_KEY not set")
	}

	stateDir := t.TempDir()
	projectsDir := filepath.Join(t.TempDir(), "projects")
	_ = os.MkdirAll(projectsDir, 0o755)

	env := []string{
		"PRBE_API_BASE_URL=" + backend,
		"PRBE_STATE_DIR=" + stateDir,
		"PRBE_CLAUDE_PROJECTS_DIR=" + projectsDir,
		"PRBE_DISABLE_KEYCHAIN=1",
		"PATH=" + os.Getenv("PATH"),
	}

	bin := filepath.Join(t.TempDir(), "prbe-agent-tap")
	build := exec.Command("go", "build", "-o", bin, "github.com/prbe-ai/prbe-agent-tap/cmd/prbe-agent-tap")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	tok, err := mintPairingJWT(signKey, customer, employee)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	cmd := exec.Command(bin, "pair", tok)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pair: %v\n%s", err, out)
	}

	projDir := filepath.Join(projectsDir, "-tmp-smoke")
	_ = os.MkdirAll(projDir, 0o755)
	sessionPath := filepath.Join(projDir, "smoke.jsonl")
	_ = os.WriteFile(sessionPath, []byte(
		`{"type":"user_prompt","content":"hello"}`+"\n"+
			`{"type":"assistant_message","content":"world"}`+"\n"), 0o600)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	watchCmd := exec.CommandContext(ctx, bin, "watch")
	watchCmd.Env = env
	_ = watchCmd.Start()

	time.Sleep(2 * time.Second)
	f, _ := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"type":"tool_use","name":"Read","input":{"path":"/tmp"}}` + "\n")
	_ = f.Close()

	_ = watchCmd.Wait()

	deviceID := readMeta(t, stateDir, "device_id")
	if deviceID == "" {
		t.Fatal("device_id not persisted by pair")
	}
	if !deviceListed(t, intKey, deviceID, "active") {
		t.Fatalf("device %s not listed as active on knowledge", deviceID)
	}

	cmd = exec.Command(bin, "revoke")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("revoke: %v\n%s", err, out)
	}
	if !deviceListed(t, intKey, deviceID, "revoked") {
		t.Fatalf("device %s not listed as revoked on knowledge", deviceID)
	}
}

func readMeta(t *testing.T, stateDir, key string) string {
	t.Helper()
	cmd := exec.Command("sqlite3", filepath.Join(stateDir, "state.db"),
		"SELECT v FROM meta WHERE k='"+key+"'")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sqlite3 read: %v", err)
	}
	return string(trimRight(out))
}

func trimRight(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func deviceListed(t *testing.T, intKey, deviceID, wantStatus string) bool {
	t.Helper()
	req, _ := http.NewRequest("GET", knowledge+"/api/devices?customer_id="+customer, nil)
	req.Header.Set("X-Internal-Knowledge-Key", intKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/devices: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/api/devices status=%d", resp.StatusCode)
	}
	var body struct {
		Devices []struct {
			DeviceID string `json:"device_id"`
			Status   string `json:"status"`
		} `json:"devices"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	for _, d := range body.Devices {
		if d.DeviceID == deviceID && d.Status == wantStatus {
			return true
		}
	}
	return false
}
