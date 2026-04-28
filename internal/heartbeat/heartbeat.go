package heartbeat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Args struct {
	DeviceToken string
	Storage     *storage.Storage
	Client      *httpclient.Client
}

func Run(ctx context.Context, a Args) error {
	if a.DeviceToken == "" {
		return fmt.Errorf("missing device token (run `prbe-agent-tap pair` first)")
	}
	resp, cls, err := a.Client.Do(ctx, httpclient.Request{
		Method: "POST", Path: "/agent-tap/heartbeat", Bearer: a.DeviceToken,
	})
	if resp != nil {
		defer resp.Body.Close()
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	switch cls {
	case httpclient.Success:
		_ = a.Storage.SetMeta("last_heartbeat_at", now)
		return nil
	case httpclient.Halt:
		_ = a.Storage.SetMeta("last_401_at", now)
		return fmt.Errorf("heartbeat rejected: device token revoked (re-pair required)")
	default:
		if err != nil {
			return fmt.Errorf("heartbeat failed (%s): %w", cls, err)
		}
		return fmt.Errorf("heartbeat failed: %s", cls)
	}
}
