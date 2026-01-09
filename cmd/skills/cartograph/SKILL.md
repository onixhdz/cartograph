---
name: cartograph
description: 'Route cartograph requests to the right reference — covers CLI commands (analyze, query, context, impact, cypher, schema, source, clone, models, serve, skills, list, status, clean, wiki), practical workflows (codebase exploration, debugging, impact analysis, PR review, safe refactoring), and deep-dive architecture analysis (end-to-end flow tracing, subsystem mapping, entry point discovery).'
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

## Agent Execution Constraints

### Always Index Before Querying

Cartograph commands that read the knowledge graph (`query`, `context`,
`impact`, `cypher`, `source`) require a prior `analyze` run. If no index
exists for the target repo, run `cartograph analyze` first.

### Re-index After Code Changes

The knowledge graph reflects code at index time. After refactoring, merging,
or pulling new code, re-index with `cartograph analyze --force` before
querying.

### Discover Commands with `--help`

Use `cartograph --help` and `cartograph <command> --help` to discover
available commands, flags, and subcommands:

```bash
# List all top-level commands
cartograph --help

# Show flags for a specific command
cartograph analyze --help
cartograph cypher --help
```

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

When launching **explore** or **task** agents to research an indexed codebase,
include cartograph CLI instructions in the agent prompt so they use the
knowledge graph instead of raw grep/glob/view. Cartograph queries return
results in ms for grep-based exploration, and they surface
structural relationships (call trees, execution flows, process labels)
that text search cannot discover.

### When to delegate with cartograph

- The target repo is already indexed (check `cartograph list`)
- The research task involves understanding architecture, tracing flows,
  finding callers/callees, or locating symbols by intent
- Multiple independent research threads can run in parallel

### Prompt template for sub-agents

Include this block in the agent prompt (adapt the repo name):

````
You have access to `cartograph`, a graph-powered code intelligence CLI.
The repository "{repo_name}" is already indexed. Use cartograph as your
PRIMARY search tool — it is 100x faster than grep and surfaces structural
relationships.

**Research loop — use this workflow:**

1. **Search** — find relevant symbols and execution flows:
   ```bash
   cartograph query "your search terms" -r {repo_name}
   ```

2. **Drill down** — get the 360° view with transitive call tree:
   ```bash
   cartograph context <symbolName> -r {repo_name} -d 3
   ```
   The `-d 3` flag traces 3 levels deep through CALLS, SPAWNS (⇢), and
   DELEGATES_TO (⤳) edges. Follow ⇢ SPAWNS edges — they reveal async/
   concurrent architecture worth tracing separately.

3. **Read source** — only when you need implementation details:
   ```bash
   cartograph source <filePath> -r {repo_name} -l <startLine>-<endLine>
   ```

4. **Repeat** — each `context -d 3` output reveals new symbols to trace.
   Follow the most interesting branches (high fan-out, cross-package hops,
   SPAWNS edges) until you've mapped the full flow.

**Additional commands:**
- `cartograph impact <symbol> -r {repo_name}` — blast radius analysis
- `cartograph cypher "<query>" -r {repo_name}` — raw graph queries
- `cartograph schema -r {repo_name}` — see available node labels and edge types

**Rules:**
- Start with `cartograph query`, NOT grep. Use grep only as a fallback
  if cartograph returns no results.
- Always use `-d 3` (not `-d 1`) when tracing call trees — depth 1 only
  shows direct callees and misses the architecture.
- Use `cartograph source` to read code, not `cat` or `view` — it works
  on the indexed snapshot and doesn't require the repo to be on disk.
````

### Example: launching parallel explore agents

```
# Research 3 aspects of a codebase in parallel
task(agent_type="explore", prompt="""
You have access to `cartograph`... [template above with repo="fastapi/fastapi"]

Research question: How does FastAPI handle request validation?
Find the validation pipeline from request receipt to error response.
""")

task(agent_type="explore", prompt="""
You have access to `cartograph`... [template above with repo="fastapi/fastapi"]

Research question: How does the dependency injection system work?
Trace from route definition through dependency resolution to injection.
""")
```

## Topic → Reference Map

### CLI Commands (Read `references/cartograph-cli.md`)

| User wants to…                                | Read file                      |
| --------------------------------------------- | ------------------------------ |
| Index one or more local/remote repositories   | `references/cartograph-cli.md` |
| Search for execution flows or symbols         | `references/cartograph-cli.md` |
| Get a 360° view of a code symbol              | `references/cartograph-cli.md` |
| Analyze blast radius of a change              | `references/cartograph-cli.md` |
| Run a raw Cypher query against the graph      | `references/cartograph-cli.md` |
| Retrieve source code from an indexed repo     | `references/cartograph-cli.md` |
| Clone a remote repo without indexing          | `references/cartograph-cli.md` |
| List all indexed repositories                 | `references/cartograph-cli.md` |
| Check index status of a repository            | `references/cartograph-cli.md` |
| Delete an index                               | `references/cartograph-cli.md` |
| Understand graph schema (nodes, edges, props) | `references/cartograph-cli.md` |
| Write Cypher queries (patterns & examples)    | `references/cartograph-cli.md` |
| Generate a repository wiki                    | `references/cartograph-cli.md` |
| Manage embedding models (list, pull, remove)  | `references/cartograph-cli.md` |
| Enable semantic search / embeddings           | `references/cartograph-cli.md` |
| Start/stop/check the background service       | `references/cartograph-cli.md` |
| Install or manage AI agent skills             | `references/cartograph-cli.md` |

### Remote / URL-based Exploration (Read both references)

| User wants to…                                | Read file                                   |
| --------------------------------------------- | ------------------------------------------- |
| Understand a repo given a URL                 | Both references (index first, then explore) |
| Explore a GitHub / GitLab / remote repository | Both references (index first, then explore) |
| Understand a repo given a GitHub shorthand    | Both references (resolve → index → explore) |
| Compare external repo(s) to current project   | Both references (index all → explore)       |
| Compare multiple external repos to each other | Both references (index all → explore)       |

### Deep-Dive Architecture Analysis (Read `references/cartograph-deep-dive.md`)

| User wants to…                                     | Read file                            |
| -------------------------------------------------- | ------------------------------------ |
| Produce a full architecture analysis of a codebase | `references/cartograph-deep-dive.md` |
| Understand end-to-end execution flows              | `references/cartograph-deep-dive.md` |
| Map all subsystems and how they connect            | `references/cartograph-deep-dive.md` |
| Find async/goroutine entry points (SPAWNS edges)   | `references/cartograph-deep-dive.md` |
| Trace a request through the entire system          | `references/cartograph-deep-dive.md` |
| Find internal processing cores that ranking misses | `references/cartograph-deep-dive.md` |
| Understand the core scheduling/processing loop     | `references/cartograph-deep-dive.md` |
| Deep codebase understanding before major changes   | `references/cartograph-deep-dive.md` |

### Workflows (Read `references/cartograph-workflows.md`)

| User wants to…                            | Read file                                 |
| ----------------------------------------- | ----------------------------------------- |
| Explore and understand a codebase         | `references/cartograph-workflows.md`      |
| Debug an error by tracing callers/callees | `references/cartograph-workflows.md`      |
| Assess blast radius before making changes | `references/cartograph-workflows.md`      |
| Review a pull request for risk            | `references/cartograph-workflows.md`      |
| Safely refactor (rename, extract, split)  | `references/cartograph-workflows.md`      |
| Find entry points and execution flows     | `references/cartograph-workflows.md`      |
| Map all call sites before a rename        | `references/cartograph-workflows.md`      |
| Determine if a change is high risk        | `references/cartograph-workflows.md`      |
| Understand unknown code quickly           | `references/cartograph-workflows.md`      |
| Delegate codebase research to sub-agents  | `SKILL.md` (sub-agent delegation section) |
| Launch explore agents with cartograph     | `SKILL.md` (sub-agent delegation section) |

## Routing Decision Tree

Apply these rules in order. **First match wins.**

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

### 2. CLI Commands

**Triggers:** User mentions a specific command (`analyze`, `query`, `context`,
`impact`, `cypher`, `schema`, `source`, `clone`, `models`, `serve`, `skills`,
`list`, `status`, `clean`, `wiki`), command flags, graph schema, Cypher syntax,
node labels, relationship types, embedding configuration, model management,
or needs a command reference.

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
- Running a background service for embedding and fast queries

## Edge Cases

- **"analyze"** or **"index"** or **"re-index"** → CLI Commands
- **"query"** or **"search"** (finding symbols/flows) → CLI Commands
- **"context"** or **"what calls X"** or **"what does X call"** or **"trace calls from X"** → CLI Commands
- **"trace 3 levels deep"** or **"follow call chain"** or **"transitive callees"** → CLI Commands (use `context --depth`)
- **"impact"** or **"blast radius"** (command reference) → CLI Commands
- **"what breaks if I change X"** (practical workflow) → Workflows
- **"cypher"** or **"graph query"** or **"MATCH"** → CLI Commands
- **"explain the full architecture"** or **"how does this system work end-to-end"** → Deep-Dive Architecture
- **"map all subsystems"** or **"trace the core processing loop"** → Deep-Dive Architecture
- **"find goroutine roots"** or **"find async entry points"** or **"SPAWNS edges"** → Deep-Dive Architecture
- **"deep architecture analysis"** or **"understand this codebase deeply"** → Deep-Dive Architecture
- **"explore this codebase"** or **"understand the architecture"** → Deep-Dive Architecture
- **"debug"** or **"trace an error"** → Workflows
- **"PR review"** or **"is this change risky"** → Workflows
- **"refactor"** or **"rename safely"** → Workflows
- **"schema"** or **"node labels"** or **"relationship types"** → CLI Commands
- **"source code"** or **"read file"** → CLI Commands
- **"how do I use cartograph"** → Vague/General (ask to disambiguate)
- **"how does https://github.com/X/Y work"** → Remote Repository URL (index + explore)
- **"explain the architecture of <repo-url>"** → Remote Repository URL (index + explore)
- **"I want to understand <repo-url>"** → Remote Repository URL (index + explore)
- **"how does hashicorp/nomad work"** → Remote Repository URL (resolve shorthand → index + explore)
- **"compare moby/moby and hashicorp/nomad to my project"** → Multi-Repo Comparison
- **"how does X compare to Y"** (X, Y are repos) → Multi-Repo Comparison
- **"what's the difference between X and Y"** (X, Y are repos) → Multi-Repo Comparison
- **"analyze nomad and docker"** (bare names) → Bare Project Names (confirm first)
- **"compare consul with my project"** (bare name) → Bare Project Names (confirm first)
- **"index docker and tell me how it works"** (bare name) → Bare Project Names (confirm first)
- **"how does etcd work"** (bare name, no URL) → Bare Project Names (confirm first)
- **"analyze this repo: <url>"** → CLI Commands (just indexing, no explore)
- **"how does X work"** or **"understand this code"** → Workflows (Research Loop)
- **"research with sub-agents"** or **"launch explore agents"** → SKILL.md (sub-agent delegation)
- **"use cartograph in agents"** or **"agent prompt template"** → SKILL.md (sub-agent delegation)
- **"models"** or **"list models"** or **"pull model"** or **"download model"** → CLI Commands
- **"embeddings"** or **"semantic search"** or **"vector search"** or **"embed"** → CLI Commands
- **"serve"** or **"start service"** or **"background service"** or **"stop server"** → CLI Commands
- **"skills"** or **"install skill"** or **"manage skills"** → CLI Commands
