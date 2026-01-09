package bbolt

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddingStore_PutGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	vec := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	if err := s.Put("node1", vec); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := s.Get("node1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != len(vec) {
		t.Fatalf("len: got %d, want %d", len(got), len(vec))
	}
	for i := range vec {
		if math.Abs(float64(got[i]-vec[i])) > 1e-7 {
			t.Errorf("vec[%d]: got %f, want %f", i, got[i], vec[i])
		}
	}
}

func TestEmbeddingStore_GetNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	got, err := s.Get("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing key, got %v", got)
	}
}

func TestEmbeddingStore_BatchPut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	entries := []EmbeddingEntry{
		{NodeID: "a", Vector: []float32{1.0, 2.0}},
		{NodeID: "b", Vector: []float32{3.0, 4.0}},
		{NodeID: "c", Vector: []float32{5.0, 6.0}},
	}
	if err := s.BatchPut(entries); err != nil {
		t.Fatalf("batch put: %v", err)
	}

	for _, e := range entries {
		got, err := s.Get(e.NodeID)
		if err != nil {
			t.Fatalf("get %s: %v", e.NodeID, err)
		}
		if len(got) != len(e.Vector) {
			t.Errorf("%s: len %d, want %d", e.NodeID, len(got), len(e.Vector))
		}
	}

	n, err := s.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("count: got %d, want 3", n)
	}
}

func TestEmbeddingStore_Has(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("x", []float32{1.0}); err != nil {
		t.Fatal(err)
	}

	has, _ := s.Has("x")
	if !has {
		t.Error("expected Has(x) = true")
	}

	has, _ = s.Has("y")
	if has {
		t.Error("expected Has(y) = false")
	}
}

func TestEmbeddingStore_Delete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("a", []float32{1.0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("b", []float32{2.0}); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete([]string{"a"}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got, _ := s.Get("a")
	if got != nil {
		t.Error("expected nil after delete")
	}

	got, _ = s.Get("b")
	if got == nil {
		t.Error("expected b to survive delete of a")
	}
}

func TestEmbeddingStore_Scan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("n1", []float32{0.1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("n2", []float32{0.2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("n3", []float32{0.3}); err != nil {
		t.Fatal(err)
	}

	var collected []string
	err = s.Scan(func(nodeID string, _ []float32) bool {
		collected = append(collected, nodeID)
		return true
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(collected) != 3 {
		t.Errorf("scan: got %d entries, want 3", len(collected))
	}
}

func TestEmbeddingStore_Scan_StopEarly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("a", []float32{1.0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("b", []float32{2.0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("c", []float32{3.0}); err != nil {
		t.Fatal(err)
	}

	count := 0
	if err := s.Scan(func(_ string, _ []float32) bool {
		count++
		return count < 2 // stop after 2
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected scan to stop after 2, got %d", count)
	}
}

func TestEmbeddingStore_Clear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("a", []float32{1.0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("b", []float32{2.0}); err != nil {
		t.Fatal(err)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	n, _ := s.Count()
	if n != 0 {
		t.Errorf("after clear: count=%d, want 0", n)
	}
}

func TestEmbeddingStore_Overwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("x", []float32{1.0, 2.0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("x", []float32{3.0, 4.0}); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Get("x")
	if len(got) != 2 || got[0] != 3.0 {
		t.Errorf("expected overwritten value [3, 4], got %v", got)
	}

	n, _ := s.Count()
	if n != 1 {
		t.Errorf("count after overwrite: got %d, want 1", n)
	}
}

func TestEmbeddingStore_384Dim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	vec := make([]float32, 384)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}

	if err := s.Put("large", vec); err != nil {
		t.Fatalf("put 384d: %v", err)
	}

	got, err := s.Get("large")
	if err != nil {
		t.Fatalf("get 384d: %v", err)
	}
	if len(got) != 384 {
		t.Fatalf("expected 384 dims, got %d", len(got))
	}
	for i := range vec {
		if math.Abs(float64(got[i]-vec[i])) > 1e-7 {
			t.Errorf("dim[%d]: got %f, want %f", i, got[i], vec[i])
		}
	}

	// Verify on-disk size is ~1536 bytes per vector.
	info, _ := os.Stat(path)
	t.Logf("DB size with one 384d vector: %d bytes", info.Size())
}

func TestEncodeDecodeVector(t *testing.T) {
	vec := []float32{-1.5, 0.0, 3.14159, math.MaxFloat32, math.SmallestNonzeroFloat32}
	encoded := encodeVector(vec)
	decoded := decodeVector(encoded)

	if len(decoded) != len(vec) {
		t.Fatalf("len: got %d, want %d", len(decoded), len(vec))
	}
	for i := range vec {
		if decoded[i] != vec[i] {
			t.Errorf("[%d]: got %v, want %v", i, decoded[i], vec[i])
		}
	}
}
