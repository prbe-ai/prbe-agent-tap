package revoke

import (
	"context"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Args struct {
	DeviceToken string
	Storage     *storage.Storage
	Client      *httpclient.Client
}

// Run is intentionally tolerant: it always wipes local state, even if the server call fails.
// Server-side revocation is best-effort because uninstall must succeed offline.
func Run(ctx context.Context, a Args) error {
	var serverErr error
	if a.DeviceToken != "" {
		resp, _, err := a.Client.Do(ctx, httpclient.Request{
			Method: "POST", Path: "/agent-tap/revoke", Bearer: a.DeviceToken,
		})
		if resp != nil {
			defer resp.Body.Close()
		}
		serverErr = err
	}

	// Always clear local state.
	_ = creds.Delete("device-token")
	if a.Storage != nil {
		for _, k := range []string{"device_id", "customer_id", "paired_at",
			"last_heartbeat_at", "last_successful_post_at", "last_401_at"} {
			_ = a.Storage.DeleteMeta(k)
		}
		_, _ = a.Storage.ClearOutbox()
	}
	return serverErr
}
