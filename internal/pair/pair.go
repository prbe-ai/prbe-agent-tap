package pair

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Args struct {
	PairingToken string
	Storage      *storage.Storage
	Client       *httpclient.Client
	Stdout       io.Writer
}

type pairRequest struct {
	PairingToken string `json:"pairing_token"`
	OS           string `json:"os"`
	Hostname     string `json:"hostname"`
}

type pairResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	CustomerID  string `json:"customer_id"`
}

func Run(ctx context.Context, a Args) error {
	if a.PairingToken == "" {
		return fmt.Errorf("pairing token required")
	}
	host, _ := os.Hostname()
	body, err := json.Marshal(pairRequest{
		PairingToken: a.PairingToken,
		OS:           osLabel(),
		Hostname:     host,
	})
	if err != nil {
		return err
	}
	resp, cls, err := a.Client.Do(ctx, httpclient.Request{
		Method: "POST", Path: "/agent-tap/pair", Body: body,
	})
	if err != nil && cls != httpclient.Halt {
		return fmt.Errorf("pair request failed: %w", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if cls == httpclient.Halt {
		return fmt.Errorf("pairing token rejected by server (request a fresh one from the dashboard)")
	}
	if cls != httpclient.Success {
		return fmt.Errorf("pair returned non-success classification %s", cls)
	}

	var pr pairResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return fmt.Errorf("decode pair response: %w", err)
	}
	if pr.DeviceToken == "" || pr.DeviceID == "" {
		return fmt.Errorf("pair response missing device_token or device_id")
	}
	if err := creds.Store("device-token", pr.DeviceToken); err != nil {
		return fmt.Errorf("store device token: %w", err)
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	for k, v := range map[string]string{
		"device_id":   pr.DeviceID,
		"customer_id": pr.CustomerID,
		"paired_at":   now,
	} {
		if err := a.Storage.SetMeta(k, v); err != nil {
			return fmt.Errorf("write meta %s: %w", k, err)
		}
	}
	if err := a.Storage.DeleteMeta("last_401_at"); err != nil {
		return err
	}
	if a.Stdout != nil {
		fmt.Fprintf(a.Stdout, "Paired. device_id=%s\nRun `prbe-agent-tap install` to start watching.\n", pr.DeviceID)
	}
	return nil
}

func osLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	default:
		return runtime.GOOS
	}
}
