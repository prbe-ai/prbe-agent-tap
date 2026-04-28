package heartbeat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestHeartbeatHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent-tap/heartbeat" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dtok" {
			t.Fatalf("auth = %q", got)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	if err := Run(context.Background(), Args{
		DeviceToken: "dtok", Storage: s, Client: hc,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if v, _ := s.GetMeta("last_heartbeat_at"); v == "" {
		t.Fatal("last_heartbeat_at not set")
	} else if _, err := strconv.ParseInt(v, 10, 64); err != nil {
		t.Fatalf("last_heartbeat_at not unix: %q", v)
	}
}

func TestHeartbeat401MarksLast401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	err := Run(context.Background(), Args{
		DeviceToken: "stale", Storage: s, Client: hc,
	})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if v, _ := s.GetMeta("last_401_at"); v == "" {
		t.Fatal("last_401_at not set")
	}
}
