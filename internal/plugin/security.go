package plugin

import (
	"errors"
	"path/filepath"

	"github.com/onixhdz/cartograph/internal/sysutil"
)

var ErrInvalidName = errors.New("plugin: invalid name")

// JoinName returns the path for a plugin-owned name under base.
func JoinName(base, name string) (string, error) {
	if base == "" || !sysutil.IsPathSegment(name) {
		return "", ErrInvalidName
	}
	return filepath.Join(base, name), nil
}
