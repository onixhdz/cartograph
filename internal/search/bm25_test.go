package search

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
)

// buildTestGraph creates a small graph with searchable nodes.
func buildTestGraph() *lpg.Graph {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:handleRequest", Name: "handleRequest"},
		FilePath:      "server.go",
		StartLine:     10,
		EndLine:       30,
		Content:       "func handleRequest(w http.ResponseWriter, r *http.Request) { }",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:validateUser", Name: "validateUser"},
		FilePath:      "auth.go",
		StartLine:     5,
		EndLine:       20,
		Content:       "func validateUser(token string) (User, error) { }",
	})
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:UserService", Name: "UserService"},
		FilePath:      "service.go",
		StartLine:     1,
		EndLine:       50,
		Content:       "type UserService struct { db *sql.DB }",
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:UserService.Create", Name: "Create"},
		FilePath:      "service.go",
		StartLine:     52,
		EndLine:       70,
		Content:       "func (s *UserService) Create(u User) error { }",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:server.go", Name: "server.go"},
		FilePath:      "server.go",
		Language:      "go",
		Size:          1024,
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:auth.go", Name: "auth.go"},
		FilePath:      "auth.go",
		Language:      "go",
		Size:          512,
	})

	graph.AddFolderNode(g, graph.FolderProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "folder:src", Name: "src"},
		FilePath:      "src",
	})
	graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "community:0", Name: "community-0"},
		Modularity:    0.5,
		Size:          3,
	})

	return g
}

func TestNewMemoryIndex(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	count, err := ix.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 docs in empty index, got %d", count)
	}
}

func TestIndexGraph(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	indexed, err := ix.IndexGraph(g)
	if err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	if indexed != 6 {
		t.Errorf("expected 6 indexed docs, got %d", indexed)
	}

	count, err := ix.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 6 {
		t.Errorf("expected 6 docs in index, got %d", count)
	}
}

func TestSearchByName(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.Search("handleRequest", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'handleRequest'")
	}
	if results[0].ID != "func:handleRequest" {
		t.Errorf("expected top result to be func:handleRequest, got %s", results[0].ID)
	}
	if results[0].Score <= 0 {
		t.Error("expected positive score")
	}
}

func TestSearchByContent(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.Search("token", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'token' (in content)")
	}
	found := false
	for _, r := range results {
		if r.ID == "func:validateUser" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected func:validateUser in results for 'token'")
	}
}

func TestSearchNoResults(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.Search("zzzznonexistent", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonsense query, got %d", len(results))
	}
}

func TestSearchLimit(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.Search("user", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results with limit=2, got %d", len(results))
	}
}

func TestSearchDefaultLimit(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.Search("user", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	_ = results
}

func TestNewIndexOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.bleve"

	ix, err := NewIndex(path)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	g := buildTestGraph()
	indexed, err := ix.IndexGraph(g)
	if err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	if indexed != 6 {
		t.Errorf("expected 6 indexed, got %d", indexed)
	}
	ix.Close()

	ix2, err := NewIndex(path)
	if err != nil {
		t.Fatalf("NewIndex (reopen): %v", err)
	}
	defer ix2.Close()

	count, err := ix2.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 6 {
		t.Errorf("expected 6 persisted docs, got %d", count)
	}

	results, err := ix2.Search("handleRequest", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results from reopened index")
	}
}

func TestDeleteIndex(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.bleve"

	ix, err := NewIndex(path)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	ix.Close()

	if err := DeleteIndex(path); err != nil {
		t.Fatalf("DeleteIndex: %v", err)
	}
}

func TestIndexEmptyGraph(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := lpg.NewGraph()
	indexed, err := ix.IndexGraph(g)
	if err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	if indexed != 0 {
		t.Errorf("expected 0 indexed for empty graph, got %d", indexed)
	}
}

func TestSearchMulti_NameAndContent(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("handleRequest", 10)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'handleRequest'")
	}
	if results[0].ID != "func:handleRequest" {
		t.Errorf("expected top result func:handleRequest, got %s", results[0].ID)
	}
	if results[0].Score <= 0 {
		t.Error("expected positive RRF score")
	}
}

func TestSearchMulti_ContentMatch(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("token", 10)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	found := false
	for _, r := range results {
		if r.ID == "func:validateUser" {
			found = true
		}
	}
	if !found {
		t.Error("expected func:validateUser in results for 'token' (content match)")
	}
}

func TestSearchMulti_NameBoost(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("User", 10)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results for 'User', got %d", len(results))
	}

	topIDs := make(map[string]bool)
	for i := range min(2, len(results)) {
		topIDs[results[i].ID] = true
		t.Logf("rank %d: %s (score=%.6f)", i+1, results[i].ID, results[i].Score)
	}
	if !topIDs["class:UserService"] && !topIDs["func:validateUser"] {
		t.Error("expected UserService or validateUser in top 2 for 'User'")
	}
}

func TestSearchMulti_NoResults(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("zzzznonexistent", 10)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchMulti_Limit(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("user", 1)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if len(results) > 1 {
		t.Errorf("expected at most 1 result with limit=1, got %d", len(results))
	}
}

func TestCleanQuery_StopWordRemoval(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"how does the app handle user login", "app handle user login"},
		{"what is the authentication flow", "authentication flow"},
		{"where are errors handled", "errors handled"},
		{"the", ""},
		{"is it a", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := CleanQuery(tc.raw)
		if got != tc.want {
			t.Errorf("CleanQuery(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestCleanQuery_CamelCaseSplit(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"handleRequest", "handle request"},
		{"UserService", "user service"},
		{"parseJSON", "parse json"},
		{"getHTTPClient", "httpclient"},
		{"validateUser", "validate user"},
	}
	for _, tc := range tests {
		got := CleanQuery(tc.raw)
		if got != tc.want {
			t.Errorf("CleanQuery(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestCleanQuery_MixedInput(t *testing.T) {
	got := CleanQuery("how does handleRequest validate the token")
	if got != "handle request validate token" {
		t.Errorf("CleanQuery mixed = %q, want %q", got, "handle request validate token")
	}
}

func TestCleanQuery_Punctuation(t *testing.T) {
	got := CleanQuery("user.Create() -- what does it do?")
	if got != "user create" {
		t.Errorf("CleanQuery punctuation = %q, want %q", got, "user create")
	}
}

func TestSearchMulti_NaturalLanguage(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("how does the user service create users", 10)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for natural-language query about user service")
	}

	found := false
	for _, r := range results {
		if r.ID == "class:UserService" || r.ID == "method:UserService.Create" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected UserService or Create in results for NL query about user service")
	}
}

func TestSearchMulti_AllStopWords(t *testing.T) {
	ix, err := NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}

	results, err := ix.SearchMulti("what is the", 10)
	if err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for all-stop-word query, got %d", len(results))
	}
}

func TestWeightedRRFMerge_HigherWeightWins(t *testing.T) {
	low := RankedList{
		Results: []SearchResult{{ID: "a", Score: 5.0}},
		Weight:  1.0,
	}
	high := RankedList{
		Results: []SearchResult{{ID: "b", Score: 5.0}},
		Weight:  3.0,
	}

	merged := WeightedRRFMerge(low, high)
	if len(merged) != 2 {
		t.Fatalf("expected 2 results, got %d", len(merged))
	}
	if merged[0].ID != "b" {
		t.Errorf("expected 'b' (3× weight) to rank first, got %q", merged[0].ID)
	}
	if merged[0].RRFScore <= merged[1].RRFScore {
		t.Errorf("expected higher score for 'b', got b=%.6f a=%.6f",
			merged[0].RRFScore, merged[1].RRFScore)
	}
}

func TestWeightedRRFMerge_ZeroWeightDefaultsToOne(t *testing.T) {
	list := RankedList{
		Results: []SearchResult{{ID: "x", Score: 1.0}},
		Weight:  0, // should be treated as 1.0
	}
	merged := WeightedRRFMerge(list)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}
	// 1.0 / (60 + 1) ≈ 0.016393
	expected := 1.0 / 61.0
	if merged[0].RRFScore < expected*0.99 || merged[0].RRFScore > expected*1.01 {
		t.Errorf("expected RRF score ≈ %.6f, got %.6f", expected, merged[0].RRFScore)
	}
}

func TestWeightedRRFMerge_CompatibleWithRRFMerge(t *testing.T) {
	set1 := []SearchResult{
		{ID: "a", Score: 3.0},
		{ID: "b", Score: 2.0},
	}
	set2 := []SearchResult{
		{ID: "b", Score: 5.0},
		{ID: "c", Score: 1.0},
	}

	rrfResults := RRFMerge(set1, set2)
	weightedResults := WeightedRRFMerge(
		RankedList{Results: set1, Weight: 1.0},
		RankedList{Results: set2, Weight: 1.0},
	)

	if len(rrfResults) != len(weightedResults) {
		t.Fatalf("length mismatch: RRF=%d, Weighted=%d", len(rrfResults), len(weightedResults))
	}
	for i := range rrfResults {
		if rrfResults[i].ID != weightedResults[i].ID {
			t.Errorf("rank %d: RRF=%s, Weighted=%s", i, rrfResults[i].ID, weightedResults[i].ID)
		}
		if rrfResults[i].RRFScore != weightedResults[i].RRFScore {
			t.Errorf("rank %d: RRF score=%.6f, Weighted score=%.6f",
				i, rrfResults[i].RRFScore, weightedResults[i].RRFScore)
		}
	}
}

func TestRRFMerge_SingleSet(t *testing.T) {
	set1 := []SearchResult{
		{ID: "a", Score: 3.0},
		{ID: "b", Score: 2.0},
		{ID: "c", Score: 1.0},
	}

	merged := RRFMerge(set1)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}
	if merged[0].ID != "a" {
		t.Errorf("expected top result 'a', got %q", merged[0].ID)
	}
	if merged[0].RRFScore <= 0 {
		t.Error("expected positive RRF score")
	}
}

func TestRRFMerge_TwoSets(t *testing.T) {
	set1 := []SearchResult{
		{ID: "a", Score: 3.0},
		{ID: "b", Score: 2.0},
	}
	set2 := []SearchResult{
		{ID: "b", Score: 5.0},
		{ID: "c", Score: 1.0},
	}

	merged := RRFMerge(set1, set2)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}
	if merged[0].ID != "b" {
		t.Errorf("expected top result 'b' (in both sets), got %q", merged[0].ID)
	}
}

func TestRRFMerge_Empty(t *testing.T) {
	merged := RRFMerge()
	if len(merged) != 0 {
		t.Errorf("expected 0 results for empty merge, got %d", len(merged))
	}
}

func TestRRFMerge_Deterministic(t *testing.T) {
	set1 := []SearchResult{
		{ID: "x", Score: 1.0},
		{ID: "y", Score: 1.0},
	}

	merged := RRFMerge(set1)
	// With same RRF scores, should be sorted by ID.
	// x is rank 1, y is rank 2 — different scores, so x first.
	if merged[0].ID != "x" {
		t.Errorf("expected 'x' first (higher rank), got %q", merged[0].ID)
	}
}

func TestIsSearchable(t *testing.T) {
	g := lpg.NewGraph()

	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:a", Name: "a"},
	})
	folder := graph.AddFolderNode(g, graph.FolderProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "folder:x", Name: "x"},
	})
	community := graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "comm:0", Name: "c"},
	})
	process := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "proc:a", Name: "p"},
	})

	if !isSearchable(fn) {
		t.Error("Function should be searchable")
	}
	if isSearchable(folder) {
		t.Error("Folder should not be searchable")
	}
	if isSearchable(community) {
		t.Error("Community should not be searchable")
	}
	if isSearchable(process) {
		t.Error("Process should not be searchable")
	}
}

func TestNewReadOnlyIndex(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.bleve"

	ix, err := NewIndex(path)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	ix.Close()

	// Open read-only — should be searchable.
	ro, err := NewReadOnlyIndex(path)
	if err != nil {
		t.Fatalf("NewReadOnlyIndex: %v", err)
	}
	defer ro.Close()

	results, err := ro.Search("handleRequest", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results from read-only index")
	}
}

func TestReadOnlyIndex_ConcurrentOpen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.bleve"

	ix, err := NewIndex(path)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	g := buildTestGraph()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	ix.Close()

	// Two read-only opens should not block each other.
	ro1, err := NewReadOnlyIndex(path)
	if err != nil {
		t.Fatalf("NewReadOnlyIndex (1): %v", err)
	}
	defer ro1.Close()

	ro2, err := NewReadOnlyIndex(path)
	if err != nil {
		t.Fatalf("NewReadOnlyIndex (2): %v", err)
	}
	defer ro2.Close()

	r1, _ := ro1.Search("handleRequest", 5)
	r2, _ := ro2.Search("handleRequest", 5)
	if len(r1) == 0 || len(r2) == 0 {
		t.Error("expected both read-only indexes to return results")
	}
}
