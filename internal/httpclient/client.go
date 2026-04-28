package httpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
)

type Options struct {
	BaseURL string
	Version string
	HTTP    *http.Client
}

type Client struct {
	base    string
	version string
	hc      *http.Client
}

func New(o Options) *Client {
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: o.BaseURL, version: o.Version, hc: hc}
}

type Request struct {
	Method string
	Path   string
	Bearer string
	Body   []byte
}

func (c *Client) Do(ctx context.Context, r Request) (*http.Response, Classification, error) {
	url := c.base + r.Path
	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, url, body)
	if err != nil {
		return nil, Retry, err
	}
	if r.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.Bearer)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("prbe-agent-tap/%s (%s/%s)", c.version, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-Trace-Id", newTraceID())
	if len(r.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, Classify(0, err), err
	}
	return resp, Classify(resp.StatusCode, nil), nil
}

func newTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
