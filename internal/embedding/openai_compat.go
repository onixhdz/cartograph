package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatProvider implements Provider using any OpenAI-compatible
// /v1/embeddings server (OpenAI, Ollama, vLLM, LocalAI, etc.).
type OpenAICompatProvider struct {
	endpoint string // base URL (no trailing slash)
	model    string // model name to send in requests
	apiKey   string // bearer token (empty = no auth)
	client   *http.Client
	dims     int // cached after first call
}

// embeddingRequest is the OpenAI-compatible request body.
type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// embeddingResponse is the OpenAI-compatible response body.
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// errorResponse is the OpenAI-compatible error format.
type errorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// NewOpenAICompatProvider creates a provider that speaks the OpenAI
// /v1/embeddings API format.
func NewOpenAICompatProvider(endpoint, model, apiKey string) (*OpenAICompatProvider, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	return &OpenAICompatProvider{
		endpoint: endpoint,
		model:    model,
		apiKey:   apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Embed sends texts to the remote /v1/embeddings endpoint and returns vectors.
func (p *OpenAICompatProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody, err := json.Marshal(embeddingRequest{
		Input: texts,
		Model: p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		p.endpoint+"/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embedding: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: request to %s: %w", p.endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp errorResponse
		_ = json.Unmarshal(body, &errResp) // best effort
		msg := errResp.Error.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("embedding: %s returned %d: %s", p.endpoint, resp.StatusCode, msg)
	}

	var result embeddingResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("embedding: decode response: %w", err)
	}

	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: expected %d results, got %d", len(texts), len(result.Data))
	}

	vectors := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("embedding: response index %d out of range [0, %d)", d.Index, len(texts))
		}
		vectors[d.Index] = d.Embedding
	}

	if p.dims == 0 && len(vectors) > 0 && len(vectors[0]) > 0 {
		p.dims = len(vectors[0])
	}

	return vectors, nil
}

// Dimensions returns the cached embedding dimensionality. Returns 384
// (MiniLM default) until the first Embed() call populates the actual value.
func (p *OpenAICompatProvider) Dimensions() int {
	if p.dims > 0 {
		return p.dims
	}
	return 384
}

// Name returns a human-readable description of this provider.
func (p *OpenAICompatProvider) Name() string {
	return fmt.Sprintf("openai-compat(%s, %s)", p.endpoint, p.model)
}

// Close releases idle HTTP connections.
func (p *OpenAICompatProvider) Close() error {
	p.client.CloseIdleConnections()
	return nil
}
