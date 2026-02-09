---
name: cartograph
description: 'Cartograph: graph-powered code intelligence. Use when the user asks to index, analyze, understand, or explore a repository or codebase. Covers CLI commands, practical workflows, and deep-dive architecture analysis.'
metadata:
  author: cartograph
  version: '1.0'
---

# Cartograph Skill Router

Cartograph is a graph-powered code intelligence tool that indexes repositories
into a knowledge graph of code symbols, files, relationships, execution flows,
and communities. Use it to search, explore, and understand codebases with
surgical precision.

## How to Use

When a user needs help with cartograph, consult the tables below to route to
the right reference. Each reference is a self-contained document — read it
fully before answering.

## Loading References

All references are topic files in the `references/` directory relative to this
file. Read the matching file and follow its instructions. Load only the
reference(s) needed for the user's request.

## Service Architecture & Execution Modes

Cartograph has three execution modes. **Always prefer the background
service** — it is faster (warm caches, no per-command startup cost),
supports embedding, and exposes an MCP endpoint for AI editors.

### 1. Background Service (Preferred)

```bash
cartograph serve start          # starts detached, auto-shuts down after 30min idle
cartograph serve start --no-idle  # no auto-shutdown
```

A long-running HTTP server that holds graphs in memory, runs embedding
jobs, and serves the JSON API over a unix socket (TCP fallback). The
service also exposes a **Streamable HTTP MCP endpoint at `/mcp`**,
enabled by default (opt-out: `--no-mcp`).

**When to start the service:**
- Before running multiple queries — avoids reloading graphs per command
- Before or during embedding (`--embed async` auto-starts it)
- When configuring an AI editor to use the `/mcp` endpoint directly

**The CLI auto-connects:** When the service is running, CLI commands
like `query`, `context`, `impact` automatically delegate to it via the
unix socket. No extra flags needed.

**The service auto-starts** when `cartograph analyze --embed async` is
used and no service is running. It remains running for subsequent
commands.

**Agent rule:** If you plan to run 2+ queries or any embedding work,
start the service first:
```bash
cartograph serve start
```

### 2. MCP Stdio Server (`cartograph mcp`)

```bash
cartograph mcp    # blocks on stdin/stdout — for AI editor integration
```

An MCP (Model Context Protocol) server over stdin/stdout JSON-RPC.
AI editors (Cursor, Claude Code, OpenCode, etc.) launch this command
as a subprocess and communicate via JSON-RPC.

**Opportunistic delegation:** The `mcp` command checks if a background
service is already running. If so, it delegates all requests to the
service via HTTP (reusing warm caches). If not, it falls back to an
in-process MemoryClient (cold start ~27ms).

**Agent rule:** When configuring MCP for an editor, there are two options
(prefer option A):

- **Option A (recommended):** Start the background service and point the
  editor at the `/mcp` endpoint. The service's address can be found in
  the lockfile or via `cartograph serve status`.
- **Option B:** Configure the editor to launch `cartograph mcp` as a
  stdio subprocess. This works without a running service but is slower
  for the first query (cold start).

### 3. In-Memory CLI Client (Fallback)

When no background service is running, CLI commands (`query`, `context`,
etc.) load graphs from disk into an in-process MemoryClient. This works
but is slower — each command pays the graph-loading cost. Embedding is
not available in this mode.

**Agent rule:** This is acceptable for one-off queries. For repeated
use, start the background service first.

### Mode Decision Tree

```
Is this an MCP / editor integration setup?
  ├── Yes → Option A: `cartograph serve start` + point editor at /mcp
  │         Option B: `cartograph mcp` as stdio subprocess
  └── No (CLI / agent workflow)
       ├── Multiple queries or embedding? → `cartograph serve start` first
       └── Single one-off query? → Just run the CLI command directly
```

## Agent Execution Constraints

### Always Start the Service for Multi-Query Sessions

When an agent workflow involves 2+ cartograph queries (which is almost
always the case — research loops, deep dives, blast radius checks),
start the background service first. This is a one-time cost that makes
every subsequent query instant:

```bash
cartograph serve start
```

The service auto-shuts down after 30 minutes of inactivity. Check if
it's already running with `cartograph serve status`.

### Always Index Before Querying

Cartograph commands that read the knowledge graph (`query`, `context`,
`impact`, `cypher`, `cat`) require a prior `analyze` run. If no index
exists for the target repo, run `cartograph analyze` first.

### Re-index After Code Changes

The knowledge graph reflects code at index time. After refactoring, merging,
or pulling new code, re-index with `cartograph analyze --force` before
querying.

### Repo Resolution

Most commands accept `-r, --repo <name>` to target a specific repository.
When omitted, cartograph auto-detects the repo from the current directory's
git root name. Always pass `--repo` explicitly when the working directory is
not inside the target repository.

**Short names are supported:** `-r nomad` resolves to `hashicorp/nomad` if
that's the only indexed repo with basename "nomad". If multiple repos share
a basename (e.g. `acme/sdk` and `corp/sdk`), cartograph returns an error
listing the full names — use `-r acme/sdk` to disambiguate.

### Auto-Detection & Idempotency

- **Current project auto-detection:** If the user is inside a git
  repository, cartograph automatically detects the repo name. The agent
  does NOT need to pass `--repo` or `.` when querying the current project.
- **Idempotent indexing:** Running `cartograph analyze` on an already-indexed
  repo is safe — it checks the HEAD commit and skips re-indexing if
  nothing changed. The agent can always include `.` in a multi-target
  analyze without penalty.
- **Check before indexing:** Use `cartograph list` to see all indexed
  repos. If the user's current project already appears, skip re-indexing
  it (unless they ask for `--force`).

## Delegating Research to Sub-Agents

For sub-agent delegation (explore/task agents with cartograph), including
prompt templates and parallel launch examples, see
[references/cartograph-delegation.md](references/cartograph-delegation.md).

## Topic → Reference Map

Load only the reference(s) needed for the user's request.

- **CLI commands, flags, Cypher syntax, schema, models, serve, MCP, skills** →
  [references/cartograph-cli.md](references/cartograph-cli.md)

- **Deep-dive architecture** (full analysis, end-to-end flows, subsystem mapping,
  async/goroutine roots, core processing loops) →
  [references/cartograph-deep-dive.md](references/cartograph-deep-dive.md)

- **Workflows** (explore, debug, impact analysis, PR review, refactoring,
  entry points, call-site mapping) →
  [references/cartograph-workflows.md](references/cartograph-workflows.md)

- **Sub-agent delegation** (prompt templates, parallel explore agents) →
  [references/cartograph-delegation.md](references/cartograph-delegation.md)

- **Wiki generation** (generate repository documentation from the knowledge
  graph, create wiki pages, bundle into HTML viewer) →
  [references/cartograph-wiki.md](references/cartograph-wiki.md)

- **Remote / URL-based exploration** → Read **both** CLI + Workflows
  (index first, then explore)

- **Multi-repo comparison** → Read **both** CLI + Workflows
  (index all targets, then query each with `--repo`)

## Routing Decision Tree

Apply these rules in order. **First match wins.**

### -1. Implicit Cartograph Requests (Confirm First)

**Triggers:** User asks to "index", "analyze", or "understand/explain" a
repository or project without explicitly mentioning cartograph.

**Action:** Confirm the user wants to use cartograph before proceeding.

**Skip confirmation when:**
- The user explicitly mentions "cartograph".
- The skill was already used in this conversation.
- The current repo is already indexed (`cartograph list`).

### 0. Remote Repository (URL or Shorthand)

**Triggers:** User provides a Git URL (e.g. `https://github.com/…`,
`git@github.com:…`), a GitHub shorthand (`org/repo`), or a host-prefixed
URL (`github.com/org/repo`). If the user gives only a **bare name**
(e.g. "docker", "nomad") without `org/repo`, route to **0c** instead.

**Resolving GitHub shorthands:** Cartograph natively handles GitHub
shorthands. When the user writes `hashicorp/nomad` or `moby/moby`
(an `org/repo` pattern), pass it directly to `cartograph analyze` —
it auto-expands to `https://github.com/org/repo`. No manual
resolution needed.

Cartograph also handles host-prefixed URLs without a scheme:
`github.com/org/repo`, `gitlab.com/org/repo`, etc. are auto-expanded.

**Action:** Read **both** `references/cartograph-cli.md` (for `analyze`
flags) and `references/cartograph-workflows.md` (for exploration steps).
The agent MUST:

1. Run `cartograph analyze <shorthand>` to index the remote repo
   (shorthands like `hashicorp/nomad` are resolved automatically).
2. Then follow the exploration workflow to answer the user's question.

### 0b. Multi-Repo Comparison

**Triggers:** User mentions **two or more** repositories (URLs or
shorthands) and wants to compare them — to each other or to their
current project. Phrases like "how does X compare to Y", "compare X
and Y to my project", "what's the difference between X and Y".

**Action:** Read **both** references. The agent MUST:

1. Index **all** repos in a single command (shorthands resolved
   automatically):
   ```bash
   cartograph analyze hashicorp/nomad moby/moby
   ```
   If the user says "my project" / "my repo" / "this codebase", the
   current directory is likely already indexed (check `cartograph list`).
   Include `.` as a target only if it is not already indexed — analyze
   is idempotent so including it is safe but unnecessary:
   ```bash
   cartograph analyze . hashicorp/nomad moby/moby
   ```
2. Then run queries (`query`, `context`, `cypher`) against each repo
   using `--repo <name>` and synthesize a comparison. For the current
   project, `--repo` can be omitted (auto-detected).

### 0c. Bare Project Names (Confirm Before Indexing)

**Triggers:** User mentions projects by **bare name** (no URL or `org/repo`
shorthand). E.g. "analyze nomad and docker", "compare consul with my
project", "how does etcd work".

**Action:** Run `cartograph analyze <bare-name>` — it searches GitHub and
prints a ranked list of matches (e.g. `moby/moby`, `docker/cli`,
`docker/compose` for "docker"). Present the suggestions to the user and
ask which one to index. Once confirmed, run
`cartograph analyze <org/repo>` with the confirmed shorthand, then follow
the exploration/comparison workflow.

### 1. Deep-Dive Architecture Analysis

**Triggers:** User asks for full architecture analysis, end-to-end flow
tracing, subsystem mapping, understanding the core processing loop,
finding goroutine/async roots, tracing a request through the entire system,
deep codebase understanding before major changes, or wants to know
"how does this system work end-to-end."

**Action:** Read `references/cartograph-deep-dive.md`.

### 1b. Workflows

**Triggers:** User mentions exploring a codebase, debugging, tracing errors,
impact analysis before a change, PR review, refactoring safely, assessing
risk, finding entry points, or mapping call sites for a rename.

**Action:** Read `references/cartograph-workflows.md`.

### 1c. Wiki Generation

**Triggers:** User asks to generate a wiki, generate documentation, create
docs from the knowledge graph, document a repository, produce a wiki page,
or mentions "wiki" in the context of documentation generation.

**Action:** Read `references/cartograph-wiki.md`.

### 2. CLI Commands

**Triggers:** User mentions a specific command (`analyze`, `query`, `context`,
`impact`, `cypher`, `schema`, `cat`, `clone`, `models`, `serve`, `mcp`,
`skills`, `list`, `status`, `clean`, `wiki`), command flags, graph schema,
Cypher syntax, node labels, relationship types, embedding configuration,
model management, MCP configuration, or needs a command reference.

**Action:** Read `references/cartograph-cli.md`.

### 3. Both References

**Triggers:** User asks a workflow question that also requires knowing exact
CLI flags or Cypher syntax (e.g., "how do I safely refactor and what flags
does analyze take?").

**Action:** Read both `references/cartograph-cli.md` and
`references/cartograph-workflows.md`.

### 4. Vague or General

**Triggers:** User says "help with cartograph" or provides no specific
context.

**Action:** Summarize cartograph capabilities and **ASK** what they want to
do. Do NOT guess — force disambiguation. Cartograph can help with:

- Indexing repositories into a knowledge graph of code
- Searching execution flows and symbols (BM25 + semantic vector search)
- 360° context views of any code symbol
- Blast radius / impact analysis before changes
- Raw Cypher queries against the code graph
- Practical workflows for debugging, PR review, and refactoring
- Managing embedding models for semantic search
- Running a background service for embedding, fast queries, and MCP
- MCP integration for AI editors (Cursor, Claude Code, OpenCode)
- Generating repository wiki documentation from the knowledge graph

## Edge Cases

These clarify ambiguous routing where two rules could plausibly match:

- **Without mentioning cartograph** ("index X", "understand this project") → Confirm cartograph first (rule -1), then route
- **"blast radius"** or **"impact"** as a command reference → CLI Commands; as a practical workflow ("what breaks if I change X") → Workflows
- **"explore this codebase"** or **"understand the architecture"** → Deep-Dive Architecture (not Workflows)
- **"mcp"**, **"editor integration"**, **"configure cursor/claude code/opencode"** → CLI Commands + Service Architecture (this file)
- **"sub-agents"**, **"agent prompt template"**, **"launch explore agents"** → [references/cartograph-delegation.md](references/cartograph-delegation.md)
- **Bare name** ("analyze docker") vs **shorthand** ("analyze moby/moby") → Bare names trigger confirm-first (rule 0c); shorthands go directly (rule 0)
- **"analyze this repo: <url>"** → CLI Commands only (just indexing, no exploration workflow)
- **"wiki"** as a CLI command reference ("what flags does wiki take") → CLI Commands; as a generation workflow ("generate wiki for this repo") → Wiki Generation (rule 1c)
