# Cartograph — Agent Instructions

Graph-powered code intelligence tool. Indexes repositories into a knowledge graph (symbols, calls, imports, inheritance, processes) and exposes CLI/MCP tools for AI editors.

## Architecture

Five layers, top to bottom:

1. **CLI** (`cmd/`) — Kong-based commands (`analyze`, `query`, `context`, `impact`, `cypher`, `schema`, `wiki`, `source`, `clone`, `models`, `serve`, `skills`). All commands operate through a `ServiceClient` interface defined in `cmd/helpers.go`.

2. **Service** (`internal/service/`) — Two `ServiceClient` implementations: an HTTP client that talks to a background server over a unix socket (TCP fallback), and a `MemoryClient` that runs everything in-process. `main.go` checks for a running server via lockfile; if found it uses the HTTP client, otherwise spins up a `MemoryClient`. The server manages per-repo graphs, search indexes, and background embedding jobs.

3. **Ingestion** (`internal/ingestion/`) — 13-step pipeline: walk filesystem → build structure → parse with tree-sitter (hand-crafted queries for 13 languages, grammar-agnostic fallback for 56+) → resolve imports/calls/heritage across files → Leiden community detection → BFS process tracing from entry points. All steps produce or mutate an in-memory `lpg.Graph`.

4. **Query** (`internal/query/`) — Tool backends (`Backend` struct) that operate on the in-memory graph. Hybrid search merges BM25 (Bleve) and vector (brute-force cosine) results via Reciprocal Rank Fusion. Cypher queries execute via `opencypher` with write-blocking security.

5. **Storage & Search** (`internal/storage/`, `internal/search/`, `internal/embedding/`) — Graph persistence uses bbolt + msgpack. Registry (`registry.json`) tracks all indexed repos. BM25 indexes built from graph nodes at query time. Hybrid search merges BM25 + vector results via Reciprocal Rank Fusion (RRF). Embedding via built-in llamacpp provider (bge-small default, 6 model aliases from Hugging Face) or external OpenAI-compatible API. Models managed via `cartograph models` (list/pull/rm). Model cache at `~/.cache/cartograph/models/`.

## Planning

Design documents, implementation plans, and roadmap details live in `.local.plans/` (gitignored via `.local.*`). Always check this directory for existing plans before starting work on a feature — there may already be a design doc or prior analysis. When creating new plans or design documents, place them in `.local.plans/`.

## Key Design Decisions

- **Single edge label**: All relationships use `EdgeLabel = "CodeRelation"` with a `type` property (e.g., `CALLS`, `IMPORTS`). Use `graph.GetEdgeRelType(e)` to read it.
- **In-memory graph**: The graph library is `cloudprivacylabs/lpg/v2`. Nodes have labels and string-keyed properties. No external database.
- **Dual client architecture**: `main.go` checks for a running background service via lockfile. If found, uses HTTP client; otherwise creates `MemoryClient` for in-process operation.
- **Node IDs follow conventions**: files → `"file:path/to/file.go"`, folders → `"folder:path"`, symbols → `"func:path:Name"`, dependencies → `"dep:source:name"`.
- **Confidence scoring**: Call resolution produces confidence values (0-1) stored on `CALLS` edges. Process detection filters by `MinConfidence` (default 0.5).
- **Tree-sitter parsing**: Uses WASM-compiled grammars via `gotreesitter`. 13 tier-1 languages have hand-crafted queries; 56+ use grammar-agnostic inference.

## Build & Test

> **CRITICAL: Always use `task build:dev` to build. NEVER use `go build` directly.**
> `go build` will fail because it cannot link the native embedding library. The only correct way to build is `task build:dev`. This is non-negotiable.

```bash
# === BUILD (MANDATORY: use task, never go build) ===
task build:dev          # Development build — ALWAYS use this

# === TEST ===
go test -short ./...    # Unit tests (skips network/integration)
go test -count=1 ./...  # All tests including integration (requires network)

# === LINT ===
golangci-lint run ./...  # Install via: brew install golangci-lint

# === RELEASE / SPECIALIZED (rarely needed) ===
task build              # Release cross-compilation
task build:embedding:lib     # Rebuild native embedding library only
task build:target:linux # Linux binary only
```

`task build:dev` auto-detects your host OS/arch, compiles the native embedding library via Zig, and produces a working binary with CGO. `task build` is for release cross-compilation only.

## Testing Patterns

- **Mock client**: Tests in `cmd/` use a `mockClient` struct implementing `ServiceClient` — see [cmd/cli_test.go](cmd/cli_test.go). No real graphs or servers needed.
- **TestMain guardrails**: `cmd/testmain_test.go` sets `GOMEMLIMIT=1GiB`, `GOGC=50`, and caps embedding workers to prevent OOM in containers.
- **E2E tests**: `cmd/e2e_test.go` tests are long-running (clone + analyze real repos) and guarded by `testing.Short()`. They run with `go test -count=1`.
- **Pipeline tests**: `internal/ingestion/*_test.go` build graphs in-memory via `NewPipeline` and assert node/edge properties directly.
- **`-short` flag**: Most unit tests run with `-short`. Tests requiring network or heavy computation check `testing.Short()` and skip.

## Conventions

- **CLI framework**: [kong](https://github.com/alecthomas/kong). Commands are struct fields on `CLI` with `cmd:""` tags. Subcommands embed in parent structs.
- **Error style**: Return `fmt.Errorf("component: %w", err)` with a component prefix. No panics outside tests.
- **Data directory**: `~/.local/share/cartograph/` (respects `$XDG_DATA_HOME`). Each repo gets `{dataDir}/{repoName}/{hash}/` with `graph.db`, BM25 index, embeddings.
- **Graph property access**: Use constants from `graph.PropXxx` (e.g., `graph.PropName`, `graph.PropFilePath`). Use typed getters like `graph.GetStringProp(node, graph.PropName)`.
- **Environment variables**: `CARTOGRAPH_TIMING=1` enables pipeline step timing. `CARTOGRAPH_EMBEDDING_WORKERS` caps concurrent WASM workers. `GITHUB_TOKEN` for private repos.
- **Serialization**: Graph persistence uses msgpack (via `vmihailenco/msgpack/v5`) in bbolt. API transport uses JSON.
- **No external services**: Everything runs locally. No databases, no cloud APIs (embedding is optional and configurable).
- **Modern Go style**: This project targets **Go 1.25+**. Prefer modern idioms over legacy patterns:
  - Use `range` over integers (`for i := range n`) instead of C-style `for i := 0; i < n; i++`.
  - Use `slices`, `maps`, and `cmp` standard library packages instead of hand-rolled helpers where appropriate.
  - Use `errors.Is` / `errors.As` instead of string matching on error messages.
  - Use structured logging (`log/slog`) if adding new log calls.
  - Use iterator patterns (`iter.Seq`, `iter.Seq2`) when appropriate for lazy sequences.
  - Prefer `slices.Contains` over manual loops, `slices.SortFunc` over `sort.Slice`, `maps.Keys`/`maps.Values` over manual collection.
  - Use `strings.CutPrefix` / `strings.CutSuffix` instead of `strings.HasPrefix` + `strings.TrimPrefix` pairs.
  - Use `context.WithoutCancel` when spawning background work that should outlive a parent context.

## Version Control

**Never commit, push, or create branches unless the user explicitly asks.** Stick to code changes only. The user controls all git operations.

## Post-Change Checklist

After **all** code changes are completed, run these steps in order:

```bash
go fix ./...
go vet ./...
task lint              # or: golangci-lint run ./...
```

1. `go fix` / `go vet` — applies automated fixes and catches common issues.
2. **`task lint`** (equivalent to `golangci-lint run ./...`) — runs the full linter suite. **This is mandatory.** Fix every reported issue before considering the work done. Do not skip this step.
