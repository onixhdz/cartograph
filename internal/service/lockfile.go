package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/realxen/cartograph/internal/sysutil"
)

// MaxLockAge is the maximum age of a lockfile before it is considered
// stale regardless of whether the PID appears alive. This guards against
// PID reuse on long-running systems.
const MaxLockAge = 7 * 24 * time.Hour // 1 week

// lockInfo is the JSON structure written inside the lockfile.
type lockInfo struct {
	PID        int    `json:"pid"`
	SocketPath string `json:"socket"`
	Network    string `json:"network,omitempty"`    // "unix" or "tcp"
	CreatedAt  int64  `json:"created_at,omitempty"` // unix epoch seconds
}

// Lockfile manages a file-based PID lock (flock + JSON) to ensure
// only one service instance runs per data directory.
type Lockfile struct {
	path       string // JSON data file (service.pid)
	lockPath   string // flock file (service.lock)
	flock      *flock.Flock
	pid        int
	socketPath string
}

// NewLockfile creates a Lockfile that will manage {dir}/service.pid
// with a companion {dir}/service.lock for the flock.
func NewLockfile(dir string) *Lockfile {
	p := filepath.Join(dir, "service.pid")
	lp := filepath.Join(dir, "service.lock")
	return &Lockfile{
		path:     p,
		lockPath: lp,
		flock:    flock.New(lp),
	}
}

// Acquire attempts to obtain an exclusive lock and write process info.
// Returns an error if the lock is already held by another process.
// The network parameter should be "unix" or "tcp".
func (l *Lockfile) Acquire(socketPath string, network ...string) error {
	locked, err := l.flock.TryLock()
	if err != nil {
		return fmt.Errorf("lockfile: flock: %w", err)
	}
	if !locked {
		return fmt.Errorf("lockfile: already locked (another service instance is running)")
	}

	l.pid = os.Getpid()
	l.socketPath = socketPath

	net := "unix"
	if len(network) > 0 && network[0] != "" {
		net = network[0]
	}

	info := lockInfo{
		PID:        l.pid,
		SocketPath: socketPath,
		Network:    net,
		CreatedAt:  time.Now().Unix(),
	}
	data, err := json.Marshal(info)
	if err != nil {
		l.flock.Unlock() //nolint:errcheck
		return fmt.Errorf("lockfile: marshal: %w", err)
	}
	// Atomic write: write to a temp file in the same directory, then
	// rename over the target. This prevents readers from seeing a
	// half-written file if the process crashes mid-write.
	if err := atomicWriteFile(l.path, data, 0o644); err != nil {
		l.flock.Unlock() //nolint:errcheck
		return fmt.Errorf("lockfile: write: %w", err)
	}
	return nil
}

// atomicWriteFile writes data to a temp file in the same directory as
// path, then renames it over path. This is crash-safe on POSIX systems
// (rename is atomic within the same filesystem).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".lockfile-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// On Windows, rename fails if the target exists and is open.
	// The flock prevents concurrent writers, so this is safe.
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Chmod(path, perm)
}

// Release unlocks the flock and removes both lockfile and data file.
func (l *Lockfile) Release() error {
	err := l.flock.Unlock()
	os.Remove(l.path)     // best-effort: remove JSON data
	os.Remove(l.lockPath) // best-effort: remove flock file
	return err
}

// IsStale returns true if the lockfile exists but:
//   - the PID recorded in it is no longer running, OR
//   - the lockfile is older than MaxLockAge (guards against PID reuse).
func (l *Lockfile) IsStale() bool {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return false
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return false
	}

	// Max-age TTL: if the lockfile is older than MaxLockAge, treat as
	// stale regardless of PID (protects against PID reuse on long-
	// running systems).
	if info.CreatedAt > 0 {
		age := time.Since(time.Unix(info.CreatedAt, 0))
		if age > MaxLockAge {
			return true
		}
	}

	return !sysutil.IsProcessRunning(info.PID)
}

// ReadInfo reads the PID, socket path, and network from the lockfile.
func (l *Lockfile) ReadInfo() (pid int, socketPath string, err error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return 0, "", fmt.Errorf("lockfile: read: %w", err)
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return 0, "", fmt.Errorf("lockfile: unmarshal: %w", err)
	}
	return info.PID, info.SocketPath, nil
}

// ReadFullInfo reads the PID, address, and network type from the lockfile.
func (l *Lockfile) ReadFullInfo() (pid int, addr, network string, err error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return 0, "", "", fmt.Errorf("lockfile: read: %w", err)
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return 0, "", "", fmt.Errorf("lockfile: unmarshal: %w", err)
	}
	net := info.Network
	if net == "" {
		net = "unix" // backward compat
	}
	return info.PID, info.SocketPath, net, nil
}
