package auth

import (
	"testing"
	"time"
)

// SetRetryDelay sets retryDelay for the duration of the test.
func SetRetryDelay(t testing.TB, d time.Duration) {
	old := retryDelay
	retryDelay = d
	t.Cleanup(func() { retryDelay = old })
}
