//go:build !embedding_cgo

package local

import (
	"errors"
	"testing"
)

// TestStubReportsUnavailable guards the default build: without the
// embedding_cgo tag, New must fail with ErrUnavailable so callers fall back
// rather than silently shipping a non-functional local backend.
func TestStubReportsUnavailable(t *testing.T) {
	if _, err := New([]byte("model")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New: got %v, want ErrUnavailable", err)
	}
}
