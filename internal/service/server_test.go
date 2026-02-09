package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
)

// mockStore implements storage.GraphStore for tests.
type mockStore struct {
	g   *lpg.Graph
	err error
}

func (m *mockStore) SaveGraph(_ *lpg.Graph) error { return m.err }
func (m *mockStore) LoadGraph() (*lpg.Graph, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.g, nil
}
func (m *mockStore) Close() error { return nil }

func TestServerLoadGetDropGraph(t *testing.T) {
	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
	}

	g := lpg.NewGraph()
	store := &mockStore{g: g}

	if err := s.LoadGraph("myrepo", store); err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	got, ok := s.GetGraph("myrepo")
	if !ok {
		t.Fatal("expected graph to be loaded")
	}
	_ = got

	s.DropGraph("myrepo")
	_, ok = s.GetGraph("myrepo")
	if ok {
		t.Error("expected graph to be dropped")
	}
}

func TestServerRepos(t *testing.T) {
	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
	}
	s.graph["a"] = lpg.NewGraph()
	s.graph["b"] = lpg.NewGraph()

	repos := s.Repos()
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
}

func TestServerSetupRoutesRegistered(t *testing.T) {
	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
	}
	handler := s.setupRoutes()

	routes := []struct {
		method string
		path   string
		status int // expected status (not 404)
	}{
		{"POST", RouteQuery, http.StatusBadRequest},   // missing body
		{"POST", RouteContext, http.StatusBadRequest}, // missing body
		{"POST", RouteCypher, http.StatusBadRequest},  // missing body
		{"POST", RouteImpact, http.StatusBadRequest},  // missing body
		{"POST", RouteReload, http.StatusBadRequest},  // missing body
		{"GET", RouteStatus, http.StatusOK},
		{"POST", RouteShutdown, http.StatusOK},
	}

	for _, rt := range routes {
		req := httptest.NewRequestWithContext(context.Background(), rt.method, rt.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Errorf("route %s %s returned 404, expected registered", rt.method, rt.path)
		}
	}
}

func TestServerStartStop(t *testing.T) {
	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		idleTimeout: DefaultIdleTimeout,
		done:        make(chan struct{}),
	}
	mux := s.setupRoutes()

	ts := httptest.NewServer(mux)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+RouteStatus, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestServerHTTPTimeoutsSet(t *testing.T) {
	tmpDir := t.TempDir()
	lf := NewLockfile(tmpDir)

	srv, err := NewServer("/dev/null/bogus.sock", lf, tmpDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.listener.Close()

	if srv.httpServer.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout should be non-zero")
	}
	if srv.httpServer.ReadTimeout == 0 {
		t.Error("ReadTimeout should be non-zero")
	}
	if srv.httpServer.WriteTimeout == 0 {
		t.Error("WriteTimeout should be non-zero")
	}
	if srv.httpServer.IdleTimeout == 0 {
		t.Error("IdleTimeout should be non-zero")
	}
}

func TestServerReadiness_NotReadyBeforeLoad(t *testing.T) {
	s := &Server{}
	if s.ready.Load() {
		t.Error("expected ready=false before any graph is loaded")
	}
}

func TestServerReadiness_ReadyAfterLoadGraph(t *testing.T) {
	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		idleTimeout: DefaultIdleTimeout,
	}

	store := &mockStore{g: lpg.NewGraph()}
	if err := s.LoadGraph("repo", store); err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	if !s.ready.Load() {
		t.Error("expected ready=true after LoadGraph")
	}
}

func TestServerReadiness_ReadyAfterLoadGraphDirect(t *testing.T) {
	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		idleTimeout: DefaultIdleTimeout,
	}

	s.LoadGraphDirect("repo", lpg.NewGraph(), nil)

	if !s.ready.Load() {
		t.Error("expected ready=true after LoadGraphDirect")
	}
}

func TestServerReadiness_StatusEndpointReflectsReady(t *testing.T) {
	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		idleTimeout: DefaultIdleTimeout,
	}

	req := httptest.NewRequestWithContext(context.Background(), "GET", RouteStatus, nil)
	rec := httptest.NewRecorder()
	s.handleStatus(rec, req)

	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resultData, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var sr StatusResult
	if err := json.Unmarshal(resultData, &sr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if sr.Ready {
		t.Error("expected ready=false before any graph is loaded")
	}
	if !sr.Running {
		t.Error("expected running=true")
	}

	s.LoadGraphDirect("repo", lpg.NewGraph(), nil)

	rec2 := httptest.NewRecorder()
	s.handleStatus(rec2, httptest.NewRequestWithContext(context.Background(), "GET", RouteStatus, nil))

	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resultData2, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(resultData2, &sr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !sr.Ready {
		t.Error("expected ready=true after LoadGraphDirect")
	}
}

func TestServerResolveRepoName_InMemoryFastPath(t *testing.T) {
	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
	}
	s.graph["myorg/myrepo"] = lpg.NewGraph()

	got, err := s.ResolveRepoName("myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "myorg/myrepo" {
		t.Errorf("got %q, want %q", got, "myorg/myrepo")
	}
}

func TestServerResolveRepoName_ShortNameViaRegistry(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "hashicorp/nomad", Hash: "h1"})

	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
		dataDir:   dir,
	}

	got, err := s.ResolveRepoName("nomad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hashicorp/nomad" {
		t.Errorf("got %q, want %q", got, "hashicorp/nomad")
	}
}

func TestServerResolveRepoName_AmbiguousShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "acme/sdk", Hash: "h1"})
	_ = reg.Add(storage.RegistryEntry{Name: "corp/sdk", Hash: "h2"})

	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
		dataDir:   dir,
	}

	_, err := s.ResolveRepoName("sdk")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous in error, got: %v", err)
	}
}

func TestServerResolveRepoName_NoDataDir(t *testing.T) {
	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
	}

	got, err := s.ResolveRepoName("anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "anything" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestRecoverEmbedJobs_LocalAutoResumed(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "myorg/myrepo", Hash: "abc123"})
	_ = reg.UpdateEmbedding("myorg/myrepo", storage.EmbeddingInfo{
		Status:   "running",
		Provider: "llamacpp",
		Model:    "bge-small-en-v1.5",
		Nodes:    50,
		Total:    200,
	})

	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
		embedJobs: make(map[string]*embedJob),
		embedSem:  make(chan struct{}, 1),
		dataDir:   dir,
	}

	s.RecoverEmbedJobs()

	job := s.GetEmbedJob("myorg/myrepo")
	if job == nil {
		t.Fatal("expected embed job to be started for llamacpp provider")
	}
	if job.Provider != "llamacpp" {
		t.Errorf("expected provider=llamacpp, got %q", job.Provider)
	}

	// Wait for the goroutine to complete (it fails fast due to missing
	// graph.db) so temp dir cleanup doesn't race with the flock.
	for range 200 {
		j := s.GetEmbedJob("myorg/myrepo")
		if j != nil && (j.Status == "failed" || j.Status == "complete") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRecoverEmbedJobs_ExternalMarkedInterrupted(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "myorg/myrepo", Hash: "abc123"})
	_ = reg.UpdateEmbedding("myorg/myrepo", storage.EmbeddingInfo{
		Status:   "running",
		Provider: "openai_compat",
		Model:    "text-embedding-3-small",
		Nodes:    50,
		Total:    200,
	})

	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
		embedJobs: make(map[string]*embedJob),
		embedSem:  make(chan struct{}, 1),
		dataDir:   dir,
	}

	s.RecoverEmbedJobs()

	job := s.GetEmbedJob("myorg/myrepo")
	if job != nil {
		t.Error("expected no embed job for openai_compat provider")
	}

	reg2, _ := storage.NewRegistry(dir)
	entry, ok := reg2.Get("myorg/myrepo")
	if !ok {
		t.Fatal("expected registry entry")
	}
	if entry.Meta.EmbeddingStatus != "interrupted" {
		t.Errorf("expected status=interrupted, got %q", entry.Meta.EmbeddingStatus)
	}
	if entry.Meta.EmbeddingError == "" {
		t.Error("expected error message to be set")
	}
}

func TestRecoverEmbedJobs_CompleteNotTouched(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "myorg/myrepo", Hash: "abc123"})
	_ = reg.UpdateEmbedding("myorg/myrepo", storage.EmbeddingInfo{
		Status:   "complete",
		Provider: "llamacpp",
		Model:    "bge-small-en-v1.5",
		Nodes:    200,
		Total:    200,
	})

	s := &Server{
		graph:     make(map[string]*lpg.Graph),
		searchIdx: make(map[string]*search.Index),
		embedJobs: make(map[string]*embedJob),
		embedSem:  make(chan struct{}, 1),
		dataDir:   dir,
	}

	s.RecoverEmbedJobs()

	job := s.GetEmbedJob("myorg/myrepo")
	if job != nil {
		t.Error("expected no embed job for complete status")
	}
}
