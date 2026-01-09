// Package remote provides Git remote operations: URL parsing, cloning
// (in-memory and on-disk), and billy filesystem walkers/readers that
// integrate with the ingestion pipeline.
package remote

import (
	"fmt"
	"regexp"
	"strings"
)

// IsGitURL reports whether the input looks like a Git remote URL
// (as opposed to a local filesystem path).
func IsGitURL(input string) bool {
	return strings.HasPrefix(input, "https://") ||
		strings.HasPrefix(input, "http://") ||
		strings.HasPrefix(input, "git@") ||
		strings.HasPrefix(input, "ssh://")
}

// knownGitHosts lists well-known Git hosting providers. Inputs prefixed
// with these are treated as remote URLs rather than local paths.
var knownGitHosts = []string{
	"github.com/",
	"gitlab.com/",
	"bitbucket.org/",
	"codeberg.org/",
	"sr.ht/",
}

// IsGitHostURL reports whether input looks like a host-prefixed Git URL
// without a scheme (e.g. "github.com/hashicorp/nomad"), auto-expanded to https.
func IsGitHostURL(input string) bool {
	for _, host := range knownGitHosts {
		if strings.HasPrefix(input, host) {
			// Must have at least one path segment after the host.
			rest := input[len(host):]
			if rest != "" && rest[0] != '/' {
				return true
			}
		}
	}
	return false
}

// ExpandGitHostURL prepends "https://" to a host-prefixed Git URL.
func ExpandGitHostURL(input string) string {
	return "https://" + input
}

// reGitHubShorthand matches "owner/repo" patterns — two URL-safe segments
// separated by a slash, with no leading dot, tilde, or slash.
var reGitHubShorthand = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// IsGitHubShorthand reports whether input looks like a GitHub "owner/repo"
// shorthand. The caller should verify the path does not exist on disk.
func IsGitHubShorthand(input string) bool {
	// Reject host-prefixed URLs — those are handled by IsGitHostURL.
	if IsGitHostURL(input) {
		return false
	}
	return reGitHubShorthand.MatchString(input)
}

// ExpandGitHubShorthand converts an "owner/repo" shorthand to a full
// GitHub HTTPS URL: "https://github.com/owner/repo".
func ExpandGitHubShorthand(shorthand string) string {
	return "https://github.com/" + shorthand
}

// reBareProjectName matches a single segment that looks like a project name
// (e.g. "nomad", "vue.js"). No slashes, no leading dot/tilde.
var reBareProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// IsBareProjectName reports whether input looks like a bare project name
// (single word, no slashes). Used to trigger a GitHub search as a fallback.
func IsBareProjectName(input string) bool {
	if input == "" || input == "." || input == ".." {
		return false
	}
	return reBareProjectName.MatchString(input)
}

// SplitRef extracts an inline @ref suffix from a target string,
// following Go module style: "owner/repo@v1.0" → ("owner/repo", "v1.0").
// Returns the original target unchanged when no ref is present or when
// the @ belongs to URL auth syntax (git@host, scheme://user@host).
func SplitRef(target string) (base, ref string) {
	// Scheme-based and SSH URLs use @ for auth — don't split.
	if strings.HasPrefix(target, "git@") ||
		strings.Contains(target, "://") {
		return target, ""
	}
	if idx := strings.LastIndex(target, "@"); idx > 0 && idx < len(target)-1 {
		return target[:idx], target[idx+1:]
	}
	return target, ""
}

// RepoIdentity holds the parsed identity of a remote repository,
// following Go-module-style canonical paths.
type RepoIdentity struct {
	// Canonical is the full module-style path, e.g.
	// "github.com/gorilla/mux" or "github.com/gorilla/mux@feature/v2".
	Canonical string

	// Name is the display name (last two path segments + optional @ref),
	// e.g. "gorilla/mux" or "gorilla/mux@feature/v2".
	Name string

	// CloneURL is the original URL suitable for passing to git.Clone,
	// e.g. "https://github.com/gorilla/mux".
	CloneURL string
}

// ParseRepoURL parses a Git URL into a RepoIdentity. If branch is non-empty
// it is appended with "@" (Go module style: path@ref).
//
// Supported formats: https://, http://, git@host:, ssh://
func ParseRepoURL(rawURL, branch string) (RepoIdentity, error) {
	cloneURL := rawURL
	modulePath, err := normalizeURL(rawURL)
	if err != nil {
		return RepoIdentity{}, err
	}

	canonical := modulePath
	if branch != "" {
		canonical += "@" + branch
	}

	// Display name: last two path segments + @ref.
	parts := strings.Split(modulePath, "/")
	var name string
	if len(parts) >= 2 {
		name = parts[len(parts)-2] + "/" + parts[len(parts)-1]
	} else {
		name = modulePath
	}
	if branch != "" {
		name += "@" + branch
	}

	return RepoIdentity{
		Canonical: canonical,
		Name:      name,
		CloneURL:  cloneURL,
	}, nil
}

// normalizeURL strips protocol, user-info, trailing .git, and slashes
// to produce a Go-module-style path (e.g. "github.com/gorilla/mux").
func normalizeURL(raw string) (string, error) {
	s := raw

	// Handle SSH shorthand: git@host:owner/repo.git
	if after, ok := strings.CutPrefix(s, "git@"); ok {
		// git@github.com:org/repo.git → github.com/org/repo.git
		s = after
		s = strings.Replace(s, ":", "/", 1)
		s = strings.TrimSuffix(s, ".git")
		return cleanPath(s)
	}

	// Handle ssh:// URLs: ssh://git@host/path
	if after, ok := strings.CutPrefix(s, "ssh://"); ok {
		s = after
		// Strip user@ prefix if present.
		if idx := strings.Index(s, "@"); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, ".git")
		return cleanPath(s)
	}

	// Handle https:// and http://
	for _, prefix := range []string{"https://", "http://"} {
		if after, ok := strings.CutPrefix(s, prefix); ok {
			s = after
			// Strip user@ prefix if present (e.g. https://user@host/path).
			if atIdx := strings.Index(s, "@"); atIdx != -1 {
				slashIdx := strings.Index(s, "/")
				if slashIdx == -1 || atIdx < slashIdx {
					s = s[atIdx+1:]
				}
			}
			s = strings.TrimSuffix(s, ".git")
			return cleanPath(s)
		}
	}

	return "", fmt.Errorf("unsupported URL format: %q", raw)
}

// cleanPath trims trailing slashes and validates the path has at least
// a host and one path segment.
func cleanPath(s string) (string, error) {
	s = strings.TrimRight(s, "/")
	if s == "" || !strings.Contains(s, "/") {
		return "", fmt.Errorf("invalid repository URL: need at least host/path, got %q", s)
	}
	return s, nil
}
