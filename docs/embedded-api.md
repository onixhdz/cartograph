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

Plugin datasets are addressed with the existing plugin SDK and registry behavior. `QueryOptions.Plugin` routes plugin query search. Cypher queries address plugin datasets by repository name, such as `mitre-cwe` or `mitre-capec`.

## Build requirements

Importers need CGO enabled and a C compiler because Cartograph uses tree-sitter bindings for analysis.

Zig and the `embedding_cgo` build tag are not required for the embedded API. v1 embedded search is lexical-only; hybrid semantic search and embedding job lifecycle APIs are deferred.

## Deferred from v1

The first embedded API deliberately excludes:

- remote URL/GitHub-shorthand analysis,
- embedding-backed semantic search and embedding job management,
- plugin ingestion lifecycle management,
- public storage interfaces,
- public query backend interfaces,
- direct graph mutation,
- direct access to `lpg.Graph`,
- HTTP service management.

These can be added later as small, deliberate API additions when there is a concrete embedded-use requirement.
