# Wiki Generation Workflow

Generate repository documentation from the knowledge graph. Cartograph
gathers all the data; the agent writes the prose.

## Workflow

1. **Generate context** — `cartograph wiki generate` collects modules,
   call edges, execution flows, source code, and project metadata.
2. **Consolidate modules** — The agent reads `project.md`, merges
   related micro-communities in `module_tree.json`, and gives them
   business-oriented names.
3. **Write pages** — The agent dispatches parallel sub-agents to write
   markdown pages, then writes the overview.
4. **Bundle** — `cartograph wiki bundle` produces a self-contained HTML
   viewer.

## Phase 1: Generate Context

```bash
# In an indexed project directory (auto-detects repo and output path):
cartograph wiki generate

# Explicit repo and output:
cartograph wiki generate -r <repo> -o path/to/wiki/
```

Default output: `<project>/.cartograph/wiki/` (local projects) or
`<data_dir>/wiki/<repo>/` (remote clones). Override with `-o`.

This produces a `context/` subdirectory with split files:

```
<wiki_dir>/context/
  project.md            # project summary (no source code) — compact
  auth-sessions.md      # per-module context with full source
  database-layer.md     # per-module context with full source
  ...
```

**`project.md`** — compact project-level summary for the orchestrating
agent and overview page writer:
- Module summary table (files, symbols, call edges, processes)
- Module file listings (which files belong to each module)
- Directory tree
- Inter-module dependency table
- Top execution flows with step traces
- Project config files (README, manifests, Dockerfile, CI — 4KB each)

**`<slug>.md`** — self-contained module context for each sub-agent:
- Module name, file list, symbol count
- Internal / outgoing / incoming call edges
- Execution flows touching this module
- Other modules list (for cross-references)
- Full source code for every file in the module

## Phase 2: Write Pages

Read `<wiki_dir>/context/project.md` to understand the project structure
and plan pages. This file is compact (no source code) and fits easily
in context. Do NOT read the per-module context files yourself — those
are for sub-agents.

### Speed rules

- **Trust the data.** The context files are the primary source of truth.
  Module groupings, call edges, and execution flows are computed from
  the graph — treat them as accurate.
- **Write first, read only if needed.** Each module's context file
  includes full source code. Only reach for additional reads when the
  context is genuinely insufficient (e.g., a config reference outside
  the module, a truncated file).
- **One pass per page.** Read the module's context file, write the page.
  Do not re-read the context multiple times.
- **Parallelize aggressively.** Launch ALL module pages as parallel
  sub-agents in one batch. Do not wait between launches.
- **Keep pages focused.** 200-500 lines per module page is the sweet
  spot. Avoid exhaustive function-by-function listings.

### Step 0: Consolidate and rename modules

`module_tree.json` is generated automatically by `cartograph wiki
generate`. The auto-generated entries come from graph community
detection, which often produces too many small modules (e.g., separate
entries for `deletepolicy`, `getpolicy`, `listpolicies` that should be
one "Policy Engine" module) and cryptic path-based names.

Before dispatching sub-agents, consolidate and rename:

1. Read `<wiki_dir>/context/project.md` and `<wiki_dir>/module_tree.json`
2. **Merge related modules.** Look for entries that share a domain
   (e.g., multiple policy CRUD operations, adapter variants for the same
   subsystem, or small utility fragments). Combine their `files` arrays
   into a single entry. Target **10-25 modules** for a typical project.
3. **Rename each module** with a concise business-oriented name (2-4
   words). Name by what it does, not by file paths.
   - `adapters-stream` → "Event Streaming"
   - `domain-orchestrator-app` → "Orchestrator Core"
   - `deletepolicy` + `getpolicy` + `listpolicies` → "Policy Engine"
4. **Set a new slug** for each merged module (URL-safe, lowercase,
   hyphens). This becomes the wiki page filename.
5. **Add a `sources` array** listing the original context file slugs
   (without `.md`) that were merged into this module. Sub-agents use
   this to know which context files to read.
6. Write back `module_tree.json`.

**Output format:**

```json
[
  {
    "name": "Policy Engine",
    "slug": "policy-engine",
    "files": ["policy/delete.go", "policy/get.go", "policy/list.go"],
    "sources": ["deletepolicy", "getpolicy", "listpolicies"]
  }
]
```

If a module was not merged (1:1 mapping), `sources` has one entry
matching the original slug. Every original slug must appear in exactly
one module's `sources` — do not drop any.

This is a fast, lightweight step — just reading a compact summary and
rewriting JSON. Do NOT read per-module context files for this.

### Step 1: Write module pages (parallel)

For each module in `module_tree.json`, write `<wiki_dir>/<slug>.md`.
Launch ALL modules as parallel sub-agents in a single message.

**Each sub-agent receives:**
1. The paths to its context files — one per entry in `sources`:
   `<wiki_dir>/context/<source>.md` — tell the sub-agent to Read these
   files (most modules have 1-3 source files)
2. The display name from `module_tree.json` (use this as the page title)
3. The system prompt below
4. The output file path

Do NOT paste the module context into the sub-agent's prompt. Give it
the file paths and let it Read them. This keeps the initial prompt
small and avoids context window overflow.

**IMPORTANT — sub-agent delegation rules:**
Tell each sub-agent explicitly:
- This is a **single-pass write task** — Read the context file(s), write
  the markdown page, and you are done.
- Do NOT loop, revise, or iterate on the output. The first draft is
  the final draft.
- Do NOT read additional files unless the context has an obvious gap
  (e.g., a truncated config reference). If you do read, read ONE
  specific file, then finish writing.
- Do NOT use search/grep tools to explore the codebase.
- Write the full page content using the Write tool in a single call.
- If you received multiple context files, read all of them first, then
  write a single unified page that covers the combined scope.

These rules prevent sub-agents from burning through their context window
by looping. Most sub-agents should finish in under 60 seconds.

**System prompt for module pages:**

> You are a technical documentation writer. Write clear, developer-focused
> documentation for a code module. Be direct and efficient.
>
> This is a single-pass write task. Read the context file(s) you are
> given, write the documentation page, and finish. The context files
> contain everything — call edges, execution flows, and full source
> code. Do not loop, revise, or explore further.
>
> Rules:
> - Start with the module heading using the display name you were given
>   — no preamble or meta-commentary
> - Reference actual function/class names from the source with file:line
>   attribution (e.g., `handleRequest` in `server/handler.go:45`)
> - Use call edges and execution flows for accuracy, but synthesize them
>   into narrative — do NOT mechanically list every edge
> - Include a Mermaid diagram ONLY if the module has complex multi-component
>   interactions (5-10 nodes max). Simple modules need no diagram.
> - Never fabricate code examples, API signatures, or function names —
>   only reference what appears in the provided source
> - Note patterns the graph may miss: config-based wiring, middleware
>   chains, convention-based routing, error handling strategies
> - Keep it concise: 200-500 lines. Developers skim documentation.

**Cover:**
- Purpose and responsibility (1-2 sentences)
- Key components with `file:line` references
- How it connects to other modules (use outgoing/incoming edges)
- Important execution flows passing through it
- Configuration, env vars, or setup (if visible in source)

### Step 2: Write overview page

Write `<wiki_dir>/overview.md` AFTER all module pages are complete.
The overview agent should Read `<wiki_dir>/context/project.md` and
`<wiki_dir>/module_tree.json` (for display names and slugs). This is
also a single-pass write task.

**System prompt for overview:**

> You are a technical documentation writer. Write the top-level overview
> for a repository wiki — the first page a new developer reads.
>
> This is a single-pass write task. All the data you need is provided.
> Read it, write the page, and finish.
>
> Rules:
> - Start with the project heading — no preamble
> - Include a Mermaid architecture diagram showing the most important
>   modules and their relationships (max 10 nodes, use inter-module edge
>   data)
> - Link to module pages using names from module_tree.json:
>   `[Display Name](slug.md)`
> - Do NOT create module index tables
> - Never fabricate — only reference what's in the provided data
> - Keep it welcoming and scannable

**Cover:**
- What the project does (from README/config data)
- High-level architecture with diagram (from inter-module edges)
- Key end-to-end workflows (from execution flows)
- Getting started / setup (from project config files)

## Phase 3: Bundle

```bash
# In the project directory (auto-detects wiki path):
cartograph wiki bundle

# Explicit repo or path:
cartograph wiki bundle -r <repo>
cartograph wiki bundle -o path/to/wiki/
```

Produces `<wiki_dir>/index.html` — a self-contained HTML file with
sidebar navigation, markdown rendering (marked.js), Mermaid support,
and responsive layout. Works offline.

Report the output path to the user.

## Quick Reference

```bash
# Full workflow (in an indexed project directory):
cartograph wiki generate          # writes context/ + module_tree.json
# ... agent consolidates & renames modules in module_tree.json ...
# ... agent dispatches sub-agents (one per module, parallel) ...
# ... each sub-agent reads context/<source>.md files, writes <slug>.md ...
cartograph wiki bundle            # bundle into HTML

# Explicit repo:
cartograph wiki generate -r owner/repo
# ... agent writes wiki pages ...
cartograph wiki bundle -r owner/repo

# Custom output directory:
cartograph wiki generate -r repo -o /tmp/wiki/
# ... agent writes pages to /tmp/wiki/ ...
cartograph wiki bundle -o /tmp/wiki/ -n "Project Name"
```

**Output structure after full workflow:**

```
<wiki_dir>/
  context/              # generated by cartograph wiki generate
    project.md          # project summary (read this first)
    community-a.md      # per-community context files (original slugs)
    community-b.md
    community-c.md
  module_tree.json      # generated, then consolidated & renamed by agent
  overview.md           # written by agent
  module-a.md           # written by sub-agents (consolidated slugs)
  module-b.md
  index.html            # generated by cartograph wiki bundle
```

## Anti-Fabrication Rules

The context file provides verified graph data and real source code.
Maintain this accuracy in the documentation:

- **Never invent function names, API signatures, or code examples.**
  Only reference symbols that appear in the provided source.
- **Never guess parameter types or return values.** If the source
  doesn't show a signature, describe behavior instead.
- **Attribute code references.** Use `functionName` in `file/path:line`
  format so readers can find the source.
- **Mermaid nodes must match real symbols.** Node labels in diagrams
  must correspond to actual functions, files, or modules from the data.
- **If something is unclear, say so.** "This module likely handles X
  based on its call edges" is better than fabricating an explanation.

## What the Graph Captures vs. What It Misses

**From the graph (structural accuracy):**
- Community-based module groupings (Leiden community detection)
- Call edges between functions/methods across files
- Execution flows (BFS traces from entry points)
- Import relationships and exported symbols

**From source code (fills gaps):**
- Config-based wiring (dependency injection, middleware registration)
- Convention-based patterns (naming conventions, directory layout)
- Error handling strategies
- Runtime patterns (dynamic dispatch, reflection, event systems)
- Comments and documentation that explain intent

Synthesize both layers. The graph provides the architecture; the source
code provides the details. If you notice references to config files, env
vars, or infrastructure not in the source, mention them.
