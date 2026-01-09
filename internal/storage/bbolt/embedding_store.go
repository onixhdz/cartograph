package bbolt

import (
	"encoding/binary"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"
)

var bucketEmbeddings = []byte("embeddings")

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
		_, err := tx.CreateBucketIfNotExists(bucketEmbeddings)
		return err
	}); err != nil {
		return nil, fmt.Errorf("embedding store: create bucket: %w", err)
	}
	return &EmbeddingStore{db: db, ownsDB: ownsDB}, nil
}

// Put stores a vector for the given node ID.
func (s *EmbeddingStore) Put(nodeID string, vector []float32) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		return b.Put([]byte(nodeID), encodeVector(vector))
	})
}

// EmbeddingEntry is a node ID + vector pair for batch operations.
type EmbeddingEntry struct {
	NodeID string
	Vector []float32
}

// BatchPut stores multiple vectors in a single transaction.
func (s *EmbeddingStore) BatchPut(entries []EmbeddingEntry) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		for _, e := range entries {
			if err := b.Put([]byte(e.NodeID), encodeVector(e.Vector)); err != nil {
				return err
			}
		}
		return nil
	})
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
	return vec, err
}

// Has returns true if a vector exists for the given node ID.
func (s *EmbeddingStore) Has(nodeID string) (bool, error) {
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		found = b.Get([]byte(nodeID)) != nil
		return nil
	})
	return found, err
}

// Delete removes vectors for the given node IDs.
func (s *EmbeddingStore) Delete(nodeIDs []string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		for _, id := range nodeIDs {
			if err := b.Delete([]byte(id)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Scan iterates over all stored embeddings, calling fn for each.
// Return false from fn to stop iteration.
func (s *EmbeddingStore) Scan(fn func(nodeID string, vector []float32) bool) error {
	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if !fn(string(k), decodeVector(v)) {
				break
			}
		}
		return nil
	})
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
	return result, err
}

// Count returns the number of stored embeddings.
func (s *EmbeddingStore) Count() (int, error) {
	var n int
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEmbeddings)
		n = b.Stats().KeyN
		return nil
	})
	return n, err
}

// Clear removes all stored embeddings.
func (s *EmbeddingStore) Clear() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bucketEmbeddings); err != nil {
			return err
		}
		_, err := tx.CreateBucket(bucketEmbeddings)
		return err
	})
}

// Close releases resources. If the store owns the DB, it closes it.
func (s *EmbeddingStore) Close() error {
	if s.ownsDB {
		return s.db.Close()
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
