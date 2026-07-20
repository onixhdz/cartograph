package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryDir(t *testing.T) {
	dataDir := t.TempDir()
	got, err := RepositoryDir(dataDir, "group/project@feature/v2", "abcdef01")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataDir, "group", "project@feature", "v2", "abcdef01")
	if got != want {
		t.Fatalf("RepositoryDir = %q, want %q", got, want)
	}
}

func TestRepositoryDirRejectsExistingSymlinkEscape(t *testing.T) {
	dataDir := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(dataDir, "group")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got, err := RepositoryDir(dataDir, "group/project", "abcdef01"); err == nil {
		t.Fatalf("RepositoryDir = %q, want symlink containment error", got)
	}
}

func TestRegistryAddRejectsUnsafeRepositoryIdentity(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []RegistryEntry{
		{Name: "../escape", Hash: "abcdef01"},
		{Name: "group/./project", Hash: "abcdef01"},
		{Name: "group\\project", Hash: "abcdef01"},
		{Name: "group/project", Hash: "../escape"},
	} {
		if err := registry.Add(entry); err == nil {
			t.Fatalf("Add(%+v) expected error", entry)
		}
	}
	if entries := registry.List(); len(entries) != 0 {
		t.Fatalf("unsafe entries persisted: %+v", entries)
	}
}

func TestRepositoryDirRejectsUnsafeIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		hash string
	}{
		{name: "../escape", hash: "abcdef01"},
		{name: "group/./project", hash: "abcdef01"},
		{name: "group/../project", hash: "abcdef01"},
		{name: "group\\project", hash: "abcdef01"},
		{name: "/absolute", hash: "abcdef01"},
		{name: "group/project", hash: "../escape"},
		{name: "group/project", hash: "hash/child"},
	} {
		t.Run(tc.name+"/"+tc.hash, func(t *testing.T) {
			if got, err := RepositoryDir(t.TempDir(), tc.name, tc.hash); err == nil {
				t.Fatalf("RepositoryDir = %q, want error", got)
			}
		})
	}
}
