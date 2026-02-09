# Delegating Research to Sub-Agents

When launching **explore** or **task** agents to research an indexed codebase,
include cartograph CLI instructions in the agent prompt so they use the
knowledge graph instead of raw grep/glob/view. **Instruct sub-agents to
start the background service** if it isn't already running -- this ensures
warm caches for the entire research session. Cartograph queries return
results in ms compared to grep-based exploration, and they surface
structural relationships (call trees, execution flows, process labels)
that text search cannot discover.

## When to delegate with cartograph

- The target repo is already indexed (check `cartograph list`)
- The research task involves understanding architecture, tracing flows,
  finding callers/callees, or locating symbols by intent
- Multiple independent research threads can run in parallel

## Prompt template for sub-agents

Include this block in the agent prompt (adapt the repo name):

````
You have access to `cartograph`, a graph-powered code intelligence CLI.
The repository "{repo_name}" is already indexed. Use cartograph as your
PRIMARY search tool -- it is 100x faster than grep and surfaces structural
relationships.

**Before you begin** -- ensure the background service is running:
```bash
cartograph serve status  # check if running
cartograph serve start   # start if not running
```

**Research loop -- use this workflow:**

1. **Search** -- find relevant symbols and execution flows:
   ```bash
   cartograph query "your search terms" -r {repo_name}
   ```

2. **Drill down** -- get the 360 view with transitive call tree:
   ```bash
   cartograph context <symbolName> -r {repo_name} -d 3
   ```
   The `-d 3` flag traces 3 levels deep through CALLS, SPAWNS, and
   DELEGATES_TO edges. Follow SPAWNS edges -- they reveal async/
   concurrent architecture worth tracing separately.

3. **Read source** -- only when you need implementation details:
   ```bash
   cartograph cat <filePath> -r {repo_name} -l <startLine>-<endLine>
   ```

4. **Repeat** -- each `context -d 3` output reveals new symbols to trace.
   Follow the most interesting branches (high fan-out, cross-package hops,
   SPAWNS edges) until you've mapped the full flow.

**Additional commands:**
- `cartograph impact <symbol> -r {repo_name}` -- blast radius analysis
- `cartograph cypher "<query>" -r {repo_name}` -- raw graph queries
- `cartograph schema -r {repo_name}` -- see available node labels and edge types

**Rules:**
- Start with `cartograph query`, NOT grep. Use grep only as a fallback
  if cartograph returns no results.
- Always use `-d 3` (not `-d 1`) when tracing call trees -- depth 1 only
  shows direct callees and misses the architecture.
- Use `cartograph cat` to read code -- it works
  on the indexed snapshot and doesn't require the repo to be on disk.
````

## Example: launching parallel explore agents

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
