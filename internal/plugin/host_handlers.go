package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/realxen/cartograph/internal/datasource"
	"github.com/realxen/cartograph/internal/jsonrpc2"
)

// HostHandler handles incoming JSON-RPC 2.0 requests and notifications from
// a plugin process. It provides host services: config resolution, caching,
// HTTP proxying, graph emission, and logging.
type HostHandler struct {
	// Config is the key-value config for this connection (resolved from
	// sources.toml Extra fields). Keys ending in "_env" have already been
	// resolved to their environment variable values.
	Config map[string]any

	// Builder receives emitted nodes and edges from the plugin.
	Builder datasource.GraphBuilder

	// Cache is the backing store for plugin-scoped caching.
	// If nil, cache operations return not-found / no-op.
	Cache CacheStore

	// HTTPClient is used for proxied HTTP requests. If nil, http.DefaultClient
	// is used.
	HTTPClient *http.Client

	// Logger receives log messages from the plugin. If nil, logs are discarded.
	Logger func(pluginName string, level string, msg string)

	// PluginName is used to prefix log messages.
	PluginName string

	// OnEmitNode is called after each emit_node notification, if set.
	// Useful for counting emissions (limits enforcement).
	OnEmitNode func()

	// OnEmitEdge is called after each emit_edge notification, if set.
	OnEmitEdge func()
}

// Compile-time check that HostHandler implements jsonrpc2.Handler.
var _ jsonrpc2.Handler = (*HostHandler)(nil)

// Handle dispatches incoming plugin-to-host RPC methods.
func (h *HostHandler) Handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	switch req.Method {
	// Request methods (expect a response).
	case "config_get":
		return h.handleConfigGet(req)
	case "cache_get":
		return h.handleCacheGet(req)
	case "cache_set":
		return h.handleCacheSet(req)
	case "http_request":
		return h.handleHTTPRequest(ctx, req)

	// Notification methods (fire-and-forget, no response).
	case "emit_node":
		return h.handleEmitNode(req)
	case "emit_edge":
		return h.handleEmitEdge(req)
	case "log":
		return h.handleLog(req)

	default:
		return nil, fmt.Errorf("%w: %q", jsonrpc2.ErrMethodNotFound, req.Method)
	}
}

type configGetParams struct {
	Key string `json:"key"`
}

func (h *HostHandler) handleConfigGet(req *jsonrpc2.Request) (any, error) {
	var params configGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}
	if params.Key == "" {
		return nil, fmt.Errorf("%w: empty key", jsonrpc2.ErrInvalidParams)
	}

	if h.Config == nil {
		return nil, jsonrpc2.NewError(-32000, fmt.Sprintf("config key %q not found", params.Key)) //nolint:wrapcheck // wire error by design
	}

	val, ok := h.Config[params.Key]
	if !ok {
		return nil, jsonrpc2.NewError(-32000, fmt.Sprintf("config key %q not found", params.Key)) //nolint:wrapcheck // wire error by design
	}
	return val, nil
}

// CacheStore is the interface for plugin-scoped key-value caching.
type CacheStore interface {
	Get(key string) (value string, found bool)
	Set(key string, value string, ttl time.Duration)
}

type cacheGetParams struct {
	Key string `json:"key"`
}

type cacheGetResult struct {
	Value string `json:"value"`
	Found bool   `json:"found"`
}

func (h *HostHandler) handleCacheGet(req *jsonrpc2.Request) (any, error) {
	var params cacheGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}
	if params.Key == "" {
		return nil, fmt.Errorf("%w: empty key", jsonrpc2.ErrInvalidParams)
	}

	if h.Cache == nil {
		return cacheGetResult{Found: false}, nil
	}

	value, found := h.Cache.Get(params.Key)
	return cacheGetResult{Value: value, Found: found}, nil
}

type cacheSetParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"` // seconds
}

type cacheSetResult struct {
	OK bool `json:"ok"`
}

func (h *HostHandler) handleCacheSet(req *jsonrpc2.Request) (any, error) {
	var params cacheSetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}
	if params.Key == "" {
		return nil, fmt.Errorf("%w: empty key", jsonrpc2.ErrInvalidParams)
	}

	if h.Cache != nil {
		h.Cache.Set(params.Key, params.Value, time.Duration(params.TTL)*time.Second)
	}
	return cacheSetResult{OK: true}, nil
}

type httpRequestParams struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
}

type httpRequestResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func (h *HostHandler) handleHTTPRequest(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	var params httpRequestParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}
	if params.Method == "" {
		return nil, fmt.Errorf("%w: empty HTTP method", jsonrpc2.ErrInvalidParams)
	}
	if params.URL == "" {
		return nil, fmt.Errorf("%w: empty URL", jsonrpc2.ErrInvalidParams)
	}

	var bodyReader io.Reader
	if params.Body != nil {
		bodyReader = strings.NewReader(*params.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, params.Method, params.URL, bodyReader)
	if err != nil {
		return nil, jsonrpc2.NewError(-32000, fmt.Sprintf("creating HTTP request: %v", err)) //nolint:wrapcheck // wire error by design
	}
	for k, v := range params.Headers {
		httpReq.Header.Set(k, v)
	}

	client := h.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, jsonrpc2.NewError(-32000, fmt.Sprintf("HTTP request failed: %v", err)) //nolint:wrapcheck // wire error by design
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, jsonrpc2.NewError(-32000, fmt.Sprintf("reading HTTP response: %v", err)) //nolint:wrapcheck // wire error by design
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return httpRequestResult{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    string(body),
	}, nil
}

type emitNodeParams struct {
	Label string         `json:"label"`
	ID    string         `json:"id"`
	Props map[string]any `json:"props"`
}

func (h *HostHandler) handleEmitNode(req *jsonrpc2.Request) (any, error) {
	var params emitNodeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}
	if params.Label == "" {
		return nil, fmt.Errorf("%w: empty label", jsonrpc2.ErrInvalidParams)
	}
	if params.ID == "" {
		return nil, fmt.Errorf("%w: empty id", jsonrpc2.ErrInvalidParams)
	}

	if h.Builder != nil {
		h.Builder.AddNode(params.Label, params.ID, params.Props)
	}
	if h.OnEmitNode != nil {
		h.OnEmitNode()
	}
	return nil, nil
}

type emitEdgeParams struct {
	From  string         `json:"from"`
	To    string         `json:"to"`
	Rel   string         `json:"rel"`
	Props map[string]any `json:"props"`
}

func (h *HostHandler) handleEmitEdge(req *jsonrpc2.Request) (any, error) {
	var params emitEdgeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}
	if params.From == "" || params.To == "" {
		return nil, fmt.Errorf("%w: from and to are required", jsonrpc2.ErrInvalidParams)
	}
	if params.Rel == "" {
		return nil, fmt.Errorf("%w: empty rel", jsonrpc2.ErrInvalidParams)
	}

	if h.Builder != nil {
		h.Builder.AddEdge(params.From, params.To, params.Rel, params.Props)
	}
	if h.OnEmitEdge != nil {
		h.OnEmitEdge()
	}
	return nil, nil
}

type logParams struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

func (h *HostHandler) handleLog(req *jsonrpc2.Request) (any, error) {
	var params logParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", jsonrpc2.ErrInvalidParams, err)
	}

	if h.Logger != nil {
		h.Logger(h.PluginName, params.Level, params.Msg)
	}
	return nil, nil
}

// MemoryCache is a simple in-memory CacheStore with TTL support.
// Suitable for testing and single-process deployments.
type MemoryCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	value   string
	expires time.Time // zero means no expiry
}

// NewMemoryCache creates a new in-memory cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		entries: make(map[string]cacheEntry),
	}
}

// Get returns the cached value and whether it was found.
// Expired entries are treated as not found.
func (c *MemoryCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if !entry.expires.IsZero() && time.Now().After(entry.expires) {
		delete(c.entries, key)
		return "", false
	}
	return entry.value, true
}

// Set stores a value with the given TTL. A zero TTL means no expiry.
func (c *MemoryCache) Set(key string, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := cacheEntry{value: value}
	if ttl > 0 {
		entry.expires = time.Now().Add(ttl)
	}
	c.entries[key] = entry
}

// Compile-time check that MemoryCache implements CacheStore.
var _ CacheStore = (*MemoryCache)(nil)
