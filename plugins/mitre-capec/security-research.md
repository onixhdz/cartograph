# CAPEC Security Research Workflow

Use this reference for security research, vulnerability triage, and attack-path
exploration in any indexed codebase.

The reliable pattern is a two-graph loop:

1. Use Cartograph on the target repo to find the real code paths.
2. Use the `mitre-capec` plugin dataset to widen or refine the attack model.
3. Return to the repo graph to confirm or reject the idea in code.

The goal is not only to find one plausible issue. The goal is to cover the
relevant attack surface by navigating the graph until the important sibling
flows, adjacent trust boundaries, and alternate entry paths have been checked.

Do not treat CAPEC as primary evidence. CAPEC is the threat taxonomy; the code
graph is the proof.

## What Works Reliably

- Use `cartograph query` and `cartograph context` on the target repository.
- Use `cartograph cypher -r mitre-capec` on the CAPEC dataset.
- Do not use `cartograph query -r mitre-capec`; plugin datasets do not support
  `query`.
- Prefer simple CAPEC Cypher forms: exact property matches, `CONTAINS` on
  names, direct relationship traversals, and `MITIGATES` lookups.
- For more concrete CAPEC Cypher examples, use the `query-examples` resource.

## Setup

Start the background service for multi-query sessions:

```sh
cartograph serve start
```

Index the target repo if needed:

```sh
cartograph status
cartograph analyze .
```

Refresh the CAPEC dataset:

```sh
cartograph plugin list
cartograph plugin ingest mitre-capec
cartograph schema -r mitre-capec
```

## Recommended Workflow

### 1. Start from code, not from taxonomy

Begin with a suspicious sink, trust boundary, file path, parser, auth check,
deserialization path, or a rough security theme.

Examples:

```sh
cartograph query "auth token session middleware" -l 8
cartograph query "path join normalize file target" -l 8
cartograph query "exec subprocess shell environment" -l 8
```

Adapt the search terms to the target system. Good starting themes include:

- auth and authorization boundaries
- file paths and storage access
- subprocess or plugin execution
- deserialization and parser entry points
- template or interpreter boundaries
- network fetch and remote content handling
- secret, token, or environment propagation

Then drill into the most relevant symbols:

```sh
cartograph context <guard-or-sink> --depth 2 --content
cartograph context <boundary-function> --depth 2 --content
cartograph context <shared-execution-path> --depth 3 --content
```

Do not stop at the first matching symbol. Use the graph to enumerate nearby
coverage:

- sibling commands that reach the same shared execution layer
- alternate callers of the same sink or guard
- neighboring trust-boundary functions in the same process
- related flows surfaced by `query`, `context`, and `impact`

The question is: have you covered the full reachable family of paths for this
security theme, or only the first one you noticed?

### 2. Convert the code behavior into a CAPEC hypothesis

Ask: what kind of attack does this code shape resemble?

Examples:

- path manipulation -> path traversal
- untrusted process execution -> malicious component or command execution
- inherited secrets or env -> credential capture or hostile component behavior
- missing validation around interpreters -> injection families
- unsafe remote content retrieval -> malicious download or tampered content
- weak authorization checks -> access control abuse patterns

### 3. Pivot into CAPEC with Cypher

Search by likely pattern name when you have a theme:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Traversal' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
```

Inspect a candidate pattern directly when you know the CAPEC ID:

```sh
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN p.capec_id, p.name, p.description, p.related_cwes, p.domains, p.severity" -r mitre-capec
```

Use a known CWE if you already have one, but verify the property shape first.
On this dataset, exact checks and direct reads are more reliable than more
complex string expressions.

### 4. Expand through CAPEC relationships

Once a pattern looks plausible, widen the investigation:

```sh
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-126'})-[:CHILD_OF]->(parent:CAPECPattern) RETURN p.capec_id, p.name, parent.capec_id, parent.name" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-126'})-[:CAN_PRECEDE]->(next:CAPECPattern) RETURN p.capec_id, p.name, next.capec_id, next.name" -r mitre-capec
cartograph cypher "MATCH (peer:CAPECPattern)-[:PEER_OF]->(p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN peer.capec_id, peer.name, p.capec_id, p.name" -r mitre-capec
```

Use these pivots to ask better repo questions, not to declare a bug.

### 5. Pull mitigations and verify the control in code

```sh
cartograph cypher "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN p.capec_id, p.name, m.id, m.name LIMIT 10" -r mitre-capec
```

Treat mitigations as concrete review prompts:

- What validation should exist here?
- What isolation boundary should exist here?
- What authorization or integrity gate should exist here?
- Is the control mandatory, optional, or bypassable?

Then return to the repo graph and source:

```sh
cartograph context <shared-guard> --depth 2 --content
cartograph context <shared-sink> --depth 2 --content
cartograph cat <file> -l <start-end>
```

At this stage, explicitly check coverage before concluding anything:

- Which callers reach this sink?
- Which sibling commands or routes bypass this guard?
- Which other process steps share the same dangerous primitive?
- Which alternate file paths or symbols represent the same trust boundary?

Useful follow-up patterns:

```sh
cartograph impact <shared-guard> --direction upstream -d 3
cartograph impact <shared-sink> --direction upstream -d 3
cartograph cypher "MATCH (caller)-[:CALLS]->(f:Function {name: '<shared-guard>'}) RETURN caller.name, caller.filePath, caller.startLine ORDER BY caller.filePath"
cartograph cypher "MATCH (p:Process)-[:STEP_IN_PROCESS]->(f:Function {name: '<shared-sink>'}) RETURN p.name, p.importance ORDER BY p.importance DESC"
```

If coverage shows multiple sibling paths, inspect all materially different
ones before reporting a conclusion.

### 6. Report only code-backed attack paths

A useful finding should name:

1. the attack vector
2. the enabling code path
3. the weak or missing control
4. the practical impact
5. any mitigation already present

The finding should also state what coverage was checked:

6. which sibling paths were reviewed
7. which alternate callers or entry points were ruled in or out

## Minimal Research Loop

For most audits, this is the default loop:

```sh
cartograph serve start
cartograph analyze .
cartograph plugin ingest mitre-capec

cartograph query "<security theme or suspicious code path>" -l 8
cartograph context <symbol> --depth 2 --content

cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS '<theme>' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
cartograph cypher "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: '<CAPEC-ID>'}) RETURN m.id, m.name LIMIT 10" -r mitre-capec

cartograph context <next-symbol> --depth 2 --content
cartograph impact <shared-sink-or-guard> --direction upstream -d 3
cartograph cat <file> -l <start-end>
```

## Practical Rules

- Prefer code context first, CAPEC second.
- Use CAPEC to widen or refine an investigation, not to replace code evidence.
- Expand with `CHILD_OF`, `PEER_OF`, and `CAN_PRECEDE` before concluding scope.
- Read `MITIGATES` edges as review prompts for missing controls.
- Keep the loop tight: `query` -> `context` -> CAPEC pivot -> confirm in code.
- Use the graph to prove coverage: inspect sibling flows, alternate callers,
  and adjacent trust boundaries before deciding a control is present or absent.
- Prefer shared sinks and shared guards over leaf functions when deciding where
  coverage is complete.
- If CAPEC Cypher gets fancy and stops working, simplify the query instead of
  fighting the syntax.
