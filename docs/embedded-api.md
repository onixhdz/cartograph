# Embedded Go API

Cartograph can be used as an in-process Go library through the root package:

```go
import "github.com/onixhdz/cartograph"
```

The embedded API is intended for long-running local tools, such as an MCP server that wants Cartograph's code intelligence without shelling out to the CLI or running a separate Cartograph service.

## Basic workflow

```go
ctx := context.Background()

client, err := cartograph.Open(cartograph.Config{})
if err != nil {
    return err
}
defer client.Close()

result, err := client.Analyze(ctx, ".", cartograph.AnalyzeOptions{})
if err != nil {
    return err
}

schema, err := client.Schema(ctx, result.RepoHash)
if err != nil {
    return err
}
_ = schema

matches, err := client.Search(ctx, result.RepoHash, "TODO", cartograph.SearchOptions{
    FixedStrings: true,
    Limit:        20,
})
if err != nil {
    return err
}
_ = matches
```

## Analysis targets

`Client.Analyze` accepts the same deterministic target strings as the CLI:

```go
// Full Git URL.
remoteResult, err := client.Analyze(ctx,
    "https://github.com/go-git/go-billy.git",
    cartograph.AnalyzeOptions{},
)

// Repository shorthand plus an inline branch or tag.
taggedResult, err := client.Analyze(ctx,
    "go-git/go-billy@v5.6.2",
    cartograph.AnalyzeOptions{},
)
```

Supported target forms are local paths, `https://`, `http://`, `ssh://`,
`git@host:path`, known-host paths such as `gitlab.com/group/project`,
`owner/repository` shorthand, inline `@ref`, and existing registry aliases.
Existing local paths win over shorthand interpretation.

Use `AnalyzeOptions.Ref` when the ref cannot be written inline, `CloneDepth` to
control shallow cloning, and `AuthToken` for private HTTPS repositories. Remote
source is cloned into temporary local storage and removed after analysis; only
the graph, source-content bucket, and search indexes remain in `DataDir`. Content
persistence stores files up to 10 MiB each with a 256 MiB repository total. The
caller's context controls cancellation, and clones use a five-minute timeout when
the context has no deadline.

Bare project names remain intentionally interactive in the CLI because several
repositories can share a name. Embedded callers must provide an unambiguous
`owner/repository`, host-prefixed path, full URL, or registry alias.

## Configuration

`cartograph.Config` is intentionally small:

- `DataDir`: Cartograph data directory. If empty, `cartograph.DefaultDataDir()` is used.

Progress reporting is per operation. Use `AnalyzeOptions.OnStep` and `AnalyzeOptions.OnFileProgress` when needed. The API is quiet by default and does not write to stdout or stderr.

## Caching and concurrency

The CLI uses a background service to avoid reloading indexes for every short-lived command. Embedded users should keep one `*cartograph.Client` open for their process lifetime instead.

- `Open` is cheap and does not load repository data.
- The first operation for a repository lazy-loads its graph and search indexes.
- Subsequent operations on the same client reuse the in-memory graph/index.
- `*Client` is safe for concurrent use by multiple goroutines.
- Multiple read-only embedded processes may read the same data directory concurrently.
- Writes, including `Analyze`, remain exclusive and can fail when another process holds conflicting storage handles.
- `Close` releases in-memory graphs, search indexes, and content resolvers.

If a standalone `cartograph serve` process already owns the data directory, `Open` returns an error matching `cartograph.ErrDataDirInUse`. This lets an embedding app fall back to its proxy/service mode explicitly.

## Read operations

The initial API includes the read operations needed by embedded tools and agents:

- `Query`
- `Search`
- `Context`
- `Impact`
- `Cypher`
- `Schema`
- `Cat`
- `Tree`
- `List`
- `Status`

`Impact` returns the upstream or downstream blast radius for a symbol using the indexed graph:

```go
impact, err := client.Impact(ctx, result.RepoHash, "SymbolName", cartograph.ImpactOptions{
    Direction: "downstream",
    Depth:     2,
})
if err != nil {
    return err
}
_ = impact
```

`Tree` returns the indexed file inventory from Cartograph's graph as sorted repo-relative paths. It is not a raw filesystem walk.

`Schema` summarizes labels, relationship types, relationship patterns, and property names so callers can guide agents away from invalid Cypher queries.

## Plugin registration

Embedded callers can register Go plugin implementations directly in-process:

```go
status, err := client.RegisterPlugin(ctx, myPlugin, cartograph.RegisterPluginOptions{
    Config: map[string]string{"token": token},
})
if err != nil {
    return err
}

matches, err := client.Query(ctx, status.Repo, "search text", cartograph.QueryOptions{
    Plugin: true,
    Limit:  5,
})
```

`RegisterPlugin` calls `Info`, stores `Resources`, runs `Ingest`, persists emitted nodes/edges as a plugin dataset, and reloads the dataset so it is immediately queryable through `QueryOptions.Plugin`. Cancellation and timeout are cooperative: Cartograph passes the context to plugin methods and host operations, but plugin code must honor the context to stop promptly.

Cypher queries can address registered plugin datasets by `status.Repo` or `status.RepoHash`.

## Build requirements

Importers need CGO enabled and a C compiler because Cartograph uses tree-sitter bindings for analysis.

Zig and the `embedding_cgo` build tag are not required for the embedded API. v1 embedded search is lexical-only; hybrid semantic search and embedding job lifecycle APIs are deferred.

## Deferred from v1

The embedded API deliberately excludes:

- interactive bare-project repository search,
- multi-repository candidate selection within one remote target,
- embedding-backed semantic search and embedding job management,
- out-of-process or polyglot plugin runtimes,
- public storage interfaces,
- public query backend interfaces,
- direct graph mutation,
- direct access to `lpg.Graph`,
- HTTP service management.

These can be added later as small, deliberate API additions when there is a concrete embedded-use requirement.
