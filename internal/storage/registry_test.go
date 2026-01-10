package storage

import (
	"strings"
	"testing"
	"time"
)

const (
	testRepoNomad = "hashicorp/nomad"
	testHashMux   = "h_mux"
)

func TestRegistryAddAndGet(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	entry := RegistryEntry{
		Name:      "myrepo",
		Path:      "/tmp/myrepo",
		Hash:      "abc12345",
		IndexedAt: time.Now().Truncate(time.Second),
		NodeCount: 10,
		EdgeCount: 20,
	}
	if err := reg.Add(entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := reg.Get("myrepo")
	if !ok {
		t.Fatal("expected to find myrepo by name")
	}
	if got.Name != entry.Name || got.Path != entry.Path || got.Hash != entry.Hash {
		t.Errorf("entry mismatch: got %+v", got)
	}
	if got.NodeCount != 10 || got.EdgeCount != 20 {
		t.Errorf("count mismatch: got %+v", got)
	}

	got2, ok := reg.Get("abc12345")
	if !ok {
		t.Fatal("expected to find myrepo by hash")
	}
	if got2.Name != "myrepo" {
		t.Errorf("hash lookup: expected name myrepo, got %s", got2.Name)
	}
}

func TestRegistryRemove(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "hash_a"})
	_ = reg.Add(RegistryEntry{Name: "b", Hash: "hash_b"})

	if err := reg.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, ok := reg.Get("a"); ok {
		t.Error("expected a to be removed")
	}
	if _, ok := reg.Get("b"); !ok {
		t.Error("expected b to still exist")
	}
}

func TestRegistryRemoveByHash(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{Name: "svc", Hash: "h1"})

	if err := reg.Remove("h1"); err != nil {
		t.Fatalf("Remove by hash: %v", err)
	}
	if _, ok := reg.Get("svc"); ok {
		t.Error("expected svc to be removed by hash")
	}
}

func TestRegistryListSorted(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	_ = reg.Add(RegistryEntry{Name: "charlie", Hash: "h3"})
	_ = reg.Add(RegistryEntry{Name: "alpha", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "bravo", Hash: "h2"})

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "bravo" || list[2].Name != "charlie" {
		t.Errorf("unexpected order: %v, %v, %v", list[0].Name, list[1].Name, list[2].Name)
	}
}

func TestRegistryPersistence(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	_ = reg.Add(RegistryEntry{Name: "persist", Path: "/tmp/persist", Hash: "xyz"})

	reg2, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry (reload): %v", err)
	}

	got, ok := reg2.Get("persist")
	if !ok {
		t.Fatal("expected to find persisted entry")
	}
	if got.Path != "/tmp/persist" || got.Hash != "xyz" {
		t.Errorf("persisted entry mismatch: %+v", got)
	}
}

func TestRegistryEmptyList(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	list := reg.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d entries", len(list))
	}
}

func TestRegistryRemoveNonExistent(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if err := reg.Remove("nonexistent"); err != nil {
		t.Errorf("Remove nonexistent should not error, got: %v", err)
	}
}

// --- Same-name collision tests ---

func TestRegistrySameNameDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Two repos both named "backend" at different paths.
	_ = reg.Add(RegistryEntry{Name: "backend", Path: "/work/a/backend", Hash: "hash_a"})
	_ = reg.Add(RegistryEntry{Name: "backend", Path: "/work/b/backend", Hash: "hash_b"})

	// Both should exist (keyed by hash, not name).
	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries for same-name repos, got %d", len(list))
	}

	// Get by hash returns the correct one.
	gotA, ok := reg.Get("hash_a")
	if !ok || gotA.Path != "/work/a/backend" {
		t.Errorf("hash_a lookup: got %+v", gotA)
	}
	gotB, ok := reg.Get("hash_b")
	if !ok || gotB.Path != "/work/b/backend" {
		t.Errorf("hash_b lookup: got %+v", gotB)
	}

	// Get by name returns one of them (deterministic but unspecified which).
	gotName, ok := reg.Get("backend")
	if !ok {
		t.Fatal("Get by name should return one of the two")
	}
	if gotName.Name != "backend" {
		t.Errorf("name mismatch: %s", gotName.Name)
	}
}

func TestRegistryResolveAmbiguous(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "backend", Path: "/a/backend", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "backend", Path: "/b/backend", Hash: "h2"})

	_, err := reg.Resolve("backend")
	if err == nil {
		t.Fatal("expected ambiguity error for same-name repos")
	}

	e, err := reg.Resolve("h1")
	if err != nil {
		t.Fatalf("Resolve by hash: %v", err)
	}
	if e.Path != "/a/backend" {
		t.Errorf("expected /a/backend, got %s", e.Path)
	}
}

func TestRegistryResolveUniqueName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "api", Hash: "h1"})

	e, err := reg.Resolve("api")
	if err != nil {
		t.Fatalf("Resolve unique: %v", err)
	}
	if e.Hash != "h1" {
		t.Errorf("hash mismatch: %s", e.Hash)
	}
}

func TestRegistryResolveNotFound(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_, err := reg.Resolve("ghost")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestRegistryResolveDidYouMean(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{Name: "pdfa", Hash: "h1", Path: "/tmp/pdfa"})
	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "h2", Path: "/tmp/nomad"})

	tests := []struct {
		input       string
		wantSuggest string
	}{
		{"pdaf", "pdfa"},   // transposition
		{"pdfaa", "pdfa"},  // extra char
		{"pdf", "pdfa"},    // missing char
		{"nomda", "nomad"}, // alias basename match
		{"zzzzzzz", ""},    // too far — no suggestion
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := reg.Resolve(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantSuggest != "" {
				if !strings.Contains(err.Error(), "did you mean") {
					t.Fatalf("expected 'did you mean' in error, got: %s", err)
				}
				if !strings.Contains(err.Error(), tt.wantSuggest) {
					t.Fatalf("expected suggestion %q in error, got: %s", tt.wantSuggest, err)
				}
			} else if strings.Contains(err.Error(), "did you mean") {
				t.Fatalf("expected no suggestion for %q, got: %s", tt.input, err)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"pdfa", "pdaf", 1},
		{"kitten", "sitting", 3},
		{"nomad", "nomda", 1},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// --- Short-name / alias resolution tests ---

func TestRegistryGetByShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "h1", Path: "/tmp/nomad"})

	got, ok := reg.Get(testRepoNomad)
	if !ok || got.Hash != "h1" {
		t.Fatalf("expected hashicorp/nomad, got %+v", got)
	}

	got, ok = reg.Get("nomad")
	if !ok {
		t.Fatal("expected to find nomad by short name")
	}
	if got.Name != testRepoNomad || got.Hash != "h1" {
		t.Errorf("short name lookup mismatch: got %+v", got)
	}
}

func TestRegistryResolveByShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "temporalio/temporal", Hash: "h2"})

	// "nomad" → hashicorp/nomad
	e, err := reg.Resolve("nomad")
	if err != nil {
		t.Fatalf("Resolve(nomad): %v", err)
	}
	if e.Name != testRepoNomad {
		t.Errorf("expected hashicorp/nomad, got %s", e.Name)
	}

	// "temporal" → temporalio/temporal
	e, err = reg.Resolve("temporal")
	if err != nil {
		t.Fatalf("Resolve(temporal): %v", err)
	}
	if e.Name != "temporalio/temporal" {
		t.Errorf("expected temporalio/temporal, got %s", e.Name)
	}
}

func TestRegistryResolveShortNameAmbiguous(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "acme/example", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "beta/example", Hash: "h2"})

	// "example" is ambiguous — should error with helpful message.
	_, err := reg.Resolve("example")
	if err == nil {
		t.Fatal("expected ambiguity error for example")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected 'ambiguous' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "acme/example") {
		t.Errorf("expected 'acme/example' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "beta/example") {
		t.Errorf("expected 'beta/example' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "full project name") {
		t.Errorf("expected 'full project name' hint in error, got: %v", err)
	}

	e, err := reg.Resolve("acme/example")
	if err != nil {
		t.Fatalf("Resolve(acme/example): %v", err)
	}
	if e.Hash != "h1" {
		t.Errorf("expected h1, got %s", e.Hash)
	}
}

func TestRegistryResolveShortNameDoesNotShadowExact(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// A repo whose full name is just "nomad" (local repo) AND
	// testRepoNomad (remote repo with same basename).
	_ = reg.Add(RegistryEntry{Name: "nomad", Hash: "local"})
	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "remote"})

	// "nomad" should match the exact name first (local), not the alias.
	e, err := reg.Resolve("nomad")
	if err != nil {
		t.Fatalf("Resolve(nomad): %v", err)
	}
	if e.Hash != "local" {
		t.Errorf("expected exact match (local), got hash %s", e.Hash)
	}
}

func TestRegistryRemoveByShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "h1"})

	if err := reg.Remove("nomad"); err != nil {
		t.Fatalf("Remove by short name: %v", err)
	}
	if _, ok := reg.Get(testRepoNomad); ok {
		t.Error("expected hashicorp/nomad to be removed via short name")
	}
}

func TestRegistrySameNameListStableOrder(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "svc", Hash: "zzz"})
	_ = reg.Add(RegistryEntry{Name: "svc", Hash: "aaa"})

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	// Same name → sorted by hash.
	if list[0].Hash != "aaa" || list[1].Hash != "zzz" {
		t.Errorf("expected hash order aaa, zzz; got %s, %s", list[0].Hash, list[1].Hash)
	}
}

// --- Linking tests (hash-based) ---

func TestRegistryLink(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "api-gateway", Hash: "h_gw"})
	_ = reg.Add(RegistryEntry{Name: "auth-service", Hash: "h_auth"})

	if err := reg.Link("api-gateway", "auth-service"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	linked := reg.GetLinkedRepos("api-gateway")
	if len(linked) != 1 || linked[0] != "h_auth" {
		t.Errorf("api-gateway linked: got %v, want [h_auth]", linked)
	}
	linked = reg.GetLinkedRepos("auth-service")
	if len(linked) != 1 || linked[0] != "h_gw" {
		t.Errorf("auth-service linked: got %v, want [h_gw]", linked)
	}
}

func TestRegistryLinkByHash(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "b", Hash: "h2"})

	if err := reg.Link("h1", "h2"); err != nil {
		t.Fatalf("Link by hash: %v", err)
	}

	linked := reg.GetLinkedRepos("h1")
	if len(linked) != 1 || linked[0] != "h2" {
		t.Errorf("expected [h2], got %v", linked)
	}
}

func TestRegistryLinkIdempotent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "b", Hash: "h2"})

	_ = reg.Link("a", "b")
	_ = reg.Link("a", "b") // duplicate

	linked := reg.GetLinkedRepos("a")
	if len(linked) != 1 {
		t.Errorf("idempotent link failed: got %d links, want 1", len(linked))
	}
}

func TestRegistryLinkNotFound(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "h1"})

	if err := reg.Link("a", "nonexistent"); err == nil {
		t.Error("expected error linking to nonexistent repo")
	}
	if err := reg.Link("nonexistent", "a"); err == nil {
		t.Error("expected error linking from nonexistent repo")
	}
}

func TestRegistryLinkSelfPrevented(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "h1"})

	err := reg.Link("a", "a")
	if err == nil {
		t.Fatal("expected error self-linking")
	}

	err = reg.Link("h1", "h1")
	if err == nil {
		t.Fatal("expected error self-linking by hash")
	}
}

func TestRegistryUnlink(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "b", Hash: "h2"})
	_ = reg.Link("a", "b")

	if err := reg.Unlink("a", "b"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	if linked := reg.GetLinkedRepos("a"); len(linked) != 0 {
		t.Errorf("a should have no links, got %v", linked)
	}
	if linked := reg.GetLinkedRepos("b"); len(linked) != 0 {
		t.Errorf("b should have no links, got %v", linked)
	}
}

func TestRegistryRemoveCleansUpDanglingLinks(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "a", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "b", Hash: "h2"})
	_ = reg.Add(RegistryEntry{Name: "c", Hash: "h3"})
	_ = reg.Link("a", "b")
	_ = reg.Link("a", "c")

	// Remove b — a and c should have b's hash cleaned from their links.
	if err := reg.Remove("b"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	linked := reg.GetLinkedRepos("a")
	if len(linked) != 1 || linked[0] != "h3" {
		t.Errorf("a should only link to h3 after b removed, got %v", linked)
	}
	if linked := reg.GetLinkedRepos("c"); len(linked) != 1 || linked[0] != "h1" {
		t.Errorf("c should still link to h1, got %v", linked)
	}
}

func TestRegistryLinkedReposPersistence(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "x", Hash: "hx"})
	_ = reg.Add(RegistryEntry{Name: "y", Hash: "hy"})
	_ = reg.Link("x", "y")

	reg2, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry reload: %v", err)
	}

	linked := reg2.GetLinkedRepos("x")
	if len(linked) != 1 || linked[0] != "hy" {
		t.Errorf("persisted link: got %v, want [hy]", linked)
	}
}

func TestRegistryGetLinkedReposNonExistent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	linked := reg.GetLinkedRepos("nonexistent")
	if linked != nil {
		t.Errorf("expected nil for nonexistent repo, got %v", linked)
	}
}

// --- Version tracking ---

func TestRegistryIndexVersion(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "svc", Hash: "h1", IndexVersion: "abc123"})

	got, ok := reg.Get("h1")
	if !ok {
		t.Fatal("expected entry")
	}
	if got.IndexVersion != "abc123" {
		t.Errorf("IndexVersion: expected abc123, got %s", got.IndexVersion)
	}

	_ = reg.Add(RegistryEntry{Name: "svc", Hash: "h1", IndexVersion: "def456"})
	got, _ = reg.Get("h1")
	if got.IndexVersion != "def456" {
		t.Errorf("IndexVersion after re-index: expected def456, got %s", got.IndexVersion)
	}
}

func TestRegistryIndexVersionPersists(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{Name: "svc", Hash: "h1", IndexVersion: "v1"})

	reg2, _ := NewRegistry(dir)
	got, _ := reg2.Get("h1")
	if got.IndexVersion != "v1" {
		t.Errorf("persisted IndexVersion: expected v1, got %s", got.IndexVersion)
	}
}

// --- Linking same-name repos by hash ---

func TestRegistryLinkSameNameReposByHash(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Two repos with the same name at different paths.
	_ = reg.Add(RegistryEntry{Name: "backend", Path: "/a/backend", Hash: "h_a"})
	_ = reg.Add(RegistryEntry{Name: "backend", Path: "/b/backend", Hash: "h_b"})
	_ = reg.Add(RegistryEntry{Name: "frontend", Hash: "h_fe"})

	if err := reg.Link("h_a", "h_fe"); err != nil {
		t.Fatalf("Link by hash: %v", err)
	}

	linked := reg.GetLinkedRepos("h_a")
	if len(linked) != 1 || linked[0] != "h_fe" {
		t.Errorf("h_a linked: got %v, want [h_fe]", linked)
	}

	// Other "backend" (h_b) should NOT be linked.
	linked = reg.GetLinkedRepos("h_b")
	if len(linked) != 0 {
		t.Errorf("h_b should not be linked, got %v", linked)
	}
}

// --- URL normalization tests ---

func TestNormalizeGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/gorilla/mux", "github.com/gorilla/mux"},
		{"https://github.com/gorilla/mux.git", "github.com/gorilla/mux"},
		{"http://github.com/gorilla/mux.git", "github.com/gorilla/mux"},
		{"git@github.com:gorilla/mux.git", "github.com/gorilla/mux"},
		{"git@github.com:org/private-api.git", "github.com/org/private-api"},
		{"ssh://git@github.com/gorilla/mux", "github.com/gorilla/mux"},
		{"https://gitlab.com/group/project", "gitlab.com/group/project"},
		{"https://github.com/gorilla/mux/", "github.com/gorilla/mux"},
		{"github.com/gorilla/mux", "github.com/gorilla/mux"},
	}

	for _, tt := range tests {
		got := NormalizeGitURL(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeGitURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGitURLToImportPath(t *testing.T) {
	got := GitURLToImportPath("https://github.com/gorilla/mux.git")
	if got != "github.com/gorilla/mux" {
		t.Errorf("GitURLToImportPath: got %q, want github.com/gorilla/mux", got)
	}
}

func TestNormalizeGitURL_VariationsProduceSameResult(t *testing.T) {
	variants := []string{
		"https://github.com/gorilla/mux",
		"https://github.com/gorilla/mux.git",
		"http://github.com/gorilla/mux.git",
		"git@github.com:gorilla/mux.git",
		"ssh://git@github.com/gorilla/mux",
	}
	expected := NormalizeGitURL(variants[0])
	for _, v := range variants[1:] {
		got := NormalizeGitURL(v)
		if got != expected {
			t.Errorf("NormalizeGitURL(%q) = %q, want %q", v, got, expected)
		}
	}
}

// --- ResolveByURL tests ---

func TestRegistryResolveByURL(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	e, ok := reg.ResolveByURL("https://github.com/gorilla/mux")
	if !ok || e.Hash != testHashMux {
		t.Errorf("exact URL match: got ok=%v hash=%s", ok, e.Hash)
	}

	e, ok = reg.ResolveByURL("git@github.com:gorilla/mux.git")
	if !ok || e.Hash != testHashMux {
		t.Errorf("SSH URL match: got ok=%v hash=%s", ok, e.Hash)
	}

	e, ok = reg.ResolveByURL("https://github.com/gorilla/mux.git")
	if !ok || e.Hash != testHashMux {
		t.Errorf(".git suffix URL match: got ok=%v hash=%s", ok, e.Hash)
	}

	_, ok = reg.ResolveByURL("https://github.com/gorilla/websocket")
	if ok {
		t.Error("expected no match for different repo")
	}
}

func TestRegistryResolveByURL_LocalReposSkipped(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "mux", Hash: "h_local", Path: "/tmp/mux"})

	_, ok := reg.ResolveByURL("https://github.com/gorilla/mux")
	if ok {
		t.Error("local repo without URL should not match URL lookup")
	}
}

// --- ResolveByImportPath tests ---

func TestRegistryResolveByImportPath_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	e, suffix, ok := reg.ResolveByImportPath("github.com/gorilla/mux")
	if !ok {
		t.Fatal("expected match")
	}
	if e.Hash != testHashMux {
		t.Errorf("hash mismatch: %s", e.Hash)
	}
	if suffix != "" {
		t.Errorf("expected empty suffix for exact match, got %q", suffix)
	}
}

func TestRegistryResolveByImportPath_SubpackageMatch(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	e, suffix, ok := reg.ResolveByImportPath("github.com/gorilla/mux/middleware")
	if !ok {
		t.Fatal("expected prefix match for subpackage")
	}
	if e.Hash != testHashMux {
		t.Errorf("hash mismatch: %s", e.Hash)
	}
	if suffix != "middleware" {
		t.Errorf("suffix: got %q, want %q", suffix, "middleware")
	}
}

func TestRegistryResolveByImportPath_LongestMatchWins(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})
	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux/v2",
		Hash: "h_mux_v2",
		URL:  "https://github.com/gorilla/mux/v2",
	})

	e, suffix, ok := reg.ResolveByImportPath("github.com/gorilla/mux/v2/router")
	if !ok {
		t.Fatal("expected match")
	}
	if e.Hash != "h_mux_v2" {
		t.Errorf("expected longest match (h_mux_v2), got %s", e.Hash)
	}
	if suffix != "router" {
		t.Errorf("suffix: got %q, want %q", suffix, "router")
	}
}

func TestRegistryResolveByImportPath_NoMatch(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	_, _, ok := reg.ResolveByImportPath("github.com/gin-gonic/gin")
	if ok {
		t.Error("expected no match for unrelated import")
	}
}

func TestRegistryResolveByImportPath_IgnoresLocalRepos(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{Name: "mux", Hash: "h1", Path: "/tmp/mux"})

	_, _, ok := reg.ResolveByImportPath("github.com/gorilla/mux")
	if ok {
		t.Error("local repo (no URL) should not match import path lookup")
	}
}

// --- Mixed local + URL cross-repo linking scenarios ---

func TestRegistryLinkLocalToURLRepo(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "my-api",
		Hash: "h_local",
		Path: "/home/user/my-api",
	})
	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	if err := reg.Link("my-api", "gorilla/mux"); err != nil {
		t.Fatalf("Link local↔URL: %v", err)
	}

	linked := reg.GetLinkedRepos("h_local")
	if len(linked) != 1 || linked[0] != testHashMux {
		t.Errorf("local linked repos: got %v, want [h_mux]", linked)
	}
	linked = reg.GetLinkedRepos(testHashMux)
	if len(linked) != 1 || linked[0] != "h_local" {
		t.Errorf("URL linked repos: got %v, want [h_local]", linked)
	}
}

func TestRegistryURLReposPersistence(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name:         "gorilla/mux",
		Hash:         testHashMux,
		URL:          "https://github.com/gorilla/mux",
		IndexVersion: "abc123",
	})

	reg2, _ := NewRegistry(dir)
	e, ok := reg2.Get(testHashMux)
	if !ok {
		t.Fatal("expected persisted URL repo")
	}
	if e.URL != "https://github.com/gorilla/mux" {
		t.Errorf("URL not persisted: %s", e.URL)
	}
	if e.IndexVersion != "abc123" {
		t.Errorf("IndexVersion not persisted: %s", e.IndexVersion)
	}
}

func TestRegistryResolveByURL_Persisted(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	reg2, _ := NewRegistry(dir)
	e, ok := reg2.ResolveByURL("git@github.com:gorilla/mux.git")
	if !ok || e.Hash != testHashMux {
		t.Errorf("persisted URL resolution: ok=%v hash=%s", ok, e.Hash)
	}
}

func TestRegistryGetByURLRepoName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_ = reg.Add(RegistryEntry{
		Name: "gorilla/mux",
		Hash: testHashMux,
		URL:  "https://github.com/gorilla/mux",
	})

	e, ok := reg.Get("gorilla/mux")
	if !ok {
		t.Fatal("expected to find by owner/name")
	}
	if e.Hash != testHashMux {
		t.Errorf("hash mismatch: %s", e.Hash)
	}
}

func TestRegistryResolveAmbiguous_MixedLocalAndURL(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Local repo named "mux".
	_ = reg.Add(RegistryEntry{Name: "mux", Hash: "h_local", Path: "/tmp/mux"})
	// URL repo also named "mux" (unlikely but possible for short names).
	_ = reg.Add(RegistryEntry{Name: "mux", Hash: "h_url", URL: "https://github.com/gorilla/mux"})

	// Resolve by name should be ambiguous.
	_, err := reg.Resolve("mux")
	if err == nil {
		t.Fatal("expected ambiguity error for mixed local+URL same-name repos")
	}

	// Resolve by hash is unambiguous.
	e, err := reg.Resolve("h_url")
	if err != nil {
		t.Fatalf("Resolve by hash: %v", err)
	}
	if e.URL != "https://github.com/gorilla/mux" {
		t.Errorf("expected URL repo, got %+v", e)
	}

	// ResolveByURL is also unambiguous.
	e, ok := reg.ResolveByURL("https://github.com/gorilla/mux")
	if !ok || e.Hash != "h_url" {
		t.Errorf("ResolveByURL: ok=%v hash=%s", ok, e.Hash)
	}
}

// --- ResolveRepoName (shared convenience function) ---

func TestResolveRepoName_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "h1"})

	got, err := ResolveRepoName(dir, testRepoNomad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testRepoNomad {
		t.Errorf("got %q, want %q", got, testRepoNomad)
	}
}

func TestResolveRepoName_ShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{Name: testRepoNomad, Hash: "h1"})

	got, err := ResolveRepoName(dir, "nomad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testRepoNomad {
		t.Errorf("got %q, want %q", got, testRepoNomad)
	}
}

func TestResolveRepoName_Ambiguous(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_ = reg.Add(RegistryEntry{Name: "acme/sdk", Hash: "h1"})
	_ = reg.Add(RegistryEntry{Name: "corp/sdk", Hash: "h2"})

	_, err := ResolveRepoName(dir, "sdk")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "acme/sdk") || !strings.Contains(err.Error(), "corp/sdk") {
		t.Errorf("expected both repo names in error, got: %v", err)
	}
}

func TestResolveRepoName_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, _ = NewRegistry(dir) // ensure registry file exists

	_, err := ResolveRepoName(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestResolveRepoName_EmptyDataDir(t *testing.T) {
	got, err := ResolveRepoName("", "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "anything" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestResolveRepoName_InvalidDataDir(t *testing.T) {
	// Non-existent directory — registry unavailable, should pass through.
	got, err := ResolveRepoName("/nonexistent/path/xyz", "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "myrepo" {
		t.Errorf("expected passthrough, got %q", got)
	}
}
