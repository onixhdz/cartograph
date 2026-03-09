package plugin

import (
	"errors"
	"runtime"
	"sync/atomic"
	"time"
)

// Default resource limits for plugin ingestion.
const (
	DefaultTimeout  = 5 * time.Minute
	DefaultMaxNodes = 100_000
	DefaultMaxEdges = 500_000
)

// Limit errors.
var (
	ErrNodeLimitExceeded = errors.New("plugin: node emission limit exceeded")
	ErrEdgeLimitExceeded = errors.New("plugin: edge emission limit exceeded")
)

// Limits defines resource constraints for a plugin ingestion run.
type Limits struct {
	// Timeout is the maximum duration for the ingest call.
	// Zero means DefaultTimeout.
	Timeout time.Duration

	// MaxNodes is the maximum number of emit_node notifications allowed.
	// Zero means DefaultMaxNodes. Negative means unlimited.
	MaxNodes int

	// MaxEdges is the maximum number of emit_edge notifications allowed.
	// Zero means DefaultMaxEdges. Negative means unlimited.
	MaxEdges int
}

// effectiveTimeout returns the timeout to use, applying the default.
func (l Limits) effectiveTimeout() time.Duration {
	if l.Timeout > 0 {
		return l.Timeout
	}
	return DefaultTimeout
}

// effectiveMaxNodes returns the max nodes limit, applying the default.
func (l Limits) effectiveMaxNodes() int {
	if l.MaxNodes < 0 {
		return -1 // unlimited
	}
	if l.MaxNodes > 0 {
		return l.MaxNodes
	}
	return DefaultMaxNodes
}

// effectiveMaxEdges returns the max edges limit, applying the default.
func (l Limits) effectiveMaxEdges() int {
	if l.MaxEdges < 0 {
		return -1 // unlimited
	}
	if l.MaxEdges > 0 {
		return l.MaxEdges
	}
	return DefaultMaxEdges
}

// emissionCounter tracks node and edge emission counts and enforces limits.
// All methods are safe for concurrent use.
type emissionCounter struct {
	nodeCount atomic.Int64
	edgeCount atomic.Int64
	maxNodes  int64
	maxEdges  int64
	breachErr atomic.Pointer[error] // stores first limit breach error
}

func newEmissionCounter(limits Limits) *emissionCounter {
	return &emissionCounter{
		maxNodes: int64(limits.effectiveMaxNodes()),
		maxEdges: int64(limits.effectiveMaxEdges()),
	}
}

// onNode increments the node count and returns an error if the limit is exceeded.
func (c *emissionCounter) onNode() error {
	n := c.nodeCount.Add(1)
	if c.maxNodes > 0 && n > c.maxNodes {
		err := ErrNodeLimitExceeded
		c.breachErr.CompareAndSwap(nil, &err)
		return ErrNodeLimitExceeded
	}
	return nil
}

// onEdge increments the edge count and returns an error if the limit is exceeded.
func (c *emissionCounter) onEdge() error {
	n := c.edgeCount.Add(1)
	if c.maxEdges > 0 && n > c.maxEdges {
		err := ErrEdgeLimitExceeded
		c.breachErr.CompareAndSwap(nil, &err)
		return ErrEdgeLimitExceeded
	}
	return nil
}

// nodes returns the current node count.
func (c *emissionCounter) nodes() int64 {
	return c.nodeCount.Load()
}

// edges returns the current edge count.
func (c *emissionCounter) edges() int64 {
	return c.edgeCount.Load()
}

// err returns the first limit breach error, or nil.
func (c *emissionCounter) err() error {
	if p := c.breachErr.Load(); p != nil {
		return *p
	}
	return nil
}

// waitForSettled polls briefly for pending notification goroutines to
// update the counter. This is needed because conn.go dispatches
// notifications in goroutines, and they may not have executed by the
// time the ingest response arrives.
func (c *emissionCounter) waitForSettled(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := c.err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	return c.err()
}
