# Deep-Dive Architecture Analysis

A structured multi-phase workflow for producing a complete architectural
understanding of a codebase using cartograph. This goes beyond simple
exploration — it systematically maps entry points, traces end-to-end flows,
identifies subsystems, and uncovers internal flows that don't appear in
top-ranked lists.

**When to use this workflow:**
- "Explain the full architecture of this project"
- "How does scheduling/request processing/data pipeline work end-to-end?"
- "Map all the major subsystems and how they connect"
- "I need to understand this codebase deeply before making changes"
- Any task requiring architectural understanding beyond surface exploration

**Prerequisite:** The repository must be indexed. Run `cartograph analyze`
first (or `cartograph analyze <url>` for remote repos). Verify with
`cartograph list`.

---

## Phase 1: Orientation — Shape of the System

**Goal:** Get a 30-second mental model of the codebase's scale, structure,
and major subsystems.

```bash
# 1a. Graph shape — what node/edge types exist, how big is the graph
cartograph schema

# 1b. Top execution flows — the system's architectural spine
cartograph cypher "MATCH (p:Process) RETURN p.name, p.importance, p.stepCount, p.callerCount, p.heuristicLabel ORDER BY p.importance DESC LIMIT 20"

# 1c. Major subsystems — community clusters with size
cartograph cypher "MATCH (c:Community) RETURN c.name, c.size ORDER BY c.size DESC LIMIT 15"
```

**What to extract:**
- **Scale:** Total nodes/edges gives you project size. >10K nodes = large project.
- **Top flows:** The top 5-10 flows by importance are the system's architectural
  backbone. Read their names — they reveal the core runtime loops.
- **Communities:** Large communities (100+ members) are major subsystems.
  Community names hint at purpose (e.g., "scheduler+reconciler",
  "state+store", "executor+drivers").

**Output a working hypothesis:** After Phase 1, you should be able to say
"This system has N major subsystems: [list]. The core runtime appears to
involve [top flow names]."

---

## Phase 2: Entry Point Discovery — Where Control Flow Begins

**Goal:** Find all architectural entry points — not just `main()`, but
concurrent task roots, RPC handlers, async spawners, and event loop callbacks.

```bash
# 2a. Async/concurrent roots — functions that are SPAWNED (run concurrently)
cartograph cypher "MATCH (spawner)-[:SPAWNS]->(target) RETURN target.name, target.filePath, spawner.name AS spawnedBy ORDER BY target.name"

# 2b. Delegate targets — functions registered as callbacks/handlers
cartograph cypher "MATCH (registrar)-[:DELEGATES_TO]->(target) RETURN target.name, target.filePath, registrar.name AS registeredBy ORDER BY target.name"

# 2c. Exported functions with no callers — potential API entry points
cartograph cypher "MATCH (f:Function) WHERE NOT ()-[:CALLS]->(f) AND f.isExported = true AND NOT f.isTest RETURN f.name, f.filePath LIMIT 20"

# 2d. Discovered execution flows ranked by architectural importance
cartograph cypher "MATCH (p:Process) RETURN p.name, p.importance, p.heuristicLabel ORDER BY p.importance DESC LIMIT 15"
```

**What to extract:**
- **SPAWNS targets** are the most architecturally significant — a developer
  explicitly chose to run these concurrently (goroutines in Go, executor
  tasks in Java, setTimeout/Worker in JS/TS, tokio::spawn in Rust,
  threading.Thread in Python, Task.Run in C#, etc.).
- **DELEGATES_TO targets** reveal framework patterns — HTTP handlers, event
  listeners, plugin callbacks, worker pool tasks.
- **High fan-out roots** are orchestrators — they set up or coordinate
  multiple subsystems.

**Categorize entry points:** Group discovered entry points into:
1. **Lifecycle** — startup, shutdown, initialization (e.g., `main`, `NewServer`, `initialize`)
2. **Runtime loops** — continuously running concurrent tasks (e.g., event loops, leader monitors, watchers)
3. **Request handlers** — RPC/HTTP/event handlers triggered by external input (e.g., API endpoints, message consumers)
4. **Background workers** — periodic or event-driven background tasks (e.g., sync loops, health checkers, garbage collectors)

---

## Phase 3: Flow Tracing — Follow the Call Graph Deep

**Goal:** Trace each major entry point through the call graph to understand
end-to-end execution paths. This is cartograph's most powerful capability.

### 3a. Trace entry points with `context --depth`

For each of the top 5 flows from Phase 1 and the key entry points from
Phase 2, use `context --depth 3` to see the full transitive call tree:

```bash
# Trace 3 levels deep from an entry point — reveals the full execution path
cartograph context <entryPointName> --depth 3

# Examples:
cartograph context NewServer --depth 3
cartograph context handleRequest --depth 3
cartograph context Worker.run --depth 3
```

The `--depth 3` output shows:
- **Symbol:** The target function
- **Callers:** Who invokes this? (reveals the trigger)
- **Call tree:** Transitive callees as an indented tree, following CALLS,
  SPAWNS (⇢), and DELEGATES_TO (⤳) edges. Children are sorted by fan-out
  (architecturally significant nodes first). Pruning counts show where
  branches were capped.
- **Processes:** Which execution flows include this symbol?

**Depth guide:** `--depth 2` for a quick look, `--depth 3` for most
exploration (covers 3 architectural layers), `--depth 4+` only when chasing
a specific deep path. Default (no flag) shows flat callees — use this for
narrow symbols.

### 3b. Follow SPAWNS edges in the call tree

SPAWNS edges (shown as ⇢ in the tree) reveal async/concurrent flow
continuations. When you see a ⇢ edge, that function runs in its own
goroutine/thread/task — it's often an independent architectural flow worth
tracing separately:

```bash
# If context --depth 3 shows: ⇢ monitorLeadership → ...
# Trace that spawned function deeper:
cartograph context monitorLeadership --depth 3
```

### 3c. Use Cypher for targeted graph queries

Use Cypher when you need specific structural queries that `context --depth`
doesn't cover — cross-package boundaries, multi-flow membership, or
upstream tracing:

```bash
# Find where a flow crosses package/directory boundaries
cartograph cypher "MATCH (a)-[:CALLS]->(b) WHERE a.filePath STARTS WITH 'src/server/' AND b.filePath STARTS WITH 'src/scheduler/' RETURN a.name, b.name, a.filePath, b.filePath"

# Follow spawn chains (task A spawns B which spawns C)
cartograph cypher "MATCH path = (a)-[:SPAWNS*1..3]->(b) RETURN [n IN nodes(path) | n.name] AS spawnChain, length(path) AS depth ORDER BY depth DESC"

# Find functions that both CALL and SPAWN other functions
cartograph cypher "MATCH (f)-[:CALLS]->(callee), (f)-[:SPAWNS]->(spawned) RETURN f.name, collect(DISTINCT callee.name) AS calls, collect(DISTINCT spawned.name) AS spawns"

# Trace upstream callers (reverse direction — not covered by --depth)
cartograph cypher "MATCH path = (entry)-[:CALLS*1..5]->(target:Function {name: 'TARGET'}) RETURN [n IN nodes(path) | n.name] AS chain"
```

### 3d. Trace the critical path through a system

The recommended workflow for tracing an end-to-end path:

```bash
# Step 1: Start with a high-level entry point, trace 3 levels deep
cartograph context NewServer --depth 3
# → See: setupRaft, ⇢ monitorLeadership → establishLeadership → planApply,
#         setupWorkers → NewWorker, NewEvalBroker, ...

# Step 2: Follow the most interesting branch deeper
cartograph context monitorLeadership --depth 3
# → See: leaderLoop → establishLeadership → ⇢ planApply, revokeLeadership

# Step 3: Follow spawned/delegated targets
cartograph context planApply --depth 3
# → See: Dequeue, applyPlan → UpsertPlanResults, evaluateNodePlan

# Step 4: Read source for the 2-3 functions that need source-level understanding
cartograph context <keyFunction> --content
```

**Key technique:** After each `context --depth 3`, follow ⇢ SPAWNS edges and
nodes with high fan-out (many children in the tree). Cross-package hops (file
paths change directories) signal architectural boundaries — pay extra
attention to these.

---

## Phase 4: Subsystem Mapping — Connect the Dots

**Goal:** Map which flows and entry points belong to which subsystems, and
identify the interfaces between them.

### 4a. Map flows to communities

```bash
# Which community does each top flow's entry point belong to?
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f), (f)-[:MEMBER_OF]->(c:Community) WHERE p.importance > 100 RETURN p.name, c.name AS community, count(f) AS stepsInCommunity ORDER BY p.name, stepsInCommunity DESC"
```

### 4b. Find cross-community bridges

```bash
# Functions that bridge two communities (architectural boundaries)
cartograph cypher "MATCH (a)-[:CALLS]->(b), (a)-[:MEMBER_OF]->(ca:Community), (b)-[:MEMBER_OF]->(cb:Community) WHERE ca <> cb RETURN a.name, ca.name AS fromCommunity, b.name, cb.name AS toCommunity LIMIT 30"
```

### 4c. Find interface boundaries

```bash
# Interfaces and their implementors — reveals abstraction layers
cartograph cypher "MATCH (c)-[:IMPLEMENTS]->(i:Interface) RETURN i.name, collect(c.name) AS implementors ORDER BY size(collect(c.name)) DESC LIMIT 15"

# Methods that override parent methods — inheritance hierarchy
cartograph cypher "MATCH (m:Method)-[:OVERRIDES]->(parent:Method) RETURN m.name, parent.name AS overrides, m.filePath LIMIT 20"
```

### 4d. Identify the data flow pattern

```bash
# What types are passed across package boundaries?
cartograph cypher "MATCH (f)-[:CALLS]->(g) WHERE f.filePath <> g.filePath WITH split(f.filePath, '/')[0] AS srcPkg, split(g.filePath, '/')[0] AS dstPkg WHERE srcPkg <> dstPkg RETURN srcPkg, dstPkg, count(*) AS callCount ORDER BY callCount DESC LIMIT 20"
```

---

## Phase 5: Gap Filling — Find What Ranking Misses

**Goal:** Cartograph's importance ranking surfaces the top architectural flows,
but some internally critical flows rank lower because they don't span many
packages or have few callers. This phase finds them.

### 5a. Find internal processing cores

Flows with high step counts but low importance often represent the "brains"
of a subsystem — they do complex work within a single package:

```bash
# High-step flows not in top 20 — internal processing cores
cartograph cypher "MATCH (p:Process) WHERE p.stepCount > 15 RETURN p.name, p.importance, p.stepCount, p.callerCount ORDER BY p.stepCount DESC LIMIT 30"
```

### 5b. Find heavily-called internal functions

Functions called by many top-flow steps are architecturally important even if
they don't head their own high-ranking flow:

```bash
# Functions that appear in multiple execution flows
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f) WITH f, count(p) AS flowCount, collect(p.name) AS flows WHERE flowCount >= 3 RETURN f.name, flowCount, f.filePath, flows ORDER BY flowCount DESC LIMIT 20"
```

### 5c. Find the "hidden glue" — functions that connect top flows

```bash
# Functions called by top-5 flows that also call into other subsystems
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f)-[:CALLS]->(g) WHERE p.importance > 150 AND f.filePath <> g.filePath WITH f, count(DISTINCT p) AS flowCount, count(DISTINCT g) AS calleeCount WHERE flowCount >= 2 AND calleeCount >= 3 RETURN f.name, flowCount, calleeCount, f.filePath ORDER BY flowCount DESC, calleeCount DESC LIMIT 15"
```

### 5d. Follow the scheduling/processing pattern

Many systems have a core processing loop where work items flow through
stages. Look for the complete pipeline:

```bash
# Find the processing pipeline: functions that receive work AND produce work
# (both called by a dispatcher AND call a submitter)
cartograph cypher "MATCH (dispatcher)-[:CALLS]->(processor)-[:CALLS]->(submitter) WHERE dispatcher.name CONTAINS 'dequeue' OR dispatcher.name CONTAINS 'dispatch' OR dispatcher.name CONTAINS 'worker' RETURN dispatcher.name, processor.name, submitter.name LIMIT 20"

# Find broker/queue patterns: Enqueue + Dequeue pairs
cartograph cypher "MATCH (enq:Function), (deq:Function) WHERE enq.name CONTAINS 'Enqueue' AND deq.name CONTAINS 'Dequeue' AND split(enq.filePath, '/')[0] = split(deq.filePath, '/')[0] RETURN enq.name, enq.filePath, deq.name, deq.filePath"
```

### 5e. Read source for key functions

Once you've identified architecturally important symbols, read their source
to understand what they actually do:

```bash
# Read source for a specific function (use file paths from context output)
cartograph cat <filePath> -l <startLine>-<endLine>

# Read source with context from symbol
cartograph context <symbolName> --content
```

---

## Phase 6: Synthesis — Produce the Architecture Map

**Goal:** Combine all findings into a structured architectural narrative.

### Output template

After completing Phases 1-5, produce a document with:

1. **System Overview** — One paragraph describing the system's purpose and scale
2. **Architecture Diagram** — ASCII or Mermaid diagram showing the core
   processing loop and subsystem interactions
3. **Subsystems** — For each major subsystem (community), describe:
   - Purpose
   - Key entry points
   - Internal processing flows
   - Interfaces with other subsystems
4. **Core Processing Loop** — The end-to-end flow that defines the system
   (e.g., request → scheduling → execution → feedback)
5. **Ranked Flows** — Top 10 flows with entry points, file locations, and
   why each matters architecturally
6. **Key Design Patterns** — Notable patterns discovered (worker pools,
   leader election, optimistic concurrency, etc.)

### Example synthesis

```
## Core Request Loop

Client submits request → API handler (e.g., RegisterJob) → validates + persists
  → Enqueue work item to broker/queue
    → Worker.dequeue() picks up the work
      → Processor.execute() → compute required changes (diff desired vs actual)
        → Planner.apply() → commit via consensus/storage
          → Downstream agents pick up changes via watcher
            → Runner.execute() → plugin/driver.start()
              → Agent reports status back to server
                → may create new work item → loop continues
```

---

## Anti-Patterns — What NOT to Do

1. **Don't stop at top-20 ranking.** The importance ranking surfaces the
   architectural spine, but internal processing cores (schedulers,
   reconcilers, evaluators) often rank lower because they live within a
   single package. Phase 5 exists to catch these.

2. **Don't read source code before querying the graph.** The graph gives you
   structure in seconds. Use `query` and `context` to find the right symbols
   first, then `source` only for the 3-5 functions that need source-level
   understanding.

3. **Don't trace every callee manually.** Use `context --depth 3` to get the
   full tree in one command. Only follow up on ⇢ SPAWNS edges or nodes with
   high fan-out that appear at the tree's depth limit.

4. **Don't skip async edges.** SPAWNS and DELEGATES_TO edges reveal the
   system's concurrent architecture. A concurrently-spawned task or
   registered handler is often more architecturally important than a
   directly-called utility.

5. **Don't confuse step count with importance.** A flow with 100 steps in
   one package may be less architecturally significant than a 20-step flow
   that crosses 5 packages. Look at package diversity, not just size.

---

## Quick Reference: Key Commands for Architecture Analysis

```bash
# Transitive call tree from any symbol (most useful command for tracing)
cartograph context <symbol> --depth 3

# Top flows by importance
cartograph cypher "MATCH (p:Process) RETURN p.name, p.importance ORDER BY p.importance DESC LIMIT 20"

# SPAWNS targets (async entry points)
cartograph cypher "MATCH (s)-[:SPAWNS]->(t) RETURN t.name, s.name AS spawnedBy, t.filePath"

# DELEGATES_TO targets (registered handlers/callbacks)
cartograph cypher "MATCH (r)-[:DELEGATES_TO]->(t) RETURN t.name, r.name AS registeredBy, t.filePath"

# Cross-package calls (architectural boundaries)
cartograph cypher "MATCH (a)-[:CALLS]->(b) WHERE a.filePath <> b.filePath RETURN a.name, b.name, a.filePath, b.filePath LIMIT 30"

# Functions in multiple flows (shared infrastructure)
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f) WITH f, count(p) AS n WHERE n >= 3 RETURN f.name, n ORDER BY n DESC"

# Upstream callers (reverse trace — not covered by --depth)
cartograph cypher "MATCH path = (entry)-[:CALLS*1..5]->(target:Function {name: 'TARGET'}) RETURN [n IN nodes(path) | n.name] AS chain"

# Package-to-package dependency map
cartograph cypher "MATCH (a)-[:CALLS]->(b) WITH split(a.filePath, '/')[0] AS src, split(b.filePath, '/')[0] AS dst WHERE src <> dst RETURN src, dst, count(*) AS weight ORDER BY weight DESC LIMIT 20"

# Community members (subsystem contents)
cartograph cypher "MATCH (f)-[:MEMBER_OF]->(c:Community {name: 'COMMUNITY_NAME'}) RETURN f.name, labels(f), f.filePath ORDER BY f.name LIMIT 30"
```
