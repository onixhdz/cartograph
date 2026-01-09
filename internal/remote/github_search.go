package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GitHubSearchResult holds a single repository result from the GitHub
// search API, trimmed to just the fields we display.
type GitHubSearchResult struct {
	FullName    string // e.g. "hashicorp/nomad"
	Description string // short repo description
	Stars       int    // stargazers_count
	URL         string // html_url
}

// gitHubSearchResponse mirrors the JSON envelope from
// GET https://api.github.com/search/repositories.
type gitHubSearchResponse struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
		HTMLURL     string `json:"html_url"`
	} `json:"items"`
}

// SearchGitHub queries the GitHub repository search API for repos matching
// the query, sorted by stars (top 5). Uses authToken as Bearer if non-empty.
// Returns nil, nil when nothing is found.
func SearchGitHub(ctx context.Context, query, authToken string) ([]GitHubSearchResult, error) {
	u := fmt.Sprintf(
		"https://api.github.com/search/repositories?q=%s+in:name&sort=stars&order=desc&per_page=5",
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("search github: new request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search github: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var body gitHubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("search github: decode response: %w", err)
	}

	results := make([]GitHubSearchResult, 0, len(body.Items))
	for _, item := range body.Items {
		results = append(results, GitHubSearchResult{
			FullName:    item.FullName,
			Description: item.Description,
			Stars:       item.Stars,
			URL:         item.HTMLURL,
		})
	}
	return results, nil
}

// FormatStars returns a human-friendly star count: "14.2k", "340", etc.
func FormatStars(n int) string {
	switch {
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return strconv.Itoa(n)
	}
}
