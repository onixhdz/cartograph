package plugintest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/realxen/cartograph/plugin"
)

// Route maps an HTTP method+URL pattern to a canned response.
type Route struct {
	// Method is the HTTP method (GET, POST, etc.). Empty matches any method.
	Method string
	// URL is matched as a prefix against the request URL. Use exact URLs or
	// prefix patterns like "https://api.example.com/v1/repos".
	URL string
	// Status is the HTTP response status code.
	Status int
	// Headers are response headers.
	Headers map[string]string
	// Body is the response body string.
	Body string
}

// RecordedRequest is an HTTP request captured by the mock.
type RecordedRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

// HTTPMock holds the route table and records requests for assertion.
type HTTPMock struct {
	mu       sync.Mutex
	routes   []Route
	requests []RecordedRequest
}

// MockHTTP creates an HTTPMock from a route table and returns the handler
// function to pass to Host.SetHTTPHandler.
//
//	mock := plugintest.MockHTTP([]plugintest.Route{
//	    {Method: "GET", URL: "https://api.example.com/repos", Status: 200, Body: `[{"name":"api"}]`},
//	})
//	host.SetHTTPHandler(mock.Handler())
func MockHTTP(routes []Route) *HTTPMock {
	return &HTTPMock{
		routes: routes,
	}
}

// Handler returns the function to pass to Host.SetHTTPHandler.
func (m *HTTPMock) Handler() func(ctx context.Context, req plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
	return func(_ context.Context, req plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
		m.mu.Lock()
		m.requests = append(m.requests, RecordedRequest{
			Method:  req.Method,
			URL:     req.URL,
			Headers: req.Headers,
			Body:    req.Body,
		})
		routes := m.routes
		m.mu.Unlock()

		for _, r := range routes {
			if r.Method != "" && !strings.EqualFold(r.Method, req.Method) {
				continue
			}
			if !strings.HasPrefix(req.URL, r.URL) {
				continue
			}
			status := r.Status
			if status == 0 {
				status = 200
			}
			return &plugin.HTTPResponse{
				Status:  status,
				Headers: r.Headers,
				Body:    r.Body,
			}, nil
		}

		return nil, fmt.Errorf("plugintest: no route matched %s %s", req.Method, req.URL)
	}
}

// Requests returns a copy of all recorded requests.
func (m *HTTPMock) Requests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RecordedRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// RequestCount returns the number of recorded requests.
func (m *HTTPMock) RequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}
