package local

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestEmbeddingThroughput measures per-call latency and projects 3K-file overhead.
// Runs only 20 inference calls to keep execution fast, then extrapolates.
func TestEmbeddingThroughput(t *testing.T) {
	modelPath := "../models/bge-small-en-v1.5-Q8_0.gguf"
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("GGUF model not found")
	}

	modelData, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read model: %v", err)
	}

	t0 := time.Now()
	p, err := New(modelData)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()
	initTime := time.Since(t0)

	ctx := context.Background()

	// Warmup (first call may trigger lazy compilation)
	t1 := time.Now()
	_, err = p.Embed(ctx, []string{"warmup"})
	if err != nil {
		t.Fatalf("warmup: %v", err)
	}
	warmupTime := time.Since(t1)

	texts := []string{
		"func main() { fmt.Println(\"hello\") }",
		"package http\n\nimport \"net/http\"\n\nfunc HandleRequest(w http.ResponseWriter, r *http.Request) {\n\tw.Write([]byte(\"ok\"))\n}",
		"class UserService {\n  constructor(private db: Database) {}\n  async getUser(id: string): Promise<User> {\n    return this.db.findOne({ id })\n  }\n}",
		"def train_model(data, epochs=10, lr=0.001):\n    model = Sequential()\n    model.add(Dense(128, activation='relu'))\n    model.compile(optimizer=Adam(lr=lr), loss='mse')\n    model.fit(data, epochs=epochs)\n    return model",
	}

	// 20 calls to get stable average
	const N = 20
	start := time.Now()
	for i := range N {
		_, err := p.Embed(ctx, []string{texts[i%len(texts)]})
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
	}
	total := time.Since(start)
	avg := total / N

	chunksPerFile := 3
	proj3K := time.Duration(int64(avg) * int64(3000*chunksPerFile))
	proj10K := time.Duration(int64(avg) * int64(10000*chunksPerFile))

	t.Logf("Init (model load): %s", initTime)
	t.Logf("First call (warmup):              %s", warmupTime)
	t.Logf("Steady-state per call:            %s (avg of %d)", avg, N)
	t.Logf("Throughput:                       %.1f embeddings/sec", float64(N)/total.Seconds())
	t.Logf("--- Projections ---")
	t.Logf("3K files × %d chunks = %d calls → %s", chunksPerFile, 3000*chunksPerFile, proj3K)
	t.Logf("10K files × %d chunks = %d calls → %s", chunksPerFile, 10000*chunksPerFile, proj10K)

	fmt.Fprintf(os.Stderr, "\n=== EMBEDDING OVERHEAD ===\n")
	fmt.Fprintf(os.Stderr, "Init:       %s\n", initTime)
	fmt.Fprintf(os.Stderr, "Per-call:   %s\n", avg)
	fmt.Fprintf(os.Stderr, "3K files:   %s (%d calls)\n", proj3K, 3000*chunksPerFile)
	fmt.Fprintf(os.Stderr, "10K files:  %s (%d calls)\n", proj10K, 10000*chunksPerFile)
	fmt.Fprintf(os.Stderr, "==========================\n")
}

// skipIfMissing checks whether bert.wasm and the GGUF model are present.
// Returns the model data if both are available, or skips the benchmark.
func skipIfMissing(b *testing.B) []byte {
	b.Helper()
	modelPath := "../models/bge-small-en-v1.5-Q8_0.gguf"
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		b.Skip("GGUF model not found")
	}
	data, err := os.ReadFile(modelPath)
	if err != nil {
		b.Fatalf("read model: %v", err)
	}
	return data
}

// BenchmarkEmbed_SingleText measures per-call latency for a single text.
func BenchmarkEmbed_SingleText(b *testing.B) {
	modelData := skipIfMissing(b)
	p, err := New(modelData)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer p.Close()

	ctx := context.Background()
	text := "func handleRequest(w http.ResponseWriter, r *http.Request) { }"

	p.Embed(ctx, []string{text}) //nolint:errcheck

	b.ResetTimer()
	for range b.N {
		_, err := p.Embed(ctx, []string{text})
		if err != nil {
			b.Fatalf("embed: %v", err)
		}
	}
}

// BenchmarkEmbed_Batch4 measures throughput for a batch of 4 texts (one per worker).
func BenchmarkEmbed_Batch4(b *testing.B) {
	modelData := skipIfMissing(b)
	p, err := New(modelData)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer p.Close()

	ctx := context.Background()
	texts := []string{
		"func main() { fmt.Println(\"hello\") }",
		"package http\n\nfunc HandleRequest(w http.ResponseWriter, r *http.Request) { }",
		"class UserService { constructor(private db: Database) {} }",
		"def train_model(data, epochs=10): pass",
	}

	p.Embed(ctx, texts[:1]) //nolint:errcheck

	b.ResetTimer()
	for range b.N {
		_, err := p.Embed(ctx, texts)
		if err != nil {
			b.Fatalf("embed: %v", err)
		}
	}
}

// BenchmarkProviderInit measures cold start time (model load).
func BenchmarkProviderInit(b *testing.B) {
	modelData := skipIfMissing(b)

	for range b.N {
		p, err := New(modelData)
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		p.Close() //nolint:errcheck
	}
}
