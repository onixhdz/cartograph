package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onixhdz/cartograph/internal/storage"
)

func TestResolveTargetSupportedStrings(t *testing.T) {
	local := t.TempDir()
	cases := []struct {
		name    string
		target  string
		ref     string
		kind    TargetKind
		value   string
		wantRef string
	}{
		{name: "local", target: local, kind: TargetLocal, value: local},
		{name: "https", target: "https://github.com/gorilla/mux.git", kind: TargetRemote, value: "https://github.com/gorilla/mux.git"},
		{name: "host prefix", target: "gitlab.com/group/project", kind: TargetRemote, value: "https://gitlab.com/group/project"},
		{name: "shorthand", target: "gorilla/mux", kind: TargetRemote, value: "https://github.com/gorilla/mux"},
		{name: "inline ref", target: "gorilla/mux@v1.8.1", kind: TargetRemote, value: "https://github.com/gorilla/mux", wantRef: "v1.8.1"},
		{name: "explicit ref", target: "https://github.com/gorilla/mux", ref: "main", kind: TargetRemote, value: "https://github.com/gorilla/mux", wantRef: "main"},
		{name: "bare unknown", target: "mux", kind: TargetUnknown, value: "mux"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTarget(tc.target, tc.ref, t.TempDir())
			if err != nil {
				t.Fatalf("ResolveTarget: %v", err)
			}
			if got.Kind != tc.kind || got.Value != tc.value || got.Ref != tc.wantRef {
				t.Fatalf("got %+v, want kind=%s value=%q ref=%q", got, tc.kind, tc.value, tc.wantRef)
			}
		})
	}
}

func TestResolveTargetClassifiesExplicitURLBeforeFilesystem(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "https:", "github.com", "owner", "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	got, err := ResolveTarget("https://github.com/owner/repo", "main", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != TargetRemote || got.Value != "https://github.com/owner/repo" || got.Ref != "main" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveTargetPrefersExistingShorthandPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "owner", "repo@local")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	got, err := ResolveTarget("owner/repo@local", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != TargetLocal || got.Value != "owner/repo@local" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveTargetRegistryAliases(t *testing.T) {
	dataDir := t.TempDir()
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name: "mux", Hash: "abc123", URL: "github.com/gorilla/mux@v1.8.1", IndexedAt: time.Now(),
		Meta: storage.Meta{Branch: "v1.8.1"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTarget("mux", "", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != TargetRemote || got.Value != "https://github.com/gorilla/mux" || got.Ref != "v1.8.1" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveTargetRegistryCloneTargets(t *testing.T) {
	cases := []struct {
		name    string
		entry   storage.RegistryEntry
		wantURL string
		wantRef string
	}{
		{
			name:    "resolved default branch remains implicit",
			entry:   storage.RegistryEntry{Name: "group/project", Hash: "gitlab", Path: "https://gitlab.com/group/project.git", URL: "gitlab.com/group/project", Meta: storage.Meta{Branch: "main"}},
			wantURL: "https://gitlab.com/group/project.git",
		},
		{
			name:    "self hosted default branch remains implicit",
			entry:   storage.RegistryEntry{Name: "team/service", Hash: "selfhosted", Path: "https://git.example.test/team/service.git", URL: "git.example.test/team/service", Meta: storage.Meta{Branch: "release"}},
			wantURL: "https://git.example.test/team/service.git",
		},
		{
			name:    "exact ref qualified GitLab name",
			entry:   storage.RegistryEntry{Name: "group/project@release/v2", Hash: "gitlab-ref", Path: "https://gitlab.com/group/project.git", URL: "gitlab.com/group/project@release/v2", Meta: storage.Meta{Branch: "release/v2"}},
			wantURL: "https://gitlab.com/group/project.git", wantRef: "release/v2",
		},
		{
			name:    "exact ref qualified self hosted name",
			entry:   storage.RegistryEntry{Name: "team/service@stable", Hash: "selfhosted-ref", Path: "https://git.example.test/team/service.git", URL: "git.example.test/team/service@stable", Meta: storage.Meta{Branch: "stable"}},
			wantURL: "https://git.example.test/team/service.git", wantRef: "stable",
		},
		{
			name:    "SSH clone URL",
			entry:   storage.RegistryEntry{Name: "org/private", Hash: "sshclone", Path: "git@gitlab.com:org/private.git", URL: "gitlab.com/org/private@v2", Meta: storage.Meta{Branch: "v2"}},
			wantURL: "git@gitlab.com:org/private.git", wantRef: "v2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			registry, err := storage.NewRegistry(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			tc.entry.IndexedAt = time.Now()
			if err := registry.Add(tc.entry); err != nil {
				t.Fatal(err)
			}
			got, err := ResolveTarget(tc.entry.Name, "", dataDir)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != TargetRemote || got.Value != tc.wantURL || got.Ref != tc.wantRef {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestResolveTargetReplaysCanonicalTagWhenHEADIsDetached(t *testing.T) {
	dataDir := t.TempDir()
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	entry := storage.RegistryEntry{
		Name: "group/project@v2.0.0", Hash: "tagged-hash",
		Path: "https://gitlab.com/group/project.git", URL: "gitlab.com/group/project@v2.0.0",
		Meta: storage.Meta{Branch: "HEAD"},
	}
	if err := registry.Add(entry); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{entry.Hash, "project"} {
		t.Run(target, func(t *testing.T) {
			got, err := ResolveTarget(target, "", dataDir)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != TargetRemote || got.Value != entry.Path || got.Ref != "v2.0.0" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestResolveTargetPreservesAmbiguousRegistryError(t *testing.T) {
	dataDir := t.TempDir()
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []storage.RegistryEntry{
		{Name: "one/project", Hash: "hash-one", URL: "github.com/one/project"},
		{Name: "two/project", Hash: "hash-two", URL: "github.com/two/project"},
	} {
		if err := registry.Add(entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ResolveTarget("project", "", dataDir); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected registry ambiguity, got %v", err)
	}
}

func TestResolveTargetRejectsConflictingRefs(t *testing.T) {
	if _, err := ResolveTarget("gorilla/mux@v1.8.1", "main", t.TempDir()); err == nil {
		t.Fatal("expected conflicting ref error")
	}
}
