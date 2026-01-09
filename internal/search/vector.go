// Package search — vector.go provides brute-force vector similarity
// search and hybrid BM25+vector search via RRF fusion.
package search

import (
	"math"
	"sort"
)

// VectorResult is a single result from a vector similarity search.
type VectorResult struct {
	ID    string
	Score float64 // cosine similarity ∈ [-1, 1]
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector is zero-length or they have different
// dimensions.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// VectorEntry is a node ID + vector pair used as input to VectorSearch.
type VectorEntry struct {
	ID     string
	Vector []float32
}

// VectorSearch performs brute-force cosine similarity search over a
// slice of entries. Returns the top-k results sorted by descending
// similarity, filtered to only include results above minScore.
func VectorSearch(query []float32, entries []VectorEntry, topK int, minScore float64) []VectorResult {
	if len(entries) == 0 || len(query) == 0 || topK <= 0 {
		return nil
	}

	results := make([]VectorResult, 0, min(topK*2, len(entries)))
	for _, e := range entries {
		score := CosineSimilarity(query, e.Vector)
		if score >= minScore {
			results = append(results, VectorResult{ID: e.ID, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].ID < results[j].ID
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

// HybridSearch merges BM25 and vector results using Reciprocal Rank Fusion.
// vectorWeight controls vector influence relative to BM25 (default 1.0).
func HybridSearch(bm25Results []SearchResult, vectorResults []VectorResult, limit int, vectorWeight float64) []RRFResult {
	if vectorWeight <= 0 {
		vectorWeight = 1.0
	}
	if limit <= 0 {
		limit = 10
	}

	vecSR := make([]SearchResult, len(vectorResults))
	for i, vr := range vectorResults {
		vecSR[i] = SearchResult(vr)
	}

	fused := WeightedRRFMerge(
		RankedList{Results: bm25Results, Weight: 1.0},
		RankedList{Results: vecSR, Weight: vectorWeight},
	)

	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused
}
