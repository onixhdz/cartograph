---
name: benchmark-test
description: 'Benchmark Test: systematic search quality evaluation for Cartograph across multiple languages and query types. Covers battery design, execution, scoring, and regression detection.'
metadata:
  version: '3.0'
---

# Cartograph Search Quality Benchmark

Systematic evaluation framework for measuring and improving Cartograph's
search quality across keyword and intent queries. Used to validate changes
to the embedding pipeline (textgen.go), hybrid search (backend.go), and
ingestion (entry_point_scoring.go) without regression.

## Quick Start — Run All Batteries

```bash
# Prerequisites: dev binary built, server running
task build:dev
./cartograph-darwin-arm64 serve start --no-detach --no-idle &

# 1. Index all test repos (clean + re-embed)
for repo in turbot/steampipe excalidraw/excalidraw fastapi/fastapi hashicorp/nomad; do
  ./cartograph-darwin-arm64 clean "$repo"
  ./cartograph-darwin-arm64 analyze "$repo" --embed=sync
done

# 2. Run batteries (see "Running a Battery" below for per-repo commands)
# 3. Score with score.py (see "Scoring" below)
```

## Architecture of the Benchmark

### Battery Files

Each test project has a battery file in `batteries/` defining 5 investigations
with keyword + intent query pairs and ground-truth expected symbols:

| File                      | Language   | Nodes | Limit  | Investigations                                          |
| ------------------------- | ---------- | ----- | ------ | ------------------------------------------------------- |
| `batteries/steampipe.md`  | Go         | 882   | 8      | query exec, plugins, db lifecycle, connections, console |
| `batteries/excalidraw.md` | TypeScript | 1253  | 8      | rendering, export, undo/redo, collaboration, elements   |
| `batteries/fastapi.md`    | Python     | 756   | 8      | routing, DI, validation, middleware, OpenAPI            |
| `batteries/nomad.md`      | Go         | 37587 | **15** | startup, scheduling, node failure, raft, client-server  |

### Query Types

Each investigation runs two queries:

- **Keyword (KW):** Technical terms a developer would search for.
  Tests BM25 precision. E.g., `"execute query SQL statement result session"`
- **Intent (INT):** Natural language question an AI agent would ask.
  Tests semantic embedding quality. E.g., `"how does steampipe execute a SQL query"`

### Scoring

Per investigation:

- Count how many expected symbols appear in KW results → **KW recall**
- Count how many expected symbols appear in INT results → **INT recall**
- **Combined** = union of KW ∪ INT found symbols
- **Criteria PASS** if combined ≥ 4 symbols (configurable)

Aggregated:

- **KW total**: sum of KW hits / total expected symbols (percentage)
- **INT total**: sum of INT hits / total expected symbols (percentage)
- **Criteria**: count of passing investigations / total

### Scoring Script

```bash
python3 .agents/skills/benchmark-test/score.py <results_file> <battery_file>
```

Example:

```bash
python3 .agents/skills/benchmark-test/score.py \
  /tmp/bat-steampipe.txt \
  .agents/skills/benchmark-test/batteries/steampipe.md
```

## Running a Battery

### Step 1: Index the repo

```bash
./cartograph-darwin-arm64 clean <owner/repo>
./cartograph-darwin-arm64 analyze <owner/repo> --embed=sync
```

### Step 2: Run queries

For each investigation in the battery file, run the KW and INT queries.
The output MUST be structured with these exact markers for the scoring
script to parse:

```
=== Investigation N: <name> ===
--- KW ---
<cartograph query output>
--- INT ---
<cartograph query output>
```

Template for a battery run (adapt repo name, queries, and limit):

```bash
BIN="./cartograph-darwin-arm64"
REPO="turbot/steampipe"
LIMIT=8
OUT="/tmp/bat-steampipe.txt"

echo "=== Investigation 1: Query execution ===" > $OUT
echo "--- KW ---" >> $OUT
$BIN query "execute query SQL statement result session" -r $REPO -l $LIMIT 2>/dev/null >> $OUT
echo "--- INT ---" >> $OUT
$BIN query "how does steampipe execute a SQL query" -r $REPO -l $LIMIT 2>/dev/null >> $OUT

# ... repeat for investigations 2-5 ...
```

### Step 3: Score

```bash
python3 .agents/skills/benchmark-test/score.py /tmp/bat-steampipe.txt \
  .agents/skills/benchmark-test/batteries/steampipe.md
```

### Recommended Limits

| Codebase size | Limit   | Rationale                                               |
| ------------- | ------- | ------------------------------------------------------- |
| < 5K nodes    | `-l 8`  | Default — enough slots for focused results              |
| 5K-20K nodes  | `-l 10` | Moderate dilution                                       |
| > 20K nodes   | `-l 15` | Large codebases need more slots to surface deep symbols |

## Current Baseline (2026-04-05, prose summary build)

```
Project      Lang    Nodes    KW          INT         Criteria
─────────────────────────────────────────────────────────────
steampipe    Go      882      36/40 (90%) 24/40 (60%) 5/5
excalidraw   TS      1253     30/40 (75%) 28/40 (70%) 5/5
fastapi      Python  756      22/39 (56%) 25/39 (64%) 5/5
nomad        Go      37587    26/41 (63%) 17/41 (41%) 4/5
─────────────────────────────────────────────────────────────
TOTAL                         114/160(71%) 94/160(59%) 19/20(95%)
```

**Model:** bge-small (384d, 24MB)
**Binary:** dev build with prose summary in textgen.go

## Improvement History

```
Phase   Change                          KW     INT    Criteria
─────────────────────────────────────────────────────────────────
1       Initial (jina-code, nomad only) 56%    27%    14/21 FAIL
2       Textgen enrichment (Role,       68%    43%    16/20
        pkg-doc, heuristic labels)
3       Vector supplement strategy      83%    53%    8/10
        (replaced RRF reranking)
4       Regex test/example detection    73%    58%    25/30 (83%)
        (3-project cross-language)
5       Prose summary (graph-to-prose)  71%    59%    19/20 (95%)
        (4-project incl. nomad)
```

### Key Findings

1. **Text representation > model size.** bge-small (33M, 384d) with enriched
   text beats jina-code (161M, 768d) with the same text. On FastAPI:
   bge-small KW 56% INT 64% vs jina-code KW 28% INT 38%.

2. **RRF reranking hurts.** Merging BM25 + vector via RRF corrupted good BM25
   rankings. Vector supplement (preserve BM25, add vector-only discoveries)
   is strictly better.

3. **Prose summaries help intent.** Templated natural language from graph
   signals ("Authentication function; bridges internal/auth; spawns concurrent
   X; part of the auth-flow flow") helps small embedding models match intent
   queries. INT improved +8 across 3 projects.

4. **Large codebases dilute results.** Nomad (37K nodes) needs `-l 15` to
   match smaller projects at `-l 8`. Investigation 3 (node failure)
   consistently fails — deep internal functions drowned by surface symbols.

5. **Python is hardest for KW.** Short symbol names (`get`, `post`, `validate`)
   are hard for BM25. But INT is strong because Python has rich docstrings.

6. **Graph non-determinism exists.** Each `analyze --force` produces slightly
   different graphs (±1% edges). Run batteries 2-3 times and average if
   measuring small deltas.

## Regression Detection Workflow

When making changes to the search pipeline:

### 1. Establish baseline

Run steampipe (fastest: ~20s to embed) using the manual battery process
described in "Running a Battery" above. Save results:

```bash
# Run all 5 investigations for steampipe (see "Running a Battery" for
# the query template). Output to a descriptive file:
#   /tmp/bat-steampipe-baseline.txt

# Then score:
python3 .agents/skills/benchmark-test/score.py \
  /tmp/bat-steampipe-baseline.txt \
  .agents/skills/benchmark-test/batteries/steampipe.md
```

### 2. Make your change

Edit `textgen.go`, `backend.go`, `entry_point_scoring.go`, etc.

### 3. Rebuild + re-embed

```bash
task build:dev
# IMPORTANT: must restart server to pick up new binary
# Kill old server, remove lockfile, start new one
kill <old-pid>
rm -f ~/.local/share/cartograph/service.lock
./cartograph-darwin-arm64 serve start --no-detach --no-idle &

# Clean and re-index
./cartograph-darwin-arm64 clean turbot/steampipe
./cartograph-darwin-arm64 analyze turbot/steampipe --embed=sync
```

### 4. Run battery again

Run the same battery queries from Step 1, save to a new file, and score:

```bash
python3 .agents/skills/benchmark-test/score.py \
  /tmp/bat-steampipe-after.txt \
  .agents/skills/benchmark-test/batteries/steampipe.md
```

### 5. Compare

- **No regression:** KW and INT should not drop by more than 2 points
- **Improvement:** Look for INT gains (intent is where most changes have impact)
- **Criteria:** Should never drop below current baseline

### 6. Cross-project validation

If steampipe shows improvement, validate on excalidraw (TS) and fastapi (Python)
to ensure the improvement is language-general, not language-specific.

## Important: Server Management

The dev binary runs as a background server. Common pitfalls:

```bash
# Check which binary is running (dev vs installed)
ps aux | grep cartograph-darwin

# The installed binary at ~/.local/bin/cartograph is DIFFERENT from dev binary
# Always verify you're testing the right one!

# Start dev server
./cartograph-darwin-arm64 serve start --no-detach --no-idle &

# If server won't start (lockfile), clean up:
rm -f ~/.local/share/cartograph/service.lock

# After rebuilding, MUST restart server:
kill <old-pid>
rm -f ~/.local/share/cartograph/service.lock
./cartograph-darwin-arm64 serve start --no-detach --no-idle &
```

## Adding a New Test Project

The user may request a battery for a specific project by name, e.g.:
- *"let's make a battery for temporal"*
- *"add a benchmark for kubernetes"*
- *"create a battery for the gin web framework"*

When this happens, resolve the project name to an `owner/repo`, index it,
and follow the steps below. If the name is ambiguous (e.g., "docker" could
be `moby/moby`, `docker/cli`, or `docker/compose`), use
`cartograph analyze <bare-name>` — it searches GitHub and prints ranked
matches. Present the suggestions to the user and confirm which one to use.

If no specific project is requested, pick one that adds coverage the
existing batteries don't have:

| Consideration             | Guidance                                                 |
| ------------------------- | -------------------------------------------------------- |
| Language                  | Prefer a language not yet covered (Rust, Java, C, etc.)  |
| Size                      | 500–5K nodes is ideal for fast iteration; >20K is a stress test |
| Architecture              | Clear subsystems (routing, auth, storage, scheduling…)   |
| Familiarity               | Well-known OSS projects make ground-truth easier to verify |

Existing coverage: Go (steampipe, nomad), TypeScript (excalidraw), Python (fastapi).

### 1. Resolve and index the project

Use `--clone` so the source code is available on disk for explore agents
to read directly. Without `--clone`, cartograph streams files from git
objects — the graph is built but there's no directory tree to `grep`/`view`.

```bash
# Index AND clone source to ~/.local/share/cartograph/<owner>/<repo>/
./cartograph-darwin-arm64 analyze <owner/repo> --embed=sync --clone
```

Check the node count — this goes in the battery header and determines the
recommended limit:

```bash
./cartograph-darwin-arm64 status -r <owner/repo>
```

### 2. Pick 5 investigation areas

Each investigation should target a distinct subsystem or concern. Good
areas are:

- **Core processing loop** — the main execution path (e.g., request handling,
  job scheduling, rendering pipeline)
- **Plugin/extension system** — how the project supports extensibility
- **Lifecycle management** — startup, shutdown, initialization
- **Configuration/state** — how settings are loaded and state is managed
- **External interfaces** — API surface, CLI commands, event handling

Aim for a mix of:
- Public entry points (easy for search to find)
- Internal implementation details (harder — tests the depth of results)
- Cross-package calls (tests whether the graph captures relationships)

### 3. Research ground-truth symbols

For each investigation, you need 7–9 symbols that represent the real
implementation. Every symbol **must** be verified against the actual source.
Use a combination of cartograph queries and explore agents.

#### Method A — Cartograph research loop (query → context → source)

Start with broad queries to discover candidates, then drill into the
graph to find the full call chain:

```bash
# 1. Discover candidate symbols with a broad query
./cartograph-darwin-arm64 query "scheduling job evaluation worker" -r <owner/repo> -l 15

# 2. Pick a promising symbol — expand its call graph to find related symbols
./cartograph-darwin-arm64 context <SymbolName> -r <owner/repo> --depth 2

# 3. Read the actual source to verify what the symbol does
./cartograph-darwin-arm64 source <SymbolName> -r <owner/repo>
```

This loop naturally surfaces the internal helpers and cross-package calls
that make good "medium" and "hard" battery symbols. Repeat for each
investigation area.

#### Method B — Explore agents on the cloned source

Launch parallel explore agents to research each investigation area against
the cloned source on disk. This is the fastest way to ground-truth 5 areas
simultaneously.

```
Launch 5 explore agents in parallel, one per investigation area.

Each agent prompt should include:
- The investigation area (e.g., "scheduling and job evaluation")
- The cloned source path: ~/.local/share/cartograph/<owner>/<repo>/
- Instructions to find 7-9 key symbols (functions, types, methods)
- Instructions to record: symbol name, file path, one-line description
```

**Example explore agent prompt:**

```
Research the SCHEDULING subsystem in ~/.local/share/cartograph/hashicorp/nomad/.

Find 7-9 key functions/types that implement job scheduling and evaluation.
Look for:
- The main scheduler entry point
- How evaluations are created and dequeued
- The bin-packing or placement algorithm
- Worker goroutines that process evaluations
- Plan submission and application

For each symbol, record:
- Exact function/type name (as it appears in source)
- File path relative to repo root
- One-line description of what it does

Use grep to search for function definitions, then read the files to
understand the call flow. Focus on non-test, non-vendor code.
```

#### Method C — Combine both (recommended)

Use cartograph to get the high-level map, then send explore agents to
verify and fill gaps:

1. Run `cartograph query` for each area → get candidate symbol names
2. Run `cartograph context <symbol> --depth 2` → discover callers/callees
3. Launch explore agents to read the cloned source and verify each symbol
   exists, confirm file paths, and write the one-line descriptions

This avoids the common mistake of picking symbols that look important
from the graph but don't actually exist as discrete functions in the source.

#### Verify symbols exist in the graph

After researching, confirm every symbol is indexed. A symbol not in the
graph will always score as a miss:

```bash
# Bulk-verify all candidate symbols
for sym in SymbolA SymbolB SymbolC; do
  ./cartograph-darwin-arm64 cypher \
    "MATCH (n:Symbol) WHERE n.name = '$sym' RETURN n.name, n.file LIMIT 1" \
    -r <owner/repo>
done
```

**Common reasons a symbol might be missing:**
- It's in a test file (excluded by default unless `--include-tests`)
- It's a generated file or vendored dependency
- It's a constant/variable rather than a function/type/method
- The name in source differs from what you expected (e.g., method receiver
  prefix, unexported name)

### 4. Write the battery file

Create `batteries/<project>.md` following this exact format:

```markdown
# <Project> Query Battery — Grounded Expected Symbols

All symbols verified against <owner/repo> source on GitHub (<date>).

## Investigation 1: <Area name> (<N> symbols)

Query keyword: `"<space-separated technical terms>"`
Query intent: `"<natural language question>"`

Expected symbols:
- `SymbolName` — path/to/file.go — brief description of what it does
- `AnotherSymbol` — path/to/other.go — brief description
...

## Investigation 2: <Area name> (<N> symbols)
...
```

**Format rules the scoring script depends on:**
- Investigation headers: `## Investigation N: <name> (<N> symbols)`
- Query lines: `Query keyword: \`"..."\`` and `Query intent: \`"..."\``
- Expected symbols: `- \`SymbolName\` — path — description` (backtick-wrapped name)
- 5 investigations, 7–9 symbols each (35–45 total)

**Writing good queries:**

| Query type | Guidelines                                                |
| ---------- | --------------------------------------------------------- |
| Keyword    | 4–7 technical terms a developer would grep for. Include function names, module names, domain terms. E.g., `"execute query SQL statement result session"` |
| Intent     | A natural language question an AI agent would ask. E.g., `"how does steampipe execute a SQL query"` |

**Choosing symbols — mix difficulty levels:**

- **2–3 easy:** Public entry points, top-level commands, exported types
  (BM25 usually finds these)
- **2–3 medium:** Internal helpers called by entry points, manager types
  (need good embeddings or graph context)
- **2–3 hard:** Deep internal functions, private helpers, cross-package
  utilities (stress-test for the search pipeline)

### 5. Run the battery and verify scoring works

```bash
# Run queries for all 5 investigations (see "Running a Battery" above)
# Save output to /tmp/bat-<project>.txt

# Score it
python3 .agents/skills/benchmark-test/score.py \
  /tmp/bat-<project>.txt \
  .agents/skills/benchmark-test/batteries/<project>.md
```

If the scoring script reports parse errors, check the output format
matches the expected markers (`=== Investigation N:`, `--- KW ---`,
`--- INT ---`).

### 6. Record the baseline

Add a row to the "Current Baseline" table in this file with:
- Project name, language, node count
- KW total, INT total, Criteria score
- Limit used

### Tips from experience

- **Start with cartograph queries, not source browsing.** Running a few
  broad queries first reveals what the search engine already finds well
  and where the gaps are — this makes battery design faster.
- **Include at least 2 "hard" symbols per investigation.** If every symbol
  is easy, the battery won't detect regressions in embedding quality.
- **Test at the recommended limit for the codebase size.** A battery
  designed at `-l 15` will give inflated scores at `-l 8`.
- **Run the battery 2–3 times** on a fresh index to account for graph
  non-determinism (±1% edges). If a symbol is borderline, note it.
- **Python projects:** Expect lower KW scores — short generic symbol names
  (`get`, `post`, `validate`) are hard for BM25. INT should compensate
  if the project has good docstrings.
- **Large projects (>20K nodes):** Some deep internal functions may be
  permanently drowned by surface-level symbols. Flag these as known
  hard cases in the battery notes rather than removing them — they track
  future improvements in ranking.

## Files

```
.agents/skills/benchmark-test/
├── SKILL.md              ← this file (methodology + baselines)
├── score.py              ← scoring script
└── batteries/
    ├── steampipe.md      ← Go (882 nodes) — fastest for feedback loops
    ├── excalidraw.md     ← TypeScript (1253 nodes)
    ├── fastapi.md        ← Python (756 nodes)
    └── nomad.md          ← Go (37587 nodes) — stress test for large codebases
```
