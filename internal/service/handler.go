package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/realxen/cartograph/internal/storage"
)

// cypherWriteRE matches Cypher write keywords that are disallowed for
// security (the service provides read-only access to the graph).
var cypherWriteRE = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|DROP|ALTER|COPY|DETACH)\b`)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{Result: v}) //nolint:errcheck
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	httpStatus := code
	switch code {
	case ErrCodeRepoNotFound:
		httpStatus = http.StatusNotFound
	case ErrCodeQueryBlocked:
		httpStatus = http.StatusForbidden
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
	json.NewEncoder(w).Encode(Response{ //nolint:errcheck
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
		return fmt.Errorf("empty request body")
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
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend := s.getBackend(req.Repo)
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
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend := s.getBackend(req.Repo)
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
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	// Block write queries at the handler level (defense in depth).
	if cypherWriteRE.MatchString(req.Query) {
		writeError(w, ErrCodeQueryBlocked, "write queries are not allowed")
		return
	}

	backend := s.getBackend(req.Repo)
	if backend == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q not indexed", req.Repo))
		return
	}

	result, err := backend.Cypher(req)
	if err != nil {
		writeError(w, ErrCodeInternal, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend := s.getBackend(req.Repo)
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

func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer()
	if !requirePOST(w, r) {
		return
	}
	var req SourceRequest
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	cr := s.getContentResolver(req.Repo)
	if cr == nil {
		writeError(w, ErrCodeRepoNotFound, fmt.Sprintf("repository %q has no content resolver", req.Repo))
		return
	}

	lineStart, lineEnd, err := parseLineRange(req.Lines)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result := SourceResult{Files: make([]SourceFile, 0, len(req.Files))}
	for _, path := range req.Files {
		data, err := cr.ReadFile(path)
		if err != nil {
			result.Files = append(result.Files, SourceFile{
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

		result.Files = append(result.Files, SourceFile{
			Path:      path,
			Content:   content,
			LineCount: lineCount,
		})
	}
	writeJSON(w, &result)
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
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
	s.resetIdleTimer()
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use GET")
		return
	}

	s.mu.RLock()
	repos := make([]RepoStatus, 0, len(s.graph))
	for name, g := range s.graph {
		nodeCount := 0
		edgeCount := 0
		if g != nil {
			nodes := g.GetNodes()
			for nodes.Next() {
				nodeCount++
			}
			edges := g.GetEdges()
			for edges.Next() {
				edgeCount++
			}
		}
		repos = append(repos, RepoStatus{
			Name:      name,
			NodeCount: nodeCount,
			EdgeCount: edgeCount,
		})
	}
	s.mu.RUnlock()

	var uptime string
	if !s.startTime.IsZero() {
		uptime = time.Since(s.startTime).Round(time.Second).String()
	}

	writeJSON(w, &StatusResult{
		Running:     true,
		Ready:       s.ready.Load(),
		LoadedRepos: repos,
		Uptime:      uptime,
	})
}

func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	backend := s.getBackend(req.Repo)
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
	s.resetIdleTimer()
	writeJSON(w, map[string]string{"status": "shutting down"})

	if s.httpServer != nil {
		go func() {
			s.Stop() //nolint:errcheck
		}()
	}
}

func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
	if err != nil {
		writeError(w, ErrCodeRepoNotFound, err.Error())
		return
	}
	req.Repo = repo

	job := s.StartEmbedJob(req)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(Response{Result: &EmbedStatusResult{ //nolint:errcheck
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
	s.resetIdleTimer()
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

	repo, err := s.resolveRepoName(req.Repo)
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

// isCypherWriteQuery reports whether q contains a Cypher write keyword.
func isCypherWriteQuery(q string) bool {
	return cypherWriteRE.MatchString(strings.TrimSpace(q))
}

// parseLineRange parses a "start-end" line range string.
// Returns (0, 0, nil) if s is empty (no range requested).
func parseLineRange(s string) (start, end int, err error) {
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
