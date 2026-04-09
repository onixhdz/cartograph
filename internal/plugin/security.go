package plugin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Security errors.
var (
	ErrChecksumMismatch = errors.New("plugin: checksum mismatch")
	ErrChecksumFormat   = errors.New("plugin: invalid checksum format")
)

// VerifyChecksum computes the SHA-256 hash of the file at binaryPath and
// compares it to the expected checksum. The checksum must be in the format
// "sha256:<hex>". Comparison uses crypto/hmac.Equal for constant-time
// comparison (no timing side-channels).
func VerifyChecksum(binaryPath string, checksum string) error {
	algo, expected, err := parseChecksum(checksum)
	if err != nil {
		return err
	}
	if algo != "sha256" {
		return fmt.Errorf("%w: unsupported algorithm %q (only sha256 is supported)", ErrChecksumFormat, algo)
	}

	actual, err := hashFile(binaryPath)
	if err != nil {
		return fmt.Errorf("plugin: hashing binary: %w", err)
	}

	if !hmac.Equal(actual, expected) {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrChecksumMismatch,
			hex.EncodeToString(expected),
			hex.EncodeToString(actual),
		)
	}
	return nil
}

// parseChecksum parses "sha256:<hex>" into the algorithm name and decoded bytes.
func parseChecksum(s string) (algo string, hash []byte, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("%w: expected \"algorithm:hex\", got %q", ErrChecksumFormat, s)
	}
	algo = parts[0]
	hash, err = hex.DecodeString(parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("%w: invalid hex in checksum: %w", ErrChecksumFormat, err)
	}
	return algo, hash, nil
}

// hashFile computes the SHA-256 hash of the file at the given path.
func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return h.Sum(nil), nil
}
