# Cartograph

**Build a nervous system for your codebase.** Cartograph indexes any repository into a knowledge graph — every function, call chain, dependency, and execution flow — then exposes it through smart tools so AI agents never miss code.

> ⚠️ **Early Development** — Cartograph is under active development. Expect frequent updates, breaking changes, and rough edges. APIs, CLI flags, and storage formats may change without notice. Not recommended for production workflows yet.

> Even smaller models get full architectural context, making them compete with frontier models on code tasks.

**TL;DR:** Point it at a repo, get a complete map. Use the CLI to search, trace impact, and explore — or connect it to your AI editor via MCP so Cursor, Claude Code, and friends stop missing dependencies and shipping blind edits.

---

## Install

### macOS (Homebrew — recommended)

```bash
brew install realxen/tap/cartograph
```

### Shell script (Linux & macOS)

```bash
curl -sSfL https://realxen.github.io/cartograph/install.sh | sh
```

Install a specific version or to a custom directory:

```bash
curl -sSfL https://realxen.github.io/cartograph/install.sh | sh -s -- --version v0.1.2
curl -sSfL https://realxen.github.io/cartograph/install.sh | sh -s -- --install-dir ~/bin
```

### Windows

Download the latest binary from [GitHub Releases](https://github.com/realxen/cartograph/releases/latest/download/cartograph-windows-amd64.exe) and add it to your `PATH`.

### Verify

```bash
cartograph --version
```

---

## Quick Start

```bash
# Install the Agent Skills
cartograph skills

# Index by GitHub shorthand — no full URL needed
cartograph analyze <path|url>

# Index a specific tag or branch (Go module style)
cartograph analyze hashicorp/nomad@v1.8.0

# Index with semantic embeddings (enables semantic search)
cartograph analyze <path|url> --embed async

# Search for execution flows
cartograph query "authentication middleware"

# See everything about a symbol — callers, callees, processes
cartograph context UserService

# What breaks if you change something?
cartograph impact validateUser
```

That's it. The graph is built, persisted locally, and ready to query.

---

## AI Editor Integration (MCP)

```json
{ "mcpServers": { "cartograph": { "command": "cartograph", "args": ["mcp"] } } }
```

| Editor          | Config location      |
| --------------- | -------------------- |
| **Claude Code** | `.mcp.json`          |
| **Cursor**      | `.cursor/mcp.json`   |
| **OpenCode**    | `.opencode/mcp.json` |

---

## Embeddings & Semantic Search

Cartograph supports **hybrid search** — BM25 full-text merged with vector similarity via Reciprocal Rank Fusion. Embeddings are optional; when enabled, `query` uses both signals for better recall.

```bash
# Embed synchronously (blocks until complete)
cartograph analyze <path|url> --embed <sync|async>

# Check progress
cartograph status --watch
```

### Providers

| Provider          | Description                                                      | Flag                             |
| ----------------- | ---------------------------------------------------------------- | -------------------------------- |
| **llamacpp**      | Built-in llama.cpp/GGUF inference (default, no external service) | `--embed-provider llamacpp`      |
| **openai_compat** | Any OpenAI-compatible API (Ollama, vLLM, LiteLLM, etc)           | `--embed-provider openai_compat` |

### Models

```bash
cartograph models list                # Show aliases + cache status
cartograph models pull nomic-code     # Download ahead of time
cartograph models rm jina-code        # Remove from cache
```

Any GGUF model on Hugging Face works: `--embed-model "org/model-GGUF"`. Models are cached at `~/.cache/cartograph/models/` and also read from the HF hub cache for zero-copy reuse.

---

## Wiki Generation

Generate a documentation wiki from the knowledge graph. Cartograph gathers the data; your AI agent writes the prose.

```bash
cartograph wiki generate    # collect context from the graph
# ... agent writes markdown pages via the wiki skill ...
cartograph wiki bundle      # package into a self-contained HTML viewer
```

The wiki skill is included in `cartograph skills install`.

---

## Language Support

**206 languages** detected via tree-sitter. **13 Tier 1 languages** get full extraction (symbols, imports, calls, heritage, types, assignments):

Go · TypeScript · JavaScript · Python · Java · Rust · C++ · C · Ruby · PHP · Kotlin · Swift · C#

**56+ Tier 2 languages** get inferred extraction via a grammar-agnostic AST engine — no hand-crafted queries needed.

---

## How It Works

1. **Structure** — Walk file tree, map folder/file relationships
2. **Parsing** — Extract symbols via tree-sitter (hand-crafted + grammar-agnostic)
3. **Resolution** — Resolve imports, calls, and inheritance across files
4. **Clustering** — Group related symbols into communities (Leiden algorithm)
5. **Processes** — Trace execution flows from entry points through call chains
6. **Search** — Build BM25 + vector indexes for hybrid retrieval

Everything is persisted locally — no external services needed.

---

## Development

**Prerequisites:** [Go 1.25+](https://go.dev/dl/), [Zig 0.14+](https://ziglang.org/download/) (for the native embedding library), and [golangci-lint](https://golangci-lint.run/docs/welcome/install/). [Task](https://taskfile.dev/) is optional but recommended.

```bash
task test              # unit tests (short mode)
task test:integration  # all tests including network
task build:dev         # build for your host OS/arch
task lint              # golangci-lint
```

Or without Task:

```bash
go test -short ./...
go build ./...
```

> **Note:** `go build` without Task skips the Zig-built embedding library — the `llamacpp` provider won't be available but everything else works.

---

## Security & Privacy

Everything runs locally. No code leaves your machine. The index is stored in `~/.local/share/cartograph/` and can be deleted with `cartograph clean`.

When using `--embed-provider openai_compat`, only symbol names and descriptions are sent — no source code.

---

## Acknowledgments

Cartograph was heavily inspired by [GitNexus](https://github.com/abhigyanpatwari/GitNexus). Its approach to code knowledge graphs shaped many of the ideas here.

---

## License

[MIT](LICENSE)
