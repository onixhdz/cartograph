package embedding

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeEmbeddingServer returns an httptest.Server that responds to
// POST /v1/embeddings with deterministic embedding vectors.
func fakeEmbeddingServer(t *testing.T, dims int, wantModel string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if wantModel != "" && req.Model != wantModel {
			resp := errorResponse{}
			resp.Error.Message = "model mismatch: got " + req.Model
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Generate deterministic embeddings: each text gets a vector
		// where element[i] = float32(textIndex + i) / dims, then normalized.
		data := make([]struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}, len(req.Input))

		for idx := range req.Input {
			vec := make([]float32, dims)
			var norm float64
			for i := range vec {
				v := float32(idx+i+1) / float32(dims)
				vec[i] = v
				norm += float64(v * v)
			}
			norm = math.Sqrt(norm)
			for i := range vec {
				vec[i] = float32(float64(vec[i]) / norm)
			}
			data[idx] = struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: vec, Index: idx}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestOpenAICompat_Embed(t *testing.T) {
	srv := fakeEmbeddingServer(t, 384, "test-model")
	defer srv.Close()

	p, err := NewOpenAICompatProvider(srv.URL, "test-model", "")
	if err != nil {
		t.Fatalf("NewOpenAICompatProvider: %v", err)
	}
	defer p.Close()

	texts := []string{"hello world", "foo bar baz"}
	vecs, err := p.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	for i, vec := range vecs {
		if len(vec) != 384 {
			t.Errorf("vector[%d] has %d dims, want 384", i, len(vec))
		}
	}

	if got := p.Dimensions(); got != 384 {
		t.Errorf("Dimensions() = %d, want 384", got)
	}
}

func TestOpenAICompat_EmptyInput(t *testing.T) {
	p, err := NewOpenAICompatProvider("http://unused", "m", "")
	if err != nil {
		t.Fatalf("NewOpenAICompatProvider: %v", err)
	}
	defer p.Close()

	vecs, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil, got %v", vecs)
	}
}

func TestOpenAICompat_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: make([]float32, 8), Index: 0},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := NewOpenAICompatProvider(srv.URL, "m", "sk-test-key")
	defer p.Close()

	_, err := p.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
}

func TestOpenAICompat_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: struct {
			Message string `json:"message"`
		}{Message: "model not found"}})
	}))
	defer srv.Close()

	p, _ := NewOpenAICompatProvider(srv.URL, "missing-model", "")
	defer p.Close()

	_, err := p.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if got := err.Error(); !contains(got, "500") || !contains(got, "model not found") {
		t.Errorf("error = %q, want to contain '500' and 'model not found'", got)
	}
}

func TestOpenAICompat_Name(t *testing.T) {
	p, _ := NewOpenAICompatProvider("http://localhost:11434", "all-minilm", "")
	defer p.Close()

	got := p.Name()
	want := "openai-compat(http://localhost:11434, all-minilm)"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestOpenAICompat_TrailingSlash(t *testing.T) {
	srv := fakeEmbeddingServer(t, 4, "m")
	defer srv.Close()

	p, _ := NewOpenAICompatProvider(srv.URL+"/", "m", "")
	defer p.Close()

	vecs, err := p.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vecs))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
