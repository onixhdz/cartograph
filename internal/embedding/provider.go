// Package embedding provides text embedding vectors for semantic search.
// Backends: "llamacpp" (built-in CGO) and "openai_compat" (any OpenAI-compatible server).
package embedding

import (
	"context"
	"errors"
	"fmt"

	"github.com/realxen/cartograph/internal/embedding/local"
)

// Provider generates embedding vectors from text.
type Provider interface {
	// Embed returns embedding vectors for one or more input texts.
	// Each returned []float32 has length Dimensions().
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the output vector dimensionality (e.g. 384 for bge-small).
	Dimensions() int

	// Name returns a human-readable provider name for logging.
	Name() string

	// Close releases resources (model memory, HTTP clients, etc).
	Close() error
}

// Config drives which embedding provider is instantiated.
type Config struct {
	// Provider selects the backend: "llamacpp" (default) or "openai_compat".
	Provider string

	// Endpoint is the base URL for the remote provider (e.g. https://api.openai.com).
	Endpoint string

	// APIKey is the bearer token for authenticated providers (OpenAI, Azure, etc).
	APIKey string

	// Model is the model name sent to the remote provider.
	Model string
}

// NewProvider creates a Provider based on the given config.
func NewProvider(cfg Config) (Provider, error) {
	return NewProviderWithProgress(cfg, nil)
}

// NewProviderWithProgress creates a Provider with an optional download
// progress callback for model downloads.
func NewProviderWithProgress(cfg Config, progress func(downloaded, total int64)) (Provider, error) {
	switch cfg.Provider {
	case "llamacpp", "":
		resolved, err := ResolveModelWithProgress(cfg.Model, progress)
		if err != nil {
			return nil, err
		}
		p, err := local.New(resolved.Bytes)
		if err != nil {
			return nil, fmt.Errorf("embedding: init local provider: %w", err)
		}
		return p, nil
	case "openai_compat":
		if cfg.Endpoint == "" {
			return nil, errors.New("embedding: openai_compat provider requires Endpoint")
		}
		if cfg.Model == "" {
			return nil, errors.New("embedding: openai_compat provider requires Model")
		}
		return NewOpenAICompatProvider(cfg.Endpoint, cfg.Model, cfg.APIKey)
	default:
		return nil, fmt.Errorf("embedding: unknown provider %q", cfg.Provider)
	}
}
