package storage

import (
	"os"
	"path/filepath"
)

// DefaultDataDir returns the default data directory for Cartograph.
//
// It respects XDG_DATA_HOME when set, otherwise it falls back to
// ~/.local/share/cartograph.
func DefaultDataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartograph")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "cartograph")
	}
	return filepath.Join(home, ".local", "share", "cartograph")
}
