# Cartograph

Cartograph is local-first code intelligence. Every change should make the tool more accurate, reliable, and useful without adding unnecessary surface area.

## Principles

- KISS: Prefer the simplest design that solves the real problem clearly.
- YAGNI: Do not build speculative capabilities, abstractions, or compatibility layers before they are needed.
- Locality of Behavior: Keep related behavior close together so code can be understood without chasing distant indirection.
- Open/Closed: Extend existing seams when appropriate, but do not force abstractions that make simple changes harder.
- Avoid new public surface area unless it is clearly needed. Extend existing concepts before adding commands, flags, endpoints, tools, fields, storage, or graph schema.
- Keep behavior consistent across all supported execution paths. Do not implement CLI-only or mode-specific behavior unless explicitly required.
- Do not expose internal implementation details as user-facing concepts just because they exist in the code.
- Keep everything local by default. Do not introduce external services, network dependencies, or cloud assumptions without an explicit requirement.
- Avoid speculative aliases, compatibility layers, convenience APIs, or migration paths unless there is shipped behavior or persisted data that requires them.
- Check existing local plans before starting substantial feature work. Put new plans or design notes in the local planning area.

## Design Discipline

- Spend design effort upfront for non-trivial changes. Simple, durable designs usually require revision, not the first idea.
- Preserve existing graph, storage, API, and UX conventions by inspecting the code before changing them.
- Always explain why when a decision is non-obvious. Rationale helps future maintainers evaluate and preserve intent.
- Solve known correctness, performance, and maintainability risks before shipping changed code.

## Safety And Bounds

- Prefer explicit, easy-to-follow control flow over clever branching or hidden behavior.
- Put bounds on work: avoid unbounded loops, queues, goroutines, filesystem walks, graph traversals, and result sets.
- Keep variable scope small. Compute values close to where they are used.
- Handle all errors intentionally with useful context. Do not ignore errors unless the reason is explicit and safe.
- Prefer modern, idiomatic Go with clear errors, minimal abstraction, and no panics outside tests.
- Use assertions or test-only checks for invariants when they make programmer errors fail early.

## Naming And Readability

- Choose names that capture the domain clearly. Avoid abbreviations unless they are already established in the codebase.
- Include units or qualifiers in names when they prevent ambiguity, such as counts, indexes, sizes, scores, durations, or byte values.
- Keep related behavior close together. Split functions only when the split improves understanding or testability.
- Comments should explain why or clarify non-obvious behavior, not restate what the code says.
- Keep code readable over clever. Remove dead code when replacing behavior.

## Quality Bar

- Correctness comes first. Prefer small, testable changes over broad rewrites.
- Test behavior, not implementation details. Add regression tests for bugs.
- Optimize only with evidence from profiling, benchmarks, or clearly identified hot paths.
- Keep repo-scale performance in mind: avoid unnecessary graph copies, unbounded work, and excessive filesystem or memory usage.
- Respect contexts, cancellation, and bounded concurrency.
- Treat persisted storage, graph schema, CLI output, MCP results, and public structs as compatibility-sensitive.

## Version Control

- Never commit, push, or create branches unless the user explicitly asks. The user controls all git operations.

## Verification

- Use the repository-supported Task workflows for full builds and tests; do not run broad raw Go build/test commands unless the user explicitly requests that unsupported path.
- Run targeted tests when they are enough to validate a small change, then run the broader supported workflow before considering substantial changes complete.
- Run linting for code changes and fix reported issues rather than leaving known failures behind.
- If verification cannot be run, report why and what remains unverified.

## Post-Change Checklist

- Confirm the change is minimal, focused, and consistent with existing conventions.
- Run the appropriate tests, build, and lint workflow for the scope of the change.
- Fix failures introduced by the change before considering the work complete.
- Report any skipped or unavailable verification clearly.