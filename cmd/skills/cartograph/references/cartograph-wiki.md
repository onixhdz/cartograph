# Wiki Generation Workflow

Generate repository documentation from the knowledge graph. Cartograph
gathers all the data; the agent writes the prose.

## Workflow

1. **Generate context** — `cartograph wiki generate` collects modules,
   call edges, execution flows, source code, and project metadata.
2. **Write pages** — The agent reads the context and writes markdown.
3. **Bundle** — `cartograph wiki bundle` produces a self-contained HTML
   viewer.

## Phase 1: Generate Context

```bash
# In an indexed project directory (auto-detects repo and output path):
cartograph wiki generate

# Explicit repo and output:
cartograph wiki generate -r <repo> -o path/to/context.md
```

Default output: `<project>/.cartograph/wiki/context.md` (local projects)
or `<data_dir>/wiki/<repo>/context.md` (remote clones). Override with
`-o`.

The output is a structured markdown document. Everything the agent needs
is already in this file — no further exploration or tool calls required.

**Contents:**
- Module summary table (files, symbols, call edges, processes per module)
- Directory tree
- Inter-module dependency table (aggregated call counts)
- Top execution flows with ordered step traces
- Project config files (README, manifests, Dockerfile, CI — 4KB each)
- Per-module details: member files, internal/outgoing/incoming call
  edges, execution flows, and full source code

## Phase 2: Write Pages

The context file contains all graph data and source code. It should be
sufficient for most pages. If something is genuinely unclear or missing
— a truncated config file, an ambiguous call chain — read the specific
file to clarify. But do not re-explore the codebase from scratch.

### Speed rules

- **Trust the data.** The context file is the primary source of truth.
  Module groupings, call edges, and execution flows are computed from
  the graph — treat them as accurate.
- **Write first, read only if needed.** The context includes full source
  code for every module file. Only reach for additional reads when the
  context is genuinely insufficient (e.g., a config reference outside
  the module, a truncated file).
- **One pass per page.** Read the module's section, write the page.
  Do not re-read the context multiple times.
- **Parallelize aggressively.** Launch ALL module pages as parallel
  sub-agents in one batch. Do not wait between launches.
- **Keep pages focused.** 200-500 lines per module page is the sweet
  spot. Avoid exhaustive function-by-function listings.

### Step 1: Generate module_tree.json (catalog-first)

Before writing any content, generate the navigation structure. This
ensures consistent slugs and page titles across all pages.

Write `<wiki_dir>/module_tree.json`:

```json
[
  {"name": "Authentication & Sessions", "slug": "authentication-sessions", "files": ["auth/login.go", "auth/session.go"]},
  {"name": "Database Layer", "slug": "database-layer", "files": ["db/pool.go", "db/query.go"]}
]
```

**Naming rules:**
- Name modules by **what they do**, not by file paths. "Request
  Pipeline" not "cmd/middleware". "User Authentication" not
  "internal/auth".
- Slugs must be URL-safe lowercase with hyphens.
- Include the files array from the context's member file list.

### Step 2: Write module pages (parallel)

For each module, write `<wiki_dir>/<slug>.md`. Launch ALL modules as
parallel sub-agents in a single message.

**Each sub-agent receives:**
1. The module's section from the context (between its `## Module:` header
   and the next module header)
2. The inter-module dependency table (for cross-references)
3. The system prompt below
4. The output file path

**IMPORTANT — sub-agent delegation rules:**
Sub-agents must write the page directly from the provided context. Tell
each sub-agent explicitly:
- This is a **single-pass write task** — read the context once, write
  the markdown file, and you are done.
- Do NOT loop, revise, or iterate on the output. The first draft is
  the final draft.
- Do NOT read additional files unless the context has an obvious gap
  (e.g., a truncated config reference). If you do read, read ONE
  specific file, then finish writing.
- Do NOT use search/grep tools to explore the codebase.
- Write the full page content using the Write tool in a single call.

These rules prevent sub-agents from burning through their context window
by looping. Most sub-agents should finish in under 30 seconds.

**System prompt for module pages:**

> You are a technical documentation writer. Write clear, developer-focused
> documentation for a code module. Be direct and efficient.
>
> This is a single-pass write task. All the data you need is provided
> below — module files, call edges, execution flows, and full source
> code. Read it once, write the page, and finish. Do not loop, revise,
> or explore further.
>
> Rules:
> - Start with the module heading — no preamble or meta-commentary
> - Name the page by what the module does, not its file paths
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

### Step 3: Write overview page

Write `<wiki_dir>/overview.md` AFTER all module pages are complete.
This is also a single-pass write task.

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
> - Link to module pages naturally in the text using `[Name](slug.md)`
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
cartograph wiki generate          # generate context
# ... agent writes wiki pages ... # parallel sub-agents
cartograph wiki bundle            # bundle into HTML

# Explicit repo:
cartograph wiki generate -r owner/repo
# ... agent writes wiki pages ...
cartograph wiki bundle -r owner/repo

# Custom paths:
cartograph wiki generate -r repo -o /tmp/ctx.md
# ... agent writes to /tmp/wiki/ ...
cartograph wiki bundle -o /tmp/wiki/ -n "Project Name"
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
