package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockfileAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "service.pid")); err != nil {
		t.Fatalf("lockfile not found: %v", err)
	}

	if err := lf.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestLockfileDoubleAcquireFails(t *testing.T) {
	dir := t.TempDir()
	lf1 := NewLockfile(dir)
	lf2 := NewLockfile(dir)

	if err := lf1.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer lf1.Release()

	if err := lf2.Acquire("/tmp/test2.sock"); err == nil {
		t.Fatal("expected error on double acquire, got nil")
	}
}

func TestLockfileReadInfoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	sock := "/tmp/carto.sock"
	if err := lf.Acquire(sock); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	pid, sp, err := lf.ReadInfo()
	if err != nil {
		t.Fatalf("read info: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid mismatch: got %d, want %d", pid, os.Getpid())
	}
	if sp != sock {
		t.Errorf("socket path mismatch: got %q, want %q", sp, sock)
	}
}

func TestLockfileIsStaleAfterRelease(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	if lf.IsStale() {
		t.Error("expected IsStale=false while process is running")
	}

	lf.Release()

	if lf.IsStale() {
		t.Error("expected IsStale=false after release (file removed)")
	}
}

func TestLockfileIsStaleWithBogusFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "service.pid")
	os.WriteFile(p, []byte(`{"pid":999999999,"socket":"/x"}`), 0o644)

	lf := NewLockfile(dir)
	if !lf.IsStale() {
		t.Error("expected stale for non-existent PID")
	}
}

func TestLockfileReadFullInfo_NetworkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	addr := "127.0.0.1:9876"
	if err := lf.Acquire(addr, "tcp"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	pid, gotAddr, network, err := lf.ReadFullInfo()
	if err != nil {
		t.Fatalf("ReadFullInfo: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid: got %d, want %d", pid, os.Getpid())
	}
	if gotAddr != addr {
		t.Errorf("addr: got %q, want %q", gotAddr, addr)
	}
	if network != "tcp" {
		t.Errorf("network: got %q, want %q", network, "tcp")
	}
}

func TestLockfileReadFullInfo_DefaultsToUnix(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	_, _, network, err := lf.ReadFullInfo()
	if err != nil {
		t.Fatalf("ReadFullInfo: %v", err)
	}
	if network != "unix" {
		t.Errorf("network: got %q, want %q", network, "unix")
	}
}

func TestLockfileReadFullInfo_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "service.pid")
	os.WriteFile(p, []byte(`{"pid":1,"socket":"/tmp/old.sock"}`), 0o644)

	lf := NewLockfile(dir)
	_, _, network, err := lf.ReadFullInfo()
	if err != nil {
		t.Fatalf("ReadFullInfo: %v", err)
	}
	if network != "unix" {
		t.Errorf("network: got %q, want %q (backward compat)", network, "unix")
	}
}

func TestLockfileAtomicWrite_NoPartialFile(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	data, err := os.ReadFile(filepath.Join(dir, "service.pid"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("lockfile contains invalid JSON (possible partial write): %v", err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("PID mismatch: got %d, want %d", info.PID, os.Getpid())
	}
}

func TestLockfileAtomicWrite_NoTempFileLeftOver(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file found: %s", e.Name())
		}
	}
}

func TestLockfileCreatedAt_Populated(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	before := time.Now().Unix()
	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	data, err := os.ReadFile(filepath.Join(dir, "service.pid"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var info lockInfo
	json.Unmarshal(data, &info)

	if info.CreatedAt < before {
		t.Errorf("CreatedAt (%d) should be >= test start (%d)", info.CreatedAt, before)
	}
	if info.CreatedAt > time.Now().Unix()+1 {
		t.Errorf("CreatedAt (%d) is in the future", info.CreatedAt)
	}
}

func TestLockfileIsStale_MaxAgeTTL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "service.pid")
	oldTime := time.Now().Add(-(MaxLockAge + time.Hour)).Unix()
	info := lockInfo{
		PID:        os.Getpid(), // alive PID
		SocketPath: "/tmp/test.sock",
		CreatedAt:  oldTime,
	}
	data, _ := json.Marshal(info)
	os.WriteFile(p, data, 0o644)

	lf := NewLockfile(dir)
	if !lf.IsStale() {
		t.Error("expected stale due to max-age TTL, even though PID is alive")
	}
}

func TestLockfileIsStale_RecentTimestampNotStale(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "service.pid")
	info := lockInfo{
		PID:        os.Getpid(),
		SocketPath: "/tmp/test.sock",
		CreatedAt:  time.Now().Unix(),
	}
	data, _ := json.Marshal(info)
	os.WriteFile(p, data, 0o644)

	lf := NewLockfile(dir)
	if lf.IsStale() {
		t.Error("expected NOT stale for recent lockfile with live PID")
	}
}

func TestLockfileIsStale_NoTimestampFallsBackToPID(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "service.pid")
	os.WriteFile(p, []byte(`{"pid":999999999,"socket":"/x"}`), 0o644)

	lf := NewLockfile(dir)
	if !lf.IsStale() {
		t.Error("expected stale for dead PID (no timestamp)")
	}
}

func TestLockfileSeparateLockFile(t *testing.T) {
	dir := t.TempDir()
	lf := NewLockfile(dir)

	if err := lf.Acquire("/tmp/test.sock"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lf.Release()

	if _, err := os.Stat(filepath.Join(dir, "service.pid")); err != nil {
		t.Error("service.pid should exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "service.lock")); err != nil {
		t.Error("service.lock should exist")
	}
}
