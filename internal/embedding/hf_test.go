package embedding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDownloadModel_Success(t *testing.T) {
	// Create a fake GGUF payload.
	payload := []byte("fake-gguf-model-data-for-testing")
	hash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(hash[:])

	// Set up test server.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/models/test-org/test-model", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"siblings": []map[string]string{
				{"rfilename": "model-Q8_0.gguf"},
				{"rfilename": "model-Q4_K_M.gguf"},
				{"rfilename": "README.md"},
			},
		})
	})
	mux.HandleFunc("/api/models/test-org/test-model/tree/main", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"rfilename": "model-Q8_0.gguf",
				"oid":       hashHex,
				"size":      len(payload),
				"lfs": map[string]any{
					"sha256": hashHex,
					"size":   len(payload),
				},
			},
		})
	})
	mux.HandleFunc("/test-org/test-model/resolve/main/model-Q8_0.gguf", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Override HF base URL.
	origBase := hfBaseURL
	hfBaseURL = server.URL
	defer func() { hfBaseURL = origBase }()

	// Test FetchModelInfo.
	info, err := FetchModelInfo("test-org/test-model", "")
	if err != nil {
		t.Fatalf("FetchModelInfo: %v", err)
	}
	if info.Filename != "model-Q8_0.gguf" {
		t.Errorf("Filename = %q, want 'model-Q8_0.gguf'", info.Filename)
	}
	if info.SHA256 != hashHex {
		t.Errorf("SHA256 = %q, want %q", info.SHA256, hashHex)
	}

	// Test DownloadModel.
	cacheDir := t.TempDir()
	var lastProgress int64
	path, err := DownloadModel(info, cacheDir, func(downloaded, total int64) {
		lastProgress = downloaded
	})
	if err != nil {
		t.Fatalf("DownloadModel: %v", err)
	}

	// Verify file was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != string(payload) {
		t.Errorf("downloaded data mismatch")
	}
	if lastProgress != int64(len(payload)) {
		t.Errorf("progress = %d, want %d", lastProgress, len(payload))
	}

	// Verify cache path structure.
	expected := filepath.Join(cacheDir, "test-org/test-model", "model-Q8_0.gguf")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

func TestDownloadModel_SHA256Mismatch(t *testing.T) {
	payload := []byte("fake-data")

	mux := http.NewServeMux()
	mux.HandleFunc("/test-org/bad-hash/resolve/main/model.gguf", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	origBase := hfBaseURL
	hfBaseURL = server.URL
	defer func() { hfBaseURL = origBase }()

	info := &HFModelInfo{
		RepoID:   "test-org/bad-hash",
		Filename: "model.gguf",
		SHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
		Size:     int64(len(payload)),
	}

	cacheDir := t.TempDir()
	_, err := DownloadModel(info, cacheDir, nil)
	if err == nil {
		t.Fatal("expected SHA256 mismatch error")
		return
	}
	if got := err.Error(); !hfContains(got, "SHA256 mismatch") {
		t.Errorf("error = %q, want SHA256 mismatch", got)
	}

	// Verify .part file was cleaned up.
	partPath := filepath.Join(cacheDir, "test-org/bad-hash", "model.gguf.part")
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Error("expected .part file to be removed after SHA256 mismatch")
	}
}

func TestSelectGGUF(t *testing.T) {
	files := []string{
		"model-Q4_K_M.gguf",
		"model-Q8_0.gguf",
		"model-Q3_K_S.gguf",
		"model-F16.gguf",
	}

	// Without hint: should select Q8_0.
	got := selectGGUF(files, "")
	if got != "model-Q8_0.gguf" {
		t.Errorf("selectGGUF(no hint) = %q, want 'model-Q8_0.gguf'", got)
	}

	// With hint: should select matching.
	got = selectGGUF(files, "Q4_K_M")
	if got != "model-Q4_K_M.gguf" {
		t.Errorf("selectGGUF(Q4_K_M) = %q, want 'model-Q4_K_M.gguf'", got)
	}

	// With hint not found.
	got = selectGGUF(files, "Q5_K_M")
	if got != "" {
		t.Errorf("selectGGUF(Q5_K_M) = %q, want empty", got)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{1048576, "1.0 MiB"},
		{137 * 1048576, "137.0 MiB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func hfContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && hfContainsStr(s, substr))
}

func hfContainsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
