package embedding

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/realxen/cartograph/internal/sysutil"
)

// ResolvedModel contains the GGUF model data after resolution via
// the model resolution chain. Tokenization is handled by llama.cpp
// at the C level — no Go-side tokenizer needed.
type ResolvedModel struct {
	Bytes  []byte // GGUF model data
	Name   string // Human-readable name for display
	Source string // "cache" | "local" | "download"
}

// ResolveModel resolves a model specifier to GGUF bytes.
//
// Resolution chain:
//  1. "" → default alias (see DefaultAlias())
//  2. Local path (/..., ./..., ~...) → read from disk
//  3. Known alias (e.g. "bge-small") → cache or download
//  4. Contains "/" → treat as HF repo ID → cache or download
//  5. Error
func ResolveModel(model string) (*ResolvedModel, error) {
	return ResolveModelWithProgress(model, nil)
}

// ResolveModelWithProgress is like ResolveModel but accepts a progress
// callback for downloads.
func ResolveModelWithProgress(model string, progress func(downloaded, total int64)) (*ResolvedModel, error) {
	model = strings.TrimSpace(model)

	// Parse optional quantization hint: "repo/name:Q4_K_M"
	var quantHint string
	if idx := strings.LastIndex(model, ":"); idx > 0 && !strings.Contains(model[idx:], "/") {
		quantHint = model[idx+1:]
		model = model[:idx]
	}

	if model == "" {
		model = DefaultAlias()
	}

	if isLocalPath(model) {
		return resolveLocalPath(model)
	}

	if alias, ok := LookupAlias(model); ok {
		return resolveAlias(model, alias, quantHint, progress)
	}

	// Deprecated alias mapping.
	if model == "bge-small-en-v1.5" {
		if alias, ok := LookupAlias("bge-small"); ok {
			return resolveAlias("bge-small", alias, quantHint, progress)
		}
	}

	if strings.Contains(model, "/") {
		return resolveHFRepo(model, quantHint, progress)
	}

	return nil, fmt.Errorf("embedding: unknown model %q — use a known alias, HF repo ID (org/name), or local path", model)
}

// resolveAlias resolves a known alias through cache → download.
func resolveAlias(name string, alias ModelAlias, quantHint string, progress func(downloaded, total int64)) (*ResolvedModel, error) {
	cacheDir, err := ModelCacheDir()
	if err != nil {
		return nil, err
	}

	filename := alias.File
	if quantHint != "" {
		filename = ""
	}

	if filename != "" {
		// 1. Check our cache.
		cachePath, err := modelCachePath(cacheDir, alias.Repo, filename)
		if err != nil {
			return nil, err
		}
		if data, err := os.ReadFile(cachePath); err == nil {
			return &ResolvedModel{Bytes: data, Name: name, Source: "cache"}, nil
		}

		// 2. Check HF Hub cache (read-only, zero-copy reuse).
		if hfPath := findInHFCache(alias.Repo, filename); hfPath != "" {
			if data, err := os.ReadFile(hfPath); err == nil {
				log.Printf("[embedding] reusing model from HF cache: %q", hfPath)
				return &ResolvedModel{Bytes: data, Name: name, Source: "cache"}, nil
			}
		}
	}

	info, err := FetchModelInfo(alias.Repo, quantHint)
	if err != nil {
		if filename != "" {
			// Offline fallback: check our cache.
			cachePath, pathErr := modelCachePath(cacheDir, alias.Repo, filename)
			if pathErr != nil {
				return nil, pathErr
			}
			if data, readErr := os.ReadFile(cachePath); readErr == nil {
				log.Printf("[embedding] offline: using cached model %q", cachePath)
				return &ResolvedModel{Bytes: data, Name: name, Source: "cache"}, nil
			}
			// Offline fallback: check HF Hub cache.
			if hfPath := findInHFCache(alias.Repo, filename); hfPath != "" {
				if data, readErr := os.ReadFile(hfPath); readErr == nil {
					log.Printf("[embedding] offline: reusing model from HF cache: %q", hfPath)
					return &ResolvedModel{Bytes: data, Name: name, Source: "cache"}, nil
				}
			}
		}
		return nil, fmt.Errorf("embedding: model %q not available — run 'cartograph models pull' while online, or provide a local model with --model /path\n  (error: %w)", name, err)
	}

	cachePath, err := DownloadModel(info, cacheDir, progress)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("embedding: read downloaded model: %w", err)
	}

	return &ResolvedModel{Bytes: data, Name: name, Source: "download"}, nil
}

// resolveHFRepo resolves an arbitrary HF repo ID through cache → download.
func resolveHFRepo(repoID, quantHint string, progress func(downloaded, total int64)) (*ResolvedModel, error) {
	cacheDir, err := ModelCacheDir()
	if err != nil {
		return nil, err
	}

	info, err := FetchModelInfo(repoID, quantHint)
	if err != nil {
		// Offline fallback: check our cache.
		repoOrg, repoName, pathErr := splitHFRepoID(repoID)
		if pathErr != nil {
			return nil, pathErr
		}
		repoDir := filepath.Join(cacheDir, repoOrg, repoName)
		entries, _ := os.ReadDir(repoDir)
		for _, e := range entries {
			if sysutil.IsPathSegment(e.Name()) && strings.HasSuffix(e.Name(), ".gguf") && !strings.HasSuffix(e.Name(), ".part") {
				cachePath := filepath.Join(repoDir, e.Name())
				data, readErr := os.ReadFile(cachePath)
				if readErr == nil {
					log.Printf("[embedding] offline: using cached model %q", cachePath)
					return &ResolvedModel{Bytes: data, Name: filepath.Base(cachePath), Source: "cache"}, nil
				}
			}
		}
		return nil, fmt.Errorf("embedding: model %q not available — run 'cartograph models pull %s' while online, or provide a local model with --model /path\n  (error: %w)", repoID, repoID, err)
	}

	// Check our cache.
	cachePath, err := modelCachePath(cacheDir, info.RepoID, info.Filename)
	if err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(cachePath); err == nil {
		return &ResolvedModel{Bytes: data, Name: info.Filename, Source: "cache"}, nil
	}

	// Check HF Hub cache (read-only).
	if hfPath := findInHFCache(info.RepoID, info.Filename); hfPath != "" {
		if data, err := os.ReadFile(hfPath); err == nil {
			log.Printf("[embedding] reusing model from HF cache: %q", hfPath)
			return &ResolvedModel{Bytes: data, Name: info.Filename, Source: "cache"}, nil
		}
	}

	downloadedPath, err := DownloadModel(info, cacheDir, progress)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(downloadedPath)
	if err != nil {
		return nil, fmt.Errorf("embedding: read downloaded model: %w", err)
	}

	return &ResolvedModel{Bytes: data, Name: info.Filename, Source: "download"}, nil
}

func modelCachePath(cacheDir, repoID, filename string) (string, error) {
	repoOrg, repoName, err := splitHFRepoID(repoID)
	if err != nil {
		return "", err
	}
	if !sysutil.IsPathSegment(filename) {
		return "", fmt.Errorf("embedding: unsafe filename %q", filename)
	}
	return filepath.Join(cacheDir, repoOrg, repoName, filename), nil
}

// ValidateServiceModel checks that a model specifier is safe for the
// service API. Local filesystem paths are rejected because the JSON
// API should not trigger arbitrary file reads.
func ValidateServiceModel(model string) error {
	model = strings.TrimSpace(model)

	// Strip optional quantization hint before checking.
	if idx := strings.LastIndex(model, ":"); idx > 0 && !strings.Contains(model[idx:], "/") {
		model = model[:idx]
	}

	if model == "" {
		return nil // default alias
	}
	if isLocalPath(model) {
		return errors.New("embedding: local paths are not allowed through the service API")
	}
	if _, ok := LookupAlias(model); ok {
		return nil
	}
	if strings.Contains(model, "/") {
		return nil // HF repo ID
	}
	return nil // unknown models are caught later by ResolveModel
}

func isLocalPath(model string) bool {
	return strings.HasPrefix(model, "/") ||
		strings.HasPrefix(model, "./") ||
		strings.HasPrefix(model, "../") ||
		strings.HasPrefix(model, "~")
}

func resolveLocalPath(path string) (*ResolvedModel, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("embedding: cannot expand ~: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embedding: read local model %q: %w", path, err)
	}

	return &ResolvedModel{Bytes: data, Name: filepath.Base(path), Source: "local"}, nil
}
