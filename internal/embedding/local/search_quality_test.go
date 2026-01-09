package local

import (
	"context"
	"math"
	"sort"
	"strings"
	"testing"
)

// searchResult pairs a document index with its cosine similarity to the query.
type searchResult struct {
	idx  int
	sim  float64
	text string
}

// vectorSearch mimics Cartograph's query/backend.go VectorSearch:
// embed query, compute cosine similarity against all docs, return ranked results above threshold.
func vectorSearch(p *Provider, ctx context.Context, query string, docs []string, threshold float64) ([]searchResult, error) {
	qvecs, err := p.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	qvec := qvecs[0]

	dvecs, err := p.Embed(ctx, docs)
	if err != nil {
		return nil, err
	}

	var results []searchResult
	for i, dvec := range dvecs {
		sim := cosine(qvec, dvec)
		if sim >= threshold {
			results = append(results, searchResult{idx: i, sim: sim, text: docs[i]})
		}
	}

	sort.Slice(results, func(a, b int) bool {
		return results[a].sim > results[b].sim
	})

	return results, nil
}

// embeddingText formats a code node the same way textgen.go does.
// role is the heuristic process label (e.g., "Initialization"); pass "" to omit.
func embeddingText(label, name, file, signature, role, doc, code string, calls, calledBy []string) string {
	var b strings.Builder
	b.WriteString(label + ": " + name + "\n")
	b.WriteString("File: " + file + "\n")
	if signature != "" {
		b.WriteString("Signature: " + signature + "\n")
	}
	if len(calls) > 0 {
		b.WriteString("Calls: " + strings.Join(calls, ", ") + "\n")
	}
	if len(calledBy) > 0 {
		b.WriteString("Called by: " + strings.Join(calledBy, ", ") + "\n")
	}
	if role != "" {
		b.WriteString("Role: " + role + "\n")
	}
	if doc != "" {
		b.WriteString("Doc: " + doc + "\n")
	}
	if code != "" {
		b.WriteString("\n" + code)
	}
	return b.String()
}

// codebaseNodes simulates a realistic codebase with nodes formatted like textgen.go.
func codebaseNodes() []string {
	return []string{
		// [0] Server startup
		embeddingText("Function", "NewServer", "internal/server/server.go",
			"func NewServer(cfg Config) (*Server, error)",
			"Initialization",
			"NewServer creates and initializes the HTTP server with all routes, middleware, and background workers.",
			`func NewServer(cfg Config) (*Server, error) {
	s := &Server{cfg: cfg}
	s.router = mux.NewRouter()
	s.registerRoutes()
	s.startBackgroundWorkers()
	return s, nil
}`,
			[]string{"registerRoutes", "startBackgroundWorkers", "NewRouter"},
			[]string{"main", "TestServer"}),

		// [1] Database connection
		embeddingText("Function", "OpenDatabase", "internal/storage/db.go",
			"func OpenDatabase(dsn string) (*sql.DB, error)",
			"Initialization",
			"OpenDatabase establishes a connection to the PostgreSQL database with connection pooling and retry logic.",
			`func OpenDatabase(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil { return nil, err }
	db.SetMaxOpenConns(25)
	return db, nil
}`,
			[]string{"sql.Open", "SetMaxOpenConns"},
			[]string{"NewServer", "main"}),

		// [2] Authentication middleware
		embeddingText("Function", "AuthMiddleware", "internal/auth/middleware.go",
			"func AuthMiddleware(next http.Handler) http.Handler",
			"Authentication",
			"AuthMiddleware validates JWT tokens from the Authorization header and injects the authenticated user into the request context.",
			`func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		claims, err := validateJWT(token)
		if err != nil { http.Error(w, "unauthorized", 401); return }
		ctx := context.WithValue(r.Context(), userKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}`,
			[]string{"validateJWT", "context.WithValue"},
			[]string{"registerRoutes"}),

		// [3] User registration handler
		embeddingText("Function", "HandleRegister", "internal/auth/register.go",
			"func HandleRegister(w http.ResponseWriter, r *http.Request)",
			"Request handler",
			"HandleRegister processes new user registration requests, validates input, hashes the password, and stores the user in the database.",
			`func HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	json.NewDecoder(r.Body).Decode(&req)
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), 14)
	user := User{Email: req.Email, PasswordHash: string(hash)}
	db.Create(&user)
	json.NewEncoder(w).Encode(user)
}`,
			[]string{"bcrypt.GenerateFromPassword", "db.Create"},
			[]string{"registerRoutes"}),

		// [4] Sorting utility (no role — not part of a process)
		embeddingText("Function", "QuickSort", "pkg/algorithms/sort.go",
			"func QuickSort(arr []int) []int",
			"",
			"QuickSort implements the quicksort algorithm with median-of-three pivot selection for improved performance on partially sorted data.",
			`func QuickSort(arr []int) []int {
	if len(arr) <= 1 { return arr }
	pivot := medianOfThree(arr)
	left, right := partition(arr, pivot)
	return append(append(QuickSort(left), pivot), QuickSort(right)...)
}`,
			[]string{"medianOfThree", "partition"},
			nil),

		// [5] Logging setup
		embeddingText("Function", "InitLogger", "internal/logging/logger.go",
			"func InitLogger(level string, output io.Writer) *slog.Logger",
			"Initialization",
			"InitLogger configures the structured logger with the specified level and output destination.",
			`func InitLogger(level string, output io.Writer) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug": lvl = slog.LevelDebug
	case "warn": lvl = slog.LevelWarn
	case "error": lvl = slog.LevelError
	default: lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: lvl}))
}`,
			nil,
			[]string{"main", "NewServer"}),

		// [6] Cache layer
		embeddingText("Function", "NewRedisCache", "internal/cache/redis.go",
			"func NewRedisCache(addr string) (*RedisCache, error)",
			"Initialization",
			"NewRedisCache creates a Redis-backed cache client with connection pooling for high-throughput key-value operations.",
			`func NewRedisCache(addr string) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 50})
	if err := client.Ping(ctx).Err(); err != nil { return nil, err }
	return &RedisCache{client: client}, nil
}`,
			[]string{"redis.NewClient", "Ping"},
			[]string{"NewServer"}),

		// [7] Config parsing
		embeddingText("Function", "LoadConfig", "internal/config/config.go",
			"func LoadConfig(path string) (*Config, error)",
			"Configuration",
			"LoadConfig reads and parses a YAML configuration file, applying environment variable overrides and default values.",
			`func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil { return nil, err }
	var cfg Config
	yaml.Unmarshal(data, &cfg)
	applyEnvOverrides(&cfg)
	return &cfg, nil
}`,
			[]string{"os.ReadFile", "yaml.Unmarshal", "applyEnvOverrides"},
			[]string{"main"}),

		// [8] Graceful shutdown
		embeddingText("Function", "Shutdown", "internal/server/server.go",
			"func (s *Server) Shutdown(ctx context.Context) error",
			"Shutdown",
			"Shutdown gracefully stops the server, draining active connections and stopping background workers within the context deadline.",
			`func (s *Server) Shutdown(ctx context.Context) error {
	s.stopWorkers()
	return s.httpServer.Shutdown(ctx)
}`,
			[]string{"stopWorkers", "httpServer.Shutdown"},
			[]string{"main"}),

		// [9] Health check endpoint
		embeddingText("Function", "HandleHealth", "internal/server/health.go",
			"func HandleHealth(w http.ResponseWriter, r *http.Request)",
			"Observability",
			"HandleHealth returns the server health status including database connectivity and cache availability.",
			`func HandleHealth(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus{Status: "ok"}
	if err := db.Ping(); err != nil { status.DB = "down" }
	if err := cache.Ping(); err != nil { status.Cache = "down" }
	json.NewEncoder(w).Encode(status)
}`,
			[]string{"db.Ping", "cache.Ping"},
			[]string{"registerRoutes"}),
	}
}

// queryTestCase defines a search quality test case.
type queryTestCase struct {
	name     string
	query    string
	wantTop3 []int // indices into codebaseNodes that should appear in top 3
	wantNot  []int // indices that should NOT appear in top 3
}

// intentQueryCases tests the gap identified in query-ux-eval.md:
// intent queries should match semantic meaning, not just keywords.
func intentQueryCases() []queryTestCase {
	return []queryTestCase{
		{
			name:     "startup_intent",
			query:    "how does the server start",
			wantTop3: []int{0}, // NewServer, not literal "start" matches
			wantNot:  []int{4}, // QuickSort is irrelevant
		},
		{
			name:     "auth_flow",
			query:    "how is authentication handled",
			wantTop3: []int{2, 3}, // AuthMiddleware, HandleRegister
			wantNot:  []int{4, 5}, // QuickSort, InitLogger
		},
		{
			name:     "database_connection",
			query:    "how does the app connect to the database",
			wantTop3: []int{1}, // OpenDatabase
			wantNot:  []int{4}, // QuickSort
		},
		{
			name:     "shutdown_graceful",
			query:    "graceful shutdown process",
			wantTop3: []int{8}, // Shutdown
			wantNot:  []int{4},
		},
		{
			name:     "caching_layer",
			query:    "how does caching work",
			wantTop3: []int{6}, // NewRedisCache
			wantNot:  []int{4},
		},
		{
			name:     "configuration_loading",
			query:    "where is the configuration loaded from",
			wantTop3: []int{7}, // LoadConfig
			wantNot:  []int{4},
		},
		{
			name:     "user_signup",
			query:    "user registration and signup flow",
			wantTop3: []int{3}, // HandleRegister
			wantNot:  []int{4, 5},
		},
		{
			name:     "health_monitoring",
			query:    "health check and monitoring endpoint",
			wantTop3: []int{9}, // HandleHealth
			wantNot:  []int{4},
		},
		// Role-based intent queries: test that heuristic labels
		// bridge natural language to code symbols.
		{
			name:     "role_initialization",
			query:    "initialization and startup code",
			wantTop3: []int{0}, // NewServer (Role: Initialization)
			wantNot:  []int{4}, // QuickSort has no role
		},
		{
			name:     "role_shutdown",
			query:    "shutdown and cleanup",
			wantTop3: []int{8}, // Shutdown (Role: Shutdown)
			wantNot:  []int{4},
		},
		{
			name:     "role_auth",
			query:    "authentication and authorization middleware",
			wantTop3: []int{2}, // AuthMiddleware (Role: Authentication)
			wantNot:  []int{4},
		},
	}
}

// TestSearchQuality runs semantic search quality tests against all available models.
// This mirrors Cartograph's actual query flow: embed code nodes, embed query, cosine rank.
func TestSearchQuality(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	docs := codebaseNodes()
	cases := intentQueryCases()
	const threshold = 0.3 // matches backend.go line 390

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			passed := 0
			total := 0

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					results, err := vectorSearch(p, context.Background(), tc.query, docs, threshold)
					if err != nil {
						t.Fatalf("search: %v", err)
					}

					// Log top 5 results
					t.Logf("Query: %q", tc.query)
					for i, r := range results {
						if i >= 5 {
							break
						}
						// Extract just the function name from the doc text
						name := r.text[:min(60, len(r.text))]
						if nl := strings.IndexByte(name, '\n'); nl > 0 {
							name = name[:nl]
						}
						t.Logf("  #%d [%.4f] %s", i+1, r.sim, name)
					}

					// Check wantTop3
					top3 := make(map[int]bool)
					for i, r := range results {
						if i >= 3 {
							break
						}
						top3[r.idx] = true
					}

					for _, wantIdx := range tc.wantTop3 {
						total++
						if top3[wantIdx] {
							passed++
						} else {
							rank := -1
							for i, r := range results {
								if r.idx == wantIdx {
									rank = i + 1
									break
								}
							}
							t.Errorf("expected doc[%d] in top 3, found at rank %d", wantIdx, rank)
						}
					}

					for _, notIdx := range tc.wantNot {
						if top3[notIdx] {
							t.Errorf("doc[%d] should NOT be in top 3", notIdx)
						}
					}
				})
			}

			// Summary
			pct := 0.0
			if total > 0 {
				pct = float64(passed) / float64(total) * 100
			}
			t.Logf("=== QUALITY SCORE: %d/%d (%.0f%%) queries matched expected top-3 ===", passed, total, pct)
		})
	}
}

// TestThresholdFiltering verifies that the 0.3 cosine threshold effectively filters
// irrelevant results without dropping relevant ones.
func TestThresholdFiltering(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	docs := codebaseNodes()

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			// Completely unrelated query — should return few/no results above threshold
			nonsense := "recipe for chocolate cake with vanilla frosting"
			results03, _ := vectorSearch(p, context.Background(), nonsense, docs, 0.3)
			results04, _ := vectorSearch(p, context.Background(), nonsense, docs, 0.4)
			t.Logf("Nonsense query at 0.3 threshold: %d results", len(results03))
			t.Logf("Nonsense query at 0.4 threshold: %d results", len(results04))

			if len(results03) > 0 {
				t.Logf("  Top nonsense match: [%.4f] %s", results03[0].sim, results03[0].text[:min(60, len(results03[0].text))])
			}

			// Relevant query — should retain results at both thresholds
			relevant := "how does authentication work"
			rr03, _ := vectorSearch(p, context.Background(), relevant, docs, 0.3)
			rr04, _ := vectorSearch(p, context.Background(), relevant, docs, 0.4)
			t.Logf("Auth query at 0.3 threshold: %d results", len(rr03))
			t.Logf("Auth query at 0.4 threshold: %d results", len(rr04))

			if len(rr03) == 0 {
				t.Error("auth query returned 0 results at 0.3 threshold — model too weak")
			}
		})
	}
}

// TestDimensionalityImpact compares how well models separate related vs unrelated pairs.
// Higher-dimensional models should produce sharper separation.
func TestDimensionalityImpact(t *testing.T) {
	models := availableModels(t)
	if len(models) < 2 {
		t.Skip("need both encoder and decoder models cached for comparison")
	}

	type separation struct {
		modelName string
		dims      int
		avgGap    float64 // average (related_sim - unrelated_sim)
	}

	// Related/unrelated pairs designed to test code understanding
	queries := []struct {
		query     string
		related   string
		unrelated string
	}{
		{
			"how to authenticate users",
			embeddingText("Function", "AuthMiddleware", "auth/middleware.go",
				"func AuthMiddleware(next http.Handler) http.Handler",
				"Authentication",
				"Validates JWT tokens and injects user context", "", nil, nil),
			embeddingText("Function", "QuickSort", "algorithms/sort.go",
				"func QuickSort(arr []int) []int",
				"",
				"Sorts an array using quicksort algorithm", "", nil, nil),
		},
		{
			"database connection pooling",
			embeddingText("Function", "OpenDatabase", "storage/db.go",
				"func OpenDatabase(dsn string) (*sql.DB, error)",
				"Initialization",
				"Opens PostgreSQL with connection pool", "", nil, nil),
			embeddingText("Function", "InitLogger", "logging/logger.go",
				"func InitLogger(level string) *slog.Logger",
				"Initialization",
				"Sets up structured JSON logging", "", nil, nil),
		},
		{
			"graceful server shutdown",
			embeddingText("Function", "Shutdown", "server/server.go",
				"func (s *Server) Shutdown(ctx context.Context) error",
				"Shutdown",
				"Drains connections and stops workers", "", nil, nil),
			embeddingText("Function", "HandleRegister", "auth/register.go",
				"func HandleRegister(w http.ResponseWriter, r *http.Request)",
				"Request handler",
				"Processes user registration", "", nil, nil),
		},
	}

	var seps []separation

	for _, m := range models {
		p := loadProvider(t, m.path)

		var totalGap float64
		for _, q := range queries {
			vecs, err := p.Embed(context.Background(), []string{q.query, q.related, q.unrelated})
			if err != nil {
				t.Fatalf("embed: %v", err)
			}
			simRelated := cosine(vecs[0], vecs[1])
			simUnrelated := cosine(vecs[0], vecs[2])
			gap := simRelated - simUnrelated
			totalGap += gap
			t.Logf("[%s] %q: related=%.4f unrelated=%.4f gap=%.4f",
				m.name, q.query[:min(30, len(q.query))], simRelated, simUnrelated, gap)
		}

		avgGap := totalGap / float64(len(queries))
		seps = append(seps, separation{
			modelName: m.name,
			dims:      p.Dimensions(),
			avgGap:    avgGap,
		})

		p.Close()
	}

	// Report comparison
	t.Logf("\n=== DIMENSIONALITY IMPACT ===")
	for _, s := range seps {
		t.Logf("  %-30s  dims=%4d  avg_gap=%.4f", s.modelName, s.dims, s.avgGap)
	}

	// Both models should have positive gaps (related > unrelated)
	for _, s := range seps {
		if s.avgGap <= 0 {
			t.Errorf("%s: average gap is %.4f (should be positive)", s.modelName, s.avgGap)
		}
	}

	// If we have both models, higher-dim should ideally have >= gap
	if len(seps) >= 2 {
		lowDim := seps[0]
		highDim := seps[1]
		if highDim.dims < lowDim.dims {
			lowDim, highDim = highDim, lowDim
		}
		improvement := (highDim.avgGap - lowDim.avgGap) / math.Abs(lowDim.avgGap) * 100
		t.Logf("  Higher-dim improvement: %.1f%%", improvement)
	}
}
