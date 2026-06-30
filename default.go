package cartograph

import "github.com/onixhdz/cartograph/internal/storage"

// DefaultDataDir returns the default data directory for Cartograph.
//
// It respects XDG_DATA_HOME when set, otherwise it falls back to
// ~/.local/share/cartograph.
func DefaultDataDir() string {
	return storage.DefaultDataDir()
}
