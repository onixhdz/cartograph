package plugin

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestJoinNameRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	got, err := JoinName(base, "plugin")
	if err != nil {
		t.Fatalf("JoinName valid: %v", err)
	}
	if got != filepath.Join(base, "plugin") {
		t.Fatalf("JoinName valid = %q", got)
	}

	if _, err := JoinName(base, "../plugin"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("JoinName traversal error = %v", err)
	}
}
