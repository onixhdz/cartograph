package remote

import "testing"

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://github.com/gorilla/mux", true},
		{"http://gitlab.com/group/project", true},
		{"git@github.com:org/repo.git", true},
		{"ssh://git@gitlab.com/g/p", true},
		{".", false},
		{"/home/user/project", false},
		{"relative/path", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsGitURL(tt.input); got != tt.want {
				t.Errorf("IsGitURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		branch    string
		canonical string
		repoName  string
		cloneURL  string
	}{
		{
			name:      "HTTPS GitHub",
			url:       "https://github.com/gorilla/mux",
			canonical: "github.com/gorilla/mux",
			repoName:  "gorilla/mux",
			cloneURL:  "https://github.com/gorilla/mux",
		},
		{
			name:      "HTTPS with .git suffix",
			url:       "https://github.com/gorilla/mux.git",
			canonical: "github.com/gorilla/mux",
			repoName:  "gorilla/mux",
			cloneURL:  "https://github.com/gorilla/mux.git",
		},
		{
			name:      "HTTPS with trailing slash",
			url:       "https://github.com/gorilla/mux/",
			canonical: "github.com/gorilla/mux",
			repoName:  "gorilla/mux",
			cloneURL:  "https://github.com/gorilla/mux/",
		},
		{
			name:      "HTTPS with branch",
			url:       "https://github.com/gorilla/mux",
			branch:    "feature/v2",
			canonical: "github.com/gorilla/mux@feature/v2",
			repoName:  "gorilla/mux@feature/v2",
			cloneURL:  "https://github.com/gorilla/mux",
		},
		{
			name:      "HTTPS with tag",
			url:       "https://github.com/gorilla/mux",
			branch:    "v1.8.0",
			canonical: "github.com/gorilla/mux@v1.8.0",
			repoName:  "gorilla/mux@v1.8.0",
			cloneURL:  "https://github.com/gorilla/mux",
		},
		{
			name:      "SSH shorthand",
			url:       "git@github.com:org/private-api.git",
			canonical: "github.com/org/private-api",
			repoName:  "org/private-api",
			cloneURL:  "git@github.com:org/private-api.git",
		},
		{
			name:      "SSH shorthand with branch",
			url:       "git@github.com:org/repo.git",
			branch:    "develop",
			canonical: "github.com/org/repo@develop",
			repoName:  "org/repo@develop",
			cloneURL:  "git@github.com:org/repo.git",
		},
		{
			name:      "SSH protocol URL",
			url:       "ssh://git@gitlab.com/group/project",
			canonical: "gitlab.com/group/project",
			repoName:  "group/project",
			cloneURL:  "ssh://git@gitlab.com/group/project",
		},
		{
			name:      "HTTP self-hosted",
			url:       "http://gitea.internal.com/devteam/backend",
			canonical: "gitea.internal.com/devteam/backend",
			repoName:  "devteam/backend",
			cloneURL:  "http://gitea.internal.com/devteam/backend",
		},
		{
			name:      "GitLab nested group",
			url:       "https://gitlab.com/group/subgroup/project",
			canonical: "gitlab.com/group/subgroup/project",
			repoName:  "subgroup/project",
			cloneURL:  "https://gitlab.com/group/subgroup/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseRepoURL(tt.url, tt.branch)
			if err != nil {
				t.Fatalf("ParseRepoURL(%q, %q) error: %v", tt.url, tt.branch, err)
			}
			if id.Canonical != tt.canonical {
				t.Errorf("Canonical = %q, want %q", id.Canonical, tt.canonical)
			}
			if id.Name != tt.repoName {
				t.Errorf("Name = %q, want %q", id.Name, tt.repoName)
			}
			if id.CloneURL != tt.cloneURL {
				t.Errorf("CloneURL = %q, want %q", id.CloneURL, tt.cloneURL)
			}
		})
	}
}

func TestParseRepoURL_Errors(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"bare host", "https://github.com"},
		{"local path", "/home/user/project"},
		{"relative path", "relative/path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRepoURL(tt.url, "")
			if err == nil {
				t.Errorf("ParseRepoURL(%q) expected error, got nil", tt.url)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/gorilla/mux", "github.com/gorilla/mux"},
		{"https://github.com/gorilla/mux.git", "github.com/gorilla/mux"},
		{"git@github.com:org/repo.git", "github.com/org/repo"},
		{"ssh://git@gitlab.com/g/p", "gitlab.com/g/p"},
		{"https://user@bitbucket.org/team/repo", "bitbucket.org/team/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := normalizeURL(tt.input)
			if err != nil {
				t.Fatalf("normalizeURL(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsGitHostURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Known hosts — should be detected.
		{"github.com/hashicorp/nomad", true},
		{"github.com/gorilla/mux", true},
		{"gitlab.com/inkscape/inkscape", true},
		{"bitbucket.org/hashicorp/tf-test-git", true},
		{"codeberg.org/forgejo/forgejo", true},
		{"sr.ht/~sircmpwn/aerc", true},

		// Subdirectory paths (go-getter style).
		{"github.com/hashicorp/nomad/api", true},

		// Not host-prefixed — protocol URLs.
		{"https://github.com/gorilla/mux", false},
		{"git@github.com:org/repo.git", false},

		// Not host-prefixed — just the host with no path.
		{"github.com/", false},
		{"gitlab.com/", false},

		// Not host-prefixed — local paths, shorthand, etc.
		{"temporalio/temporal", false},
		{"./foo", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsGitHostURL(tt.input); got != tt.want {
				t.Errorf("IsGitHostURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandGitHostURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/hashicorp/nomad", "https://github.com/hashicorp/nomad"},
		{"gitlab.com/inkscape/inkscape", "https://gitlab.com/inkscape/inkscape"},
		{"bitbucket.org/team/repo", "https://bitbucket.org/team/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ExpandGitHostURL(tt.input); got != tt.want {
				t.Errorf("ExpandGitHostURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsGitHubShorthand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid shorthands.
		{"temporalio/temporal", true},
		{"hashicorp/nomad", true},
		{"gorilla/mux", true},
		{"my-org/my-repo", true},
		{"user123/project.go", true},
		{"a/b", true},
		{"org/repo_name", true},

		// Not shorthands — URLs.
		{"https://github.com/gorilla/mux", false},
		{"git@github.com:org/repo.git", false},
		{"ssh://git@gitlab.com/g/p", false},

		// Not shorthands — host-prefixed URLs (handled by IsGitHostURL).
		{"github.com/hashicorp/nomad", false},
		{"gitlab.com/inkscape/inkscape", false},
		{"bitbucket.org/team/repo", false},

		// Not shorthands — local paths.
		{".", false},
		{"./relative/path", false},
		{"../parent/path", false},
		{"/absolute/path", false},
		{"~user/path", false},

		// Not shorthands — too many or too few segments.
		{"just-a-name", false},
		{"a/b/c", false},

		// Not shorthands — edge cases.
		{"", false},
		{"/", false},
		{".hidden/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsGitHubShorthand(tt.input); got != tt.want {
				t.Errorf("IsGitHubShorthand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandGitHubShorthand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"temporalio/temporal", "https://github.com/temporalio/temporal"},
		{"hashicorp/nomad", "https://github.com/hashicorp/nomad"},
		{"gorilla/mux", "https://github.com/gorilla/mux"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ExpandGitHubShorthand(tt.input); got != tt.want {
				t.Errorf("ExpandGitHubShorthand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsBareProjectName(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid bare project names.
		{"nomad", true},
		{"temporal", true},
		{"vue.js", true},
		{"go-getter", true},
		{"React", true},
		{"my_project", true},
		{"a", true},
		{"123", true},

		// Not bare names — contain slashes (owner/repo or paths).
		{"hashicorp/nomad", false},
		{"temporalio/temporal", false},
		{"github.com/gorilla/mux", false},

		// Not bare names — path-like.
		{".", false},
		{"..", false},
		{"./foo", false},
		{"../bar", false},
		{"/absolute", false},
		{".hidden", false},

		// Not bare names — edge cases.
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsBareProjectName(tt.input); got != tt.want {
				t.Errorf("IsBareProjectName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatStars(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{14200, "14.2k"},
		{100000, "100.0k"},
	}
	for _, tt := range tests {
		if got := FormatStars(tt.input); got != tt.want {
			t.Errorf("FormatStars(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
