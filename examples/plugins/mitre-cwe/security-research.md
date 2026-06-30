# CWE Security Research Workflow

Use the MITRE CWE plugin to ground code-backed security research in weakness classes, mitigations, detection methods, consequences, and examples.

CWE is an investigation aid, not proof of a vulnerability. Report a CWE only after returning to repository code and confirming evidence.

## When To Use CWE

Use CWE first when researching:

- Root cause and weakness classification.
- Mitigations and detection methods.
- Concrete vulnerable-code examples.
- Observed examples and CVE references.
- Related CAPEC attacker-behavior pivots.

Use CAPEC after CWE when attacker prerequisites, attack sequencing, or adversary behavior would improve the review.

## MCP Query Examples

Search CWE through Cartograph MCP plugin query:

```json
{
  "repo": "mitre-cwe",
  "plugin": true,
  "query": "SSRF",
  "limit": 5
}
```

Other useful queries:

```json
{"repo":"mitre-cwe","plugin":true,"query":"path traversal","limit":5}
{"repo":"mitre-cwe","plugin":true,"query":"SQL injection","limit":5}
{"repo":"mitre-cwe","plugin":true,"query":"authorization missing ownership tenant","limit":5}
```

## CLI Fallback Examples

```bash
cartograph query -p mitre-cwe "SSRF"
cartograph query -p mitre-cwe "path traversal"
cartograph query -p mitre-cwe "CWE-79"
```

Read exact CWE properties with Cypher:

```bash
cartograph cypher -p mitre-cwe 'MATCH (w:CWEWeakness {cwe_id:"CWE-918"}) RETURN w.name, w.description, w.mitigations, w.detection_methods, w.examples, w.observed_examples'
```

Traverse direct relationships:

```bash
cartograph cypher -p mitre-cwe 'MATCH (w:CWEWeakness {cwe_id:"CWE-918"})-[r]->(other:CWEWeakness) RETURN type(r), r.nature, other.cwe_id, other.name LIMIT 20'
```

Find CAPEC pivots from a CWE:

```bash
cartograph cypher -p mitre-cwe 'MATCH (w:CWEWeakness {cwe_id:"CWE-918"}) RETURN w.related_capecs'
```

Then query CAPEC if attacker-behavior context is needed:

```bash
cartograph query -p mitre-capec "CAPEC related to server side request forgery"
```

## Code-First Loop

1. Use Cartograph repository tools to find concrete code paths:
   - `cartograph query`
   - `cartograph search`
   - `cartograph context`
   - `cartograph impact`
   - `cartograph cat`
   - `cartograph cypher`
2. Identify a candidate source, sink, missing guard, or weak control.
3. Query CWE for matching weakness classes.
4. Use CWE mitigations, detection methods, consequences, examples, and mapping guidance as review prompts.
5. Return to code and verify:
   - attacker-controlled source or trust boundary
   - dangerous sink or protected resource
   - reachable path or concrete missing guard
   - missing, weak, or bypassable control
   - practical impact and preconditions
6. Optionally query CAPEC for attack prerequisites and behavior.
7. Report only code-backed findings.

## Evidence Rules

Do not report a weakness just because a CWE query matched.

A reported finding must answer:

1. What can the attacker control?
2. Where does that input enter the system?
3. What dangerous operation or protected resource does it reach?
4. Which code path connects the two, or which entry point is missing a guard?
5. What control should stop it?
6. Is that control missing, weak, or bypassable on this path?
7. What practical impact follows?
8. What preconditions are required?
9. What sibling or alternate paths were checked?
10. What exact file and line evidence supports the conclusion?

If any answer is missing, keep the item as a candidate or deferred issue rather than a final finding.

## CWE vs CAPEC

- CWE: weakness/root cause, mitigations, detection methods, vulnerability mapping, examples.
- CAPEC: attack patterns, adversary behavior, attack sequencing, prerequisites.

Recommended workflow: use CWE first for vulnerability research, then use CAPEC only as an optional attacker-behavior expansion.
