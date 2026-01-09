package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HFModelInfo describes a GGUF file hosted on Hugging Face.
type HFModelInfo struct {
	RepoID   string // e.g. "nomic-ai/nomic-embed-code-GGUF"
	Filename string // e.g. "nomic-embed-code-Q8_0.gguf"
	SHA256   string // hex-encoded hash from HF API
	Size     int64  // file size in bytes
}

// hfSibling is a file entry from the HF API response.
type hfSibling struct {
	Filename string `json:"rfilename"`
}

// hfRepoInfo is the subset of the HF API /api/models/{repo} response we need.
type hfRepoInfo struct {
	Siblings []hfSibling `json:"siblings"`
}

// hfBaseURL is the Hugging Face API base URL. Overridable for testing.
var hfBaseURL = "https://huggingface.co"

// quantPriority ranks GGUF quantizations — higher is preferred.
var quantPriority = map[string]int{
	"Q8_0":   100,
	"Q6_K":   90,
	"Q5_K_M": 80,
	"Q5_K_S": 75,
	"Q5_1":   70,
	"Q5_0":   65,
	"Q4_K_M": 60,
	"Q4_K_S": 55,
	"Q4_1":   50,
	"Q4_0":   45,
	"Q3_K_M": 40,
	"Q3_K_S": 35,
	"Q2_K":   30,
	"F16":    20,
	"F32":    10,
}

// FetchModelInfo queries the HF API for available GGUF files in a repo
// and selects the best quantization. If quantHint is non-empty (e.g.
// "Q4_K_M"), it filters to that specific quantization.
func FetchModelInfo(repoID, quantHint string) (*HFModelInfo, error) {
	client := hfHTTPClient()

	// List files in the repo.
	url := fmt.Sprintf("%s/api/models/%s", hfBaseURL, repoID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("hf: build request: %w", err)
	}
	addHFAuth(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hf: fetch repo info for %s: %w", repoID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hf: repo %s returned HTTP %d", repoID, resp.StatusCode)
	}

	var info hfRepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("hf: decode repo info: %w", err)
	}

	// Filter to .gguf files.
	var ggufFiles []string
	for _, s := range info.Siblings {
		if strings.HasSuffix(s.Filename, ".gguf") {
			ggufFiles = append(ggufFiles, s.Filename)
		}
	}
	if len(ggufFiles) == 0 {
		return nil, fmt.Errorf("hf: no .gguf files found in %s", repoID)
	}

	// Select the best file.
	selected := selectGGUF(ggufFiles, quantHint)
	if selected == "" {
		if quantHint != "" {
			return nil, fmt.Errorf("hf: no GGUF matching quantization %q in %s", quantHint, repoID)
		}
		return nil, fmt.Errorf("hf: could not select GGUF from %s", repoID)
	}

	// Get file metadata (SHA256, size) via the LFS pointer endpoint.
	sha, size, err := fetchFileMeta(client, repoID, selected)
	if err != nil {
		return nil, err
	}

	return &HFModelInfo{
		RepoID:   repoID,
		Filename: selected,
		SHA256:   sha,
		Size:     size,
	}, nil
}

// selectGGUF picks the best GGUF from a list of filenames.
func selectGGUF(files []string, quantHint string) string {
	if quantHint != "" {
		quantHint = strings.ToUpper(quantHint)
		for _, f := range files {
			upper := strings.ToUpper(f)
			if strings.Contains(upper, quantHint) {
				return f
			}
		}
		return ""
	}

	// Sort by quantization priority (highest first).
	type scored struct {
		file  string
		score int
	}
	var candidates []scored
	for _, f := range files {
		upper := strings.ToUpper(f)
		best := 0
		for q, p := range quantPriority {
			if strings.Contains(upper, q) && p > best {
				best = p
			}
		}
		candidates = append(candidates, scored{f, best})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if len(candidates) > 0 {
		return candidates[0].file
	}
	return ""
}

// fetchFileMeta retrieves SHA256 and size for a specific file via the HF API.
func fetchFileMeta(client *http.Client, repoID, filename string) (sha string, size int64, err error) {
	// Use the file header endpoint to get LFS info.
	url := fmt.Sprintf("%s/api/models/%s/tree/main", hfBaseURL, repoID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("hf: build tree request: %w", err)
	}
	addHFAuth(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("hf: fetch tree for %s: %w", repoID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("hf: tree for %s returned HTTP %d", repoID, resp.StatusCode)
	}

	var entries []struct {
		Type string `json:"type"`
		Path string `json:"rfilename"`
		OID  string `json:"oid"`
		Size int64  `json:"size"`
		LFS  *struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"lfs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", 0, fmt.Errorf("hf: decode tree: %w", err)
	}

	for _, e := range entries {
		if e.Path == filename || filepath.Base(e.Path) == filename {
			if e.LFS != nil {
				return e.LFS.SHA256, e.LFS.Size, nil
			}
			return e.OID, e.Size, nil
		}
	}

	// File found in siblings but not in tree — return without SHA256.
	return "", 0, nil
}

// DownloadModel downloads a model GGUF to the cache directory.
// Returns the path to the cached file.
func DownloadModel(info *HFModelInfo, cacheDir string, progress func(downloaded, total int64)) (string, error) {
	destDir := filepath.Join(cacheDir, info.RepoID)
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", fmt.Errorf("hf: create cache dir: %w", err)
	}

	destPath := filepath.Join(destDir, info.Filename)
	partPath := destPath + ".part"

	// Check for partial download (resume support).
	var existingSize int64
	if fi, err := os.Stat(partPath); err == nil {
		existingSize = fi.Size()
	}

	url := fmt.Sprintf("%s/%s/resolve/main/%s", hfBaseURL, info.RepoID, info.Filename)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("hf: build download request: %w", err)
	}
	addHFAuth(req)

	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	client := hfHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hf: download %s: %w", info.Filename, err)
	}
	defer resp.Body.Close()

	// Handle response codes.
	switch resp.StatusCode {
	case http.StatusOK:
		existingSize = 0 // server doesn't support range, start fresh
	case http.StatusPartialContent:
		// resume works
	case http.StatusRequestedRangeNotSatisfiable:
		// already complete
		if existingSize > 0 {
			if err := os.Rename(partPath, destPath); err != nil {
				return "", fmt.Errorf("hf: rename partial: %w", err)
			}
			return destPath, nil
		}
		return "", fmt.Errorf("hf: server returned 416 for %s", info.Filename)
	default:
		return "", fmt.Errorf("hf: download %s returned HTTP %d", info.Filename, resp.StatusCode)
	}

	// Open file for writing (append if resuming).
	flags := os.O_CREATE | os.O_WRONLY
	if existingSize > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flags, 0o600)
	if err != nil {
		return "", fmt.Errorf("hf: open partial file: %w", err)
	}

	// Stream the download.
	total := resp.ContentLength + existingSize
	downloaded := existingSize

	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				_ = f.Close()
				return "", fmt.Errorf("hf: write: %w", writeErr)
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = f.Close()
			return "", fmt.Errorf("hf: read: %w", readErr)
		}
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("hf: close partial file: %w", err)
	}

	// Verify SHA256 if available.
	if info.SHA256 != "" {
		hash, err := hashFile(partPath)
		if err != nil {
			return "", fmt.Errorf("hf: hash verification: %w", err)
		}
		if hash != info.SHA256 {
			_ = os.Remove(partPath)
			return "", fmt.Errorf("hf: SHA256 mismatch for %s: expected %s, got %s", info.Filename, info.SHA256, hash)
		}
	}

	// Rename .part → final.
	if err := os.Rename(partPath, destPath); err != nil {
		return "", fmt.Errorf("hf: rename to final: %w", err)
	}

	return destPath, nil
}

// ModelCacheDir returns the cartograph model cache directory.
// Respects XDG_CACHE_HOME if set, otherwise defaults to ~/.cache/cartograph/models.
func ModelCacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartograph", "models"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("hf: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "cartograph", "models"), nil
}

// hfHubCacheDir returns the HF Hub cache directory for read-only lookup.
// Checks HF_HUB_CACHE > HF_HOME/hub > XDG_CACHE_HOME/huggingface/hub > ~/.cache/huggingface/hub.
func hfHubCacheDir() string {
	if d := os.Getenv("HF_HUB_CACHE"); d != "" {
		return d
	}
	if d := os.Getenv("HF_HOME"); d != "" {
		return filepath.Join(d, "hub")
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "huggingface", "hub")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "huggingface", "hub")
}

// findInHFCache searches the HF Hub cache for a model file (read-only).
// HF cache layout: {hub_dir}/models--{org}--{repo}/snapshots/{rev}/{filename}
// Returns the path if found, empty string otherwise.
func findInHFCache(repoID, filename string) string {
	hubDir := hfHubCacheDir()
	if hubDir == "" {
		return ""
	}

	// HF cache uses "models--org--repo" directory naming.
	safeName := "models--" + strings.ReplaceAll(repoID, "/", "--")
	snapshotsDir := filepath.Join(hubDir, safeName, "snapshots")

	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return ""
	}

	// Check each snapshot revision for the file.
	for _, rev := range entries {
		if !rev.IsDir() {
			continue
		}
		candidate := filepath.Join(snapshotsDir, rev.Name(), filename)
		if fi, err := os.Stat(candidate); err == nil && fi.Size() > 0 {
			return candidate
		}
	}
	return ""
}

// hashFile computes the SHA256 hex digest of a file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hf: open file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hf: compute hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// addHFAuth adds the HF_TOKEN header if set.
func addHFAuth(req *http.Request) {
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		token = os.Getenv("HUGGING_FACE_HUB_TOKEN") // legacy fallback
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// hfHTTPClient returns an HTTP client with reasonable timeouts.
func hfHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Minute, // large models take a while
	}
}

// FormatBytes returns a human-readable size string.
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
