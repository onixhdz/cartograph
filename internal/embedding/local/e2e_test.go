package local

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
)

// testModel holds a cached model's metadata for table-driven tests.
type testModel struct {
	name      string
	path      string
	minDims   int
	isDecoder bool
}

// availableModels returns models present on disk. Tests skip when none are found.
func availableModels(t *testing.T) []testModel {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping e2e embedding tests in -short mode")
	}
	home, _ := os.UserHomeDir()

	candidates := []testModel{
		{
			name:    "bge-small (encoder)",
			path:    home + "/.cache/cartograph/models/CompendiumLabs/bge-small-en-v1.5-gguf/bge-small-en-v1.5-q8_0.gguf",
			minDims: 384,
		},
		{
			name:      "qwen3-embedding (decoder)",
			path:      home + "/.cache/cartograph/models/Qwen/Qwen3-Embedding-0.6B-GGUF/Qwen3-Embedding-0.6B-Q8_0.gguf",
			minDims:   512,
			isDecoder: true,
		},
	}

	var found []testModel
	for _, c := range candidates {
		if _, err := os.Stat(c.path); err == nil {
			found = append(found, c)
		}
	}
	return found
}

// loadProvider creates a single-worker provider from a model path.
func loadProvider(t *testing.T, path string) *Provider {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	p, err := NewWithWorkers(data, 1)
	if err != nil {
		t.Fatalf("NewWithWorkers: %v", err)
	}
	return p
}

// cosine returns the cosine similarity between two equal-length vectors.
func cosine(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// vecNorm returns L2 norm of a vector.
func vecNorm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// TestBasicEmbedding verifies dimensions, unit norm, and non-zero output for each model.
func TestBasicEmbedding(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached — pull with: cartograph models pull bge-small")
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			dims := p.Dimensions()
			t.Logf("dims=%d", dims)
			if dims < m.minDims {
				t.Fatalf("expected >= %d dims, got %d", m.minDims, dims)
			}

			vecs, err := p.Embed(context.Background(), []string{"hello world"})
			if err != nil {
				t.Fatalf("embed: %v", err)
			}

			vec := vecs[0]
			if len(vec) != dims {
				t.Fatalf("vec len=%d, want %d", len(vec), dims)
			}

			norm := vecNorm(vec)
			t.Logf("norm=%.6f first5=%v", norm, vec[:5])

			if math.Abs(norm-1.0) > 0.01 {
				t.Errorf("expected unit norm, got %.6f", norm)
			}

			nonZero := 0
			for _, v := range vec {
				if v != 0 {
					nonZero++
				}
			}
			if nonZero < dims/2 {
				t.Errorf("too many zeros: %d/%d non-zero", nonZero, dims)
			}
		})
	}
}

// TestDifferentTextsDiffer verifies that semantically different texts produce different embeddings.
func TestDifferentTextsDiffer(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	pairs := [][2]string{
		{"hello world", "func main() { fmt.Println(x) }"},
		{"sorting algorithm", "HTTP server configuration"},
		{"the quick brown fox", "SELECT * FROM users WHERE id = 1"},
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			for _, pair := range pairs {
				vecs, err := p.Embed(context.Background(), []string{pair[0], pair[1]})
				if err != nil {
					t.Fatalf("embed: %v", err)
				}
				sim := cosine(vecs[0], vecs[1])
				t.Logf("cos(%q, %q) = %.4f", pair[0], pair[1][:min(30, len(pair[1]))], sim)
				if sim > 0.99 {
					t.Errorf("too similar (%.4f): %q vs %q", sim, pair[0], pair[1])
				}
				if sim < -1.01 || sim > 1.01 {
					t.Errorf("cosine out of range: %.4f", sim)
				}
			}
		})
	}
}

// TestDeterministic verifies that embedding the same text twice yields the same result.
func TestDeterministic(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			text := "deterministic embedding test"
			v1, err := p.Embed(context.Background(), []string{text})
			if err != nil {
				t.Fatalf("embed1: %v", err)
			}
			v2, err := p.Embed(context.Background(), []string{text})
			if err != nil {
				t.Fatalf("embed2: %v", err)
			}

			sim := cosine(v1[0], v2[0])
			if sim < 0.9999 {
				t.Errorf("same text produced different embeddings: cosine=%.6f", sim)
			}
		})
	}
}

// TestBatchEmbedding verifies that batch embedding produces same results as individual calls.
func TestBatchEmbedding(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	texts := []string{
		"first document about Go programming",
		"second document about Python scripting",
		"third document about database design",
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			batchVecs, err := p.Embed(context.Background(), texts)
			if err != nil {
				t.Fatalf("batch embed: %v", err)
			}
			if len(batchVecs) != len(texts) {
				t.Fatalf("batch returned %d vecs, want %d", len(batchVecs), len(texts))
			}

			for i, text := range texts {
				single, err := p.Embed(context.Background(), []string{text})
				if err != nil {
					t.Fatalf("single embed[%d]: %v", i, err)
				}
				sim := cosine(batchVecs[i], single[0])
				if sim < 0.9999 {
					t.Errorf("batch vs single mismatch for text[%d]: cosine=%.6f", i, sim)
				}
			}

			for i, v := range batchVecs {
				norm := vecNorm(v)
				if math.Abs(norm-1.0) > 0.01 {
					t.Errorf("batch vec[%d] norm=%.6f, want ~1.0", i, norm)
				}
			}
		})
	}
}

// TestTokenCount verifies tokenizer returns sensible counts.
func TestTokenCount(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			cases := []struct {
				text   string
				minTok int
				maxTok int
			}{
				{"hello", 1, 5},
				{"hello world this is a test", 4, 15},
				{"func main() { fmt.Println(\"hello\") }", 5, 30},
			}

			for _, tc := range cases {
				n := p.TokenCount(tc.text)
				t.Logf("TokenCount(%q) = %d", tc.text[:min(30, len(tc.text))], n)
				if n < tc.minTok || n > tc.maxTok {
					t.Errorf("TokenCount(%q) = %d, want [%d, %d]", tc.text, n, tc.minTok, tc.maxTok)
				}
			}
		})
	}
}

// TestLongTextTruncation verifies that text exceeding max context doesn't crash.
func TestLongTextTruncation(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			longText := strings.Repeat("the quick brown fox jumps over the lazy dog ", 2000)
			t.Logf("long text: %d chars", len(longText))

			vecs, err := p.Embed(context.Background(), []string{longText})
			if err != nil {
				t.Fatalf("embed long text: %v", err)
			}

			norm := vecNorm(vecs[0])
			t.Logf("norm=%.6f", norm)
			if math.Abs(norm-1.0) > 0.01 {
				t.Errorf("long text norm=%.6f, want ~1.0", norm)
			}
		})
	}
}

// TestEmptyBatch verifies that empty input returns nil without error.
func TestEmptyBatch(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	m := models[0]
	p := loadProvider(t, m.path)
	defer p.Close()

	vecs, err := p.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("empty batch error: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty batch, got %d vecs", len(vecs))
	}
}

// TestSemanticSimilarity verifies that related texts are more similar than unrelated ones.
func TestSemanticSimilarity(t *testing.T) {
	models := availableModels(t)
	if len(models) == 0 {
		t.Skip("no models cached")
	}

	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p := loadProvider(t, m.path)
			defer p.Close()

			texts := []string{
				"how to sort a list in python",                             // [0] query
				"def quicksort(lst): return sorted(lst)",                   // [1] related code
				"the weather in paris is sunny and warm during the summer", // [2] unrelated SQL
			}

			vecs, err := p.Embed(context.Background(), texts)
			if err != nil {
				t.Fatalf("embed: %v", err)
			}

			simRelated := cosine(vecs[0], vecs[1])
			simUnrelated := cosine(vecs[0], vecs[2])
			t.Logf("sim(query, related)=%.4f  sim(query, unrelated)=%.4f", simRelated, simUnrelated)

			if simRelated <= simUnrelated {
				t.Errorf("expected related > unrelated: %.4f <= %.4f", simRelated, simUnrelated)
			}
		})
	}
}
