package revoke

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestRevokeHappyPathClearsState(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())
	_ = creds.Store("device-token", "dtok")

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/agent-tap/revoke" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	_ = s.SetMeta("device_id", "dev-1")
	_ = s.SetMeta("customer_id", "cust-1")

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	if err := Run(context.Background(), Args{DeviceToken: "dtok", Storage: s, Client: hc}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d want 1", hits)
	}
	if v, _ := creds.Load("device-token"); v != "" {
		t.Fatalf("creds left over: %q", v)
	}
	if v, _ := s.GetMeta("device_id"); v != "" {
		t.Fatalf("device_id left over: %q", v)
	}
}

func TestRevoke401StillClearsLocally(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())
	_ = creds.Store("device-token", "stale")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	_ = s.SetMeta("device_id", "dev-1")

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	_ = Run(context.Background(), Args{DeviceToken: "stale", Storage: s, Client: hc})

	if v, _ := creds.Load("device-token"); v != "" {
		t.Fatalf("creds left over: %q", v)
	}
}
