package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoSendsBearerAndUserAgent(t *testing.T) {
	var gotAuth, gotUA, gotTrace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotTrace = r.Header.Get("X-Trace-Id")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Version: "test"})
	resp, cls, err := c.Do(context.Background(), Request{
		Method: "POST", Path: "/x", Bearer: "tok-abc", Body: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if cls != Success {
		t.Fatalf("classification = %v", cls)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "prbe-agent-tap/test ") {
		t.Fatalf("user-agent = %q", gotUA)
	}
	if gotTrace == "" {
		t.Fatal("missing X-Trace-Id")
	}
}

func TestDoClassifies5xxAsRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Version: "test"})
	_, cls, _ := c.Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if cls != Retry {
		t.Fatalf("classification = %v want Retry", cls)
	}
}

func TestDoClassifies401AsHalt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Version: "test"})
	_, cls, _ := c.Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if cls != Halt {
		t.Fatalf("classification = %v want Halt", cls)
	}
}

func TestDoNetworkErrorIsRetry(t *testing.T) {
	c := New(Options{BaseURL: "http://127.0.0.1:1", Version: "test"})
	_, cls, _ := c.Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if cls != Retry {
		t.Fatalf("classification = %v want Retry", cls)
	}
}
