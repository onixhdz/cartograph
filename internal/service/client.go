package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client communicates with the background service over a unix domain
// socket or a TCP connection.
type Client struct {
	addr       string // socket path or host:port
	network    string // "unix" or "tcp"
	httpClient *http.Client
}

// NewClient creates a Client that talks to the service via a unix socket.
func NewClient(socketPath string) *Client {
	return newClient(networkUnix, socketPath)
}

// NewTCPClient creates a Client that talks to the service via TCP.
func NewTCPClient(addr string) *Client {
	return newClient("tcp", addr)
}

// NewAutoClient creates a Client, choosing unix or tcp based on the address
// format. If addr contains a colon and does not look like an absolute path,
// it is treated as TCP; otherwise unix.
func NewAutoClient(addr string) *Client {
	if looksLikeTCP(addr) {
		return NewTCPClient(addr)
	}
	return NewClient(addr)
}

func newClient(network, addr string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	return &Client{
		addr:    addr,
		network: network,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// looksLikeTCP returns true if addr looks like a TCP host:port rather than
// a filesystem path. Heuristic: contains a colon and doesn't start with /
// or a drive letter like C:\.
func looksLikeTCP(addr string) bool {
	if strings.HasPrefix(addr, "/") {
		return false
	}
	if len(addr) >= 3 && addr[1] == ':' && (addr[2] == '\\' || addr[2] == '/') {
		return false
	}
	return strings.Contains(addr, ":")
}

// do sends a request and decodes the response envelope.
func (c *Client) do(method, route string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("client: marshal: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := "http://localhost" + route

	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("client: new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("client: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("client: read response: %w", err)
	}

	var envelope Response
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("client: decode response: %w (body: %s)", err, string(respBody))
	}

	if envelope.Error != nil {
		return envelope.Error
	}

	if result != nil && envelope.Result != nil {
		raw, err := json.Marshal(envelope.Result)
		if err != nil {
			return fmt.Errorf("client: re-marshal result: %w", err)
		}
		if err := json.Unmarshal(raw, result); err != nil {
			return fmt.Errorf("client: decode result: %w", err)
		}
	}
	return nil
}

// Query performs a hybrid search query.
func (c *Client) Query(req QueryRequest) (*QueryResult, error) {
	var res QueryResult
	if err := c.do(http.MethodPost, RouteQuery, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Context retrieves 360° symbol context.
func (c *Client) Context(req ContextRequest) (*ContextResult, error) {
	var res ContextResult
	if err := c.do(http.MethodPost, RouteContext, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Cypher executes a read-only Cypher query.
func (c *Client) Cypher(req CypherRequest) (*CypherResult, error) {
	var res CypherResult
	if err := c.do(http.MethodPost, RouteCypher, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Impact computes blast radius analysis.
func (c *Client) Impact(req ImpactRequest) (*ImpactResult, error) {
	var res ImpactResult
	if err := c.do(http.MethodPost, RouteImpact, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Cat retrieves file content from an indexed repository.
func (c *Client) Cat(req CatRequest) (*CatResult, error) {
	var res CatResult
	if err := c.do(http.MethodPost, RouteCat, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Reload requests the service to drop and re-load a repo's graph.
func (c *Client) Reload(req ReloadRequest) error {
	return c.do(http.MethodPost, RouteReload, req, nil)
}

// Status retrieves the service's health status.
func (c *Client) Status() (*StatusResult, error) {
	var res StatusResult
	if err := c.do(http.MethodGet, RouteStatus, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Schema retrieves the graph schema (node labels, edge types, properties).
func (c *Client) Schema(req SchemaRequest) (*SchemaResult, error) {
	var res SchemaResult
	if err := c.do(http.MethodPost, RouteSchema, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Shutdown requests a graceful shutdown of the service.
func (c *Client) Shutdown() error {
	return c.do(http.MethodPost, RouteShutdown, nil, nil)
}

// Embed sends a POST /api/embed request to trigger background embedding.
// Returns the initial job status (typically 202 Accepted).
func (c *Client) Embed(req EmbedRequest) (*EmbedStatusResult, error) {
	var res EmbedStatusResult
	if err := c.do(http.MethodPost, RouteEmbed, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// EmbedStatus polls the embedding status for a repo.
func (c *Client) EmbedStatus(req EmbedStatusRequest) (*EmbedStatusResult, error) {
	var res EmbedStatusResult
	if err := c.do(http.MethodPost, RouteEmbedStatus, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
