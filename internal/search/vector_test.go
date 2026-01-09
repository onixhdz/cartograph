package search

import (
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := CosineSimilarity(a, a)
	if math.Abs(got-1.0) > 1e-6 {
		t.Errorf("identical vectors: expected 1.0, got %f", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	got := CosineSimilarity(a, b)
	if math.Abs(got) > 1e-6 {
		t.Errorf("orthogonal vectors: expected 0.0, got %f", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	got := CosineSimilarity(a, b)
	if math.Abs(got-(-1.0)) > 1e-6 {
		t.Errorf("opposite vectors: expected -1.0, got %f", got)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("different lengths: expected 0, got %f", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("zero vector: expected 0, got %f", got)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	got := CosineSimilarity(nil, nil)
	if got != 0 {
		t.Errorf("nil vectors: expected 0, got %f", got)
	}
}

func TestVectorSearch_Basic(t *testing.T) {
	query := []float32{1, 0, 0}
	entries := []VectorEntry{
		{ID: "a", Vector: []float32{1, 0, 0}},     // cos=1.0
		{ID: "b", Vector: []float32{0, 1, 0}},     // cos=0.0
		{ID: "c", Vector: []float32{0.7, 0.7, 0}}, // cos≈0.707
	}
	results := VectorSearch(query, entries, 10, 0.0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected top result 'a', got %q", results[0].ID)
	}
	if results[1].ID != "c" {
		t.Errorf("expected 2nd result 'c', got %q", results[1].ID)
	}
}

func TestVectorSearch_TopK(t *testing.T) {
	query := []float32{1, 0}
	entries := []VectorEntry{
		{ID: "a", Vector: []float32{1, 0}},
		{ID: "b", Vector: []float32{0.9, 0.1}},
		{ID: "c", Vector: []float32{0.5, 0.5}},
	}
	results := VectorSearch(query, entries, 2, 0.0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestVectorSearch_MinScore(t *testing.T) {
	query := []float32{1, 0}
	entries := []VectorEntry{
		{ID: "a", Vector: []float32{1, 0}},     // cos=1.0
		{ID: "b", Vector: []float32{0, 1}},     // cos=0.0
		{ID: "c", Vector: []float32{0.7, 0.7}}, // cos≈0.707
	}
	results := VectorSearch(query, entries, 10, 0.5)
	if len(results) != 2 {
		t.Fatalf("expected 2 results above 0.5, got %d", len(results))
	}
	for _, r := range results {
		if r.Score < 0.5 {
			t.Errorf("result %s score %f below minScore 0.5", r.ID, r.Score)
		}
	}
}

func TestVectorSearch_Empty(t *testing.T) {
	results := VectorSearch([]float32{1, 0}, nil, 10, 0.0)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty entries, got %d", len(results))
	}

	results = VectorSearch(nil, []VectorEntry{{ID: "a", Vector: []float32{1, 0}}}, 10, 0.0)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil query, got %d", len(results))
	}
}

const testIDHandleRequest = "func:handleRequest"

func TestHybridSearch_MergesBoth(t *testing.T) {
	bm25 := []SearchResult{
		{ID: testIDHandleRequest, Score: 5.0},
		{ID: "func:parseInput", Score: 3.0},
	}
	vector := []VectorResult{
		{ID: "func:validateUser", Score: 0.95},
		{ID: testIDHandleRequest, Score: 0.80},
	}
	results := HybridSearch(bm25, vector, 10, 1.0)
	if len(results) == 0 {
		t.Fatal("expected hybrid results")
	}
	// handleRequest should be top because it appears in both lists.
	if results[0].ID != testIDHandleRequest {
		t.Errorf("expected top result 'func:handleRequest', got %q", results[0].ID)
	}
	// All 3 unique IDs should appear.
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.ID] = true
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 unique results, got %d", len(ids))
	}
}

func TestHybridSearch_BM25Only(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "a", Score: 5.0},
		{ID: "b", Score: 3.0},
	}
	results := HybridSearch(bm25, nil, 10, 1.0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with no vector input, got %d", len(results))
	}
}

func TestHybridSearch_VectorOnly(t *testing.T) {
	vector := []VectorResult{
		{ID: "a", Score: 0.95},
		{ID: "b", Score: 0.80},
	}
	results := HybridSearch(nil, vector, 10, 1.0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with no BM25 input, got %d", len(results))
	}
}

func TestHybridSearch_Limit(t *testing.T) {
	bm25 := []SearchResult{
		{ID: "a", Score: 5.0},
		{ID: "b", Score: 3.0},
		{ID: "c", Score: 2.0},
	}
	vector := []VectorResult{
		{ID: "d", Score: 0.95},
		{ID: "e", Score: 0.80},
	}
	results := HybridSearch(bm25, vector, 3, 1.0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results with limit=3, got %d", len(results))
	}
}

func TestHybridSearch_VectorWeight(t *testing.T) {
	// With high vector weight, vector-only results should rank higher.
	bm25 := []SearchResult{
		{ID: "bm25only", Score: 5.0},
	}
	vector := []VectorResult{
		{ID: "veconly", Score: 0.95},
	}
	// With vectorWeight=5.0, veconly should beat bm25only.
	results := HybridSearch(bm25, vector, 10, 5.0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "veconly" {
		t.Errorf("with 5× vector weight, expected 'veconly' first, got %q", results[0].ID)
	}
}

func BenchmarkCosineSimilarity_384(b *testing.B) {
	a := make([]float32, 384)
	bv := make([]float32, 384)
	for i := range a {
		a[i] = float32(i) * 0.01
		bv[i] = float32(384-i) * 0.01
	}
	b.ResetTimer()
	for range b.N {
		CosineSimilarity(a, bv)
	}
}

func BenchmarkVectorSearch_10K(b *testing.B) {
	query := make([]float32, 384)
	for i := range query {
		query[i] = float32(i) * 0.01
	}
	entries := make([]VectorEntry, 10000)
	for i := range entries {
		vec := make([]float32, 384)
		for j := range vec {
			vec[j] = float32((i*384+j)%1000) * 0.001
		}
		entries[i] = VectorEntry{ID: "node:" + string(rune('a'+i%26)), Vector: vec}
	}
	b.ResetTimer()
	for range b.N {
		VectorSearch(query, entries, 10, 0.0)
	}
}
