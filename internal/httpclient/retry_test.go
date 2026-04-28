package httpclient

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   Classification
	}{
		{"200 ok", 200, nil, Success},
		{"201 created", 201, nil, Success},
		{"400 bad request", 400, nil, Poison},
		{"403 forbidden", 403, nil, Poison},
		{"404 not found", 404, nil, Poison},
		{"401 unauthorized", 401, nil, Halt},
		{"408 timeout", 408, nil, Retry},
		{"429 rate limit", 429, nil, Retry},
		{"500 server error", 500, nil, Retry},
		{"502 bad gateway", 502, nil, Retry},
		{"503 unavailable", 503, nil, Retry},
		{"504 gateway timeout", 504, nil, Retry},
		{"other 4xx", 418, nil, Retry},
		{"network error", 0, errFake("dial tcp: connection refused"), Retry},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.status, c.err)
			if got != c.want {
				t.Fatalf("Classify(%d, %v) = %v want %v", c.status, c.err, got, c.want)
			}
		})
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
