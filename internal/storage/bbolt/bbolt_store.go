// Package bbolt implements the storage.GraphStore interface using bbolt
// (an embedded key-value store) with msgpack serialization.
package bbolt

import (
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
	bolt "go.etcd.io/bbolt"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/storage"
)

var (
	bucketNodes = []byte("nodes")
	bucketEdges = []byte("edges")
	bucketMeta  = []byte("meta")
	keyAll      = []byte("all")
)

// byteWriter is a minimal io.Writer backed by a growing byte slice.
type byteWriter struct{ buf []byte }

func (w *byteWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// Store is a bbolt-backed implementation of storage.GraphStore.
type Store struct {
	db   *bolt.DB
	path string
}

// compile-time interface check
var _ storage.GraphStore = (*Store)(nil)

// New opens (or creates) a bbolt database at the given path and returns a Store.
func New(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{
		NoSync:         true,
		NoFreelistSync: true,
	})
	if err != nil {
		return nil, fmt.Errorf("bbolt: open %s: %w", path, err)
	}
	return &Store{db: db, path: path}, nil
}

// SaveGraph persists the entire in-memory graph into bbolt using msgpack.
// Nodes and edges are each stored as a single blob to minimize B+‑tree overhead.
func (s *Store) SaveGraph(g *lpg.Graph) error {
	// Pre-serialize everything outside the transaction to minimize
	// the time spent holding the bbolt write lock.
	props := make(map[string]any, 16)
	nodeCount := 0
	edgeCount := 0

	// Collect nodes as a msgpack array via the streaming encoder.
	var nodeBuf []byte
	{
		enc := msgpack.GetEncoder()
		defer msgpack.PutEncoder(enc)

		// Count nodes first so we can write a fixed-length array header.
		graph.ForEachNode(g, func(_ *lpg.Node) bool { nodeCount++; return true })

			nodeBuf = make([]byte, 0, nodeCount*256)
		w := byteWriter{buf: nodeBuf}
		enc.Reset(&w)
		_ = enc.EncodeArrayLen(nodeCount)

		graph.ForEachNode(g, func(node *lpg.Node) bool {
			clear(props)
			node.ForEachProperty(func(key string, value any) bool {
				props[key] = value
				return true
			})
			props["_labels"] = node.GetLabels().Slice()
			props["_id"] = props[graph.PropID]
			_ = enc.EncodeMap(props)
			return true
		})
		nodeBuf = w.buf
	}

	var edgeBuf []byte
	{
		enc := msgpack.GetEncoder()
		defer msgpack.PutEncoder(enc)

		graph.ForEachEdge(g, func(_ *lpg.Edge) bool { edgeCount++; return true })

		edgeBuf = make([]byte, 0, edgeCount*128)
		w := byteWriter{buf: edgeBuf}
		enc.Reset(&w)
		_ = enc.EncodeArrayLen(edgeCount)

		graph.ForEachEdge(g, func(edge *lpg.Edge) bool {
			clear(props)
			edge.ForEachProperty(func(key string, value any) bool {
				props[key] = value
				return true
			})
			props["_from"] = graph.GetStringProp(edge.GetFrom(), graph.PropID)
			props["_to"] = graph.GetStringProp(edge.GetTo(), graph.PropID)
			props["_label"] = edge.GetLabel()
			_ = enc.EncodeMap(props)
			return true
		})
		edgeBuf = w.buf
	}

	meta := map[string]any{
		"nodeCount": nodeCount,
		"edgeCount": edgeCount,
		"version":   1,
	}
	metaData, err := msgpack.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketNodes, bucketEdges, bucketMeta} {
			_ = tx.DeleteBucket(name)
			if _, err := tx.CreateBucket(name); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		if err := tx.Bucket(bucketNodes).Put(keyAll, nodeBuf); err != nil {
			return err
		}
		if err := tx.Bucket(bucketEdges).Put(keyAll, edgeBuf); err != nil {
			return err
		}
		return tx.Bucket(bucketMeta).Put([]byte("info"), metaData)
	})
}

// LoadGraph deserializes the stored graph from bbolt into a new lpg.Graph.
func (s *Store) LoadGraph() (*lpg.Graph, error) {
	g := lpg.NewGraph()
	nodeMap := make(map[string]*lpg.Node)

	err := s.db.View(func(tx *bolt.Tx) error {
		nodesBkt := tx.Bucket(bucketNodes)
		if nodesBkt == nil {
			return fmt.Errorf("bbolt: nodes bucket not found")
		}
		edgesBkt := tx.Bucket(bucketEdges)
		if edgesBkt == nil {
			return fmt.Errorf("bbolt: edges bucket not found")
		}

		nodesBlob := nodesBkt.Get(keyAll)
		if nodesBlob != nil {
			var nodeList []map[string]any
			if err := msgpack.Unmarshal(nodesBlob, &nodeList); err != nil {
				return fmt.Errorf("unmarshal nodes blob: %w", err)
			}
			for _, props := range nodeList {
				labelsRaw, _ := props["_labels"]
				var labels []string
				if arr, ok := labelsRaw.([]any); ok {
					for _, item := range arr {
						if s, ok := item.(string); ok {
							labels = append(labels, s)
						}
					}
				}
				nodeID, _ := props["_id"].(string)
				delete(props, "_labels")
				delete(props, "_id")
				normalizeProps(props)
				node := g.NewNode(labels, props)
				nodeMap[nodeID] = node
			}
		}

		edgesBlob := edgesBkt.Get(keyAll)
		if edgesBlob != nil {
			var edgeList []map[string]any
			if err := msgpack.Unmarshal(edgesBlob, &edgeList); err != nil {
				return fmt.Errorf("unmarshal edges blob: %w", err)
			}
			for _, props := range edgeList {
				fromID, _ := props["_from"].(string)
				toID, _ := props["_to"].(string)
				edgeLabel, _ := props["_label"].(string)
				delete(props, "_from")
				delete(props, "_to")
				delete(props, "_label")
				normalizeProps(props)
				fromNode, ok := nodeMap[fromID]
				if !ok {
					return fmt.Errorf("edge references unknown from-node %q", fromID)
				}
				toNode, ok := nodeMap[toID]
				if !ok {
					return fmt.Errorf("edge references unknown to-node %q", toID)
				}
				g.NewEdge(fromNode, toNode, edgeLabel, props)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return g, nil
}

// Close closes the underlying bbolt database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying bbolt database. This is used to share the
// database with ContentStore (same graph.db file, different bucket).
func (s *Store) DB() *bolt.DB {
	return s.db
}

// normalizeProps converts numeric values that msgpack may deserialize as
// int8/int16/int64/uint* back to the types the graph helpers expect (int,
// float64, bool).
func normalizeProps(props map[string]any) {
	for k, v := range props {
		switch val := v.(type) {
		case int8:
			props[k] = int(val)
		case int16:
			props[k] = int(val)
		case int32:
			props[k] = int(val)
		case int64:
			props[k] = int(val)
		case uint8:
			props[k] = int(val)
		case uint16:
			props[k] = int(val)
		case uint32:
			props[k] = int(val)
		case uint64:
			props[k] = int(val)
		case float32:
			props[k] = float64(val)
		}
	}
}
