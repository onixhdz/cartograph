package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realxen/cartograph/internal/query"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/storage/bbolt"
)

// TestE2E_IncrementalEmbedding exercises the full incremental embedding
// lifecycle: analyze→embed→re-embed(skip)→modify→re-embed(partial)→query.
// Requires a local GGUF model; skipped in short mode.
func TestE2E_IncrementalEmbedding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E embedding test in short mode")
	}

	home, _ := os.UserHomeDir()
	modelPath := filepath.Join(home, ".cache/cartograph/models/CompendiumLabs/bge-small-en-v1.5-gguf/bge-small-en-v1.5-q8_0.gguf")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skip("skipping: bge-small model not found at", modelPath)
	}

	// ── Setup: fixture repo ──────────────────────────────────────

	fixtureDir := createFixtureRepo(t)

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	dataDir := filepath.Join(tmpDir, "cartograph")
	_ = os.MkdirAll(dataDir, 0o750)

	socketPath := filepath.Join(dataDir, "test-embed.sock")
	lf := service.NewLockfile(dataDir)
	t.Cleanup(func() { _ = lf.Release() })

	if err := lf.Acquire(socketPath, "unix"); err != nil {
		t.Fatalf("acquire lockfile: %v", err)
	}

	srv, err := service.NewServer(socketPath, lf, dataDir)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.SetBackendFactory(func(repo string) service.ToolBackend {
		g, idx, ok := srv.GetRepoResources(repo)
		if !ok {
			return nil
		}
		return &query.Backend{Graph: g, Index: idx}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	client := service.NewAutoClient(srv.Addr)
	cli := &CLI{Client: client}
	repoName := filepath.Base(fixtureDir)

	// ── Step 1: Analyze the fixture repo ──────────────────────────

	t.Log("Step 1: analyzing fixture repo...")

	analyzeCmd := &AnalyzeCmd{
		Targets: []string{fixtureDir},
		Embed:   "off",
	}
	out := captureStdout(t, func() {
		if err := analyzeCmd.Run(cli); err != nil {
			t.Fatalf("analyze: %v", err)
		}
	})
	t.Log(out)
	if !strings.Contains(out, "Graph:") {
		t.Fatalf("expected 'Graph:' in analyze output, got:\n%s", out)
	}

	// ── Step 2: First embed — full run ────────────────────────────

	t.Log("Step 2: first embedding (full)...")

	embedResult := embedAndWait(t, client, repoName)
	if embedResult.Status != statusComplete {
		t.Fatalf("embed failed: %s", embedResult.Error)
	}
	firstEmbedCount := embedResult.Progress
	t.Logf("  Embedded %d nodes (%s / %dd)", firstEmbedCount, embedResult.Model, embedResult.Dims)

	if firstEmbedCount == 0 {
		t.Fatal("expected >0 embedded nodes")
	}

	embStorePath := findEmbeddingStorePath(t, dataDir, repoName)
	count1 := countEmbeddings(t, embStorePath)
	if count1 != firstEmbedCount {
		t.Errorf("embedding store count (%d) != reported count (%d)", count1, firstEmbedCount)
	}

	// ── Step 3: Query with embeddings — baseline quality ──────────

	t.Log("Step 3: querying with embeddings (baseline)...")

	qr1, err := client.Query(service.QueryRequest{
		Repo:  repoName,
		Text:  "authentication",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	baseline := len(qr1.Definitions) + len(qr1.Processes)
	t.Logf("  Baseline results for 'authentication': %d definitions, %d processes",
		len(qr1.Definitions), len(qr1.Processes))

	// The fixture has an AuthService class — it should appear.
	foundAuth := false
	for _, d := range qr1.Definitions {
		if strings.Contains(d.Name, "Auth") || strings.Contains(d.Name, "auth") {
			foundAuth = true
			break
		}
	}
	if !foundAuth {
		t.Error("expected 'Auth*' in search results for 'authentication'")
	}

	// ── Step 4: Re-embed — should skip (no changes) ───────────────

	t.Log("Step 4: re-embedding (should skip)...")

	embedResult2 := embedAndWait(t, client, repoName)
	if embedResult2.Status != statusComplete {
		t.Fatalf("re-embed failed: %s", embedResult2.Error)
	}

	count2 := countEmbeddings(t, embStorePath)
	if count2 != count1 {
		t.Errorf("embedding count changed after skip: %d → %d", count1, count2)
	}
	t.Logf("  Re-embed: %d vectors (unchanged, as expected)", count2)

	// ── Step 5: Modify fixture, re-analyze, re-embed (partial) ────

	t.Log("Step 5: modifying fixture and re-embedding...")

	addFileToFixture(t, fixtureDir, "payment.go", `package fixture

// PaymentProcessor handles credit card and billing operations.
type PaymentProcessor struct {
	GatewayURL string
	APIKey     string
}

// ProcessPayment charges the given amount to the customer's card.
func (p *PaymentProcessor) ProcessPayment(customerID string, amount float64) error {
	// Validate input
	if amount <= 0 {
		return nil
	}
	// Call payment gateway
	return nil
}

// RefundPayment issues a refund for a previous transaction.
func (p *PaymentProcessor) RefundPayment(transactionID string) error {
	return nil
}
`)

	analyzeCmd2 := &AnalyzeCmd{
		Targets: []string{fixtureDir},
		Force:   true, // force re-analysis to pick up the new file
		Embed:   "off",
	}
	out2 := captureStdout(t, func() {
		if err := analyzeCmd2.Run(cli); err != nil {
			t.Fatalf("re-analyze: %v", err)
		}
	})
	t.Log(out2)

	_ = client.Reload(service.ReloadRequest{Repo: repoName})

	embedResult3 := embedAndWait(t, client, repoName)
	if embedResult3.Status != statusComplete {
		t.Fatalf("partial re-embed failed: %s", embedResult3.Error)
	}

	count3 := countEmbeddings(t, embStorePath)
	t.Logf("  After adding payment.go: %d vectors (was %d)", count3, count2)
	if count3 <= count2 {
		t.Errorf("expected more embeddings after adding code: %d <= %d", count3, count2)
	}

	// ── Step 6: Query — verify search quality preserved ───────────

	t.Log("Step 6: verifying search quality after incremental embed...")

	qr2, err := client.Query(service.QueryRequest{
		Repo:  repoName,
		Text:  "authentication",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query after re-embed: %v", err)
	}
	postResults := len(qr2.Definitions) + len(qr2.Processes)
	if postResults < baseline {
		t.Errorf("search quality regressed: %d results (was %d)", postResults, baseline)
	}

	qr3, err := client.Query(service.QueryRequest{
		Repo:  repoName,
		Text:  "payment processing",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query payment: %v", err)
	}
	foundPayment := false
	for _, d := range qr3.Definitions {
		if strings.Contains(d.Name, "Payment") || strings.Contains(d.Name, "payment") {
			foundPayment = true
			break
		}
	}
	if !foundPayment {
		t.Error("expected 'Payment*' in search results for 'payment processing' after incremental embed")
	}
	t.Logf("  Auth results: %d, Payment results: %d definitions",
		len(qr2.Definitions), len(qr3.Definitions))

	t.Log("✓ E2E incremental embedding test passed")
}

// createFixtureRepo creates a small Go project with git history in a temp dir.
func createFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fixture-repo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	files := map[string]string{
		"go.mod": "module fixture\n\ngo 1.22\n",
		"main.go": `package fixture

import "fmt"

// Application is the main entry point that wires services together.
type Application struct {
	Auth   *AuthService
	Router *Router
}

// Run starts the application and listens for incoming requests.
func (a *Application) Run() error {
	fmt.Println("starting application")
	return a.Router.ListenAndServe()
}
`,
		"auth.go": `package fixture

import "errors"

// AuthService handles user authentication and session management.
type AuthService struct {
	SecretKey string
	TokenTTL  int
}

// Authenticate validates user credentials and returns a JWT token.
func (s *AuthService) Authenticate(username, password string) (string, error) {
	if username == "" || password == "" {
		return "", errors.New("missing credentials")
	}
	return "token-" + username, nil
}

// ValidateToken checks whether a JWT token is valid and not expired.
func (s *AuthService) ValidateToken(token string) (bool, error) {
	if token == "" {
		return false, errors.New("empty token")
	}
	return true, nil
}

// RevokeToken invalidates a token, ending the user's session.
func (s *AuthService) RevokeToken(token string) error {
	return nil
}
`,
		"router.go": `package fixture

import "net/http"

// Router maps HTTP routes to handler functions.
type Router struct {
	Mux  *http.ServeMux
	Auth *AuthService
}

// ListenAndServe starts the HTTP server on port 8080.
func (r *Router) ListenAndServe() error {
	r.Mux.HandleFunc("/login", r.handleLogin)
	r.Mux.HandleFunc("/api/", r.handleAPI)
	return http.ListenAndServe(":8080", r.Mux)
}

// handleLogin processes login requests, delegating to AuthService.
func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	token, err := r.Auth.Authenticate(req.FormValue("user"), req.FormValue("pass"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	w.Write([]byte(token))
}

// handleAPI serves protected API endpoints after token validation.
func (r *Router) handleAPI(w http.ResponseWriter, req *http.Request) {
	token := req.Header.Get("Authorization")
	valid, _ := r.Auth.ValidateToken(token)
	if !valid {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}
	w.Write([]byte("ok"))
}
`,
		"helpers.go": `package fixture

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashPassword produces a SHA-256 hash of the input password.
func HashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:])
}

// SanitizeInput removes leading/trailing whitespace and lowercases the input.
func SanitizeInput(input string) string {
	return strings.TrimSpace(strings.ToLower(input))
}
`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Initialize git repo so the pipeline can read HEAD.
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), args[0], args[1:]...) //nolint:gosec // args are test-controlled literals
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	return dir
}

// addFileToFixture adds a file and commits it.
func addFileToFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), args[0], args[1:]...) //nolint:gosec // args are test-controlled literals
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}
	run("git", "add", name)
	run("git", "commit", "-m", "add "+name)
}

// embedAndWait triggers embedding and polls until complete or failed.
func embedAndWait(t *testing.T, client *service.Client, repo string) *service.EmbedStatusResult {
	t.Helper()

	_, err := client.Embed(service.EmbedRequest{
		Repo:     repo,
		Provider: "llamacpp",
	})
	if err != nil {
		t.Fatalf("embed request: %v", err)
	}

	deadline := time.Now().Add(5 * time.Minute)
	for {
		if time.Now().After(deadline) {
			t.Fatal("embedding timed out after 5 minutes")
		}
		time.Sleep(500 * time.Millisecond)

		st, err := client.EmbedStatus(service.EmbedStatusRequest{Repo: repo})
		if err != nil {
			t.Fatalf("embed status: %v", err)
		}
		switch st.Status {
		case statusComplete:
			return st
		case statusFailed:
			return st
		}
	}
}

// findEmbeddingStorePath locates the embeddings.db for a repo.
func findEmbeddingStorePath(t *testing.T, dataDir, repoName string) string {
	t.Helper()
	reg, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	entry, ok := reg.Get(repoName)
	if !ok {
		t.Fatalf("repo %q not in registry", repoName)
	}
	return filepath.Join(dataDir, entry.Name, entry.Hash, "embeddings.db")
}

// countEmbeddings opens the embedding store and returns the vector count.
func countEmbeddings(t *testing.T, path string) int {
	t.Helper()
	s, err := bbolt.NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("open embedding store: %v", err)
	}
	defer s.Close()

	n, err := s.Count()
	if err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	return n
}
