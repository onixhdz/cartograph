package extractors

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// skipUnlessVariance skips unless CARTOGRAPH_VARIANCE=1 is set.
// Run with: CARTOGRAPH_VARIANCE=1 go test -run TestTreeSitter -v ./internal/ingestion/extractors/
func skipUnlessVariance(t *testing.T) {
	t.Helper()
	if os.Getenv("CARTOGRAPH_VARIANCE") != "1" {
		t.Skip("diagnostic test — set CARTOGRAPH_VARIANCE=1 to run")
	}
}

// Tests isolate gotreesitter concurrency variance caused by global sync.Pool
// instances that recycle mutable objects across goroutines, silently
// corrupting parse trees under concurrent use.

// testGoSources is a collection of non-trivial Go source snippets that
// exercise different grammar productions. Each has a known expected match
// count (determined by serial parsing).
var testGoSources = map[string]string{
	"server.go": `package main

import (
	"fmt"
	"net/http"
)

type Server struct {
	Host string
	Port int
}

type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

func NewServer(host string, port int) *Server {
	return &Server{Host: host, Port: port}
}

func (s *Server) Start() error {
	fmt.Println("starting on", s.Host)
	return nil
}

func (s *Server) Stop() error {
	return nil
}

func main() {
	s := NewServer("localhost", 8080)
	s.Start()
}
`,
	"config.go": `package config

import (
	"os"
	"strconv"
)

type Config struct {
	Debug   bool
	Port    int
	Host    string
	Workers int
}

type Loader interface {
	Load() (*Config, error)
}

func DefaultConfig() *Config {
	return &Config{Host: "localhost", Port: 8080}
}

func FromEnv() (*Config, error) {
	c := DefaultConfig()
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		c.Port = p
	}
	c.Host = os.Getenv("HOST")
	return c, nil
}

func (c *Config) Validate() error {
	if c.Port <= 0 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	return nil
}
`,
	"middleware.go": `package middleware

import "net/http"

type Middleware func(http.Handler) http.Handler

type Chain struct {
	middlewares []Middleware
}

func NewChain(mw ...Middleware) *Chain {
	return &Chain{middlewares: mw}
}

func (c *Chain) Then(handler http.Handler) http.Handler {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		handler = c.middlewares[i](handler)
	}
	return handler
}

func (c *Chain) Append(mw ...Middleware) *Chain {
	newMW := make([]Middleware, 0, len(c.middlewares)+len(mw))
	newMW = append(newMW, c.middlewares...)
	newMW = append(newMW, mw...)
	return &Chain{middlewares: newMW}
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { recover() }()
		next.ServeHTTP(w, r)
	})
}
`,
	"cache.go": `package cache

import (
	"sync"
	"time"
)

type Entry struct {
	Value   interface{}
	Expires time.Time
}

type Cache struct {
	mu    sync.RWMutex
	items map[string]*Entry
	ttl   time.Duration
}

type Eviction interface {
	ShouldEvict(key string, entry *Entry) bool
}

func New(ttl time.Duration) *Cache {
	return &Cache{items: make(map[string]*Entry), ttl: ttl}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.Expires) {
		return nil, false
	}
	return e.Value, true
}

func (c *Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = &Entry{Value: value, Expires: time.Now().Add(c.ttl)}
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *Cache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.items {
		if now.After(e.Expires) {
			delete(c.items, k)
		}
	}
}
`,
	"router.go": `package router

import (
	"net/http"
	"strings"
)

type Route struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
}

type Router struct {
	routes []Route
	prefix string
}

type Middleware interface {
	Wrap(http.Handler) http.Handler
}

func New() *Router {
	return &Router{}
}

func (r *Router) Handle(method, pattern string, handler http.HandlerFunc) {
	r.routes = append(r.routes, Route{Method: method, Pattern: r.prefix + pattern, Handler: handler})
}

func (r *Router) GET(pattern string, handler http.HandlerFunc) {
	r.Handle("GET", pattern, handler)
}

func (r *Router) POST(pattern string, handler http.HandlerFunc) {
	r.Handle("POST", pattern, handler)
}

func (r *Router) PUT(pattern string, handler http.HandlerFunc) {
	r.Handle("PUT", pattern, handler)
}

func (r *Router) DELETE(pattern string, handler http.HandlerFunc) {
	r.Handle("DELETE", pattern, handler)
}

func (r *Router) Group(prefix string) *Router {
	return &Router{routes: r.routes, prefix: r.prefix + prefix}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for _, route := range r.routes {
		if route.Method == req.Method && strings.HasPrefix(req.URL.Path, route.Pattern) {
			route.Handler(w, req)
			return
		}
	}
	http.NotFound(w, req)
}
`,
	"pool.go": `package pool

import "sync"

type Task func() error

type WorkerPool struct {
	size int
	work chan Task
	wg   sync.WaitGroup
}

type Result struct {
	TaskID int
	Err    error
}

func NewWorkerPool(size int) *WorkerPool {
	return &WorkerPool{size: size, work: make(chan Task, size*2)}
}

func (p *WorkerPool) Start() {
	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for task := range p.work {
				task()
			}
		}()
	}
}

func (p *WorkerPool) Submit(task Task) {
	p.work <- task
}

func (p *WorkerPool) Stop() {
	close(p.work)
	p.wg.Wait()
}

func (p *WorkerPool) Resize(newSize int) {
	p.Stop()
	p.size = newSize
	p.work = make(chan Task, newSize*2)
	p.Start()
}
`,
}

// goQuery is the standard Go extraction query used by cartograph.
var goQuery = goQueries

// serialMatchCount parses each source with a fresh, single-threaded parser
// and returns the total match count. This is the ground truth baseline —
// no concurrency, no shared state, no pools.
func serialMatchCount(t *testing.T, sources map[string]string, queryStr string) int {
	t.Helper()
	lang := grammars.DetectLanguageByName("go").Language()
	q, err := ts.NewQuery(queryStr, lang)
	if err != nil {
		t.Fatalf("compile query: %v", err)
	}

	total := 0
	// Process in sorted key order for determinism.
	keys := sortedKeys(sources)
	for _, name := range keys {
		src := sources[name]
		parser := ts.NewParser(lang)
		tree, err := parser.Parse([]byte(src))
		if err != nil {
			t.Fatalf("serial parse %s: %v", name, err)
		}
		matches := q.Execute(tree)
		total += len(matches)
	}
	return total
}

// serialPerFileMatchCounts returns per-file match counts (sorted by key).
func serialPerFileMatchCounts(t *testing.T, sources map[string]string, queryStr string) map[string]int {
	t.Helper()
	lang := grammars.DetectLanguageByName("go").Language()
	q, err := ts.NewQuery(queryStr, lang)
	if err != nil {
		t.Fatalf("compile query: %v", err)
	}

	counts := make(map[string]int)
	for name, src := range sources {
		parser := ts.NewParser(lang)
		tree, err := parser.Parse([]byte(src))
		if err != nil {
			t.Fatalf("serial parse %s: %v", name, err)
		}
		matches := q.Execute(tree)
		counts[name] = len(matches)
	}
	return counts
}

// Test 1: Serial baseline — verify determinism with 1 goroutine.
func TestTreeSitter_Serial_Deterministic(t *testing.T) {
	skipUnlessVariance(t)
	baseline := serialMatchCount(t, testGoSources, goQuery)
	t.Logf("serial baseline: %d total matches across %d files", baseline, len(testGoSources))

	for i := range 5 {
		got := serialMatchCount(t, testGoSources, goQuery)
		if got != baseline {
			t.Errorf("serial run %d: got %d matches, want %d (variance in serial!)", i, got, baseline)
		}
	}
}

// Test 2: Concurrent NewParser — each goroutine gets its own Parser (no pool).
func TestTreeSitter_ConcurrentNewParser_Variance(t *testing.T) {
	skipUnlessVariance(t)
	baseline := serialMatchCount(t, testGoSources, goQuery)
	baselinePer := serialPerFileMatchCounts(t, testGoSources, goQuery)
	t.Logf("serial baseline: %d total matches", baseline)

	keys := sortedKeys(testGoSources)
	lang := grammars.DetectLanguageByName("go").Language()

	const runs = 10
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		perFile := make(map[string]int)
		totalMatches := 0
		errors := 0

		var wg sync.WaitGroup
		for _, name := range keys {
			wg.Add(1)
			go func(fname string, src []byte) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				parser := ts.NewParser(lang)
				tree, err := parser.Parse(src)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}

				q, err := ts.NewQuery(goQuery, lang)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}
				matches := q.Execute(tree)

				mu.Lock()
				perFile[fname] = len(matches)
				totalMatches += len(matches)
				mu.Unlock()
			}(name, []byte(testGoSources[name]))
		}
		wg.Wait()

		if totalMatches != baseline || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE — got %d matches (want %d), errors=%d", run, totalMatches, baseline, errors)
			// Show per-file diffs.
			for _, k := range keys {
				got, want := perFile[k], baselinePer[k]
				if got != want {
					t.Logf("  %s: got %d, want %d (diff %+d)", k, got, want, got-want)
				}
			}
		}
	}

	t.Logf("NewParser: %d/%d runs matched serial baseline", runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("CONFIRMED: %d/%d concurrent NewParser runs produced different results than serial", variantRuns, runs)
	}
}

// Test 3: Concurrent ParserPool — uses the library's recommended concurrent API.
func TestTreeSitter_ConcurrentParserPool_Variance(t *testing.T) {
	skipUnlessVariance(t)
	baseline := serialMatchCount(t, testGoSources, goQuery)
	baselinePer := serialPerFileMatchCounts(t, testGoSources, goQuery)
	t.Logf("serial baseline: %d total matches", baseline)

	keys := sortedKeys(testGoSources)
	lang := grammars.DetectLanguageByName("go").Language()
	pool := ts.NewParserPool(lang)

	const runs = 10
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		perFile := make(map[string]int)
		totalMatches := 0
		errors := 0

		var wg sync.WaitGroup
		for _, name := range keys {
			wg.Add(1)
			go func(fname string, src []byte) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				tree, err := pool.Parse(src)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}

				q, err := ts.NewQuery(goQuery, lang)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}
				matches := q.Execute(tree)

				mu.Lock()
				perFile[fname] = len(matches)
				totalMatches += len(matches)
				mu.Unlock()
			}(name, []byte(testGoSources[name]))
		}
		wg.Wait()

		if totalMatches != baseline || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE — got %d matches (want %d), errors=%d", run, totalMatches, baseline, errors)
			for _, k := range keys {
				got, want := perFile[k], baselinePer[k]
				if got != want {
					t.Logf("  %s: got %d, want %d (diff %+d)", k, got, want, got-want)
				}
			}
		}
	}

	t.Logf("ParserPool: %d/%d runs matched serial baseline", runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("CONFIRMED: %d/%d concurrent ParserPool runs produced different results than serial", variantRuns, runs)
	}
}

// Test 4: Shared Query — concurrent Execute() on the same *ts.Query instance.
func TestTreeSitter_SharedQuery_Variance(t *testing.T) {
	skipUnlessVariance(t)
	baseline := serialMatchCount(t, testGoSources, goQuery)
	baselinePer := serialPerFileMatchCounts(t, testGoSources, goQuery)
	t.Logf("serial baseline: %d total matches", baseline)

	keys := sortedKeys(testGoSources)
	lang := grammars.DetectLanguageByName("go").Language()

	// Shared query — same *ts.Query for all goroutines.
	sharedQ, err := ts.NewQuery(goQuery, lang)
	if err != nil {
		t.Fatalf("compile query: %v", err)
	}

	// Pre-parse all files serially (so trees are known-good).
	trees := make(map[string]*ts.Tree)
	for _, name := range keys {
		parser := ts.NewParser(lang)
		tree, err := parser.Parse([]byte(testGoSources[name]))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		trees[name] = tree
	}

	const runs = 10
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		perFile := make(map[string]int)
		totalMatches := 0
		errors := 0

		var wg sync.WaitGroup
		for _, name := range keys {
			wg.Add(1)
			go func(fname string) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				// Same query, different trees — is Execute thread-safe?
				matches := sharedQ.Execute(trees[fname])

				mu.Lock()
				perFile[fname] = len(matches)
				totalMatches += len(matches)
				mu.Unlock()
			}(name)
		}
		wg.Wait()

		if totalMatches != baseline || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE — got %d matches (want %d), errors=%d", run, totalMatches, baseline, errors)
			for _, k := range keys {
				got, want := perFile[k], baselinePer[k]
				if got != want {
					t.Logf("  %s: got %d, want %d (diff %+d)", k, got, want, got-want)
				}
			}
		}
	}

	t.Logf("SharedQuery: %d/%d runs matched serial baseline", runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("CONFIRMED: %d/%d concurrent SharedQuery.Execute runs produced different results than serial", variantRuns, runs)
	}
}

// Test 5: High-contention stress — GOMAXPROCS goroutines parsing ALL files.
func TestTreeSitter_HighContention_Variance(t *testing.T) {
	skipUnlessVariance(t)
	baseline := serialMatchCount(t, testGoSources, goQuery)
	t.Logf("serial baseline: %d total matches", baseline)

	keys := sortedKeys(testGoSources)
	lang := grammars.DetectLanguageByName("go").Language()
	pool := ts.NewParserPool(lang)

	workers := max(runtime.NumCPU(), 4)

	const runs = 5
	variantRuns := 0

	for run := range runs {
		// Each worker parses ALL files — high contention on global pools.
		var mu sync.Mutex
		workerTotals := make([]int, workers)
		errors := 0

		var wg sync.WaitGroup
		for w := range workers {
			wg.Add(1)
			go func(workerID int) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				total := 0
				for _, name := range keys {
					src := []byte(testGoSources[name])
					tree, err := pool.Parse(src)
					if err != nil {
						mu.Lock()
						errors++
						mu.Unlock()
						continue
					}

					q, err := ts.NewQuery(goQuery, lang)
					if err != nil {
						mu.Lock()
						errors++
						mu.Unlock()
						continue
					}
					matches := q.Execute(tree)
					total += len(matches)
				}
				mu.Lock()
				workerTotals[workerID] = total
				mu.Unlock()
			}(w)
		}
		wg.Wait()

		// Every worker parsed the same files — they should all get the baseline count.
		allMatch := true
		for w, total := range workerTotals {
			if total != baseline {
				allMatch = false
				t.Logf("run %d worker %d: got %d matches (want %d, diff %+d)", run, w, total, baseline, total-baseline)
			}
		}
		if !allMatch || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE DETECTED (errors=%d)", run, errors)
		}
	}

	t.Logf("HighContention (%d workers): %d/%d runs fully consistent", workers, runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("CONFIRMED: %d/%d high-contention runs produced different results than serial", variantRuns, runs)
	}
}

// Test 6: Per-goroutine Query — each goroutine gets its own *ts.Query from a sync.Pool.
func TestTreeSitter_PerGoroutineQuery_Variance(t *testing.T) {
	skipUnlessVariance(t)
	baseline := serialMatchCount(t, testGoSources, goQuery)
	baselinePer := serialPerFileMatchCounts(t, testGoSources, goQuery)
	t.Logf("serial baseline: %d total matches", baseline)

	keys := sortedKeys(testGoSources)
	lang := grammars.DetectLanguageByName("go").Language()
	pool := ts.NewParserPool(lang)

	queryPool := sync.Pool{
		New: func() any {
			q, _ := ts.NewQuery(goQuery, lang)
			return q
		},
	}

	const runs = 10
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		perFile := make(map[string]int)
		totalMatches := 0
		errors := 0

		var wg sync.WaitGroup
		for _, name := range keys {
			wg.Add(1)
			go func(fname string, src []byte) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				tree, err := pool.Parse(src)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}

				// Borrow a query from the pool — each goroutine gets its own.
				q, _ := queryPool.Get().(*ts.Query)
				defer queryPool.Put(q)

				matches := q.Execute(tree)

				mu.Lock()
				perFile[fname] = len(matches)
				totalMatches += len(matches)
				mu.Unlock()
			}(name, []byte(testGoSources[name]))
		}
		wg.Wait()

		if totalMatches != baseline || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE — got %d matches (want %d), errors=%d", run, totalMatches, baseline, errors)
			for _, k := range keys {
				got, want := perFile[k], baselinePer[k]
				if got != want {
					t.Logf("  %s: got %d, want %d (diff %+d)", k, got, want, got-want)
				}
			}
		}
	}

	t.Logf("PerGoroutineQuery+ParserPool: %d/%d runs matched serial baseline", runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("CONFIRMED: %d/%d per-goroutine query runs produced different results than serial", variantRuns, runs)
	}
}

// Test 7: Real repo files — raw concurrent parsing without parseMu. Documents upstream bug.
func TestTreeSitter_RealRepoFiles_Variance(t *testing.T) {
	skipUnlessVariance(t)
	// Collect all .go files in the repo.
	repoSources := collectRepoGoFiles(t)
	if len(repoSources) < 10 {
		t.Skipf("only %d .go files found, need >=10 for meaningful test", len(repoSources))
	}
	t.Logf("found %d .go files", len(repoSources))

	baseline := serialMatchCount(t, repoSources, goQuery)
	t.Logf("serial baseline: %d total matches across %d files", baseline, len(repoSources))

	lang := grammars.DetectLanguageByName("go").Language()
	pool := ts.NewParserPool(lang)
	keys := sortedKeys(repoSources)

	const runs = 5
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		totalMatches := 0
		errors := 0
		variantFiles := 0

		var wg sync.WaitGroup
		for _, name := range keys {
			wg.Add(1)
			go func(_ string, src []byte) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				tree, err := pool.Parse(src)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}

				q, err := ts.NewQuery(goQuery, lang)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}
				matches := q.Execute(tree)

				mu.Lock()
				totalMatches += len(matches)
				mu.Unlock()
			}(name, []byte(repoSources[name]))
		}
		wg.Wait()

		if totalMatches != baseline || errors > 0 {
			variantRuns++
			_ = variantFiles
			t.Logf("run %d: VARIANCE — got %d matches (want %d, diff %+d), errors=%d",
				run, totalMatches, baseline, totalMatches-baseline, errors)
		}
	}

	t.Logf("RealRepo (%d files): %d/%d runs matched serial baseline", len(repoSources), runs-variantRuns, runs)
	if variantRuns > 0 {
		// Log only — this documents the upstream gotreesitter bug.
		// Our production code serializes Parse via parseMu (validated by Test 11).
		t.Logf("UPSTREAM BUG: %d/%d raw concurrent runs produced different results (gotreesitter global sync.Pool race)", variantRuns, runs)
	}
}

// Test 8: Parse tree structure — compares S-expressions to detect tree corruption.
func TestTreeSitter_ParseTreeStructure_Variance(t *testing.T) {
	skipUnlessVariance(t)
	source := []byte(testGoSources["server.go"])
	lang := grammars.DetectLanguageByName("go").Language()

	// Get ground-truth S-expression from serial parse.
	parser := ts.NewParser(lang)
	refTree, err := parser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	refSexp := refTree.RootNode().SExpr(lang)
	refChildCount := countNodes(refTree.RootNode())
	t.Logf("reference tree: %d nodes, sexp length=%d", refChildCount, len(refSexp))

	pool := ts.NewParserPool(lang)
	workers := runtime.NumCPU()

	const runs = 10
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		mismatches := 0

		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						mismatches++
						mu.Unlock()
					}
					wg.Done()
				}()

				tree, err := pool.Parse(source)
				if err != nil {
					mu.Lock()
					mismatches++
					mu.Unlock()
					return
				}

				sexp := tree.RootNode().SExpr(lang)
				ncount := countNodes(tree.RootNode())
				if ncount != refChildCount || sexp != refSexp {
					mu.Lock()
					mismatches++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		if mismatches > 0 {
			variantRuns++
			t.Logf("run %d: %d/%d workers produced different parse trees", run, mismatches, workers)
		}
	}

	t.Logf("ParseTreeStructure: %d/%d runs fully consistent", runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("CONFIRMED: %d/%d runs had parse tree structure differences", variantRuns, runs)
	}
}

// Test 9: Large-scale synthetic files — 200+ generated sources, no real repo dependency.
func TestTreeSitter_LargeSynthetic_Variance(t *testing.T) {
	skipUnlessVariance(t)
	// Generate 200 unique Go source files from templates.
	largeSources := make(map[string]string, 200)
	templates := []string{
		`package gen%d
import "fmt"
type Service%d struct { Name string; Port int }
type Handler%d interface { Handle() error }
func New%d(name string) *Service%d { return &Service%d{Name: name} }
func (s *Service%d) Start() error { fmt.Println(s.Name); return nil }
func (s *Service%d) Stop() error { return nil }
`,
		`package gen%d
import "sync"
type Cache%d struct { mu sync.Mutex; data map[string]int }
func NewCache%d() *Cache%d { return &Cache%d{data: make(map[string]int)} }
func (c *Cache%d) Get(k string) int { c.mu.Lock(); defer c.mu.Unlock(); return c.data[k] }
func (c *Cache%d) Set(k string, v int) { c.mu.Lock(); defer c.mu.Unlock(); c.data[k] = v }
func (c *Cache%d) Delete(k string) { c.mu.Lock(); defer c.mu.Unlock(); delete(c.data, k) }
`,
		`package gen%d
import "io"
type Reader%d interface { Read(p []byte) (int, error) }
type Writer%d interface { Write(p []byte) (int, error) }
type ReadWriter%d interface { Reader%d; Writer%d }
type Processor%d struct { r io.Reader; w io.Writer }
func NewProcessor%d(r io.Reader, w io.Writer) *Processor%d { return &Processor%d{r: r, w: w} }
func (p *Processor%d) Process() error { return nil }
`,
	}

	for i := range 200 {
		tmpl := templates[i%len(templates)]
		// Each %d placeholder gets the same unique number.
		nPlaceholders := strings.Count(tmpl, "%d")
		args := make([]any, nPlaceholders)
		for j := range args {
			args[j] = i
		}
		src := fmt.Sprintf(tmpl, args...)
		largeSources[fmt.Sprintf("gen_%03d.go", i)] = src
	}

	baseline := serialMatchCount(t, largeSources, goQuery)
	t.Logf("synthetic baseline: %d total matches across %d files", baseline, len(largeSources))

	lang := grammars.DetectLanguageByName("go").Language()
	pool := ts.NewParserPool(lang)
	keys := sortedKeys(largeSources)

	const runs = 5
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		totalMatches := 0
		errors := 0

		var wg sync.WaitGroup
		for _, name := range keys {
			wg.Add(1)
			go func(_ string, src []byte) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				tree, err := pool.Parse(src)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}
				q, err := ts.NewQuery(goQuery, lang)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}
				matches := q.Execute(tree)
				mu.Lock()
				totalMatches += len(matches)
				mu.Unlock()
			}(name, []byte(largeSources[name]))
		}
		wg.Wait()

		if totalMatches != baseline || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE — got %d matches (want %d, diff %+d), errors=%d",
				run, totalMatches, baseline, totalMatches-baseline, errors)
		}
	}

	t.Logf("LargeSynthetic (%d files): %d/%d runs matched serial baseline", len(largeSources), runs-variantRuns, runs)
	if variantRuns > 0 {
		// Log only — documents the upstream gotreesitter bug at scale.
		t.Logf("UPSTREAM BUG: %d/%d raw concurrent runs produced different results (gotreesitter global sync.Pool race)", variantRuns, runs)
	}
}

// Test 10: Serial-parse + concurrent-execute — isolates Execute as thread-safe.
func TestTreeSitter_RealRepo_SerialParseConcurrentExecute(t *testing.T) {
	skipUnlessVariance(t)
	repoSources := collectRepoGoFiles(t)
	if len(repoSources) < 10 {
		t.Skipf("only %d .go files found", len(repoSources))
	}
	t.Logf("found %d .go files", len(repoSources))

	lang := grammars.DetectLanguageByName("go").Language()
	keys := sortedKeys(repoSources)

	// Step 1: Parse ALL files serially — no concurrency, no races.
	trees := make(map[string]*ts.Tree, len(repoSources))
	serialTotal := 0
	for _, name := range keys {
		parser := ts.NewParser(lang)
		tree, err := parser.Parse([]byte(repoSources[name]))
		if err != nil {
			t.Logf("serial parse skip %s: %v", name, err)
			continue
		}
		q, err := ts.NewQuery(goQuery, lang)
		if err != nil {
			t.Fatalf("compile query: %v", err)
		}
		matches := q.Execute(tree)
		serialTotal += len(matches)
		trees[name] = tree
	}
	t.Logf("serial baseline (parse+execute): %d matches from %d trees", serialTotal, len(trees))

	// Step 2: Execute concurrently on the known-good serial trees.
	treeKeys := sortedKeys(repoSources)

	const runs = 5
	variantRuns := 0

	for run := range runs {
		var mu sync.Mutex
		totalMatches := 0
		errors := 0

		var wg sync.WaitGroup
		for _, name := range treeKeys {
			tree, ok := trees[name]
			if !ok {
				continue
			}
			wg.Add(1)
			go func(_ string, tr *ts.Tree) {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						errors++
						mu.Unlock()
					}
					wg.Done()
				}()

				q, err := ts.NewQuery(goQuery, lang)
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
					return
				}
				matches := q.Execute(tr)
				mu.Lock()
				totalMatches += len(matches)
				mu.Unlock()
			}(name, tree)
		}
		wg.Wait()

		if totalMatches != serialTotal || errors > 0 {
			variantRuns++
			t.Logf("run %d: VARIANCE — got %d matches (want %d, diff %+d), errors=%d",
				run, totalMatches, serialTotal, totalMatches-serialTotal, errors)
		}
	}

	t.Logf("SerialParse+ConcurrentExecute (%d files): %d/%d runs matched baseline",
		len(trees), runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("Execute variance: %d/%d runs differed — Execute is also non-deterministic", variantRuns, runs)
	} else {
		t.Logf("CONCLUSION: Query.Execute is thread-safe — variance comes from Parser.Parse")
	}
}

// Test 11: Production path — validates parseMu fix via ParseFiles end-to-end.
func TestTreeSitter_ProductionPath_Deterministic(t *testing.T) {
	skipUnlessVariance(t)
	repoSources := collectRepoGoFiles(t)
	if len(repoSources) < 10 {
		t.Skipf("only %d .go files found", len(repoSources))
	}
	t.Logf("found %d .go files", len(repoSources))

	// Build FileInput list with in-memory source data.
	sourceData := make(map[string][]byte, len(repoSources))
	var files []FileInput
	for name, src := range repoSources {
		sourceData[name] = []byte(src)
		files = append(files, FileInput{
			Path:     name,
			Language: "go",
			Size:     int64(len(src)),
		})
	}

	readFile := func(path string) ([]byte, error) {
		data, ok := sourceData[path]
		if !ok {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return data, nil
	}

	// Run with Workers=1 to get deterministic baseline.
	baselineResult := ParseFiles(files, ParseOptions{
		Workers:  1,
		ReadFile: readFile,
	})
	baselineSymbols := len(baselineResult.Symbols)
	baselineImports := len(baselineResult.Imports)
	baselineCalls := len(baselineResult.Calls)
	t.Logf("baseline (1 worker): %d symbols, %d imports, %d calls, %d errors",
		baselineSymbols, baselineImports, baselineCalls, len(baselineResult.Errors))

	// Run with multiple workers N times — should match baseline every time.
	const runs = 5
	variantRuns := 0

	for run := range runs {
		result := ParseFiles(files, ParseOptions{
			Workers:  runtime.NumCPU(),
			ReadFile: readFile,
		})
		syms := len(result.Symbols)
		imps := len(result.Imports)
		calls := len(result.Calls)
		errs := len(result.Errors)

		if syms != baselineSymbols || imps != baselineImports || calls != baselineCalls {
			variantRuns++
			t.Logf("run %d: VARIANCE — symbols=%d (want %d, diff %+d), imports=%d (want %d), calls=%d (want %d), errors=%d",
				run, syms, baselineSymbols, syms-baselineSymbols,
				imps, baselineImports, calls, baselineCalls, errs)
		}
	}

	t.Logf("ProductionPath (%d files, %d workers): %d/%d runs matched baseline",
		len(files), runtime.NumCPU(), runs-variantRuns, runs)
	if variantRuns > 0 {
		t.Errorf("PRODUCTION PATH VARIANCE: %d/%d runs with parseMu produced different results", variantRuns, runs)
	} else {
		t.Logf("SUCCESS: parseMu serialization eliminates variance in production code path")
	}
}

// ---------------------------------------------------------------------------
// Diagnostic summary test — runs all patterns and prints a summary table.
// ---------------------------------------------------------------------------
func TestTreeSitter_VarianceSummary(t *testing.T) {
	skipUnlessVariance(t)
	lang := grammars.DetectLanguageByName("go").Language()
	baseline := serialMatchCount(t, testGoSources, goQuery)
	keys := sortedKeys(testGoSources)

	type scenario struct {
		name    string
		runFunc func() (int, int) // returns (totalMatches, errors)
	}

	pool := ts.NewParserPool(lang)

	scenarios := []scenario{
		{
			name: "NewParser-per-goroutine",
			runFunc: func() (int, int) {
				var mu sync.Mutex
				total, errs := 0, 0
				var wg sync.WaitGroup
				for _, name := range keys {
					wg.Add(1)
					go func(src []byte) {
						defer func() {
							if r := recover(); r != nil {
								mu.Lock()
								errs++
								mu.Unlock()
							}
							wg.Done()
						}()
						p := ts.NewParser(lang)
						tree, err := p.Parse(src)
						if err != nil {
							mu.Lock()
							errs++
							mu.Unlock()
							return
						}
						q, _ := ts.NewQuery(goQuery, lang)
						matches := q.Execute(tree)
						mu.Lock()
						total += len(matches)
						mu.Unlock()
					}([]byte(testGoSources[name]))
				}
				wg.Wait()
				return total, errs
			},
		},
		{
			name: "ParserPool-shared",
			runFunc: func() (int, int) {
				var mu sync.Mutex
				total, errs := 0, 0
				var wg sync.WaitGroup
				for _, name := range keys {
					wg.Add(1)
					go func(src []byte) {
						defer func() {
							if r := recover(); r != nil {
								mu.Lock()
								errs++
								mu.Unlock()
							}
							wg.Done()
						}()
						tree, err := pool.Parse(src)
						if err != nil {
							mu.Lock()
							errs++
							mu.Unlock()
							return
						}
						q, _ := ts.NewQuery(goQuery, lang)
						matches := q.Execute(tree)
						mu.Lock()
						total += len(matches)
						mu.Unlock()
					}([]byte(testGoSources[name]))
				}
				wg.Wait()
				return total, errs
			},
		},
	}

	const runs = 20

	t.Logf("%-30s  %s", "Scenario", "Results (20 runs)")
	t.Logf("%-30s  %s", strings.Repeat("-", 30), strings.Repeat("-", 40))

	for _, sc := range scenarios {
		matches := make([]int, runs)
		errCount := 0
		for i := range runs {
			m, e := sc.runFunc()
			matches[i] = m
			errCount += e
		}

		min, max := matches[0], matches[0]
		for _, m := range matches[1:] {
			if m < min {
				min = m
			}
			if m > max {
				max = m
			}
		}
		consistent := min == max && max == baseline

		status := "OK"
		if !consistent {
			status = fmt.Sprintf("VARIANCE [%d..%d] want %d", min, max, baseline)
		}
		if errCount > 0 {
			status += fmt.Sprintf(" +%d panics/errors", errCount)
		}

		t.Logf("%-30s  %s", sc.name, status)
		if !consistent {
			// Log only — this is a raw-library diagnostic, not a production gate.
			t.Logf("%s: non-deterministic — range [%d..%d], want %d (upstream gotreesitter bug)", sc.name, min, max, baseline)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func countNodes(n *ts.Node) int {
	if n == nil {
		return 0
	}
	count := 1
	for i := range n.ChildCount() {
		count += countNodes(n.Child(i))
	}
	return count
}

func collectRepoGoFiles(t *testing.T) map[string]string {
	t.Helper()

	sources := make(map[string]string)
	root := filepath.Join("..", "..", "..", "..") // back up to repo root from extractors pkg
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // skip errors
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == "node_modules" || base == ".git" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			data, err := os.ReadFile(path) //nolint:gosec // G122
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}
			// Use relative path as key for readable output.
			rel, _ := filepath.Rel(root, path)
			sources[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	return sources
}
