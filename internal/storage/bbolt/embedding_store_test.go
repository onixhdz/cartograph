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

func TestEmbeddingStore_BatchPutWithHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	entries := []EmbeddingEntryWithHash{
		{NodeID: "n1", Vector: []float32{0.1, 0.2}, ContentHash: "hash_a"},
		{NodeID: "n2", Vector: []float32{0.3, 0.4}, ContentHash: "hash_b"},
		{NodeID: "n3", Vector: []float32{0.5, 0.6}, ContentHash: ""},
	}
	if err := s.BatchPutWithHash(entries); err != nil {
		t.Fatalf("batch put with hash: %v", err)
	}

	vec, err := s.Get("n1")
	if err != nil || vec == nil {
		t.Fatalf("get n1: %v", err)
	}
	if len(vec) != 2 {
		t.Errorf("n1 vec len: got %d, want 2", len(vec))
	}

	hashes, err := s.GetHashBatch([]string{"n1", "n2", "n3", "n4"})
	if err != nil {
		t.Fatalf("get hash batch: %v", err)
	}
	if hashes["n1"] != "hash_a" {
		t.Errorf("n1 hash: got %q, want %q", hashes["n1"], "hash_a")
	}
	if hashes["n2"] != "hash_b" {
		t.Errorf("n2 hash: got %q, want %q", hashes["n2"], "hash_b")
	}
	if _, ok := hashes["n3"]; ok {
		t.Errorf("n3 should have no hash (empty), got %q", hashes["n3"])
	}
	if _, ok := hashes["n4"]; ok {
		t.Errorf("n4 should not exist")
	}
}

func TestEmbeddingStore_NodeIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.BatchPut([]EmbeddingEntry{
		{NodeID: "a", Vector: []float32{1}},
		{NodeID: "b", Vector: []float32{2}},
		{NodeID: "c", Vector: []float32{3}},
	}); err != nil {
		t.Fatalf("batch put: %v", err)
	}

	ids, err := s.NodeIDs()
	if err != nil {
		t.Fatalf("node ids: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("got %d ids, want 3", len(ids))
	}
}

func TestEmbeddingStore_DeleteByPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.BatchPutWithHash([]EmbeddingEntryWithHash{
		{NodeID: "file:src/a.go:func:foo", Vector: []float32{1}, ContentHash: "h1"},
		{NodeID: "file:src/a.go:func:bar", Vector: []float32{2}, ContentHash: "h2"},
		{NodeID: "file:src/b.go:func:baz", Vector: []float32{3}, ContentHash: "h3"},
	}); err != nil {
		t.Fatalf("batch put: %v", err)
	}

	deleted, err := s.DeleteByPrefix("file:src/a.go:")
	if err != nil {
		t.Fatalf("delete by prefix: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted: got %d, want 2", deleted)
	}

	has, _ := s.Has("file:src/b.go:func:baz")
	if !has {
		t.Error("b.go entry should still exist")
	}

	has, _ = s.Has("file:src/a.go:func:foo")
	if has {
		t.Error("a.go:foo should be deleted")
	}

	hashes, _ := s.GetHashBatch([]string{"file:src/a.go:func:foo", "file:src/b.go:func:baz"})
	if _, ok := hashes["file:src/a.go:func:foo"]; ok {
		t.Error("a.go:foo hash should be deleted")
	}
	if hashes["file:src/b.go:func:baz"] != "h3" {
		t.Errorf("b.go:baz hash: got %q, want %q", hashes["file:src/b.go:func:baz"], "h3")
	}
}

func TestEmbeddingStore_DeleteCleansMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.BatchPutWithHash([]EmbeddingEntryWithHash{
		{NodeID: "n1", Vector: []float32{1}, ContentHash: "h1"},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := s.Delete([]string{"n1"}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	hashes, _ := s.GetHashBatch([]string{"n1"})
	if _, ok := hashes["n1"]; ok {
		t.Error("hash should be deleted after Delete()")
	}
}

func TestEmbeddingStore_ClearBothBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.BatchPutWithHash([]EmbeddingEntryWithHash{
		{NodeID: "n1", Vector: []float32{1}, ContentHash: "h1"},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	count, _ := s.Count()
	if count != 0 {
		t.Errorf("count after clear: got %d, want 0", count)
	}

	hashes, _ := s.GetHashBatch([]string{"n1"})
	if _, ok := hashes["n1"]; ok {
		t.Error("hash should be cleared")
	}
}

func TestEmbeddingStore_OpenExistingWithoutMetaBucket(t *testing.T) {
	// Backward compat: DB without embedding_meta bucket should open fine.
	path := filepath.Join(t.TempDir(), "legacy.db")

	s, err := NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("n1", []float32{1}); err != nil {
		t.Fatalf("put: %v", err)
	}
	hashes, err := s.GetHashBatch([]string{"n1"})
	if err != nil {
		t.Fatalf("get hash: %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("legacy node should have no hash, got %v", hashes)
	}
}
