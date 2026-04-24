package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/realxen/cartograph/internal/storage"
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
	if !ok || g == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	writeJSON(w, BuildTreeResult(req.Repo, g))
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use GET")
		return
	}

	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		writeError(w, ErrCodeInternal, fmt.Sprintf("open registry: %v", err))
		return
	}

	entries := registry.List()
	repos := make([]RepoListEntry, 0, len(entries))
	for _, entry := range entries {
		repos = append(repos, repoListEntryFromRegistry(entry))
	}
	writeJSON(w, &ListResult{Repos: repos})
}

func repoListEntryFromRegistry(entry storage.RegistryEntry) RepoListEntry {
	entryType := "local"
	if entry.URL != "" {
		entryType = "url"
		if entry.Meta.ClonedOnly {
			entryType = "cloned"
		} else if entry.Meta.SourcePath != "" {
			entryType = "url, cloned"
		}
	}

	builtWith := entry.Meta.BinaryVersion
	if builtWith == "" {
		builtWith = "-"
	}

	return RepoListEntry{
		Name:      entry.Name,
		Hash:      entry.Hash,
		Type:      entryType,
		IndexedAt: entry.IndexedAt.Format("2006-01-02T15:04:05Z07:00"),
		NodeCount: entry.NodeCount,
		EdgeCount: entry.EdgeCount,
		BuiltWith: builtWith,
		Embedding: embeddingStatusLabel(entry.Meta),
	}
}

func embeddingStatusLabel(meta storage.Meta) string {
	switch meta.EmbeddingStatus {
	case "":
		return "none"
	case storage.EmbeddingStatusComplete:
		if meta.EmbeddingDuration != "" {
			return fmt.Sprintf("complete (%d nodes, %s)", meta.EmbeddingTotal, meta.EmbeddingDuration)
		}
		return fmt.Sprintf("complete (%d nodes)", meta.EmbeddingTotal)
	case storage.EmbeddingStatusRunning:
		if meta.EmbeddingTotal > 0 {
			pct := meta.EmbeddingNodes * 100 / meta.EmbeddingTotal
			return fmt.Sprintf("running (%d/%d, %d%%)", meta.EmbeddingNodes, meta.EmbeddingTotal, pct)
		}
		return "running"
	case storage.EmbeddingStatusFailed:
		if meta.EmbeddingError != "" {
			return fmt.Sprintf("failed (%s)", meta.EmbeddingError)
		}
		return statusFailed
	default:
		return meta.EmbeddingStatus
	}
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

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer(r.Context())
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use GET")
		return
	}
	writeJSON(w, s.BuildStatus())
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
