# CAPEC Query Examples

Use these examples when the main workflow has already identified a security
theme or a likely CAPEC pattern family.

These examples are intentionally focused on CAPEC-side Cypher only.

Do not use `cartograph query -r mitre-capec` here. Plugin datasets do not
support `query`; use `cartograph cypher -r mitre-capec` instead.

## Basic Search Structure

Search by pattern name:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS '<theme>' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
```

Read a known CAPEC pattern directly:

```sh
cartograph cypher "MATCH (p:CAPECPattern {capec_id: '<CAPEC-ID>'}) RETURN p.capec_id, p.name, p.description, p.related_cwes, p.domains, p.severity" -r mitre-capec
```

Get mitigations for a known CAPEC pattern:

```sh
cartograph cypher "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: '<CAPEC-ID>'}) RETURN m.id, m.name LIMIT 10" -r mitre-capec
```

Expand to parent or next-step patterns:

```sh
cartograph cypher "MATCH (p:CAPECPattern {capec_id: '<CAPEC-ID>'})-[:CHILD_OF]->(parent:CAPECPattern) RETURN p.capec_id, p.name, parent.capec_id, parent.name" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern {capec_id: '<CAPEC-ID>'})-[:CAN_PRECEDE]->(next:CAPECPattern) RETURN p.capec_id, p.name, next.capec_id, next.name" -r mitre-capec
```

## Theme Examples

Path traversal:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Traversal' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN p.capec_id, p.name, p.description, p.related_cwes LIMIT 1" -r mitre-capec
cartograph cypher "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: 'CAPEC-126'}) RETURN m.id, m.name LIMIT 10" -r mitre-capec
```

Command execution:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Execution' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-549'}) RETURN p.capec_id, p.name, p.description LIMIT 1" -r mitre-capec
cartograph cypher "MATCH (m:CAPECMitigation)-[:MITIGATES]->(p:CAPECPattern {capec_id: 'CAPEC-549'}) RETURN m.id, m.name LIMIT 10" -r mitre-capec
```

Environment or configuration manipulation:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Environment' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-176'}) RETURN p.capec_id, p.name, p.description LIMIT 1" -r mitre-capec
```

Remote content trust:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Download' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern {capec_id: 'CAPEC-185'}) RETURN p.capec_id, p.name, p.description LIMIT 1" -r mitre-capec
```

Injection families:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Injection' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
```

Authentication or access-control themes:

```sh
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Authentication' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
cartograph cypher "MATCH (p:CAPECPattern) WHERE p.name CONTAINS 'Privilege' RETURN p.capec_id, p.name LIMIT 10" -r mitre-capec
```

## Notes

- Prefer simple `CONTAINS` queries on `p.name`.
- Use exact `capec_id` matches once a candidate looks relevant.
- Use `MITIGATES`, `CHILD_OF`, and `CAN_PRECEDE` to widen the investigation.
- If a more complex Cypher expression fails, simplify it instead of assuming full
  Neo4j-style behavior.
