// Package bbolt content_store provides a zstd-compressed key-value content
// bucket inside a BBolt database. It stores full file content for repositories
// analyzed in-memory (no source on disk), keyed by relative file path.
package bbolt

import (
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
	bolt "go.etcd.io/bbolt"
)

var bucketContent = []byte("content")

// ContentStore provides zstd-compressed file content storage inside a BBolt
// database. It is used to persist full file content for in-memory analyzed
// repos where source files do not exist on disk.
type ContentStore struct {
	db     *bolt.DB
	enc    *zstd.Encoder
	dec    *zstd.Decoder
	ownsDB bool // true if we opened the DB and should close it
}

// initStore creates the content bucket and zstd codecs for a BBolt DB.
func initStore(db *bolt.DB, ownsDB bool) (*ContentStore, error) {
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketContent)
		if err != nil {
			return fmt.Errorf("create content bucket: %w", err)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("content store: create bucket: %w", err)
	}

	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("content store: zstd encoder: %w", err)
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("content store: zstd decoder: %w", err)
	}

	return &ContentStore{db: db, enc: enc, dec: dec, ownsDB: ownsDB}, nil
}

// NewContentStore opens (or creates) a BBolt database at the given path
// and returns a ContentStore that owns the DB (Close will close it).
func NewContentStore(path string) (*ContentStore, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("content store: open %s: %w", path, err)
	}

	cs, err := initStore(db, true)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return cs, nil
}

// NewContentStoreFromDB wraps an already-open BBolt database as a
// ContentStore. The caller retains ownership of db — Close will
// release zstd resources but will not close the DB.
func NewContentStoreFromDB(db *bolt.DB) (*ContentStore, error) {
	return initStore(db, false)
}

// Put stores file content under the given relative path. The data is
// zstd-compressed before writing.
func (cs *ContentStore) Put(path string, data []byte) error {
	compressed := cs.enc.EncodeAll(data, nil)
	if err := cs.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return errors.New("content store: bucket not found")
		}
		return bkt.Put([]byte(path), compressed)
	}); err != nil {
		return fmt.Errorf("content store: put %q: %w", path, err)
	}
	return nil
}

// PutBatch stores multiple files in a single BBolt transaction.
// This is significantly faster than calling Put in a loop.
func (cs *ContentStore) PutBatch(files map[string][]byte) error {
	if err := cs.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return errors.New("content store: bucket not found")
		}
		for path, data := range files {
			compressed := cs.enc.EncodeAll(data, nil)
			if err := bkt.Put([]byte(path), compressed); err != nil {
				return fmt.Errorf("content store: put %q: %w", path, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("content store: put batch: %w", err)
	}
	return nil
}

// Get retrieves and decompresses file content for the given path.
// Returns an error if the path is not found.
func (cs *ContentStore) Get(path string) ([]byte, error) {
	var result []byte
	err := cs.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return errors.New("content store: bucket not found")
		}
		compressed := bkt.Get([]byte(path))
		if compressed == nil {
			return fmt.Errorf("content store: %q not found", path)
		}
		var err error
		result, err = cs.dec.DecodeAll(compressed, nil)
		if err != nil {
			return fmt.Errorf("content store: decompress %q: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("content store: get: %w", err)
	}
	return result, nil
}

// Has reports whether the content store contains data for the given path.
func (cs *ContentStore) Has(path string) bool {
	found := false
	_ = cs.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return nil
		}
		found = bkt.Get([]byte(path)) != nil
		return nil
	})
	return found
}

// Delete removes the content for the given path.
func (cs *ContentStore) Delete(path string) error {
	if err := cs.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return nil
		}
		return bkt.Delete([]byte(path))
	}); err != nil {
		return fmt.Errorf("content store: delete: %w", err)
	}
	return nil
}

// Count returns the number of files stored in the content bucket.
func (cs *ContentStore) Count() int {
	count := 0
	_ = cs.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return nil
		}
		count = bkt.Stats().KeyN
		return nil
	})
	return count
}

// Paths returns all stored file paths.
func (cs *ContentStore) Paths() []string {
	var paths []string
	_ = cs.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketContent)
		if bkt == nil {
			return nil
		}
		return bkt.ForEach(func(k, _ []byte) error {
			paths = append(paths, string(k))
			return nil
		})
	})
	return paths
}

// Close releases zstd resources. If the ContentStore owns the DB
// (created via NewContentStore), it also closes the DB. If created
// via NewContentStoreFromDB, the DB is left open.
func (cs *ContentStore) Close() error {
	_ = cs.enc.Close()
	cs.dec.Close()
	if cs.ownsDB {
		if err := cs.db.Close(); err != nil {
			return fmt.Errorf("content store: close: %w", err)
		}
	}
	return nil
}
