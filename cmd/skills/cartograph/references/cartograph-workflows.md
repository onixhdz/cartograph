# Cartograph Workflows

Practical patterns for using cartograph in common coding tasks.

## Repo Resolution

Cartograph resolves repository references flexibly:

```bash
# Analyze by GitHub shorthand — auto-expands to full URL
cartograph analyze hashicorp/nomad

# Host-prefixed URL — also auto-expands
cartograph analyze github.com/hashicorp/nomad

# Query with short name — resolves to hashicorp/nomad if unambiguous
cartograph query "scheduler" -r nomad

# If multiple repos share a basename, use the full name
cartograph query "scheduler" -r hashicorp/nomad
```

## Exploring a Codebase

Use when you need to understand architecture, trace execution flows, or find components.

```bash
# First, see what's in the graph (node labels, edge types, properties)
cartograph schema

# Search by keyword for entry points
cartograph query "authentication"

# Drill into a specific symbol — 360° view
cartograph context handleLogin --content

# Trace 3 levels deep into the call tree (follows CALLS, SPAWNS, DELEGATES_TO)
cartograph context handleLogin --depth 3

# Find all entry points (functions not called by anything, excluding tests)
cartograph cypher "MATCH (f:Function) WHERE NOT ()-[:CALLS]->(f) AND f.isExported = true AND NOT f.isTest RETURN f.name, f.filePath"

# List discovered execution flows (ranked by architectural importance)
cartograph cypher "MATCH (p:Process) RETURN p.name, p.importance, p.heuristicLabel ORDER BY p.importance DESC LIMIT 20"
```

**Approach:** Start with `schema` to understand the graph shape → `query` for orientation → `context --depth 3` to trace flows → `cypher` for custom structural queries.

## Debugging

Use when tracing errors, finding callers/callees, or understanding call chains.

```bash
# Find the function involved in the error — shows callers, callees, and processes
cartograph context parseConfig --content

# Trace downstream: what does this function call? (3 levels deep)
cartograph context parseConfig --depth 3

# Trace all callers (upstream — who invokes this?)
cartograph cypher "MATCH (caller)-[:CALLS]->(target:Function {name: 'parseConfig'}) RETURN caller.name, caller.filePath, caller.startLine"

# Full upstream call chain
cartograph cypher "MATCH path = (entry)-[:CALLS*1..5]->(target:Function {name: 'parseConfig'}) RETURN [n IN nodes(path) | n.name] AS chain"

# Read source at specific lines
cartograph source src/config/parser.go -l 40-80
```

**Tip:** Use `context --depth 3` for downstream tracing (what does this call?) and `impact --direction upstream` for upstream tracing (what leads to this?).

## Impact Analysis

Use when assessing blast radius before changes.

```bash
# What breaks if you change this? (downstream)
cartograph impact processOrder

# What depends on this? (upstream)
cartograph impact processOrder --direction upstream

# Limit traversal depth
cartograph impact processOrder -d 3

# Find affected files
cartograph cypher "MATCH (f:Function {name: 'processOrder'})-[:CALLS*1..3]->(g) RETURN DISTINCT g.filePath AS affectedFile"

# Which execution flows are affected?
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f:Function {name: 'processOrder'}) RETURN p.name, p.stepCount"
```

**Depth guide:** 1 = direct callers only (quick check), 2-3 = typical refactoring, 5 = full blast radius, 10+ = complete audit.

## PR Review

Use when reviewing pull requests to assess risk.

```bash
# Understand a changed function's role
cartograph context changedFunction --content

# Check blast radius
cartograph impact changedFunction

# Is it part of a critical execution flow?
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f:Function {name: 'changedFunction'}) RETURN p.name, p.stepCount"

# Find all callers (these could break)
cartograph cypher "MATCH (caller)-[:CALLS]->(f:Function {name: 'changedFunction'}) RETURN caller.name, caller.filePath"
```

**Risk signals:** 10+ affected symbols = high risk. Part of payment/auth/security process = critical. Many callers (fan-in > 5) = verify backward compatibility.

## Safe Refactoring

Use when renaming, extracting, or splitting code.

```bash
# Before refactoring: map all call sites
cartograph cypher "MATCH (caller)-[:CALLS]->(f {name: 'oldName'}) RETURN caller.name, caller.filePath, caller.startLine ORDER BY caller.filePath"

# Find all imports of the file
cartograph cypher "MATCH (f:File)-[:IMPORTS]->(target:File {filePath: 'src/utils.go'}) RETURN f.filePath"

# After refactoring: re-index and verify
cartograph analyze --force
cartograph context newName
```

**Always re-index after refactoring** with `cartograph analyze --force`. The graph reflects code at index time.

## Research Loop — Understanding Unknown Code

The fastest way to understand any part of an indexed codebase. Use this
iterative loop instead of grep/view when exploring architecture, tracing
flows, or answering "how does X work?" questions.

### The Loop: query → context --depth → source

```bash
# Step 1: SEARCH — find the right symbols and flows
cartograph query "authentication middleware"
# → Returns: Process matches (execution flows) + Definition matches (symbols)
# → Pick the most relevant symbol names from the results

# Step 2: DRILL DOWN — trace the call tree 3 levels deep
cartograph context handleAuth --depth 3
# → Returns: Callers, transitive call tree (with SPAWNS ⇢ and DELEGATES_TO ⤳),
#            and which execution flows include this symbol
# → Follow the most interesting branches:
#     - ⇢ SPAWNS edges (async/concurrent — architecturally significant)
#     - High fan-out nodes (orchestrators that coordinate subsystems)
#     - Cross-package hops (file paths change directories = architectural boundary)

# Step 3: READ SOURCE — only for the 2-3 functions that need source-level detail
cartograph source src/auth/middleware.go -l 40-80
# → Or use: cartograph context handleAuth --content (includes inline source)

# Step 4: REPEAT — each context output reveals new symbols to trace
cartograph context validateToken --depth 3
cartograph context sessionStore.Get --depth 2
# → Keep following until you've mapped the complete flow
```

### When to use each depth

| Depth | Use case |
|-------|----------|
| (none) | Quick check — flat list of direct callees |
| `-d 2` | Quick look — covers 2 architectural layers |
| `-d 3` | **Default for exploration** — covers 3 layers, reveals full flow structure |
| `-d 4+` | Chasing a specific deep path through many layers |

### Combining with other commands

```bash
# After tracing a flow, check blast radius before changing it
cartograph impact handleAuth -d 3

# Use Cypher for structural queries context can't answer
# (e.g., "which flows share this function?")
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f:Function {name: 'handleAuth'}) RETURN p.name, p.importance"

# Find all upstream callers (reverse direction — context traces downstream)
cartograph cypher "MATCH path = (entry)-[:CALLS*1..5]->(target:Function {name: 'handleAuth'}) RETURN [n IN nodes(path) | n.name] AS chain"
```

### Research loop vs grep

| | cartograph research loop | grep/view |
|---|---|---|
| **Speed** | ~100ms per query | ~10-15s per search |
| **Call trees** | `context -d 3` shows full transitive tree | Must trace manually file-by-file |
| **Execution flows** | Surfaces process labels and flow membership | No flow awareness |
| **Async edges** | Shows SPAWNS (⇢) and DELEGATES_TO (⤳) | Invisible — must read source to find |
| **Cross-package** | Call tree shows hops across directories | Must search each package separately |
| **Source code** | `source -l` for specific lines | `view` or `cat` (needs file on disk) |
| **When to prefer** | Architecture, flows, "how does X work?" | Exact string matching, config files, non-code |

**Rule of thumb:** Start with cartograph. Fall back to grep only when
searching for string literals, configuration values, or non-code files
that aren't in the knowledge graph.
