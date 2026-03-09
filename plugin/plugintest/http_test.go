package plugintest

import (
	"net/http"
	"testing"

	"github.com/realxen/cartograph/plugin"
)

func stubHTTPRequest(method, url string) plugin.HTTPRequest {
	return plugin.HTTPRequest{Method: method, URL: url}
}

func TestMockHTTP_RouteMatching(t *testing.T) {
	mock := MockHTTP([]Route{
		{Method: "GET", URL: "https://api.example.com/repos", Status: 200, Body: `[{"name":"api"}]`},
		{Method: "POST", URL: "https://api.example.com/repos", Status: 201, Body: `{"id":1}`},
		{URL: "https://api.example.com/health", Status: 200, Body: `ok`}, // any method
	})
	handler := mock.Handler()

	tests := []struct {
		name       string
		method     string
		url        string
		wantStatus int
		wantBody   string
	}{
		{"GET repos", "GET", "https://api.example.com/repos", 200, `[{"name":"api"}]`},
		{"POST repos", "POST", "https://api.example.com/repos", 201, `{"id":1}`},
		{"GET health", "GET", "https://api.example.com/health", 200, `ok`},
		{"POST health", "POST", "https://api.example.com/health", 200, `ok`},
		{"GET repos with query", "GET", "https://api.example.com/repos?page=2", 200, `[{"name":"api"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := handler(nil, stubHTTPRequest(tt.method, tt.url))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("status: got %d, want %d", resp.Status, tt.wantStatus)
			}
			if resp.Body != tt.wantBody {
				t.Errorf("body: got %q, want %q", resp.Body, tt.wantBody)
			}
		})
	}
}

func TestMockHTTP_NoMatch(t *testing.T) {
	mock := MockHTTP([]Route{
		{Method: "GET", URL: "https://api.example.com/repos", Status: 200},
	})
	handler := mock.Handler()

	_, err := handler(nil, stubHTTPRequest("GET", "https://other.com/repos"))
	if err == nil {
		t.Fatal("expected error for unmatched route")
	}
}

func TestMockHTTP_MethodMismatch(t *testing.T) {
	mock := MockHTTP([]Route{
		{Method: "GET", URL: "https://api.example.com/repos", Status: 200},
	})
	handler := mock.Handler()

	_, err := handler(nil, stubHTTPRequest("POST", "https://api.example.com/repos"))
	if err == nil {
		t.Fatal("expected error for method mismatch")
	}
}

func TestMockHTTP_RequestRecording(t *testing.T) {
	mock := MockHTTP([]Route{
		{URL: "https://api.example.com/", Status: 200, Body: "ok"},
	})
	handler := mock.Handler()

	_, _ = handler(nil, stubHTTPRequest("GET", "https://api.example.com/repos"))
	_, _ = handler(nil, stubHTTPRequest("POST", "https://api.example.com/data"))

	if mock.RequestCount() != 2 {
		t.Fatalf("request count: got %d, want 2", mock.RequestCount())
	}

	reqs := mock.Requests()
	if reqs[0].Method != http.MethodGet || reqs[0].URL != "https://api.example.com/repos" {
		t.Errorf("request 0: got %s %s", reqs[0].Method, reqs[0].URL)
	}
	if reqs[1].Method != http.MethodPost || reqs[1].URL != "https://api.example.com/data" {
		t.Errorf("request 1: got %s %s", reqs[1].Method, reqs[1].URL)
	}
}

func TestMockHTTP_DefaultStatus(t *testing.T) {
	mock := MockHTTP([]Route{
		{URL: "https://api.example.com/", Body: "ok"}, // Status 0 → defaults to 200
	})
	handler := mock.Handler()

	resp, err := handler(nil, stubHTTPRequest("GET", "https://api.example.com/foo"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("status: got %d, want 200", resp.Status)
	}
}
