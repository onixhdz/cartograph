# Roadmap

## Goal

The knowledge graph is the foundation — symbols, relationships, call chains, processes, communities indexed from source. The value is in how you traverse it.

Most structural code questions (blast radius, call chains, process ownership, subsystem boundaries) are graph traversal problems. Grep and embeddings can't answer them reliably at scale. Cartograph exposes the graph via CLI, MCP, and Cypher so developers and AI agents can navigate it without reading source files directly.

---

## Features

### Active

| Feature                         | Status         | Priority | Description                                                                                                                                                                                                     |
| ------------------------------- | -------------- | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Web UI                          | 🚧 In Progress | Low      | Browser-based graph visualization; node/edge explorer, process flows, Cypher query runner                                                                                                                       |
| Package Architecture Map        | 🚧 Partial     | Medium   | Folder-level `IMPORTS` aggregation exists; DOT/Mermaid/JSON architecture-map output is not implemented yet                                                                                                      |
| Plugin System                   | 🚧 Partial     | Low      | Exec-based plugin install/list/rm/ingest flows and SDK are shipped; WASM plugins and extractor mapping are not implemented yet                                                                                  |
| Cross-Repo Analysis             | 🔲 Planned     | Critical | Federate multiple repo graphs; trace call chains across service boundaries; critical for real microservice and multi-repo analysis                                                                              |
| Incremental Re-Indexing         | 🔲 Planned     | High     | Diff/staleness detection plus incremental refresh; only re-parse changed files (10–100× speedup)                                                                                                                |
| CloudGraph                      | 🔲 Planned     | High     | Plugin-based cloud/infra data sources (AWS, GitHub, k8s, SaaS) ingested into the knowledge graph; query infrastructure alongside code via Cypher                                                                |
| Semantic Config & Infra Parsing | 🔲 Planned     | High     | Classify config/infra/schema files and extract evidence-backed runtime facts from contracts, deployment manifests, SQL, Terraform/HCL, Docker, etc.; replaces generic fallback parsing with semantic extractors |
| Security Flow Analysis          | 🔲 Planned     | High     | CodeQL-inspired security research primitives: call-site preservation, value facts, local data flow, temporary source/sink models, partial-flow exploration, and explainable path evidence                       |
| Architecture Summary            | 🔲 Planned     | Medium   | Auto-generate subsystem overview from community + centrality + entry points                                                                                                                                     |
| Dead Code Detection             | 🔲 Planned     | Medium   | Reachability BFS from entry points; transitive dead code detection                                                                                                                                              |
| Watch Mode                      | 🔲 Planned     | Medium   | `fsnotify` + incremental re-index; graph stays current while you code                                                                                                                                           |
| Vulnerability Surface           | 🔲 Planned     | Medium   | Map CVEs to IMPORTS edges; flag only reachable vulnerabilities                                                                                                                                                  |
| PR Context Generation           | 🔲 Planned     | Low      | Blast radius + suggested reviewers + risk score from diff                                                                                                                                                       |
| Git History Intelligence        | 🔲 Planned     | Low      | Overlay churn, change coupling, and ownership onto graph nodes                                                                                                                                                  |
| Architecture Guardrails         | 🔲 Planned     | Low      | Cypher-defined rules enforced in CI; exit 1 on violations                                                                                                                                                       |
| Model2Vec Static Embeddings     | 🔲 Planned     | Low      | CGO-free embedding path; static lookup table (~30MB); two-stage: instant static, GGML upgrade in background                                                                                                     |
| Binary Quantization             | 🔲 Planned     | Low      | Asymmetric binary doc embeddings (1 bit/dim) with float32 queries; ~32× storage reduction; popcount search for large repos                                                                                      |
| Test Coverage Overlay           | 🔲 Planned     | Low      | Import lcov/go cover; risk-weighted gaps = coverage × churn × fan-in                                                                                                                                            |
| API Compatibility CI            | 🔲 Planned     | Low      | Add Go public API compatibility checks for the embedded API and plugin SDK packages so breaking changes are caught before release                                                                               |

### Completed

| Feature                    | Status  | Priority | Description                                                                                                          |
| -------------------------- | ------- | -------- | -------------------------------------------------------------------------------------------------------------------- |
| Graph indexing (`analyze`) | ✅ Done | —        | Parse source into symbol/relationship graph; bbolt + bleve storage                                                   |
| Remote repo analysis       | ✅ Done | —        | Clone and analyze GitHub repos by URL or `org/repo` shorthand                                                        |
| Query + semantic search    | ✅ Done | —        | BM25 + embedding hybrid search with process and graph enrichment                                                     |
| Context & impact analysis  | ✅ Done | —        | Caller/callee chains, blast radius, process membership                                                               |
| Cypher queries             | ✅ Done | —        | OpenCypher over the in-memory graph                                                                                  |
| Embeddings (GGML/CGO)      | ✅ Done | —        | Local inference via CGO-linked llama.cpp; remote provider fallback                                                   |
| Service (`serve`)          | ✅ Done | —        | Background HTTP/JSON service; process lifecycle management                                                           |
| Model management           | ✅ Done | —        | `models pull/list/rm`; GGUF download with SHA256                                                                     |
| Source & schema navigation | ✅ Done | —        | `source`, `schema` commands                                                                                          |
| Wiki generation            | ✅ Done | —        | `wiki generate` writes graph context for docs and `wiki bundle` builds a self-contained HTML viewer                  |
| Trigram Regex Search       | ✅ Done | Medium   | `google/codesearch` trigram index; `search`/`grep` source search; MCP `cartograph_search` tool                       |
| Release Pipeline           | ✅ Done | High     | Zig-based CGO cross-compilation, GitHub Releases on tag push, and Homebrew distribution are in place                 |
| MCP Protocol               | ✅ Done | High     | MCP is shipped via `cartograph mcp` (stdio) and the built-in `/mcp` Streamable HTTP endpoint                         |
| Cross-Language Parity      | ✅ Done | High     | Python and TypeScript extractor parity work landed; Tier 1 language support is validated against benchmark batteries |
| Schema Versioning          | ✅ Done | Medium   | Index metadata stores schema/algorithm versions and compatibility checks prompt re-indexing on incompatible upgrades |

---

## Sequencing

```
MVP
 ├─► Release Pipeline            → needed before any public adoption
 │    └─► Homebrew tap               → install without Go/Zig toolchain
 ├─► MCP Protocol                → core agent-facing value
 ├─► Schema Versioning           → safe binary upgrades
 ├─► Cross-Language Parity       → Python + TS quality matches Go
 ├─► Model2Vec Static Embeddings → CGO-free embed path; instant analyze
 │    └─► Binary Quantization    → 32× smaller vectors; scales to large repos
 ├─► Incremental Re-Indexing     → unlocks speed
 │    └─► Watch Mode             → zero-friction re-index
 ├─► Git History Intelligence    → structural + historical queries
 │    └─► Test Coverage Overlay  → risk = coverage × churn × fan-in
 ├─► Dead Code Detection         → nearly free from existing graph
 ├─► Trigram Regex Search        → zero-dep, enhances query + MCP
 ├─► Package Architecture Map    → aggregation only, quick win
 ├─► Architecture Summary        → surfaces subsystems organically
 ├─► PR Context Generation       → daily use, drives adoption
 ├─► Cross-Repo Analysis         → dependency/import workspace foundation for microservice / enterprise use cases; does not require CloudGraph
 │    ├─► Semantic Config & Infra Parsing → contract/deployment evidence for runtime boundaries
 │    ├─► CloudGraph             → enriches runtime/service identity with cloud/SaaS APIs alongside code
 │    └─► Vulnerability Surface  → needs cross-repo dep graph
 ├─► Security Flow Analysis      → CodeQL-inspired call-site, value-flow, and source-to-sink research primitives
 ├─► Architecture Guardrails     → CI integration + retention
 └─► Plugin System               → community + extensibility
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
