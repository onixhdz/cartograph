//go:build !embedding_cgo

// Package local provides embedding via native CGO-linked inference.
//
// This stub is compiled when the embedding_cgo build tag is absent. It lets the
// binary build and test without the native inference library (and its Zig
// toolchain) by reporting that the local backend is unavailable. Construction
// already returns an error on the native path, so callers handle this case.
package local

import (
	"context"
	"errors"
)

// ErrUnavailable reports that the binary was built without the native embedding
// backend, i.e. without the embedding_cgo build tag.
var ErrUnavailable = errors.New("llamacpp: local embedding backend not built (missing embedding_cgo build tag)")

// Provider is a placeholder so the package satisfies embedding.Provider in stub
// builds. Its methods are never reached because New always fails.
type Provider struct{}

// New always fails in stub builds; use the embedding_cgo tag for native embedding.
func New(modelBytes []byte) (*Provider, error) { return nil, ErrUnavailable }

// NewWithWorkers always fails in stub builds; use the embedding_cgo tag for native embedding.
func NewWithWorkers(modelBytes []byte, n int) (*Provider, error) { return nil, ErrUnavailable }

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, ErrUnavailable
}

func (p *Provider) Dimensions() int { return 0 }

func (p *Provider) Name() string { return "llamacpp(unavailable)" }

func (p *Provider) Close() error { return nil }
