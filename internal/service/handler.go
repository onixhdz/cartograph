package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/realxen/cartograph/internal/embedding"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/sysutil"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Response{Result: v}) //nolint:errchkjson
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	httpStatus := code
	switch code {
	case ErrCodeRepoNotFound:
		httpStatus = http.StatusNotFound
	case ErrCodeQueryBlocked:
		httpStatus = http.StatusForbidden
	case ErrCodeIncompatible:
		httpStatus = http.StatusConflict
	case ErrCodeMethodUnknown:
		httpStatus = http.StatusNotFound
	case ErrCodeInvalidParams:
		httpStatus = http.StatusBadRequest
	case ErrCodeInternal:
		httpStatus = http.StatusInternalServerError
	default:
		if httpStatus < 100 || httpStatus > 599 {
			httpStatus = http.StatusInternalServerError
		}
	}
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(Response{ //nolint:errchkjson
		Error: &APIError{Code: code, Message: msg},
	})
}

// maxRequestBody is the maximum allowed request body size (1 MiB).
const maxRequestBody = 1 << 20

func decodeJSON(r *http.Request, v any) error {
	// Enforce a body size limit to prevent denial-of-service via
	// excessively large payloads.
	r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) == 0 {
		return errors.New("empty request body")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

// requirePOST returns true if the request is POST; otherwise it writes
// a 405 error and returns false.
func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use POST")
		return false
	}
	return true
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req QueryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo
	registry, err := storage.NewRegistry(s.dataDir)
	if err == nil {
		if entry, ok := registry.Get(req.Repo); ok && entry.Meta.PluginName != "" {
			if !req.Plugin {
				msg := ErrPluginQueryBlocked.Error() + "; use plugin references and cartograph cypher -p <plugin-dataset> or cartograph query -p <plugin-dataset>"
				writeError(w, ErrCodeQueryBlocked, msg)
				return
			}
		}
	}

	backend, err := s.GetBackend(req.Repo)
	if err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	result, err := backend.Query(req)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req SearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}
	if req.Pattern == "" {
		writeError(w, http.StatusBadRequest, "missing pattern")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo
	if registry, err := storage.NewRegistry(s.dataDir); err == nil {
		if entry, ok := registry.Get(req.Repo); ok && entry.Meta.PluginName != "" {
			writeError(w, ErrCodeQueryBlocked, "raw source search is not available for plugin datasets")
			return
		}
	}

	backend, err := s.GetBackend(req.Repo)
	if err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}
	result, err := backend.Search(req)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req ContextRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend, err := s.GetBackend(req.Repo)
	if err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	result, err := backend.Context(req)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleCypher(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req CypherRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	// Block write queries at the API layer before reaching any backend.
	if IsWriteQuery(req.Query) {
		writeError(w, ErrCodeQueryBlocked, ErrWriteQuery.Error())
		return
	}

	backend, err := s.GetBackend(req.Repo)
	if err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	result, err := backend.Cypher(req)
	if err != nil {
		if errors.Is(err, ErrWriteQuery) {
			writeError(w, ErrCodeQueryBlocked, err.Error())
		} else {
			writeError(w, ErrCodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req ImpactRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend, err := s.GetBackend(req.Repo)
	if err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	result, err := backend.Impact(req)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleCat(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req CatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}
	if len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "missing files")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	cr := s.GetContentResolver(req.Repo)
	if cr == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q has no content resolver", req.Repo))
		return
	}

	lineStart, lineEnd, err := ParseLineRange(req.Lines)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result := CatResult{Files: make([]CatFile, 0, len(req.Files))}
	for _, path := range req.Files {
		data, err := cr.ReadFile(path)
		if err != nil {
			result.Files = append(result.Files, CatFile{
				Path:  path,
				Error: err.Error(),
			})
			continue
		}
		content := string(data)
		lineCount := strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") && len(content) > 0 {
			lineCount++
		}

		if lineStart > 0 && lineEnd > 0 {
			lines := strings.Split(content, "\n")
			if lineStart > len(lines) {
				lineStart = len(lines)
			}
			if lineEnd > len(lines) {
				lineEnd = len(lines)
			}
			content = strings.Join(lines[lineStart-1:lineEnd], "\n")
		}

		result.Files = append(result.Files, CatFile{
			Path:      path,
			Content:   content,
			LineCount: lineCount,
		})
	}
	writeJSON(w, &result)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req TreeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	if err := s.lazyLoadGraph(req.Repo); err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	g, _, ok := s.GetRepoResources(req.Repo)
	if !ok {
		if entry, found := s.registryEntry(req.Repo); found && entry.Hash != req.Repo {
			req.Repo = entry.Hash
			g, _, ok = s.GetRepoResources(req.Repo)
		}
	}
	if !ok || g == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	writeJSON(w, BuildTreeResult(req.Repo, g))
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req ReloadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	if err := s.ReloadGraph(req.Repo); err != nil {
		writeError(w, ErrCodeInternal, fmt.Sprintf("reload %q: %v", req.Repo, err))
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use GET")
		return
	}
	writeJSON(w, s.BuildHealth())
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use GET")
		return
	}
	result, err := s.BuildList()
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

// BuildList returns every indexed repository recorded in the registry.
func (s *Server) BuildList() (*ListResult, error) {
	return listRepos(s.dataDir)
}

// listRepos reads the on-disk registry and returns every indexed repository,
// loaded or not. Shared by the HTTP handler and the in-process MemoryClient.
func listRepos(dataDir string) (*ListResult, error) {
	if dataDir == "" {
		return &ListResult{Repos: []RepoInfo{}}, nil
	}
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return nil, fmt.Errorf("list: open registry: %w", err)
	}
	entries := registry.List()
	repos := make([]RepoInfo, 0, len(entries))
	for _, e := range entries {
		repos = append(repos, buildRepoInfo(e))
	}
	return &ListResult{Repos: repos}, nil
}

func buildRepoInfo(e storage.RegistryEntry) RepoInfo {
	embedding := e.Meta.EmbeddingStatus
	if embedding == "" {
		embedding = embeddingStatusNone
	}
	info := RepoInfo{
		Name:      e.Name,
		Hash:      e.Hash,
		Type:      repoTypeLabel(e),
		NodeCount: e.NodeCount,
		EdgeCount: e.EdgeCount,
		BuiltWith: e.Meta.BinaryVersion,
		Embedding: embedding,
	}
	if !e.IndexedAt.IsZero() {
		info.IndexedAt = e.IndexedAt.Format(time.RFC3339)
	}
	return info
}

// Repository classification labels and the default embedding status, shared by
// the list and status builders.
const (
	repoTypeLocal       = "local"
	repoTypeURL         = "url"
	repoTypeClonedOnly  = "cloned (not indexed)"
	repoTypeURLCloned   = "url, cloned"
	embeddingStatusNone = "none"
)

// repoTypeLabel classifies a registry entry by origin and index state.
func repoTypeLabel(e storage.RegistryEntry) string {
	if e.URL == "" {
		return repoTypeLocal
	}
	switch {
	case e.Meta.ClonedOnly:
		return repoTypeClonedOnly
	case e.Meta.SourcePath != "":
		return repoTypeURLCloned
	default:
		return repoTypeURL
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req StatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}
	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	result, err := s.BuildStatus(repo)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

// BuildStatus returns per-repository index detail from the registry.
func (s *Server) BuildStatus(repo string) (*StatusResult, error) {
	return buildStatus(s.dataDir, repo)
}

// buildStatus reads a single repository's index detail from the registry,
// mirroring `cartograph status`. Shared by the HTTP handler and MemoryClient.
func buildStatus(dataDir, repo string) (*StatusResult, error) {
	if dataDir == "" {
		return nil, errors.New("repo status: no data directory")
	}
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return nil, fmt.Errorf("repo status: open registry: %w", err)
	}
	entry, err := registry.Resolve(repo)
	if err != nil {
		return nil, fmt.Errorf("repo status: %w", err)
	}
	m := entry.Meta
	embedding := m.EmbeddingStatus
	if embedding == "" {
		embedding = embeddingStatusNone
	}
	res := &StatusResult{
		Name:              entry.Name,
		Hash:              entry.Hash,
		Path:              entry.Path,
		URL:               entry.URL,
		Type:              repoTypeLabel(entry),
		Indexed:           !m.ClonedOnly,
		NodeCount:         entry.NodeCount,
		EdgeCount:         entry.EdgeCount,
		Commit:            m.CommitHash,
		Branch:            m.Branch,
		Languages:         m.Languages,
		Duration:          m.Duration,
		BuiltWith:         m.BinaryVersion,
		EmbeddingStatus:   embedding,
		EmbeddingProgress: m.EmbeddingNodes,
		EmbeddingTotal:    m.EmbeddingTotal,
		EmbeddingModel:    m.EmbeddingModel,
		EmbeddingProvider: m.EmbeddingProvider,
		EmbeddingDims:     m.EmbeddingDims,
		EmbeddingError:    m.EmbeddingError,
	}
	if !entry.IndexedAt.IsZero() {
		res.IndexedAt = entry.IndexedAt.Format(time.RFC3339)
	}
	repoDir := filepath.Join(dataDir, entry.Name, entry.Hash)
	for _, name := range []string{"graph.db", "search.bleve", "search.regex", "embeddings.db"} {
		if size, ok := artifactSize(filepath.Join(repoDir, name)); ok {
			res.Artifacts = append(res.Artifacts, RepoArtifact{Name: name, Bytes: size})
		}
	}
	return res, nil
}

// artifactSize returns the byte size of a file, or the recursive size of a
// directory artifact (e.g. search.bleve). Reports ok=false when absent.
func artifactSize(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	if !info.IsDir() {
		return info.Size(), true
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if fi, statErr := d.Info(); statErr == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, true
}

func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req SchemaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend, err := s.GetBackend(req.Repo)
	if err != nil {
		writeError(w, ErrCodeIncompatible, err.Error())
		return
	}
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	result, err := backend.Schema(req)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	writeJSON(w, map[string]string{"status": "shutting down"})

	if s.httpServer != nil {
		go func() { //nolint:gosec,contextcheck // G118: intentional background context for async shutdown
			_ = s.Stop(context.Background())
		}()
	}
}

func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req EmbedRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	if err := embedding.ValidateServiceModel(req.Model); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	job := s.StartEmbedJob(r.Context(), req)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(Response{Result: &EmbedStatusResult{ //nolint:errchkjson
		Repo:     job.Repo,
		Status:   job.Status,
		Progress: job.Progress,
		Total:    job.Total,
		Model:    job.Model,
		Provider: job.Provider,
		Dims:     job.Dims,
	}})
}

func (s *Server) handleEmbedStatus(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req EmbedStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "missing repo")
		return
	}

	repo, err := s.ResolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	job := s.GetEmbedJob(req.Repo)
	if job == nil {
		if s.dataDir != "" {
			registry, err := storage.NewRegistry(s.dataDir)
			if err == nil {
				if entry, ok := registry.Get(req.Repo); ok && entry.Meta.EmbeddingStatus != "" {
					total := entry.Meta.EmbeddingTotal
					if total == 0 {
						total = entry.Meta.EmbeddingNodes
					}
					writeJSON(w, &EmbedStatusResult{
						Repo:     req.Repo,
						Status:   entry.Meta.EmbeddingStatus,
						Progress: entry.Meta.EmbeddingNodes,
						Total:    total,
						Model:    entry.Meta.EmbeddingModel,
						Provider: entry.Meta.EmbeddingProvider,
						Dims:     entry.Meta.EmbeddingDims,
						Error:    entry.Meta.EmbeddingError,
						Duration: entry.Meta.EmbeddingDuration,
					})
					return
				}
			}
		}
		writeJSON(w, &EmbedStatusResult{
			Repo:   req.Repo,
			Status: "",
		})
		return
	}

	writeJSON(w, &EmbedStatusResult{
		Repo:            job.Repo,
		Status:          job.Status,
		Progress:        job.Progress,
		Total:           job.Total,
		Model:           job.Model,
		Provider:        job.Provider,
		Dims:            job.Dims,
		Error:           job.Error,
		Duration:        job.Duration,
		DownloadFile:    job.DownloadFile,
		DownloadPercent: job.DownloadPercent,
	})
}

func (s *Server) handleAnalyzePreflight(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req AnalyzePreflightRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := BuildAnalyzePreflight(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, res)
}

func (s *Server) handlePluginIngest(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req PluginIngestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PluginName == "" {
		writeError(w, http.StatusBadRequest, "missing pluginName")
		return
	}
	if !sysutil.IsPathSegment(req.PluginName) || (req.ConnectionName != "" && !sysutil.IsPathSegment(req.ConnectionName)) {
		writeError(w, http.StatusBadRequest, "invalid pluginName or connectionName")
		return
	}
	job := s.StartPluginIngestJob(r.Context(), req)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(Response{Result: &PluginIngestStatusResult{ //nolint:errchkjson
		PluginName: job.PluginName,
		Status:     job.Status,
		Nodes:      job.Nodes,
		Edges:      job.Edges,
		Error:      job.Error,
		Duration:   job.Duration,
	}})
}

func (s *Server) handlePluginIngestStatus(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if !requirePOST(w, r) {
		return
	}
	var req PluginIngestStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PluginName == "" {
		writeError(w, http.StatusBadRequest, "missing pluginName")
		return
	}
	if !sysutil.IsPathSegment(req.PluginName) {
		writeError(w, http.StatusBadRequest, "invalid pluginName")
		return
	}
	job := s.GetPluginIngestJob(req.PluginName)
	if job == nil {
		writeJSON(w, &PluginIngestStatusResult{PluginName: req.PluginName, Status: ""})
		return
	}
	writeJSON(w, &PluginIngestStatusResult{
		PluginName: job.PluginName,
		Status:     job.Status,
		Nodes:      job.Nodes,
		Edges:      job.Edges,
		Error:      job.Error,
		Duration:   job.Duration,
	})
}

// ParseLineRange parses a "start-end" line range string.
// Returns (0, 0, nil) if s is empty (no range requested).
func ParseLineRange(s string) (start, end int, err error) {
	if s == "" {
		return 0, 0, nil
	}
	if _, err := fmt.Sscanf(s, "%d-%d", &start, &end); err != nil {
		return 0, 0, fmt.Errorf("invalid line range %q (expected format: start-end)", s)
	}
	if start < 1 || end < start {
		return 0, 0, fmt.Errorf("invalid line range %d-%d", start, end)
	}
	return start, end, nil
}
