package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// RegistryEntry describes a single indexed repository.
type RegistryEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	IndexedAt time.Time `json:"indexedAt"`
	NodeCount int       `json:"nodeCount"`
	EdgeCount int       `json:"edgeCount"`

	// URL is the original Git URL (normalized: https, no trailing .git).
	// Empty for locally analyzed repos.
	URL string `json:"url,omitempty"`

	// IndexVersion changes each time the repo is re-indexed.
	// Used to detect stale cross-repo edges.
	IndexVersion string `json:"indexVersion,omitempty"`

	// LinkedRepos lists hashes (not names) of repos that this repo has
	// declared cross-repo relationships with. Hashes are used instead of
	// names because names are not unique (two repos can share a basename).
	LinkedRepos []string `json:"linkedRepos,omitempty"`

	// Meta holds per-repo details (commit hash, languages, duration, etc.)
	// inline so the registry is the single source of truth.
	Meta Meta `json:"meta"`
}

// Registry manages a persistent list of indexed repositories.
// Entries are keyed by Hash; name-based lookups may be ambiguous.
type Registry struct {
	path    string
	flock   *flock.Flock // cross-process file lock for safe read-modify-write
	entries map[string]RegistryEntry // keyed by Hash
	mu      sync.RWMutex
}

// NewRegistry loads or creates a registry at {dir}/registry.json.
func NewRegistry(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	regPath := filepath.Join(dir, "registry.json")
	r := &Registry{
		path:    regPath,
		flock:   flock.New(regPath + ".lock"),
		entries: make(map[string]RegistryEntry),
	}
	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return r, nil
}

// Add adds or updates an entry and persists the registry.
// When updating, embedding state is preserved from the previous entry.
func (r *Registry) Add(entry RegistryEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.lockAndReload(); err != nil {
		return err
	}
	defer r.flock.Unlock() //nolint:errcheck
	if prev, ok := r.entries[entry.Hash]; ok {
		entry.Meta.EmbeddingStatus = prev.Meta.EmbeddingStatus
		entry.Meta.EmbeddingModel = prev.Meta.EmbeddingModel
		entry.Meta.EmbeddingDims = prev.Meta.EmbeddingDims
		entry.Meta.EmbeddingProvider = prev.Meta.EmbeddingProvider
		entry.Meta.EmbeddingNodes = prev.Meta.EmbeddingNodes
		entry.Meta.EmbeddingTotal = prev.Meta.EmbeddingTotal
		entry.Meta.EmbeddingError = prev.Meta.EmbeddingError
		entry.Meta.EmbeddingDuration = prev.Meta.EmbeddingDuration
	}
	r.entries[entry.Hash] = entry
	return r.save()
}

// Remove removes an entry by name-or-hash and persists the registry.
// It also cleans up dangling references in other entries' LinkedRepos.
// Removing a non-existent entry is not an error.
func (r *Registry) Remove(nameOrHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.lockAndReload(); err != nil {
		return err
	}
	defer r.flock.Unlock() //nolint:errcheck

	hash := r.resolveToHashLocked(nameOrHash)
	if hash == "" {
		return nil // not found, not an error
	}

	delete(r.entries, hash)

	// Clean up dangling links in other entries.
	for k, e := range r.entries {
		if slices.Contains(e.LinkedRepos, hash) {
			e.LinkedRepos = removeString(e.LinkedRepos, hash)
			r.entries[k] = e
		}
	}
	return r.save()
}

// Get returns the entry for the given name or hash.
// Supports short-name aliases (e.g. "nomad" matches "hashicorp/nomad").
func (r *Registry) Get(nameOrHash string) (RegistryEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.entries[nameOrHash]; ok {
		return e, true
	}

	for _, e := range r.entries {
		if e.Name == nameOrHash {
			return e, true
		}
	}

	for _, e := range r.entries {
		if repoBasename(e.Name) == nameOrHash && e.Name != nameOrHash {
			return e, true
		}
	}
	return RegistryEntry{}, false
}

// Resolve looks up a repo by name or hash, returning an error on ambiguity.
// Supports short-name aliases (e.g. "nomad" resolves to "hashicorp/nomad").
func (r *Registry) Resolve(nameOrHash string) (RegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.entries[nameOrHash]; ok {
		return e, nil
	}

	// Hash-prefix match: allow short hashes (e.g. first 8 chars from `list`).
	if len(nameOrHash) >= 4 {
		var prefixMatches []RegistryEntry
		for _, e := range r.entries {
			if strings.HasPrefix(e.Hash, nameOrHash) {
				prefixMatches = append(prefixMatches, e)
			}
		}
		switch len(prefixMatches) {
		case 1:
			return prefixMatches[0], nil
		default:
			if len(prefixMatches) > 1 {
				names := make([]string, len(prefixMatches))
				for i, m := range prefixMatches {
					names[i] = fmt.Sprintf("%s (%s)", m.Name, m.Hash)
				}
				sort.Strings(names)
				return RegistryEntry{}, fmt.Errorf(
					"hash prefix %q is ambiguous — matches: %s",
					nameOrHash, strings.Join(names, ", "),
				)
			}
		}
	}

	var matches []RegistryEntry
	for _, e := range r.entries {
		if e.Name == nameOrHash {
			matches = append(matches, e)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		var aliasMatches []RegistryEntry
		for _, e := range r.entries {
			if repoBasename(e.Name) == nameOrHash && e.Name != nameOrHash {
				aliasMatches = append(aliasMatches, e)
			}
		}
		switch len(aliasMatches) {
		case 0:
			if suggestion := r.closestRepoName(nameOrHash); suggestion != "" {
				return RegistryEntry{}, fmt.Errorf("repo %q not found in registry — did you mean %q?", nameOrHash, suggestion)
			}
			return RegistryEntry{}, fmt.Errorf("repo %q not found in registry", nameOrHash)
		case 1:
			return aliasMatches[0], nil
		default:
			names := make([]string, len(aliasMatches))
			for i, m := range aliasMatches {
				names[i] = m.Name
			}
			sort.Strings(names)
			return RegistryEntry{}, fmt.Errorf(
				"repo name %q is ambiguous — multiple repositories share this name.\nUse the full project name: %s",
				nameOrHash, strings.Join(names, ", "),
			)
		}
	default:
		hashes := make([]string, len(matches))
		for i, m := range matches {
			hashes[i] = m.Hash
		}
		return RegistryEntry{}, fmt.Errorf(
			"repo name %q is ambiguous (%d repos); use hash to disambiguate: %v",
			nameOrHash, len(matches), hashes,
		)
	}
}

// List returns all entries sorted by name (then hash for stable order
// when names collide).
func (r *Registry) List() []RegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]RegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Hash < result[j].Hash
	})
	return result
}

// Link declares a bidirectional cross-repo relationship between two repos
// identified by name or hash. Both repos must already exist in the
// registry. A repo cannot be linked to itself.
func (r *Registry) Link(a, b string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.lockAndReload(); err != nil {
		return err
	}
	defer r.flock.Unlock() //nolint:errcheck

	hashA := r.resolveToHashLocked(a)
	hashB := r.resolveToHashLocked(b)
	if hashA == "" {
		return fmt.Errorf("repo %q not found in registry", a)
	}
	if hashB == "" {
		return fmt.Errorf("repo %q not found in registry", b)
	}
	if hashA == hashB {
		return fmt.Errorf("cannot link a repo to itself")
	}

	ea := r.entries[hashA]
	eb := r.entries[hashB]

	if !slices.Contains(ea.LinkedRepos, hashB) {
		ea.LinkedRepos = append(ea.LinkedRepos, hashB)
		r.entries[hashA] = ea
	}
	if !slices.Contains(eb.LinkedRepos, hashA) {
		eb.LinkedRepos = append(eb.LinkedRepos, hashA)
		r.entries[hashB] = eb
	}
	return r.save()
}

// Unlink removes a bidirectional cross-repo relationship between two repos.
func (r *Registry) Unlink(a, b string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.lockAndReload(); err != nil {
		return err
	}
	defer r.flock.Unlock() //nolint:errcheck

	hashA := r.resolveToHashLocked(a)
	hashB := r.resolveToHashLocked(b)

	if hashA != "" {
		if ea, ok := r.entries[hashA]; ok {
			ea.LinkedRepos = removeString(ea.LinkedRepos, hashB)
			r.entries[hashA] = ea
		}
	}
	if hashB != "" {
		if eb, ok := r.entries[hashB]; ok {
			eb.LinkedRepos = removeString(eb.LinkedRepos, hashA)
			r.entries[hashB] = eb
		}
	}
	return r.save()
}

// GetLinkedRepos returns the hashes of repos linked to the given repo.
func (r *Registry) GetLinkedRepos(nameOrHash string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hash := r.resolveToHashLocked(nameOrHash)
	if hash == "" {
		return nil
	}
	e, ok := r.entries[hash]
	if !ok {
		return nil
	}
	result := make([]string, len(e.LinkedRepos))
	copy(result, e.LinkedRepos)
	return result
}

// EmbeddingInfo holds the fields written atomically by an embed job.
type EmbeddingInfo struct {
	Status   string
	Model    string
	Dims     int
	Provider string
	Nodes    int
	Total    int
	Error    string
	Duration string
}

// UpdateEmbedding atomically updates the embedding fields in the entry's
// Meta and persists the registry. This is the only writer for embedding
// state — no read-modify-write race.
func (r *Registry) UpdateEmbedding(nameOrHash string, info EmbeddingInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.lockAndReload(); err != nil {
		return err
	}
	defer r.flock.Unlock() //nolint:errcheck

	hash := r.resolveToHashLocked(nameOrHash)
	if hash == "" {
		return fmt.Errorf("repo %q not found in registry", nameOrHash)
	}
	e := r.entries[hash]
	e.Meta.EmbeddingStatus = info.Status
	e.Meta.EmbeddingModel = info.Model
	e.Meta.EmbeddingDims = info.Dims
	e.Meta.EmbeddingProvider = info.Provider
	e.Meta.EmbeddingNodes = info.Nodes
	e.Meta.EmbeddingTotal = info.Total
	e.Meta.EmbeddingError = info.Error
	e.Meta.EmbeddingDuration = info.Duration
	r.entries[hash] = e
	return r.save()
}

// resolveToHashLocked resolves a name-or-hash to a hash.
// Tries direct hash, exact name, then short-name alias. Returns "" if not found.
func (r *Registry) resolveToHashLocked(nameOrHash string) string {
	if _, ok := r.entries[nameOrHash]; ok {
		return nameOrHash
	}
	if len(nameOrHash) >= 4 {
		var hit string
		for h := range r.entries {
			if strings.HasPrefix(h, nameOrHash) {
				if hit != "" {
					break // ambiguous — fall through to name matching
				}
				hit = h
			}
		}
		if hit != "" {
			return hit
		}
	}
	for _, e := range r.entries {
		if e.Name == nameOrHash {
			return e.Hash
		}
	}
	// Short-name / alias: match basename of owner/repo names.
	for _, e := range r.entries {
		if repoBasename(e.Name) == nameOrHash && e.Name != nameOrHash {
			return e.Hash
		}
	}
	return ""
}

// save writes the registry entries to disk as JSON. Uses temp+rename
// for atomicity so a crash mid-write doesn't corrupt the file.
func (r *Registry) save() error {
	list := make([]RegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// lockAndReload acquires the cross-process file lock and reloads
// entries from disk so the caller operates on the latest state.
// The caller MUST call r.flock.Unlock() when done (typically via defer).
func (r *Registry) lockAndReload() error {
	if err := r.flock.Lock(); err != nil {
		return fmt.Errorf("registry: acquire file lock: %w", err)
	}
	// Reload from disk so we don't overwrite entries added by another process.
	fresh := make(map[string]RegistryEntry)
	r.entries = fresh
	if err := r.load(); err != nil && !os.IsNotExist(err) {
		r.flock.Unlock() //nolint:errcheck
		return fmt.Errorf("registry: reload: %w", err)
	}
	return nil
}

// load reads the registry entries from disk. Supports both old format
// (keyed by Name) and new format (keyed by Hash) transparently.
func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	// Treat empty files as an empty registry (can happen if a previous
	// write was interrupted or truncated).
	if len(data) == 0 {
		return nil
	}
	var list []RegistryEntry
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	for _, e := range list {
		if e.Hash != "" {
			r.entries[e.Hash] = e
		} else {
			// Legacy entry without hash — key by name as fallback.
			r.entries[e.Name] = e
		}
	}
	return nil
}

// ResolveByURL finds a registry entry by its Git URL. The input URL
// is normalized before comparison so that variations like trailing .git,
// git@ SSH syntax, and http vs https all match correctly.
func (r *Registry) ResolveByURL(rawURL string) (RegistryEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	norm := NormalizeGitURL(rawURL)
	if norm == "" {
		return RegistryEntry{}, false
	}
	for _, e := range r.entries {
		if e.URL != "" && NormalizeGitURL(e.URL) == norm {
			return e, true
		}
	}
	return RegistryEntry{}, false
}

// ResolveByImportPath finds a registry entry whose URL matches a
// language-level import path (e.g. "github.com/gorilla/mux").
// Prefix-based: "github.com/gorilla/mux/middleware" also matches.
// Returns the entry and remaining path suffix.
func (r *Registry) ResolveByImportPath(importPath string) (entry RegistryEntry, suffix string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clean := importPath
	for _, prefix := range []string{"https://", "http://", "ssh://"} {
		clean = strings.TrimPrefix(clean, prefix)
	}

	// Find the longest matching entry (most specific match wins).
	var bestEntry RegistryEntry
	var bestImport string
	for _, e := range r.entries {
		if e.URL == "" {
			continue
		}
		eImport := GitURLToImportPath(e.URL)
		if eImport == "" {
			continue
		}
		if clean == eImport || strings.HasPrefix(clean, eImport+"/") {
			if len(eImport) > len(bestImport) {
				bestEntry = e
				bestImport = eImport
			}
		}
	}

	if bestImport == "" {
		return RegistryEntry{}, "", false
	}

	suf := strings.TrimPrefix(clean, bestImport)
	suf = strings.TrimPrefix(suf, "/")
	return bestEntry, suf, true
}

// NormalizeGitURL converts a Git URL to a canonical form for comparison.
//
// Examples:
//
//	https://github.com/gorilla/mux.git   → github.com/gorilla/mux
//	git@github.com:gorilla/mux.git       → github.com/gorilla/mux
func NormalizeGitURL(rawURL string) string {
	s := rawURL

	for _, prefix := range []string{"https://", "http://", "ssh://"} {
		s = strings.TrimPrefix(s, prefix)
	}

	// Handle SSH syntax: git@host:owner/repo → host/owner/repo
	if after, ok := strings.CutPrefix(s, "git@"); ok {
		s = after
		s = strings.Replace(s, ":", "/", 1)
	}

	s = strings.TrimSuffix(s, ".git")

	s = strings.TrimRight(s, "/")

	return s
}

// GitURLToImportPath converts a Git URL to a language-level import path.
func GitURLToImportPath(rawURL string) string {
	return NormalizeGitURL(rawURL)
}

// repoBasename returns the last component of a slash-separated repo
// name. For "hashicorp/nomad" it returns "nomad". For a plain name
// like "myrepo" it returns "myrepo" unchanged.
func repoBasename(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// levenshtein returns the Damerau-Levenshtein distance (optimal string
// alignment variant) between two strings. It counts insertions, deletions,
// substitutions, and transpositions of adjacent characters as single edits.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// We need two previous rows for the transposition check.
	prevPrev := make([]int, lb+1) // row i-2
	prev := make([]int, lb+1)     // row i-1
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			min := curr[j-1] + 1 // insertion
			if del := prev[j] + 1; del < min {
				min = del // deletion
			}
			if sub := prev[j-1] + cost; sub < min {
				min = sub // substitution
			}
			// Transposition of adjacent characters.
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				if trans := prevPrev[j-2] + 1; trans < min {
					min = trans
				}
			}
			curr[j] = min
		}
		prevPrev = prev
		prev = curr
	}
	return prev[lb]
}

// closestRepoName returns the best "did you mean?" suggestion from all
// registry entries. It compares against full names and basenames and
// returns the best match within a reasonable edit-distance threshold.
// Returns "" if no close match exists.
func (r *Registry) closestRepoName(input string) string {
	best := ""
	bestDist := int(^uint(0) >> 1) // max int

	for _, e := range r.entries {
		for _, candidate := range []string{e.Name, repoBasename(e.Name)} {
			d := levenshtein(strings.ToLower(input), strings.ToLower(candidate))
			if d < bestDist {
				bestDist = d
				best = candidate
			}
		}
	}

	// Only suggest if the distance is ≤ 40 % of the longer string's length
	// (at least 1 and at most 4).
	maxLen := max(len(best), len(input))
	threshold := min(
		// 40 %
		max(

			maxLen*2/5, 1), 4)
	if bestDist <= threshold {
		return best
	}
	return ""
}

func removeString(slice []string, s string) []string {
	result := slice[:0]
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

// ResolveRepoName opens the registry at dataDir and resolves a repo
// identifier (hash, name, or alias) to its canonical name.
func ResolveRepoName(dataDir, name string) (string, error) {
	if dataDir == "" {
		return name, nil
	}
	reg, err := NewRegistry(dataDir)
	if err != nil {
		return name, nil // registry unavailable — pass through
	}
	entry, err := reg.Resolve(name)
	if err != nil {
		return "", err
	}
	return entry.Name, nil
}
