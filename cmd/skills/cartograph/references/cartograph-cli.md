# Cartograph CLI Reference

Complete command reference for the cartograph CLI tool.

## Commands

### `cartograph analyze [path|url ...]`

Index one or more repositories — builds the knowledge graph from source code.
Accepts multiple targets in a single command; each is indexed independently.

**Arguments:**
- `path|url ...` — One or more local paths or Git URLs (defaults to current directory)

**Flags:**
- `--force` — Force full re-analysis, ignoring cache
- `--clone` — Clone URL to disk (keeps source + git history)
- `--branch <name>` — Branch or tag to analyze
- `--depth <n>` — Clone depth (default: 1, 0 = full history)
- `--auth-token <token>` — Auth token for private repos (env: `GITHUB_TOKEN`)
- `--embed <mode>` — Embedding mode: `off` (default), `async`, or `sync`
- `--embed-provider <provider>` — Embedding provider: `llamacpp` (default) or `openai_compat`
- `--embed-endpoint <url>` — Endpoint URL for remote embedding providers (e.g., `https://api.openai.com`)
- `--embed-api-key <token>` — API key for remote providers (env: `CARTOGRAPH_EMBEDDING_API_KEY`)
- `--embed-model <name>` — Model alias (e.g., `nomic-code`) or Hugging Face repo ID

**Examples:**
```bash
# Index current directory
cartograph analyze

# Index a specific path
cartograph analyze /path/to/repo

# Index a remote repository
cartograph analyze https://github.com/org/repo

# GitHub shorthand — auto-expands to https://github.com/hashicorp/nomad
cartograph analyze hashicorp/nomad

# Index a specific tag or branch (Go module style: target@ref)
cartograph analyze hashicorp/nomad@v1.8.0
cartograph analyze hashicorp/nomad@release/1.7.x
cartograph analyze github.com/gorilla/mux@v1.8.0

# Host-prefixed URL — auto-expands to https://github.com/org/repo
cartograph analyze github.com/org/repo

# Bare project name — searches GitHub and suggests matches
cartograph analyze nomad

# Index multiple repositories in one command
cartograph analyze hashicorp/nomad moby/moby

# Mix local and remote targets
cartograph analyze . hashicorp/nomad /other/local/repo

# Index with full git clone
cartograph analyze --clone https://github.com/org/repo

# Force re-index
cartograph analyze --force

# Index with semantic embeddings (blocks until complete)
cartograph analyze <path|url> --embed sync

# Index with background embeddings
cartograph analyze <path|url> --embed async

# Index with a specific embedding model
cartograph analyze <path|url> --embed sync --embed-model nomic-code

# Index using an external OpenAI-compatible provider
cartograph analyze <path|url> --embed sync \
  --embed-provider openai_compat \
  --embed-endpoint https://api.openai.com \
  --embed-api-key $OPENAI_API_KEY \
  --embed-model text-embedding-3-small
```

### `cartograph query "<search>"`

Search the knowledge graph for execution flows and symbols. Uses BM25 full-text search, or hybrid BM25 + vector search when embeddings are available.

**Arguments:**
- `search` — Search query text (required)

**Flags:**
- `-r, --repo <name>` — Repository name (auto-detected from git). Accepts short names: `-r nomad` resolves to `hashicorp/nomad` if unambiguous.
- `-l, --limit <n>` — Maximum results (default: 10)
- `--content` — Include source content in results
- `--include-tests` — Include test and example files in results (excluded by default)

**Examples:**
```bash
# Search for authentication-related flows
cartograph query "authentication middleware"

# Search with source content
cartograph query "database connection" --content

# Search a specific repo (full name)
cartograph query "error handling" -r hashicorp/nomad

# Short name — resolves to hashicorp/nomad if unambiguous
cartograph query "scheduler evaluation" -r nomad
```

**Output:** Returns matching Processes (execution flows with relevance scores) and Definitions (symbol matches with file locations).

### `cartograph context <name>`

360° view of a code symbol — shows callers, callees, processes, and relationships.

**Arguments:**
- `name` — Symbol name to look up

**Flags:**
- `-r, --repo <name>` — Repository name (short names like `nomad` resolve automatically)
- `-f, --file <path>` — File path to disambiguate symbol
- `-d, --depth <N>` — Callee traversal depth (default: 1). Depth 1 shows direct callees; depth 2+ shows a transitive call tree
- `--uid <id>` — Unique symbol ID
- `--content` — Include source content
- `--include-tests` — Include test and example files in results (excluded by default)

**Examples:**
```bash
# Get context for a function
cartograph context handleLogin

# Disambiguate with file path
cartograph context parse -f src/parser.go

# Trace 3 levels deep into the call tree
cartograph context NewServer --depth 3

# Include source code
cartograph context processOrder --content
```

**Output:** Symbol details + Callers (who calls this) + Callees (what this calls, flat at depth 1 or tree at depth 2+) + Processes (execution flows involving this symbol). The call tree follows CALLS, SPAWNS, and DELEGATES_TO edges.

### `cartograph impact <target>`

Blast radius analysis — what breaks if you change a symbol.

**Arguments:**
- `target` — Symbol name or ID to analyze (required)

**Flags:**
- `-r, --repo <name>` — Repository name (short names like `nomad` resolve automatically)
- `-f, --file <path>` — File path to disambiguate target
- `--direction <dir>` — `upstream` or `downstream` (default: downstream)
- `-d, --depth <n>` — Maximum traversal depth (default: 5)
- `--include-tests` — Include test and example files in results (excluded by default)

**Examples:**
```bash
# Downstream impact (what breaks)
cartograph impact parseConfig

# Upstream impact (what depends on this)
cartograph impact parseConfig --direction upstream

# Limit depth
cartograph impact handleRequest -d 3

# Disambiguate with file
cartograph impact validate -f src/validators/input.go
```

**Output:** Target symbol + list of affected symbols with file locations and depth.

### `cartograph cypher "<query>"`

Execute raw OpenCypher queries against the knowledge graph.

**Arguments:**
- `query` — Cypher query string (required)

**Flags:**
- `-r, --repo <name>` — Repository name (short names like `nomad` resolve automatically)

**Examples:**
```bash
# Find all functions that call a specific function
cartograph cypher "MATCH (f:Function)-[:CALLS]->(g:Function) WHERE g.name = 'handleError' RETURN f.name, f.filePath"

# Find classes and their methods
cartograph cypher "MATCH (c:Class)-[:HAS_METHOD]->(m:Method) RETURN c.name, m.name, m.filePath"

# Find deeply nested call chains
cartograph cypher "MATCH p = (f:Function)-[:CALLS*1..3]->(g:Function) WHERE f.name = 'main' RETURN [n IN nodes(p) | n.name]"

# Find all exported functions
cartograph cypher "MATCH (f:Function) WHERE f.isExported = true RETURN f.name, f.filePath ORDER BY f.name"

# Find process steps
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(s) RETURN p.name, s.name ORDER BY p.name"

# Find the most architecturally important execution flows
cartograph cypher "MATCH (p:Process) RETURN p.name, p.importance, p.stepCount, p.callerCount ORDER BY p.importance DESC LIMIT 10"
```

### `cartograph source <file...>`

Retrieve full source code from an indexed repository.

**Arguments:**
- `file` — File path(s) relative to repo root (required, multiple allowed)

**Flags:**
- `-r, --repo <name>` — Repository name (short names like `nomad` resolve automatically)
- `-l, --lines <range>` — Line range (e.g. `40-60`)

**Examples:**
```bash
# Get full file
cartograph source src/handler.go

# Get specific lines
cartograph source src/handler.go -l 40-60

# Multiple files
cartograph source src/handler.go src/router.go
```

### `cartograph clone <url>`

Clone a remote repository to disk without indexing.

**Arguments:**
- `url` — Git URL to clone (required)

**Flags:**
- `--branch <name>` — Branch or tag to clone
- `--depth <n>` — Clone depth (default: 1)
- `--auth-token <token>` — Auth token for private repos

### `cartograph list`

List all indexed repositories.

**Output:** Table with repository name, URL/path, indexed timestamp, and graph size (nodes/edges).

### `cartograph status [--repo <name>] [--watch]`

Show index status for a repository.

**Flags:**
- `-r, --repo <name>` — Repository name (short names resolve automatically; defaults to current directory)
- `-w, --watch` — Continuously refresh status until embedding completes (shows a live progress)

**Output:** Name, path, URL, indexed time, node/edge counts, commit hash, branch, languages, duration, artifact sizes.

### `cartograph clean [--all]`

Delete index for current repository.

**Flags:**
- `--all` — Delete indexes for all repositories

### `cartograph schema [repo]`

Show the graph schema for a repository — node labels, relationship types, property keys, and counts. Invaluable before writing Cypher queries.

**Arguments:**
- `repo` — Repository name or hash (optional, defaults to current directory). Accepts GitHub shorthands like `hashicorp/nomad` if already indexed.

**When to use:**
- **Before writing Cypher queries** — run `schema` first to see what labels, relationship types, and properties exist in the graph so you can write accurate `MATCH` patterns.
- **Exploring an unfamiliar indexed repo** — quickly see how many functions, classes, communities, and processes were discovered.
- **Verifying index quality** — check that expected node types and edge types were extracted after `analyze`.

**Example:**
```bash
# Schema for current directory's repo
cartograph schema

# Schema for a specific indexed repo
cartograph schema hashicorp/nomad
```

**Output:** Node labels with counts, relationship types with counts, available property keys, total nodes/edges, and example Cypher queries.

## Graph Schema

### Node Labels

| Label | Description |
|---|---|
| `Function` | Standalone function |
| `Method` | Method on a class/struct |
| `Class` | Class definition |
| `Interface` | Interface definition |
| `Struct` | Struct definition |
| `Enum` | Enum definition |
| `File` | Source file |
| `Folder` | Directory |
| `Community` | Leiden community cluster |
| `Process` | Execution flow (entry point → call chain) |
| `Namespace` | Namespace/module |
| `Trait` | Trait (Rust) |
| `Impl` | Impl block (Rust) |
| `TypeAlias` | Type alias |
| `Const` | Constant |
| `Property` | Property/field |
| `Record` | Record type |
| `Constructor` | Constructor |
| `Macro` | Macro definition |
| `Typedef` | C/C++ typedef |
| `Union` | C/C++ union |
| `Delegate` | Delegate type |
| `Annotation` | Annotation/decorator |

### Relationship Types

| Type | Description |
|---|---|
| `CALLS` | Function/method calls another |
| `IMPORTS` | File imports another file/module |
| `DEFINES` | File defines a symbol |
| `CONTAINS` | Folder contains file, class contains method |
| `EXTENDS` | Class extends another class |
| `IMPLEMENTS` | Class implements an interface |
| `HAS_METHOD` | Class/struct has a method |
| `HAS_PROPERTY` | Class/struct has a property |
| `OVERRIDES` | Method overrides parent method |
| `MEMBER_OF` | Symbol is a member of a community |
| `STEP_IN_PROCESS` | Symbol is a step in an execution flow |
| `ACCESSES` | Function accesses a property/field |
| `USES` | Symbol uses another symbol |

### Key Properties

**Symbols:** `name`, `filePath`, `startLine`, `endLine`, `isExported`, `content`, `description`, `signature`, `parameterCount`, `returnType`

**Files:** `name`, `filePath`, `language`, `size`, `content`

**Processes:** `name`, `entryPoint`, `heuristicLabel`, `stepCount`, `callerCount`, `importance`

**Communities:** `name`, `modularity`, `size`

**Edges:** `type`, `confidence`, `reason`, `step`

## Common Cypher Patterns

```cypher
-- Find all callers of a function
MATCH (caller)-[:CALLS]->(target:Function {name: 'myFunc'})
RETURN caller.name, caller.filePath

-- Find the full call chain from an entry point
MATCH path = (entry:Function {name: 'main'})-[:CALLS*1..5]->(f:Function)
RETURN [n IN nodes(path) | n.name] AS chain

-- Find classes implementing an interface
MATCH (c:Class)-[:IMPLEMENTS]->(i:Interface {name: 'Handler'})
RETURN c.name, c.filePath

-- Find unused exported functions (no callers)
MATCH (f:Function {isExported: true})
WHERE NOT ()-[:CALLS]->(f)
RETURN f.name, f.filePath

-- Find circular dependencies between files
MATCH (a:File)-[:IMPORTS]->(b:File)-[:IMPORTS]->(a)
RETURN a.filePath, b.filePath

-- Find the most-called functions
MATCH (caller)-[:CALLS]->(f:Function)
RETURN f.name, f.filePath, count(caller) AS callerCount
ORDER BY callerCount DESC LIMIT 10

-- Find the most architecturally important execution flows
MATCH (p:Process)
RETURN p.name, p.importance, p.heuristicLabel, p.callerCount
ORDER BY p.importance DESC LIMIT 10
```

## Models

### `cartograph models list`

List all known model aliases and their cache status.

**Output:** Alias names with sizes and cache status. The default model is marked with `*`.

### `cartograph models pull [model]`

Download a model to the local cache.

**Arguments:**
- `model` — Model alias (e.g., `nomic-code`) or Hugging Face repo ID (optional, defaults to `bge-small`)

**Examples:**
```bash
# Pull the default model
cartograph models pull

# Pull a specific model by alias
cartograph models pull nomic-code

# Pull any GGUF model from Hugging Face
cartograph models pull "org/model-name-GGUF"
```

### `cartograph models rm <model>`

Remove a cached model from disk.

**Arguments:**
- `model` — Model alias or Hugging Face repo ID (required)

**Available model aliases:**

| Alias        | Repo                                            | Dimensions | Notes           |
| ------------ | ----------------------------------------------- | ---------- | --------------- |
| `bge-small`  | `CompendiumLabs/bge-small-en-v1.5-gguf`         | 384        | **Default**     |
| `bge-base`   | `CompendiumLabs/bge-base-en-v1.5-gguf`          | 768        | General purpose |
| `nomic-code` | `nomic-ai/nomic-embed-code-GGUF`                | 768        | Code-optimized  |
| `nomic-text` | `nomic-ai/nomic-embed-text-v1.5-GGUF`           | 768        | Text            |
| `jina-code`  | `ggml-org/jina-embeddings-v2-base-code-Q8_0-GGUF` | 768      | Code-optimized  |
| `qwen3`      | `Qwen/Qwen3-Embedding-0.6B-GGUF`               | 1024       | 0.6B parameters |

Models are cached at `~/.cache/cartograph/models/`. Cartograph also reads from the Hugging Face hub cache (`~/.cache/huggingface/hub/`) for zero-copy reuse.

## Background Service

### `cartograph serve start`

Start the background service.

**Flags:**
- `--socket <path>` — Unix socket path (auto-generated by default)
- `--no-idle` — Disable idle timeout
- `--timeout <min>` — Idle timeout in minutes (default: 30)
- `--no-detach` — Run in foreground (default: detaches)

### `cartograph serve stop`

Stop the running background service.

### `cartograph serve status`

Check if the background service is running and healthy.

**Notes:**
- The service auto-starts when `--embed async` is used during analysis.
- When running, the CLI connects to it automatically instead of loading graphs in-process.
- The service handles embedding jobs in the background and provides fast query access.

## Skills

### `cartograph skills list`

List installed AI agent skills.

### `cartograph skills install <skill>`

Install a skill from a URL or local path.

### `cartograph skills uninstall <skill>`

Remove an installed skill.

## Troubleshooting

- **"No index found"** — Run `cartograph analyze` first to index the repository
- **Stale results** — Re-run `cartograph analyze --force` to rebuild the index
- **Wrong repo detected** — Use `-r <name>` flag to specify the repository explicitly
- **Ambiguous repo name** — If `-r sdk` matches multiple repos (e.g. `acme/sdk` and `corp/sdk`), cartograph lists them. Use the full name: `-r acme/sdk`
