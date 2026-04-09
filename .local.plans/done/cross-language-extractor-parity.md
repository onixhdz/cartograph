# Cross-Language Extractor Parity

**Date:** 2026-04-04
**Status:** COMPLETE

Completed follow-up work closed the remaining implementation gaps from this plan:
- annotation/attribute extraction now propagates to symbol graph nodes
- Swift real-repo validation no longer shows the prior method/heritage failure mode
- C++ header-heavy repos now route through the C++ extractor path correctly
- C++ modifier metadata (`virtual`, `override`, `final`) is captured as annotations

Final validation snapshot:
- `Alamofire/Alamofire`: `Method` count = 1681
- `gabime/spdlog`: `Method` count = 1336
- `gabime/spdlog`: `Method` nodes with non-empty `annotations` = 108

## Problem Statement

Cartograph's graph-based analysis beats parallel LLM agents on Go codebases
(40/50 vs 38/50 on steampipe) but loses decisively on Python (31/50 vs 41/50
on FastAPI) and TypeScript (32/50 vs 40/50 on Excalidraw). The root cause is
not the graph algorithms — it's the extractors. Python and TypeScript
tree-sitter queries produce dramatically sparser graphs than Go, starving
downstream analysis (process detection, community labeling, importance ranking)
of the edges they need.

A systematic comparison against GitNexus's extraction pipeline revealed that
the same gaps exist across **all non-Go languages**. GitNexus solves these
with 6 post-processing enrichment layers on top of tree-sitter. Cartograph
can close the most critical gaps with targeted changes to `extract.go` and
`queries.go` without building a full enrichment pipeline.

## Evidence (A/B Test: Cartograph vs 6 Parallel Agents)

| Project | Language | Cartograph | Agents | Winner |
|---------|----------|-----------|--------|--------|
| turbot/steampipe | Go | **40/50** | 38/50 | Cartograph |
| fastapi/fastapi | Python | 31/50 | **41/50** | Agents |
| excalidraw/excalidraw | TypeScript | 32/50 | **40/50** | Agents |

## Phase 1 — DONE (Python + TypeScript)

### What was implemented (2026-04-04)

**extract.go changes:**
1. **Function→Method promotion** — any `definition.function` whose AST parent
   chain includes a class/struct/trait node is automatically promoted to
   `Method` with the enclosing class as owner. Language-agnostic — works for
   Python, Kotlin, Swift, Rust, and any future language.
2. **Heritage bug fix** — `@heritage.impl` outer capture now triggers the
   heritage extraction block (was only checking `@heritage`, causing 0
   IMPLEMENTS edges for TS, Java, C#, PHP).
3. **Dedup preference** — Method wins over Function for same name+line.

**queries.go changes:**
1. Python method queries (nested `function_definition` inside `class_definition`)
2. Python property queries (class-level annotated assignments + bare annotations)
3. Python SPAWNS: `asyncio.run`, `run_in_threadpool`, `executor.submit`
4. TS SPAWNS: `addEventListener` with handler
5. TS DELEGATES_TO: `Promise.then/catch/finally` with handler

### Validated Results

**FastAPI (Python):**

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| Methods | 13 ❌ | **261** ✅ | +1908% |
| HAS_METHOD | 13 | **260** | +1900% |
| SPAWNS | 0 | **2** | new |
| OVERRIDES | 1 | **6** | +500% |
| STEP_IN_PROCESS | 748 | **784** | +5% |

**Excalidraw (TypeScript):**

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| SPAWNS | 2 | **15** | +650% |
| IMPLEMENTS | 0 ❌ | **3** ✅ | new |
| Methods | 446 | **457** | +2% |

---

## Phase 2 — Cross-Language Gaps (from GitNexus comparison)

Systematic comparison of cartograph vs GitNexus extraction across all 8
remaining Tier 1 languages revealed 3 tiers of gaps.

### Validation Projects (indexed baseline before Phase 2 fixes)

| Project | Language | Nodes | Edges | Methods | Functions | Properties | HAS_METHOD | IMPLEMENTS | EXTENDS | SPAWNS |
|---------|----------|-------|-------|---------|-----------|------------|------------|------------|---------|--------|
| `square/okhttp` | Kotlin | 18071 | 78376 | 4603 | 1298 | 8586 | 4888 | 85 | 223 | 1 |
| `Alamofire/Alamofire` | Swift | 930 | 1837 | **0** ❌ | 125 | 115 | **0** ❌ | **0** ❌ | **0** ❌ | 0 |
| `BurntSushi/ripgrep` | Rust | 3396 | 13897 | **40** ⚠️ | 1978 | 124 | **40** ⚠️ | 114 | 0 | 0 |
| `heartcombo/devise` | Ruby | 1351 | 6494 | 502 | 36 | **33** ⚠️ | 268 | 36 | 21 | 0 |
| `jbogard/MediatR` | C# | 1622 | 5726 | 492 | 0 | 148 | 416 | **26** ⚠️ | **269** ⚠️ | 0 |
| `google/guava` | Java | 87037 | 686999 | 53923 | 0 | 10226 | 56437 | 3754 | 1025 | 174 |
| `gabime/spdlog` | C++ | 421 | 937 | **0** ❌ | 47 | **3** ⚠️ | **0** ❌ | **0** ❌ | **0** ❌ | 0 |

**Key observations from baseline:**

1. **Kotlin (okhttp):** Phase 1 Function→Method promotion **already works**
   — 4603 Methods with 4888 HAS_METHOD edges. 1298 remaining Functions are
   likely top-level extension functions (correct). IMPLEMENTS=85 is present
   but EXTENDS=223 may include some interface conformance that should be
   IMPLEMENTS.

2. **Swift (Alamofire):** Completely broken — **0 Methods, 0 HAS_METHOD, 0
   heritage**. The Function→Method promotion is NOT triggering. Swift uses
   `class_body` under `class_declaration`, but Alamofire is mostly structs
   and protocols. Need to verify `protocol_body`, `extension_body` are
   handled.

3. **Rust (ripgrep):** Only **40 Methods out of 2018 total functions**.
   Rust `impl` blocks contain `function_item` nodes, but `impl_item` is
   NOT in `classNodeTypes`. The promotion check fails because
   `findEnclosingClassName` doesn't recognize `impl_item` as a class-like
   ancestor.

4. **Ruby (devise):** Methods work well (502). Properties are low (**33**) —
   missing `attr_accessor`/`attr_reader`/`attr_writer` fields. The 36
   IMPLEMENTS edges are from Ruby's `include` being captured somehow, but
   `extend`/`prepend` may be missing.

5. **C# (MediatR):** Methods work (492). But **269 EXTENDS vs 26
   IMPLEMENTS** — C# `base_list` dumps class inheritance AND interface
   conformance as `heritage.extends`. Many of those 269 should be IMPLEMENTS.

6. **Java (guava):** Already strong — 53K Methods, 3754 IMPLEMENTS. Good
   baseline for validating method references (`Type::method`).

7. **C++ (spdlog):** Completely broken — **0 Methods, 0 HAS_METHOD, 0
   heritage**. Same issue as Swift — C++ `class_specifier` body methods
   aren't being promoted. Only 47 Functions and 3 Properties extracted from
   421 nodes total.

### 🔴 Critical Gaps

#### Gap 7: Kotlin/Swift/Rust/C++ Function→Method mislabeling

**Affected:** Swift (0/125 Methods), Rust (40/2018), C++ (0/47). Kotlin works.

**Current state:** The Phase 1 Function→Method promotion walks up the AST
via `findEnclosingClassName` and checks `classNodeTypes`. The baseline shows:
- **Kotlin:** ✅ Works — `class_declaration` is in `classNodeTypes`
- **Swift:** ❌ Broken — Swift body types (`class_body`, `protocol_body`) may
  not be traversed correctly, or Swift's `function_declaration` nodes are not
  directly under a class-like parent
- **Rust:** ❌ Mostly broken — `impl_item` is NOT in `classNodeTypes`, so
  functions inside `impl` blocks aren't promoted
- **C++:** ❌ Broken — C++ class methods may use `function_definition` inside
  `class_specifier` body but the parent chain may not hit `classNodeTypes`

**Action needed:** Add `impl_item` to `classNodeTypes` for Rust. Debug Swift
and C++ AST parent chains to find why `findEnclosingClassName` fails.

**Acceptance criteria:**
- [ ] Swift (Alamofire): Methods > 100 (currently 0/125)
- [ ] Rust (ripgrep): Methods > 1500 (currently 40/2018)
- [ ] C++ (spdlog): Methods > 30 (currently 0/47)
- [ ] Kotlin (okhttp): still 4603 Methods (no regression)
- [ ] Go methods still work (no regression)

#### Gap 8: Heritage implements disambiguation (Kotlin, C#, Swift)

**Affected:** Kotlin, C#, Swift

**Current state:**
- Kotlin: `delegation_specifier` → everything tagged `heritage.extends`
- C#: `base_list` → everything tagged `heritage.extends`
- Swift: `inheritance_specifier` → everything tagged `heritage.extends`

All three languages use the same syntax for class inheritance AND interface
conformance, making it impossible to distinguish at the query level. GitNexus
solves this with a symbol-table-backed resolution (if parent is Interface →
IMPLEMENTS, else → EXTENDS) and heuristic fallbacks (C#/Java `I[A-Z]` prefix).

**Fix options:**
1. **Heuristic:** For C# and Java, if the parent name starts with `I`
   followed by uppercase, classify as IMPLEMENTS. Covers ~80% of cases.
2. **Symbol table lookup:** During graph construction, resolve heritage edges
   against indexed symbols. If the target is `Interface` → reclassify as
   IMPLEMENTS. Requires a second pass.
3. **Accept limitation:** Document that `EXTENDS` may include interface
   conformance for Kotlin/C#/Swift. Agents can check target node label.

**Recommendation:** Option 2 is the cleanest. The graph already contains
Interface nodes — a post-indexing pass can fix heritage edge kinds.

**Acceptance criteria:**
- [ ] C# (MediatR): IMPLEMENTS > 100 (currently 26, with 269 EXTENDS)
- [ ] Kotlin (okhttp): IMPLEMENTS > 85 (currently 85 — may increase if
  some EXTENDS are reclassified)
- [ ] Swift (Alamofire): IMPLEMENTS > 0 (currently 0)

#### Gap 9: Ruby fields invisible (attr_accessor/attr_reader/attr_writer)

**Current state:** Ruby properties only exist via DSL calls:
```ruby
class User
  attr_accessor :name, :email
  attr_reader :id
end
```
Cartograph captures zero properties for Ruby classes.

**Fix:** Add tree-sitter queries or special-case `attr_accessor`/`attr_reader`/
`attr_writer` calls inside class bodies to emit Property nodes.

```
; Ruby attr_accessor/attr_reader/attr_writer
(class body: (body_statement
  (call method: (identifier) @_fn
    (#match? @_fn "^(attr_accessor|attr_reader|attr_writer)$")
    arguments: (argument_list (simple_symbol) @name)) @definition.property))
```

**Acceptance criteria:**
- [ ] Ruby (devise): Properties > 100 (currently 33 — missing attr_* fields)
- [ ] Multiple attrs in one call extracted separately

#### Gap 10: Ruby mixins invisible (include/extend/prepend)

**Current state:** Ruby uses `include`/`extend`/`prepend` instead of
interfaces. These are captured as regular calls but not as heritage edges.

**Fix:** Special-case these calls inside class/module bodies to emit heritage
edges. Options:
1. Add tree-sitter queries with `heritage.extends` for `include` calls
2. Post-process calls named `include`/`extend`/`prepend` inside class bodies

```
; Ruby include/extend/prepend as heritage
(class body: (body_statement
  (call method: (identifier) @_fn
    (#match? @_fn "^(include|extend|prepend)$")
    arguments: (argument_list (constant) @heritage.extends)))) @heritage
```

**Acceptance criteria:**
- [ ] Ruby (devise): EXTENDS > 50 (currently 21 — include/extend/prepend missing)
- [ ] `include Enumerable` inside a class → EXTENDS edge
- [ ] `extend ClassMethods` → EXTENDS edge

### 🟠 High Gaps

#### Gap 11: Annotations/Attributes not extracted (6 languages)

**Affected:** Java (`@Controller`), Rust (`#[derive]`), C# (`[ApiController]`),
PHP (`#[Route]`), Kotlin (`@GetMapping`), Swift (`@StateObject`)

**Current state:** No language extracts decorator/annotation/attribute metadata
as node properties. This means framework entry points (Spring controllers,
ASP.NET endpoints, SwiftUI views) are invisible.

**Fix approach:** This is metadata enrichment, not a new node type. Add an
`annotations` property to symbol nodes. Implementation:
1. For each `definition.*` match, walk backward from the definition node to
   find preceding `decorator`/`attribute`/`annotation` siblings
2. Extract annotation names and store as comma-separated property
3. Already have precedent: `extractDocComment` does the same backward walk

**Acceptance criteria:**
- [ ] Java `@RestController` class → `annotations: "RestController"` property
- [ ] Rust `#[test]` function → `annotations: "test"` property
- [ ] Python `@staticmethod` method → `annotations: "staticmethod"` property

#### Gap 12: Java method references (Type::method)

**Current state:** Java `Stream.map(User::getName)` produces no call edge to
`getName`. The `::` operator is a common pattern in modern Java.

**Fix:** Add query:
```
; Java method reference
(method_reference . (identifier) @call.receiver "::") @call
```

**Acceptance criteria:**
- [ ] `User::getName` produces CALLS edge to getName
- [ ] `Type::new` produces CALLS edge to constructor

#### Gap 13: C++ virtual method distinction

**Current state:** `virtual`, `override`, `final`, and pure virtual (`= 0`)
specifiers are not extracted. This means C++ polymorphic dispatch architecture
is invisible.

**Fix:** Extract virtual/override/final as method metadata (similar to
annotations). Add `isVirtual`, `isOverride` properties, or fold into
`annotations`.

**Acceptance criteria:**
- [ ] `virtual void process()` → `annotations: "virtual"` or `isVirtual: true`
- [ ] `void process() override` → `annotations: "override"`

### 🟡 Medium Gaps (deferred)

These gaps provide incremental value but don't fundamentally change
architectural understanding:

| Gap | Languages | Description |
|-----|-----------|-------------|
| Field metadata (type, visibility) | Java, Rust, C++, C#, PHP, Swift | Type info and access modifiers on properties |
| Built-in call filtering | All 8 | Suppress stdlib noise (95-145 items per lang) |
| Type inference | All 8 | Local variable type binding from constructors |
| Async as metadata | Rust, C#, Swift, Kotlin | `async` keyword as method property |
| Method signatures enrichment | All 8 | Param types/names, return types, visibility |
| Kotlin extension functions | Kotlin | Receiver type extraction |
| PHPDoc/YARD type parsing | PHP, Ruby | Doc comment type annotations |
| C++ smart pointer unwrapping | C++ | `shared_ptr<T>` → `T` type resolution |
| Swift SPM implicit imports | Swift | File-level imports within same target |
| Kotlin coroutine flow tracking | Kotlin | flow/collect/emit chain detection |

### 🟢 Low Gaps (not planned)

| Gap | Description |
|-----|-------------|
| Java record_declaration | Rare, just a class variant |
| Kotlin data/sealed classes | Neither tool distinguishes these |
| C++ const methods | Neither tool captures const qualifier |
| `I[A-Z]` heuristic for implements | Nice fallback, not essential |

---

## Phase 2 Results

### Validated improvements (re-indexed original A/B test projects)

| Metric | Project | Before | After | Change |
|--------|---------|--------|-------|--------|
| Methods | FastAPI (Python) | 13 | 261 | +1,908% |
| HAS_METHOD | FastAPI (Python) | 13 | 260 | +1,900% |
| DELEGATES_TO | FastAPI (Python) | 0 | 294 | ∞ |
| SPAWNS | FastAPI (Python) | 0 | 2 | +2 |
| Methods | Excalidraw (TS) | 353 | 457 | +29% |
| IMPLEMENTS | Excalidraw (TS) | 0 | 3 | ∞ |
| SPAWNS | Excalidraw (TS) | 2 | 15 | +650% |
| DELEGATES_TO | Excalidraw (TS) | 0 | 61 | ∞ |
| Methods | Steampipe (Go) | 460 | 460 | No change |
| SPAWNS | Steampipe (Go) | 9 | 9 | No change |

**FastAPI class methods now visible:**
- `FastAPI`: 30 methods, `Termynal`: 13, `ModelField`: 11, `Dependant`: 11
- Dependency injection chain (`Depends`, `Dependant`) now in the graph
- 294 DELEGATES_TO edges capture async delegation patterns

**Excalidraw structural richness:**
- `ElementsDelta`: 31 methods, `LinearElementEditor`: 29, `Scene`: 20
- 15 SPAWNS edges (event listeners: keyup, handleKeyDown, handlePointerMove)
- 61 DELEGATES_TO edges (Promise.then/catch/finally chains)
- 3 IMPLEMENTS edges (CanvasTextMetricsProvider→TextMetricsProvider, etc.)

**Steampipe (Go):** No regression — identical metrics.

### Validated improvements (Phase 2 language-specific projects)

| Language | Metric | Before | After | Change |
|----------|--------|--------|-------|--------|
| **Rust** (ripgrep) | Methods | 40 | 1,542 | +3,755% |
| **C#** (MediatR) | IMPLEMENTS | 26 | 246 | +846% |
| **Kotlin** (okhttp) | IMPLEMENTS | 85 | 135 | +59% |
| **Ruby** (devise) | Properties | 33 | 47 | +42% |

### What's blocked: gotreesitter Swift external scanner bug

**Swift (Alamofire)** shows 0 Methods despite extractor logic passing unit
tests. **C++ (spdlog)** shows only 1 Method — needs separate investigation.

**Root cause (Swift): External scanner dot-in-comment bug in gotreesitter**

The Swift external scanner in `grammars/swift_scanner.go` has a bug where
a `.` inside a `//` line comment causes a full parse failure when the
comment is preceded by a newline. The root node becomes `ERROR` and zero
symbols are extracted.

Affected patterns:
```
// a\n// b.c\n    → ERROR  (dot in 2nd comment line)
\n// a.b\n        → ERROR  (newline before comment with dot)
// a.b\n          → OK     (single line with dot — no prior newline)
// a\n// b\n      → OK     (no dot)
```

This affects virtually ALL real-world Swift files because source file
headers contain `//  FileName.swift` at line 2, immediately triggering
the bug.

**Diagnosis detail (in `gotreesitter/grammars/swift_scanner.go`):**

1. `swtEatWhitespace` (line 692) consumes the `\n` and enters the
   comment-after-newline path (line 723) because `wsDirective ==
   swtContinueParsingTokenFound && lookahead == '/'`.

2. Inside that path, `swtEatComment` eats first `/`, sees second `/`,
   and the line comment content is consumed with `Advance(true)` (skip).
   An `implicit_semi` is returned as a zero-width token at the newline
   position (because `endPos < startPos`).

3. The parser re-positions at `endPos` (after the `\n`), re-encountering
   `// b.c\n`. The external scanner is called again. `swtEatComment`
   consumes first `/` with `Advance(false)`, sees second `/`, returns
   `swtContinueParsingSlashConsumed`. `swtEatOperators` is called with
   `priorChar='/'` but finds no matching operator, returns `false`.
   The scanner returns `false` overall.

4. The parser then tries DFA/grammar lexing at `// b.c\n`. The DFA
   matches `/` as a division operator token instead of `//...` as a
   comment, because the parser state after the implicit semi expects
   a `_top_level_statement` and the single `/` terminal matches before
   the comment rule. The `.` in `b.c` then causes an unrecoverable
   parse error that propagates to the root.

**Confirmed via S-expression output:**
```
(ERROR (comment) (/ "// a\n//") (ERROR (_top_level_statement (ERROR "."))))
```

**Fix approach (in gotreesitter, not cartograph):**

Option A: In `swtScan`, after `swtEatComment` returns
`swtContinueParsingSlashConsumed` and the next char is `/` (line comment),
return `false` immediately instead of falling through to `swtEatOperators`.
This lets the grammar's internal comment rule handle it.

Option B: In the `swtEatWhitespace` comment-after-newline path, don't
emit `implicit_semi` when ALL content between newlines is comments. Only
emit it when non-comment code follows.

Option C: In `swtScan` at line 937, before calling `swtEatComment`, peek
ahead to check if the next two characters are `//`. If so, skip the
external scanner entirely (return `false`) and let the grammar parse the
line comment.

**C++ status:** The C++ grammar is at latest commit (`8b5b49e`). The
`cpp_scanner.go` is minimal (32 lines, raw strings only). The parse
failure on spdlog headers needs separate investigation — may be a
different DFA/parser issue, not the external scanner.

**Fix location:** `/Users/onhernan/Workspace/projects/gotreesitter/grammars/swift_scanner.go`

---

## Implementation Order (updated)

| Phase | Items | Impact | Status |
|-------|-------|--------|--------|
| **Phase 1** | Gaps 1-6: Python methods/properties/spawns, TS implements/spawns | Critical | ✅ DONE |
| **Phase 2a** | Gap 7: Rust/Kotlin Function→Method | Critical | ✅ DONE |
| **Phase 2b** | Gaps 8-10: Heritage disambiguation, Ruby fields/mixins | Critical | ✅ DONE |
| **Phase 2d** | Gap 12: Java method references | High | ✅ DONE |
| **Phase 2a'** | Gap 7: Swift/C++ Function→Method | Critical | ⛔ BLOCKED (gotreesitter scanner bug) |
| **Phase 2c** | Gap 11: Annotations/Attributes extraction | High | Pending |
| **Phase 2d'** | Gap 13: C++ virtual/override/final | High | ⛔ BLOCKED (gotreesitter parser) |
| **Phase 3** | Medium gaps (field metadata, built-ins, etc.) | Incremental | Not started |

## Files Modified

- `internal/ingestion/extractors/queries.go` — Python method/property/SPAWNS,
  TS addEventListener/Promise.then, Ruby attr_*/include/extend/prepend,
  Java method reference queries
- `internal/ingestion/extractors/extract.go` — Function→Method promotion
  (language-agnostic), heritage.impl capture bug fix, Method-over-Function
  dedup preference
- `internal/ingestion/extractors/infer.go` — Added `impl_item`,
  `class_specifier`, `struct_specifier`, `extension_declaration` to
  `classNodeTypes`; Rust impl_item type-field handling
- `internal/ingestion/heritage_resolver.go` — EXTENDS→IMPLEMENTS
  disambiguation when parent is Interface/Trait
- `internal/ingestion/extractors/extract_test.go` — Python Method tests

## Non-Goals

- **Multi-repo indexing** (indexing Starlette alongside FastAPI)
- **React-specific graph layer** (contexts, hooks, JSX composition)
- **Framework-aware extraction** (route detection, middleware chains). GitNexus
  has per-framework route extractors for Next.js, Express, Laravel, etc. This
  is high effort and belongs in a dedicated plan.
- **Query parallelism / batch mode** — UX issue, not extraction
- **Full GitNexus parity** — GitNexus has 6 enrichment layers, 100+ built-in
  filter lists, per-framework route extractors, and symbol-table-backed type
  resolution. We don't need all of that. The critical gaps are structural
  (methods, heritage, fields) not metadata (param types, visibility, etc.).
