package bbolt

import (
	"encoding/binary"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketEmbeddings    = []byte("embeddings")
	bucketEmbeddingMeta = []byte("embedding_meta")
)

// EmbeddingStore provides vector storage in a dedicated bbolt bucket.
// Keys are node IDs; values are raw little-endian float32 vectors (4 bytes/dim).
type EmbeddingStore struct {
	db     *bolt.DB
	ownsDB bool
}

// NewEmbeddingStore opens (or creates) a bbolt database at the given
// path and returns an EmbeddingStore that owns the DB connection.
func NewEmbeddingStore(path string) (*EmbeddingStore, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{
		NoSync:         true,
		NoFreelistSync: true,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding store: open %s: %w", path, err)
	}
	return newEmbeddingStore(db, true)
}

// NewEmbeddingStoreFromDB creates an EmbeddingStore reusing an existing
// bbolt DB connection (e.g. the same DB as the graph store). The caller
// retains ownership of the DB — Close will not close it.
func NewEmbeddingStoreFromDB(db *bolt.DB) (*EmbeddingStore, error) {
	return newEmbeddingStore(db, false)
}

func newEmbeddingStore(db *bolt.DB, ownsDB bool) (*EmbeddingStore, error) {
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketEmbeddings); err != nil {
			return fmt.Errorf("create embeddings bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(bucketEmbeddingMeta); err != nil {
			return fmt.Errorf("create embedding_meta bucket: %w", err)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("embedding store: create bucket: %w", err)
	}
	return &EmbeddingStore{db: db, ownsDB: ownsDB}, nil
}

// Put stores a vector for the given node ID.
func (s *EmbeddingStore) Put(nodeID string, vector []float32) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		return b.Put([]byte(nodeID), encodeVector(vector))
	}); err != nil {
		return fmt.Errorf("embedding store: put: %w", err)
	}
	return nil
}

// EmbeddingEntry is a node ID + vector pair for batch operations.
type EmbeddingEntry struct {
	NodeID string
	Vector []float32
}

// BatchPut stores multiple vectors in a single transaction.
func (s *EmbeddingStore) BatchPut(entries []EmbeddingEntry) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		for _, e := range entries {
			if err := b.Put([]byte(e.NodeID), encodeVector(e.Vector)); err != nil {
				return fmt.Errorf("put %q: %w", e.NodeID, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("embedding store: batch put: %w", err)
	}
	return nil
}

// Get retrieves the vector for a node ID. Returns nil, nil if not found.
func (s *EmbeddingStore) Get(nodeID string) ([]float32, error) {
	var vec []float32
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		data := b.Get([]byte(nodeID))
		if data == nil {
			return nil
		}
		vec = decodeVector(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embedding store: get: %w", err)
	}
	return vec, nil
}

// Has returns true if a vector exists for the given node ID.
func (s *EmbeddingStore) Has(nodeID string) (bool, error) {
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		found = b.Get([]byte(nodeID)) != nil
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("embedding store: has: %w", err)
	}
	return found, nil
}

// Delete removes vectors and metadata for the given node IDs.
func (s *EmbeddingStore) Delete(nodeIDs []string) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		vecs := tx.Bucket(bucketEmbeddings)
		meta := tx.Bucket(bucketEmbeddingMeta)
		for _, id := range nodeIDs {
			key := []byte(id)
			if err := vecs.Delete(key); err != nil {
				return fmt.Errorf("delete %q: %w", id, err)
			}
			_ = meta.Delete(key) // best-effort
		}
		return nil
	}); err != nil {
		return fmt.Errorf("embedding store: delete: %w", err)
	}
	return nil
}

// Scan iterates over all stored embeddings, calling fn for each.
// Return false from fn to stop iteration.
func (s *EmbeddingStore) Scan(fn func(nodeID string, vector []float32) bool) error {
	if err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if !fn(string(k), decodeVector(v)) {
				break
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("embedding store: scan: %w", err)
	}
	return nil
}

// HasBatch checks which node IDs already have stored vectors.
// Returns a set of node IDs that exist in the store.
func (s *EmbeddingStore) HasBatch(nodeIDs []string) (map[string]bool, error) {
	result := make(map[string]bool, len(nodeIDs))
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		for _, id := range nodeIDs {
			if b.Get([]byte(id)) != nil {
				result[id] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embedding store: has batch: %w", err)
	}
	return result, nil
}

// Count returns the number of stored embeddings.
func (s *EmbeddingStore) Count() (int, error) {
	var n int
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		n = b.Stats().KeyN
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("embedding store: count: %w", err)
	}
	return n, nil
}

// Clear removes all stored embeddings and metadata.
func (s *EmbeddingStore) Clear() error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketEmbeddings, bucketEmbeddingMeta} {
			if err := tx.DeleteBucket(name); err != nil {
				return fmt.Errorf("delete bucket %s: %w", name, err)
			}
			if _, err := tx.CreateBucket(name); err != nil {
				return fmt.Errorf("recreate bucket %s: %w", name, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("embedding store: clear: %w", err)
	}
	return nil
}

// EmbeddingEntryWithHash extends EmbeddingEntry with a content hash
// for staleness detection during incremental embedding.
type EmbeddingEntryWithHash struct {
	NodeID      string
	Vector      []float32
	ContentHash string
}

// BatchPutWithHash stores vectors and their content hashes in a single
// transaction. The vector goes into the embeddings bucket; the hash
// goes into embedding_meta.
func (s *EmbeddingStore) BatchPutWithHash(entries []EmbeddingEntryWithHash) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		vecs := tx.Bucket(bucketEmbeddings)
		meta := tx.Bucket(bucketEmbeddingMeta)
		for _, e := range entries {
			key := []byte(e.NodeID)
			if err := vecs.Put(key, encodeVector(e.Vector)); err != nil {
				return fmt.Errorf("put vec %q: %w", e.NodeID, err)
			}
			if e.ContentHash != "" {
				if err := meta.Put(key, []byte(e.ContentHash)); err != nil {
					return fmt.Errorf("put hash %q: %w", e.NodeID, err)
				}
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("embedding store: batch put with hash: %w", err)
	}
	return nil
}

// GetHashBatch returns stored content hashes for the given node IDs.
// Missing entries are omitted from the result map.
func (s *EmbeddingStore) GetHashBatch(nodeIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(nodeIDs))
	err := s.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketEmbeddingMeta)
		for _, id := range nodeIDs {
			if v := meta.Get([]byte(id)); v != nil {
				result[id] = string(v)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embedding store: get hash batch: %w", err)
	}
	return result, nil
}

// NodeIDs returns all stored node IDs (keys only, no vector decode).
func (s *EmbeddingStore) NodeIDs() ([]string, error) {
	var ids []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			ids = append(ids, string(k))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embedding store: node ids: %w", err)
	}
	return ids, nil
}

// DeleteByPrefix removes all embeddings and metadata whose node ID
// starts with the given prefix. Returns the number of deleted entries.
func (s *EmbeddingStore) DeleteByPrefix(prefix string) (int, error) {
	var deleted int
	pfx := []byte(prefix)
	if err := s.db.Update(func(tx *bolt.Tx) error {
		vecs := tx.Bucket(bucketEmbeddings)
		meta := tx.Bucket(bucketEmbeddingMeta)
		c := vecs.Cursor()
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			if err := c.Delete(); err != nil {
				return fmt.Errorf("delete key: %w", err)
			}
			_ = meta.Delete(k) // best-effort, may not exist
			deleted++
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("embedding store: delete by prefix: %w", err)
	}
	return deleted, nil
}

func hasPrefix(s, prefix []byte) bool {
	return len(s) >= len(prefix) && string(s[:len(prefix)]) == string(prefix)
}

// Close releases resources. If the store owns the DB, it closes it.
func (s *EmbeddingStore) Close() error {
	if s.ownsDB {
		if err := s.db.Close(); err != nil {
			return fmt.Errorf("embedding store: close: %w", err)
		}
	}
	return nil
}

// encodeVector encodes a float32 slice as little-endian bytes.
func encodeVector(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeVector decodes a little-endian byte slice back to float32.
func decodeVector(data []byte) []float32 {
	n := len(data) / 4
	vec := make([]float32, n)
	for i := range n {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}
