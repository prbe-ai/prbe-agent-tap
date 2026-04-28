package httpclient

import (
	"math/rand"
	"time"
)

const (
	backoffBase = 1 * time.Second
	backoffCap  = 5 * time.Minute
)

// Backoff returns the delay before the next attempt:
//
//	min(2^attempt * 1s, 5min) + jitter ∈ [0, 1s)
func Backoff(attempt int) time.Duration {
	exp := backoffBase << attempt
	if exp <= 0 || exp > backoffCap {
		exp = backoffCap
	}
	jitter := time.Duration(rand.Int63n(int64(time.Second)))
	return exp + jitter
}
