package cartograph

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"

	internalplugin "github.com/onixhdz/cartograph/internal/plugin"
	pluginsdk "github.com/onixhdz/cartograph/plugin"

	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/remote"
	"github.com/onixhdz/cartograph/internal/service"
	"github.com/onixhdz/cartograph/internal/storage"
)

func TestEmbeddedClientAnalyzeAndRead(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/embedded\n\ngo 1.25\n")
	writeFile(t, filepath.Join(repoDir, "main.go"), `package main

func Hello() string { return "hello" }

func main() { _ = Hello() }
`)

	client, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer client.Close()

	res, err := client.Analyze(context.Background(), repoDir, AnalyzeOptions{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if res.RepoName == "" || res.RepoHash == "" || res.NodeCount == 0 || res.EdgeCount == 0 {
		t.Fatalf("unexpected analyze result: %+v", res)
	}

	if _, err := client.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := client.Status(context.Background(), res.RepoHash); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := client.Schema(context.Background(), res.RepoHash); err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if _, err := client.Search(context.Background(), res.RepoHash, "Hello", SearchOptions{FixedStrings: true, Limit: 5}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := client.Cat(context.Background(), res.RepoHash, []string{"main.go"}, CatOptions{}); err != nil {
		t.Fatalf("Cat: %v", err)
	}
	if _, err := client.Query(context.Background(), res.RepoHash, "Hello", QueryOptions{Limit: 5}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if _, err := client.Context(context.Background(), res.RepoHash, "Hello", ContextOptions{}); err != nil {
		t.Fatalf("Context: %v", err)
	}
	if _, err := client.Impact(context.Background(), res.RepoHash, "Hello", ImpactOptions{Depth: 1}); err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if _, err := client.Cypher(context.Background(), res.RepoHash, "MATCH (n) RETURN n LIMIT 1", CypherOptions{}); err != nil {
		t.Fatalf("Cypher: %v", err)
	}
}

func TestEmbeddedClientAnalyzeRemoteShorthand(t *testing.T) {
	client, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var cloned remote.CloneOptions
	cloneCount := 0
	cloneDir := t.TempDir()
	client.cloneRemote = func(_ context.Context, opts remote.CloneOptions) (*remote.CloneResult, error) {
		cloneCount++
		cloned = opts
		fs := memfs.New()
		writeMemoryFile(t, fs, "go.mod", "module example.com/remote\n\ngo 1.25\n")
		writeMemoryFile(t, fs, "main.go", "package main\nfunc Hello() string { return \"hello\" }\nfunc main() { _ = Hello() }\n")
		return &remote.CloneResult{FS: fs, HeadSHA: strings.Repeat("a", 40), Branch: "v1.0.0", DiskPath: cloneDir}, nil
	}

	client.lsRemote = func(context.Context, remote.CloneOptions) (string, error) {
		return strings.Repeat("a", 40), nil
	}

	result, err := client.Analyze(context.Background(), "example/project@v1.0.0", AnalyzeOptions{
		CloneDepth: 2, AuthToken: "secret",
	})
	if err != nil {
		t.Fatalf("Analyze remote: %v", err)
	}
	if result.RepoHash == "" || result.NodeCount == 0 || result.Commit != strings.Repeat("a", 40) {
		t.Fatalf("unexpected result: %+v", result)
	}
	if cloned.URL != "https://github.com/example/project" || cloned.Branch != "v1.0.0" || cloned.Depth != 2 || cloned.AuthToken != "secret" {
		t.Fatalf("clone options = %+v", cloned)
	}
	if _, err := os.Stat(cloneDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary clone directory was not removed: %v", err)
	}
	cat, err := client.Cat(context.Background(), result.RepoHash, []string{"main.go"}, CatOptions{})
	if err != nil || len(cat.Files) != 1 || !strings.Contains(cat.Files[0].Content, "Hello") {
		t.Fatalf("Cat after remote analysis: result=%+v err=%v", cat, err)
	}

	second, err := client.Analyze(context.Background(), "example/project@v1.0.0", AnalyzeOptions{})
	if err != nil {
		t.Fatalf("second Analyze: %v", err)
	}
	if !second.Skipped || cloneCount != 1 {
		t.Fatalf("second analysis should reuse commit without cloning: result=%+v clones=%d", second, cloneCount)
	}
}

func TestEmbeddedClientAnalyzeRegistryReplayPreservesCloneURLAndExplicitRef(t *testing.T) {
	cases := []struct {
		name           string
		initialTarget  string
		initialRef     string
		resolvedBranch string
		replayTarget   string
		wantURL        string
		wantRef        string
	}{
		{
			name:           "GitLab resolved default branch",
			initialTarget:  "https://gitlab.com/group/subgroup/project.git",
			resolvedBranch: "main",
			replayTarget:   "subgroup/project",
			wantURL:        "https://gitlab.com/group/subgroup/project.git",
		},
		{
			name:           "self hosted explicit ref",
			initialTarget:  "https://git.example.test/team/service.git",
			initialRef:     "release/v2",
			resolvedBranch: "release/v2",
			replayTarget:   "service",
			wantURL:        "https://git.example.test/team/service.git",
			wantRef:        "release/v2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := Open(Config{DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			cloneCount := 0
			commit := strings.Repeat("c", 40)
			client.cloneRemote = func(_ context.Context, opts remote.CloneOptions) (*remote.CloneResult, error) {
				cloneCount++
				fs := memfs.New()
				writeMemoryFile(t, fs, "go.mod", "module example.com/replay\n\ngo 1.25\n")
				writeMemoryFile(t, fs, "main.go", "package main\nfunc main() {}\n")
				return &remote.CloneResult{FS: fs, HeadSHA: commit, Branch: tc.resolvedBranch}, nil
			}
			var replayOptions remote.CloneOptions
			client.lsRemote = func(_ context.Context, opts remote.CloneOptions) (string, error) {
				replayOptions = opts
				return commit, nil
			}

			if _, err := client.Analyze(context.Background(), tc.initialTarget, AnalyzeOptions{Ref: tc.initialRef}); err != nil {
				t.Fatalf("initial Analyze: %v", err)
			}
			replayed, err := client.Analyze(context.Background(), tc.replayTarget, AnalyzeOptions{})
			if err != nil {
				t.Fatalf("registry replay Analyze: %v", err)
			}
			if !replayed.Skipped || cloneCount != 1 {
				t.Fatalf("registry replay result=%+v cloneCount=%d", replayed, cloneCount)
			}
			if replayOptions.URL != tc.wantURL || replayOptions.Branch != tc.wantRef {
				t.Fatalf("registry replay options=%+v, want URL=%q ref=%q", replayOptions, tc.wantURL, tc.wantRef)
			}
		})
	}
}

func TestEmbeddedClientAnalyzeRemoteCancellation(t *testing.T) {
	client, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.cloneRemote = func(ctx context.Context, _ remote.CloneOptions) (*remote.CloneResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.Analyze(ctx, "example/project", AnalyzeOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestEmbeddedClientAnalyzeRemoteCancellationAfterClone(t *testing.T) {
	dataDir := t.TempDir()
	client, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	client.cloneRemote = func(context.Context, remote.CloneOptions) (*remote.CloneResult, error) {
		fs := memfs.New()
		writeMemoryFile(t, fs, "main.go", "package main\nfunc main() {}\n")
		cancel()
		return &remote.CloneResult{FS: fs, HeadSHA: strings.Repeat("b", 40), Branch: "main"}, nil
	}
	_, err = client.Analyze(ctx, "example/project", AnalyzeOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	registry, registryErr := storage.NewRegistry(dataDir)
	if registryErr != nil {
		t.Fatal(registryErr)
	}
	if len(registry.List()) != 0 {
		t.Fatalf("canceled clone published registry entries: %+v", registry.List())
	}
}

func TestEmbeddedClientAnalyzeClearsStaleEmbeddingMetadata(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "main.go"), "package main\nfunc Before() {}\n")
	client, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	first, err := client.Analyze(context.Background(), repoDir, AnalyzeOptions{})
	if err != nil {
		t.Fatalf("first Analyze: %v", err)
	}
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.UpdateEmbedding(first.RepoHash, storage.EmbeddingInfo{
		Status: storage.EmbeddingStatusComplete, Model: "stale-model", Dims: 384,
		Provider: "stale-provider", Nodes: 10, Total: 10, Duration: "1s",
	}); err != nil {
		t.Fatal(err)
	}
	if !client.client.HasCompleteEmbeddings(first.RepoHash) {
		t.Fatal("expected seeded embedding metadata to be complete")
	}

	writeFile(t, filepath.Join(repoDir, "main.go"), "package main\nfunc After() {}\n")
	second, err := client.Analyze(context.Background(), repoDir, AnalyzeOptions{Force: true})
	if err != nil {
		t.Fatalf("second Analyze: %v", err)
	}
	if second.Skipped {
		t.Fatal("forced analysis unexpectedly skipped")
	}
	registry, err = storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Get(first.RepoHash)
	if !ok {
		t.Fatal("reanalyzed repository missing from registry")
	}
	if entry.Meta.EmbeddingStatus != "" || entry.Meta.EmbeddingModel != "" || entry.Meta.EmbeddingDims != 0 ||
		entry.Meta.EmbeddingProvider != "" || entry.Meta.EmbeddingNodes != 0 || entry.Meta.EmbeddingTotal != 0 {
		t.Fatalf("stale embedding metadata survived reanalysis: %+v", entry.Meta)
	}
	if client.client.HasCompleteEmbeddings(first.RepoHash) {
		t.Fatal("reanalyzed repository still reports complete embeddings")
	}
}

func TestEmbeddedClientAnalyzeCancellationAfterPersistenceSettlesCache(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "main.go"), "package main\nfunc OldUniqueSymbol() {}\n")
	client, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	first, err := client.Analyze(context.Background(), repoDir, AnalyzeOptions{})
	if err != nil {
		t.Fatalf("first Analyze: %v", err)
	}
	writeFile(t, filepath.Join(repoDir, "main.go"), "package main\nfunc NewUniqueSymbol() {}\n")
	ctx, cancel := context.WithCancel(context.Background())
	client.onAnalysisPersisted = cancel
	result, err := client.Analyze(ctx, repoDir, AnalyzeOptions{Force: true})
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Analyze result=%+v error=%v, want context cancellation", result, err)
	}
	client.onAnalysisPersisted = nil

	searchResult, err := client.Search(context.Background(), first.RepoHash, "NewUniqueSymbol", SearchOptions{FixedStrings: true})
	if err != nil {
		t.Fatalf("Search after canceled publication: %v", err)
	}
	if searchResult.MatchCount == 0 {
		t.Fatalf("cache was not settled on the persisted graph: %+v", searchResult)
	}
}

func writeMemoryFile(t *testing.T, fs billy.Filesystem, path, content string) {
	t.Helper()
	file, err := fs.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(content)); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

type registerPluginTestPlugin struct {
	version        string
	label          string
	fail           bool
	blockResources bool
}

func (p registerPluginTestPlugin) Info() pluginsdk.Info {
	version := p.version
	if version == "" {
		version = "v1"
	}
	label := p.label
	if label == "" {
		label = "Widget"
	}
	return pluginsdk.Info{
		Name:        "test-plugin",
		Version:     version,
		Description: "test plugin",
		Entities: []pluginsdk.Entity{{
			Name:  "Widget",
			Label: label,
			Query: &pluginsdk.EntityQuery{
				SearchProps: []string{"name", "description"},
				Display:     []pluginsdk.DisplayField{{Prop: "name", Label: "Name"}},
			},
		}},
	}
}

func (p registerPluginTestPlugin) Resources(ctx context.Context) ([]pluginsdk.PluginResource, error) {
	if p.blockResources {
		<-ctx.Done()
		return nil, fmt.Errorf("resources canceled: %w", ctx.Err())
	}
	return []pluginsdk.PluginResource{{Name: "Guide", Content: "# Guide"}}, nil
}

func (p registerPluginTestPlugin) Ingest(ctx context.Context, host pluginsdk.Host, _ pluginsdk.IngestOptions) (pluginsdk.IngestResult, error) {
	if p.fail {
		return pluginsdk.IngestResult{}, errors.New("ingest failed")
	}
	err := host.Emit(ctx, pluginsdk.Node{
		ID:    "widget:1",
		Label: "Widget",
		Properties: map[string]any{
			"name":        "Sprocket",
			"description": "A useful widget",
		},
	})
	if err != nil {
		return pluginsdk.IngestResult{}, fmt.Errorf("emit widget: %w", err)
	}
	return pluginsdk.IngestResult{Nodes: 1}, nil
}

func TestClientRegisterPluginPersistsAndQueries(t *testing.T) {
	dataDir := t.TempDir()
	client, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer client.Close()

	status, err := client.RegisterPlugin(context.Background(), registerPluginTestPlugin{}, RegisterPluginOptions{})
	if err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if status.PluginName != "test-plugin" || status.Repo != "test-plugin" || status.NodeCount != 1 || status.ResourceCount != 1 {
		t.Fatalf("status = %+v", status)
	}
	if status.RepoHash != internalplugin.PluginDatasetHash("test-plugin", "test-plugin") {
		t.Fatalf("repo hash = %q", status.RepoHash)
	}

	res, err := client.Query(context.Background(), status.Repo, "sprocket", QueryOptions{Plugin: true, Limit: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.PluginResults) != 1 || res.PluginResults[0].NodeID != "widget:1" {
		t.Fatalf("plugin results = %#v", res.PluginResults)
	}

	status2, err := client.RegisterPlugin(context.Background(), registerPluginTestPlugin{}, RegisterPluginOptions{})
	if err != nil {
		t.Fatalf("RegisterPlugin second: %v", err)
	}
	if status2.RepoHash != status.RepoHash {
		t.Fatalf("second repo hash = %q, want %q", status2.RepoHash, status.RepoHash)
	}
}

func TestClientRegisterPluginTimeoutCoversResources(t *testing.T) {
	client, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer client.Close()

	_, err = client.RegisterPlugin(context.Background(), registerPluginTestPlugin{blockResources: true}, RegisterPluginOptions{Timeout: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "resources canceled") {
		t.Fatalf("err = %v, want resources cancellation", err)
	}
}

func TestClientRegisterPluginFailedReregisterKeepsOldDatasetQueryable(t *testing.T) {
	dataDir := t.TempDir()
	client, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	status, err := client.RegisterPlugin(context.Background(), registerPluginTestPlugin{}, RegisterPluginOptions{})
	if err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	client, err = Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	_, err = client.RegisterPlugin(context.Background(), registerPluginTestPlugin{version: "v2", label: "OtherWidget", fail: true}, RegisterPluginOptions{})
	if err == nil {
		t.Fatal("expected failed re-register")
	}
	reg, err := internalplugin.LoadInstalledRegistry(internalplugin.InstalledRegistryPath(dataDir))
	if err != nil {
		t.Fatalf("LoadInstalledRegistry: %v", err)
	}
	installed := internalplugin.FindInstalledPlugin(reg, "test-plugin")
	if installed == nil || installed.Version != "v1" || len(installed.Entities) != 1 || installed.Entities[0].Label != "Widget" {
		t.Fatalf("failed re-register changed installed metadata: %+v", installed)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}

	client, err = Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Open third: %v", err)
	}
	defer client.Close()
	res, err := client.Query(context.Background(), status.Repo, "sprocket", QueryOptions{Plugin: true, Limit: 5})
	if err != nil {
		t.Fatalf("Query old dataset: %v", err)
	}
	if len(res.PluginResults) != 1 || res.PluginResults[0].NodeID != "widget:1" {
		t.Fatalf("plugin results after failed re-register = %#v", res.PluginResults)
	}
}

func TestClientTreeReturnsIndexedFiles(t *testing.T) {
	const repo = "tree-repo"
	mc := service.NewMemoryClient("")
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{FilePath: "main.go", BaseNodeProps: graph.BaseNodeProps{ID: "file://main.go", Name: "main.go"}})
	graph.AddFileNode(g, graph.FileProps{FilePath: "./internal\\subdir/../a_test.go", BaseNodeProps: graph.BaseNodeProps{ID: "file://internal/a_test.go", Name: "a_test.go"}})
	graph.AddFileNode(g, graph.FileProps{FilePath: "main.go", BaseNodeProps: graph.BaseNodeProps{ID: "file://main-duplicate.go", Name: "main.go"}})
	graph.AddFileNode(g, graph.FileProps{FilePath: "../escape.go", BaseNodeProps: graph.BaseNodeProps{ID: "file://escape.go", Name: "escape.go"}})
	graph.AddFileNode(g, graph.FileProps{FilePath: "foo/../..", BaseNodeProps: graph.BaseNodeProps{ID: "file://parent.go", Name: "parent.go"}})
	mc.LoadGraph(repo, g, nil)
	client := &Client{client: mc}
	defer client.Close()

	res, err := client.Tree(context.Background(), repo, TreeOptions{})
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	want := []string{"internal/a_test.go", "main.go"}
	if strings.Join(res.Files, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %#v, want %#v", res.Files, want)
	}
}

func TestClientTreeMissingRepo(t *testing.T) {
	mc := service.NewMemoryClient("")
	client := &Client{client: mc}
	defer client.Close()

	_, err := client.Tree(context.Background(), "missing", TreeOptions{})
	if err == nil {
		t.Fatal("expected missing repo error")
	}
	if !strings.Contains(err.Error(), `cartograph tree "missing"`) || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("error = %v, want useful tree and repository context", err)
	}
}

func TestConvertTreeResultCopiesFiles(t *testing.T) {
	in := &service.TreeResult{Repo: "repo", Files: []string{"a.go"}}
	out := convertTreeResult(in)
	if out == nil {
		t.Fatal("nil result")
	}
	in.Files[0] = "mutated.go"
	if out.Files[0] != "a.go" {
		t.Fatalf("files aliased input: %#v", out.Files)
	}
	out.Files[0] = "out-mutated.go"
	if in.Files[0] != "mutated.go" {
		t.Fatalf("input aliased output: %#v", in.Files)
	}
}

func TestConvertImpactResultMapsFieldsAndCopiesAffected(t *testing.T) {
	in := &service.ImpactResult{
		Target: service.SymbolMatch{
			Name:      "Target",
			FilePath:  "target.go",
			StartLine: 10,
			EndLine:   12,
			Label:     "Function",
			Repo:      "repo",
		},
		Affected: []service.SymbolMatch{{
			Name:        "Affected",
			FilePath:    "affected.go",
			StartLine:   20,
			EndLine:     21,
			Label:       "Function",
			ProcessName: "process",
			Content:     "func Affected() {}",
			Score:       0.75,
			Repo:        "repo",
			Signature:   "func Affected()",
		}},
		Depth: 2,
	}

	out := convertImpactResult(in)
	if out == nil {
		t.Fatal("nil result")
	}
	expectedTarget := SymbolMatch{
		Name:      "Target",
		FilePath:  "target.go",
		StartLine: 10,
		EndLine:   12,
		Label:     "Function",
		Repo:      "repo",
	}
	if out.Target != expectedTarget {
		t.Fatalf("target = %+v, want %+v", out.Target, expectedTarget)
	}
	if out.Depth != 2 {
		t.Fatalf("depth = %d, want 2", out.Depth)
	}
	if len(out.Affected) != 1 {
		t.Fatalf("affected len = %d, want 1", len(out.Affected))
	}
	expectedAffected := SymbolMatch{
		Name:        "Affected",
		FilePath:    "affected.go",
		StartLine:   20,
		EndLine:     21,
		Label:       "Function",
		ProcessName: "process",
		Content:     "func Affected() {}",
		Score:       0.75,
		Repo:        "repo",
		Signature:   "func Affected()",
	}
	if out.Affected[0] != expectedAffected {
		t.Fatalf("affected = %+v, want %+v", out.Affected[0], expectedAffected)
	}

	in.Affected[0].Name = "mutated"
	if out.Affected[0].Name != "Affected" {
		t.Fatalf("affected aliased input: %+v", out.Affected[0])
	}
	out.Affected[0].Name = "out-mutated"
	if in.Affected[0].Name != "mutated" {
		t.Fatalf("input aliased output: %+v", in.Affected[0])
	}
}

func TestOpenReturnsErrDataDirInUseForLiveService(t *testing.T) {
	dataDir := t.TempDir()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	lock := service.NewLockfile(dataDir)
	if err := lock.Acquire(ln.Addr().String(), "tcp"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lock.Release()

	_, err = Open(Config{DataDir: dataDir})
	if !errors.Is(err, ErrDataDirInUse) {
		t.Fatalf("Open error = %v, want ErrDataDirInUse", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
