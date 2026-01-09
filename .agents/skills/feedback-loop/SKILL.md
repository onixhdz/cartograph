---
name: feedback-loop
description: 'Feedback Loop: iterative algorithm improvement driven by evaluation plan acceptance criteria'
metadata:
  version: '1.0'
---

# Feedback Loop — Iterative Algorithm Improvement

Drive measurable improvements to cartograph's analysis, ranking, and query algorithms by executing an evaluate → diagnose → fix → re-index → re-evaluate cycle until every acceptance criterion in the feedback plan passes.

## Prerequisites

- A feedback plan file exists in `plans/` (e.g., `plans/cartograph-feedback-nomad-eval.md`).
- The target repository is already indexed or can be indexed with `cartograph analyze`.
- The `cartograph` binary can be built with `task build:dev` from the workspace root.
- `cartograph` is already on `$PATH` via dev symlink.

## Workflow

Repeat the cycle below until **all** acceptance criteria in the plan pass or you are confident no further code changes can improve results.

### Phase 0 — Read the Feedback Plan

1. Read the feedback plan file end-to-end. Identify:
   - **Acceptance criteria** — the numbered pass/fail conditions at the top.
   - **Reproduction commands** — the exact `cartograph` invocations to run.
   - **Remaining issues** — the narrative descriptions of what is still broken.
   - **Scores** — the per-capability scores and overall target.
2. Build a todo list from the acceptance criteria so progress is trackable.

### Phase 1 — Build & Start Service

1. **Kill the running binary and stop the background service** before building to ensure the old binary is not held by a running process and the new binary is picked up cleanly:
   ```bash
   pkill -f cartograph 2>/dev/null || true
   cartograph serve stop 2>/dev/null || true
   ```
   This is a no-op if no process or service is running.
2. Run `cd /workspaces/cartograph && task build:dev` to compile the updated binary.
3. If the build fails, fix compilation errors before proceeding.
4. **Start the background service** so queries can be dispatched in parallel:
   ```bash
   cartograph serve start
   ```
   Wait briefly for the service to be ready before proceeding to evaluation.

### Phase 2 — Evaluate

1. Run every reproduction command from the plan **exactly as written**.
   - With the service running, **dispatch independent queries in parallel** to speed up evaluation. Group queries that don't depend on each other and launch them concurrently.
   - Capture output for each command.
   - For cypher queries, pipe through `head -40` to keep output manageable.
2. For each acceptance criterion, record **pass** or **fail** with a one-line evidence note.
3. If all criteria pass, stop — report success and the final scores.

### Phase 3 — Diagnose & Fix

1. Pick the **highest-impact failing criterion** (the one most likely to move the overall score).
2. Search the codebase for the relevant code paths (ranking, scoring, filtering, sorting, community labeling, flow detection, query relevance, etc.).
3. Implement a fix that:
   - Is **general-purpose** — benefits all repositories and supported languages, not just the evaluation target.
   - Is **algorithmically meaningful** — changes scoring formulas, sort behavior, filtering thresholds, deduplication logic, or structural heuristics.
   - Does **not** game the system — no hard-coded repo names, no special-case symbol lists, no test-only overrides.
4. Run `go build ./...` to verify compilation. Fix any errors.
5. **Prefer unit tests over full re-index.** If the change can be validated with a native Go test (existing or new), run `go test ./...` or targeted test files first. This is faster and gives tighter feedback. Only proceed to Phase 4 (re-index + e2e) when the fix requires real project index data to verify — e.g., changes to ranking output, query relevance, or community labels that depend on a full graph.
6. If unit tests fail, fix before proceeding. If unit tests pass but the change needs e2e validation, continue to Phase 4.

### Phase 4 — Re-index (when needed)

**Not every code change requires a full re-index.** Before re-indexing, determine whether the change affects the stored graph or only how results are read/ranked at query time. There are three tiers:

#### Tier 1: No re-index needed (skip to Phase 5)
- **Query-time logic** — ranking, sorting, boosting, filtering, deduplication in `internal/query/` (e.g., `boost.go`, `backend.go`, `cypher_funcs.go`).
- **Output formatting** — display, serialization, or CLI presentation changes in `cmd/`.
- **Search scoring** — BM25 or vector scoring adjustments in `internal/search/` that operate on already-indexed data.
- **Service/client plumbing** — request routing, caching, or protocol changes in `internal/service/`.

For these, just rebuild (Phase 1) and jump straight to Phase 5 (re-evaluate).

#### Tier 2: Full re-index required
- **Ingestion pipeline** — walker, structure extraction, process, heritage/import/call resolution, MRO, interfaces, community detection, entry-point scoring (`internal/ingestion/`).
- **Graph schema** — new node labels, relationship types, or property changes (`internal/graph/`).
- **Storage format** — changes to how graph data is persisted (`internal/storage/`).
- **Extractors** — language-specific AST parsing or symbol extraction (`internal/ingestion/extractors/`).

For these, re-index the evaluation target:
```bash
cartograph analyze <repo> --force --embed=off 2>&1
```
Replace `<repo>` with the repository URL or short name from the plan.
**Always use `--embed=off`** — the user will run embeddings manually after the loop. Embedding is slow and unrelated to most structural/ranking fixes.

Wait for indexing to complete before proceeding.

### Phase 5 — Re-evaluate

1. Return to **Phase 2**. Run the reproduction commands again.
2. Compare results against the previous iteration.
3. If the fix caused a regression on a previously passing criterion, revert or adjust.
4. Update the todo list with pass/fail status.
5. If criteria still fail, return to **Phase 3** with the next highest-impact issue.

## Drift & Gaming Guard

Before every Phase 3 fix and after every Phase 5 re-evaluation, run this self-check. If any answer is **yes**, stop, revert the change, and re-read the feedback plan to realign.

1. **Am I drifting?** Is this change unrelated to a failing acceptance criterion? Am I solving a problem I invented rather than one the plan describes?
2. **Am I gaming?** Does this change make the metric pass without improving actual result quality? Examples: hard-coding expected output, filtering by repo-specific strings, relaxing thresholds so bad results slip through, rewriting the plan to match current output.
3. **Am I biased?** Would this change hurt results for a different language or repo structure? Am I optimizing for the evaluation target at the expense of generality?
4. **Am I looping without progress?** Have the last 2+ iterations changed the same code area without measurable improvement? If so, step back, summarize what was tried, and pick a different approach or a different failing criterion.
5. **Am I inflating scope?** Am I refactoring unrelated code, adding features not requested, or changing architecture beyond what the failing criterion requires?

If you catch yourself answering **yes** to any of these: stop immediately, revert the last change, re-read Phase 0 to ground yourself in the plan's actual acceptance criteria, and pick the next highest-impact failing criterion with a fresh approach.

## Constraints

- **No commits.** Do not run `git add`, `git commit`, or any other git write operation. Leave all changes unstaged — the user will review and commit separately.
- **No plan modifications.** Do not edit the feedback plan file. If you believe the cycle is complete, say so — the user will update the plan separately.
- **No bias.** Fixes must not be tailored to a specific project. Ask: "Would this change also improve results for a Python web framework? A TypeScript monorepo? A Rust compiler?" If not, rethink.
- **No gaming.** Do not add conditional logic that checks repository names, file counts, or other markers of the evaluation target. Do not inflate scores by loosening thresholds that hide real problems.
- **Meaningful changes only.** Every code change must have a clear algorithmic rationale. If a change doesn't improve the metric it targets, revert it.
- **Incremental progress.** Fix one issue per iteration. Verify it before moving on. Stacking multiple untested changes makes regressions hard to diagnose.
- **Test preservation.** Existing unit tests must continue to pass after each change. Add new tests when introducing new scoring or filtering logic.

## When to Stop

- All acceptance criteria pass, OR
- The user explicitly asks you to stop.
- If context is compacted mid-session, **re-read this skill file immediately** before resuming work to ensure the workflow is followed correctly.

Do **not** stop after a fixed number of iterations. Keep iterating as long as there are failing criteria and plausible code improvements to try.

## Output

After the final iteration, provide:

1. A summary of each acceptance criterion with pass/fail status.
2. A list of code changes made, with one-line rationale per change.
3. The final capability scores (if the plan includes a scoring table).
4. Any remaining issues that could not be resolved within the loop.
