package outbox

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Config struct {
	Storage        *storage.Storage
	Client         *httpclient.Client
	BearerProvider func() (string, error)
	PollInterval   time.Duration
	Now            func() time.Time
	MaxBytes       int64
}

type Drainer struct {
	cfg Config
}

func New(cfg Config) *Drainer {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 100 * 1024 * 1024
	}
	return &Drainer{cfg: cfg}
}

// Run blocks until the context is cancelled or 401 is received.
// On 401, returns the sentinel halt error after clearing the outbox + setting last_401_at.
func (d *Drainer) Run(ctx context.Context) error {
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := d.tick(ctx); err != nil {
				return err
			}
		}
	}
}

func (d *Drainer) tick(ctx context.Context) error {
	now := d.cfg.Now().Unix()
	row, ok, err := d.cfg.Storage.NextDueBatch(now)
	if err != nil {
		return fmt.Errorf("NextDueBatch: %w", err)
	}
	if !ok {
		_, _ = d.cfg.Storage.EnforceOutboxCap(d.cfg.MaxBytes)
		return nil
	}
	bearer, err := d.cfg.BearerProvider()
	if err != nil || bearer == "" {
		_ = d.cfg.Storage.MarkFailure(row.ID, now+30, "no device token")
		return nil
	}
	resp, cls, doErr := d.cfg.Client.Do(ctx, httpclient.Request{
		Method: "POST", Path: "/webhooks/claude_code", Bearer: bearer, Body: row.Body,
	})
	if resp != nil {
		_ = resp.Body.Close()
	}
	switch cls {
	case httpclient.Success:
		_ = d.cfg.Storage.MarkSuccess(row.ID)
		_ = d.cfg.Storage.SetMeta("last_successful_post_at", strconv.FormatInt(now, 10))
		return nil
	case httpclient.Poison:
		_ = d.cfg.Storage.MarkSuccess(row.ID)
		return nil
	case httpclient.Halt:
		_, _ = d.cfg.Storage.ClearOutbox()
		_ = d.cfg.Storage.SetMeta("last_401_at", strconv.FormatInt(now, 10))
		return fmt.Errorf("halted: device token revoked")
	default: // Retry
		errMsg := "transient failure"
		if doErr != nil {
			errMsg = doErr.Error()
		}
		next := now + int64(httpclient.Backoff(int(row.AttemptCount)).Seconds())
		_ = d.cfg.Storage.MarkFailure(row.ID, next, errMsg)
		return nil
	}
}
