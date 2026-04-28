package pair

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestPairHappyPathWritesCredsAndMeta(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent-tap/pair" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["pairing_token"] != "ptok" || body["os"] == "" || body["hostname"] == "" {
			t.Fatalf("bad body %v", body)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"device_id":    "dev-1",
			"device_token": "dtok",
			"customer_id":  "cust-1",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := storage.Open(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	var stdout bytes.Buffer
	if err := Run(context.Background(), Args{
		PairingToken: "ptok",
		Storage:      s,
		Client:       hc,
		Stdout:       &stdout,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	tok, _ := creds.Load("device-token")
	if tok != "dtok" {
		t.Fatalf("token in creds = %q want dtok", tok)
	}
	devID, _ := s.GetMeta("device_id")
	if devID != "dev-1" {
		t.Fatalf("device_id = %q", devID)
	}
	cust, _ := s.GetMeta("customer_id")
	if cust != "cust-1" {
		t.Fatalf("customer_id = %q", cust)
	}
	last401, _ := s.GetMeta("last_401_at")
	if last401 != "" {
		t.Fatalf("last_401_at should be cleared, got %q", last401)
	}
	if !strings.Contains(stdout.String(), "Paired") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPair401Rejects(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	err := Run(context.Background(), Args{
		PairingToken: "bad", Storage: s, Client: hc,
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
}
