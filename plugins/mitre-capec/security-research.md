# CAPEC Security Research in Cartograph

Use this reference when the task is security research, vulnerability triage, or
attack-path exploration. Start by loading the Cartograph skill/router
references, then use CAPEC as a secondary knowledge source for attack patterns
and mitigations.

## Recommended Workflow

1. Start from suspicious code, a finding, or a CWE hint.
2. Query the code graph first to identify the affected files, functions,
routes, and nearby symbols.
3. If a CWE is known, use it to pivot into CAPEC-related patterns.
4. Expand through CAPEC relationships: `CHILD_OF`, `PEER_OF`, and
`CAN_PRECEDE`.
5. Read `MITIGATES` relationships to identify defensive patterns or missing
controls.
6. Use the resulting patterns to guide the next files and sibling flows to
inspect.

## Practical Guidance

- Prefer code context first, CAPEC second.
- Use CAPEC to widen or refine an investigation, not to replace code evidence.
- When a pattern looks relevant, inspect peer and child patterns before
concluding scope.
- Treat mitigations as investigation hints: they often reveal the missing
validation, isolation, or authorization step to verify in code.
