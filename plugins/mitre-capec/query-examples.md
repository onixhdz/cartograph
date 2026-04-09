# CAPEC Query Examples

Use these examples after `cartograph query -p mitre-capec` has identified a
likely CAPEC pattern family or candidate CAPEC ID.

Start with plugin query for discovery:

```sh
cartograph query -p mitre-capec "sql injection"
cartograph query -p mitre-capec "path traversal"
cartograph query -p mitre-capec "CWE-89"
```

Then use Cypher for exact reads, relationship traversal, and mitigation pivots.

Do not use `cartograph query -r mitre-capec` here. Plugin datasets require `-p`
for plugin query.

## Basic Search Structure

Search by pattern name:

```sh
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern) WHERE p.name CONTAINS '<theme>' RETURN p.capec_id, p.name LIMIT 10"
```

Read a known CAPEC pattern directly:

```sh
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: '<CAPEC-ID>'}) RETURN p.capec_id, p.name, p.description, p.related_cwes, p.domains, p.severity"
```

Get mitigations for a known CAPEC pattern:

```sh
cartograph cypher -p mitre-capec "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: '<CAPEC-ID>'}) RETURN m.id, m.name LIMIT 10"
```

Expand to parent or next-step patterns:

```sh
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: '<CAPEC-ID>'})-[:CHILD_OF]->(parent:CAPECPattern) RETURN p.capec_id, p.name, parent.capec_id, parent.name"
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: '<CAPEC-ID>'})-[:CAN_PRECEDE]->(next:CAPECPattern) RETURN p.capec_id, p.name, next.capec_id, next.name"
```

## Theme Examples

Path traversal:

```sh
cartograph query -p mitre-capec "path traversal"
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN p.capec_id, p.name, p.description, p.related_cwes LIMIT 1"
cartograph cypher -p mitre-capec "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN m.id, m.name LIMIT 10"
```

Command execution:

```sh
cartograph query -p mitre-capec "command execution"
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: 'CAPEC-549'}) RETURN p.capec_id, p.name, p.description LIMIT 1"
cartograph cypher -p mitre-capec "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: 'CAPEC-549'}) RETURN m.id, m.name LIMIT 10"
```

Environment or configuration manipulation:

```sh
cartograph query -p mitre-capec "environment manipulation"
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: 'CAPEC-176'}) RETURN p.capec_id, p.name, p.description LIMIT 1"
```

Remote content trust:

```sh
cartograph query -p mitre-capec "malicious download"
cartograph cypher -p mitre-capec "MATCH (p:CAPECPattern {capec_id: 'CAPEC-185'}) RETURN p.capec_id, p.name, p.description LIMIT 1"
```

Injection families:

```sh
cartograph query -p mitre-capec "injection"
```

Authentication or access-control themes:

```sh
cartograph query -p mitre-capec "authentication"
cartograph query -p mitre-capec "privilege escalation"
```

## Notes

- Prefer `query -p` for discovery and simple `cypher -p` for exact follow-up.
- Use exact `capec_id` matches once a candidate looks relevant.
- Use `MITIGATES`, `CHILD_OF`, and `CAN_PRECEDE` to widen the investigation.
- If a more complex Cypher expression fails, simplify it instead of assuming full
  Neo4j-style behavior.
