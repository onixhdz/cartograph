# Roadmap

## Goal

The knowledge graph is the foundation — symbols, relationships, call chains, processes, communities indexed from source. The value is in how you traverse it.

Most structural code questions (blast radius, call chains, process ownership, subsystem boundaries) are graph traversal problems. Grep and embeddings can't answer them reliably at scale. Cartograph exposes the graph via CLI, MCP, and Cypher so developers and AI agents can navigate it without reading source files directly.

---

## Features

| #   | Feature                     | Status     | Priority | Description                                                                                                                                      |
| --- | --------------------------- | ---------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| —   | Graph indexing (`analyze`)  | ✅ Done    | —        | Parse source into symbol/relationship graph; bbolt + bleve storage                                                                               |
| —   | Remote repo analysis        | ✅ Done    | —        | Clone and analyze GitHub repos by URL or `org/repo` shorthand                                                                                    |
| —   | Query + semantic search     | ✅ Done    | —        | BM25 + embedding hybrid search with process and graph enrichment                                                                                 |
| —   | Context & impact analysis   | ✅ Done    | —        | Caller/callee chains, blast radius, process membership                                                                                           |
| —   | Cypher queries              | ✅ Done    | —        | OpenCypher over the in-memory graph                                                                                                              |
| —   | Embeddings (GGML/CGO)       | ✅ Done    | —        | Local inference via CGO-linked llama.cpp; remote provider fallback                                                                               |
| —   | Service (`serve`)           | ✅ Done    | —        | Background HTTP/JSON service; process lifecycle management                                                                                       |
| —   | Model management            | ✅ Done    | —        | `models pull/list/rm`; GGUF download with SHA256                                                                                                 |
| —   | Source & schema navigation  | ✅ Done    | —        | `source`, `schema` commands                                                                                                                      |
| —   | Wiki generation             | 🚧 Stub    | —        | `wiki` command registered; not yet implemented                                                                                                   |
| 1   | Release Pipeline            | 🔲 Planned | High     | CGO cross-compilation via Zig; GitHub Releases + Homebrew tap; automated on tag push                                                             |
| 2   | MCP Protocol                | 🔲 Planned | High     | Implement MCP over the existing service; expose graph tools as structured MCP tool calls                                                         |
| 3   | Cross-Language Parity       | 🔲 Planned | High     | Python and TypeScript extractor quality on par with Go; validated against LLM agent baselines                                                    |
| 4   | Model2Vec Static Embeddings | 🔲 Planned | High     | CGO-free embedding path; static lookup table (~30MB); two-stage: instant static, GGML upgrade in background                                      |
| 5   | Incremental Re-Indexing     | 🔲 Planned | High     | Diff-based re-index; only re-parse changed files (10–100× speedup)                                                                               |
| 6   | PR Context Generation       | 🔲 Planned | High     | Blast radius + suggested reviewers + risk score from diff                                                                                        |
| 7   | Git History Intelligence    | 🔲 Planned | High     | Overlay churn, change coupling, and ownership onto graph nodes                                                                                   |
| 8   | Cross-Repo Analysis         | 🔲 Planned | High     | Federate multiple repo graphs; trace call chains across service boundaries                                                                       |
| 9   | CloudGraph                  | 🔲 Planned | High     | Plugin-based cloud/infra data sources (AWS, GitHub, k8s, SaaS) ingested into the knowledge graph; query infrastructure alongside code via Cypher |
| 10  | Schema Versioning           | 🔲 Planned | Medium   | Detect stale indexes on binary upgrade; migrate or prompt re-index; version stored in meta                                                       |
| 11  | Trigram Regex Search        | 🔲 Planned | Medium   | `google/codesearch` trigram index; `query --regex`; MCP `regex_search` tool                                                                      |
| 12  | Package Architecture Map    | 🔲 Planned | Medium   | Aggregate IMPORTS into package-level graph; DOT/Mermaid/JSON output                                                                              |
| 13  | Architecture Summary        | 🔲 Planned | Medium   | Auto-generate subsystem overview from community + centrality + entry points                                                                      |
| 14  | Dead Code Detection         | 🔲 Planned | Medium   | Reachability BFS from entry points; transitive dead code detection                                                                               |
| 15  | Watch Mode                  | 🔲 Planned | Medium   | `fsnotify` + incremental re-index; graph stays current while you code                                                                            |
| 16  | Architecture Guardrails     | 🔲 Planned | Medium   | Cypher-defined rules enforced in CI; exit 1 on violations                                                                                        |
| 17  | Test Coverage Overlay       | 🔲 Planned | Medium   | Import lcov/go cover; risk-weighted gaps = coverage × churn × fan-in                                                                             |
| 18  | Vulnerability Surface       | 🔲 Planned | Medium   | Map CVEs to IMPORTS edges; flag only reachable vulnerabilities                                                                                   |
| 19  | Stale Index Detection       | 🔲 Planned | Medium   | Detect when remote index lags upstream HEAD; `cartograph update` to refresh                                                                      |
| 20  | TUI Explorer                | 🔲 Planned | Medium   | `bubbletea` graph walker, process viewer, Cypher REPL                                                                                            |
| 21  | Plugin System               | 🔲 Planned | Low      | Exec/WASM plugins for custom extractors; file extension → plugin mapping                                                                         |
| 22  | Web UI                      | 🔲 Planned | Low      | Browser-based graph visualization; node/edge explorer, process flows, Cypher query runner                                                        |

---

## Sequencing

```
MVP
 ├─► 17: Release Pipeline            → needed before any public adoption
 │    └─► Homebrew tap               → install without Go/Zig toolchain
 ├─► 20: MCP Protocol                → core agent-facing value
 ├─► 19: Schema Versioning           → safe binary upgrades
 ├─► 18: Cross-Language Parity       → Python + TS quality matches Go
 ├─► 12: Model2Vec Static Embeddings → CGO-free embed path; instant analyze
 ├─► 3:  Incremental Re-Indexing     → unlocks speed
 │    └─► 5: Watch Mode              → zero-friction re-index
 ├─► 2:  Git History Intelligence    → structural + historical queries
 │    └─► 8: Test Coverage Overlay   → risk = coverage × churn × fan-in
 ├─► 6:  Dead Code Detection         → nearly free from existing graph
 ├─► 13: Trigram Regex Search        → zero-dep, enhances query + MCP
 ├─► 14: Package Architecture Map    → aggregation only, quick win
 ├─► 15: Architecture Summary        → surfaces subsystems organically
 ├─► 7:  PR Context Generation       → daily use, drives adoption
 ├─► 21: CloudGraph                  → ingest cloud/SaaS APIs into the graph alongside code
 ├─► 1:  Cross-Repo Analysis         → microservice / enterprise use case
 │    └─► 11: Vulnerability Surface  → needs cross-repo dep graph
 ├─► 4:  Architecture Guardrails     → CI integration + retention
 ├─► 9:  TUI Explorer                → polish + demos
 ├─► 16: Stale Index Detection       → keeps remote repos fresh
 └─► 10: Plugin System               → community + extensibility
```

---

## How It Compares

| Capability                            | Cartograph | Typical alternatives           |
| ------------------------------------- | ---------- | ------------------------------ |
| No file lock — concurrent CLI + serve | ✅         | ❌ (file lock contention)      |
| CGO-free embedding path (Model2Vec)   | ✅ planned | ❌ (requires native libs)      |
| Incremental re-indexing               | ✅ planned | ❌ (full re-analyze every run) |
| Cross-repo analysis                   | ✅ planned | ❌                             |
| Git history intelligence              | ✅ planned | ❌ (separate paid tools)       |
| Vulnerability reachability            | ✅ planned | ❌ open source / 💰 paid       |
| Architecture guardrails in CI         | ✅ planned | ❌ (separate config + tools)   |
| Single binary                         | ✅         | ❌                             |
