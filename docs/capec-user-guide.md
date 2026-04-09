# CAPEC User Guide

This guide explains how to use the `mitre-capec` plugin together with
Cartograph to identify attack vectors in Cartograph's own plugin system.

The workflow here is the one used for a real review of this repository:

1. Load the CAPEC dataset through the plugin system.
2. Use Cartograph to map the plugin-system entry points and trust boundaries.
3. Match those code paths to CAPEC-style attack classes.
4. Read only the specific source slices needed to confirm or reject each risk.

## What this workflow is good at

Use this process when you want to answer questions like:

- Which attack vectors exist in the plugin system?
- Are plugin installs integrity-checked?
- Can a malicious plugin execute during listing, probing, or ingest?
- Does the host leak environment data to plugins?
- Are path, handshake, or resource-limit controls actually enforced?

## Prerequisites

You need:

- an indexed repository
- the Cartograph background service running for fast repeated queries
- the `mitre-capec` plugin installed

Useful checks:

```sh
cartograph serve status
cartograph list
cartograph status
cartograph plugin list
```

If the repo is not indexed yet:

```sh
cartograph analyze .
```

If the service is not running yet:

```sh
cartograph serve start
```

## Step 1: Refresh the CAPEC dataset

Start by ingesting the CAPEC plugin data:

```sh
cartograph plugin ingest mitre-capec
```

That gives you the attack-pattern dataset for the session and confirms the
plugin is installed and functional.

Example output includes counts like:

- `CAPECPattern`
- `CAPECMitigation`
- `MITIGATES`
- `CAN_PRECEDE`

## Step 2: Treat CAPEC as the threat taxonomy

In this use-case, the CAPEC plugin is the reference taxonomy, while the
Cartograph repo graph is the thing being audited.

That means the analysis loop is:

1. use CAPEC to frame the kinds of attacks you are looking for
2. use Cartograph queries to find the trust boundaries in the target code
3. confirm those risks by tracing actual execution paths

For the Cartograph plugin system, the high-value CAPEC-style categories are:

- malicious plugin execution
- supply-chain compromise / tampered binary execution
- path traversal or unsafe file targeting
- credential or environment-data capture
- resource exhaustion

## Step 3: Find the plugin-system entry points

Use `query` first to find the important plugin flows and symbols:

```sh
cartograph query "plugin install checksum binary ingest" -l 12
cartograph query "plugin handshake grpc execution plugin source" -l 12
cartograph query "plugin list install remove ingest" -l 12
```

For this repo, these searches surfaced the main review targets:

- `cmd.PluginInstallCmd.Run`
- `cmd.PluginListCmd.Run`
- `cmd.PluginIngestCmd.Run`
- `runIngest`
- `resolvePluginBinary`
- `VerifyChecksum`
- `LaunchPlugin`
- `readHandshake`
- `parseHandshake`
- `probePluginInfo`
- `internal/plugin/lifecycle.go`

This is the key transition from "security idea" to "concrete code path".

## Step 4: Trace the sensitive flows

Use `context` to understand what each security-sensitive symbol does and who
calls it:

```sh
cartograph context VerifyChecksum --depth 2 --content
cartograph context LaunchPlugin --depth 3 --content
cartograph context parseHandshake --depth 2 --content
cartograph context probePluginInfo --depth 2 --content
cartograph context runIngest --depth 2 --content
```

What to look for:

- whether checksum verification is mandatory or optional
- whether plugins execute during install, list, probe, or ingest
- whether the host passes environment variables to the plugin process
- whether handshake logic validates trust or only protocol compatibility
- whether default limits exist for plugin-driven graph emission

## Step 5: Pull exact symbol locations

Once the right symbols are known, use `cypher` to get exact file and line
ranges:

```sh
cartograph cypher "MATCH (f:Function {name: 'VerifyChecksum'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
cartograph cypher "MATCH (f:Function {name: 'LaunchPlugin'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
cartograph cypher "MATCH (f:Function {name: 'readHandshake'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
cartograph cypher "MATCH (f:Function {name: 'parseHandshake'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
cartograph cypher "MATCH (f:Function {name: 'resolvePluginBinary'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
cartograph cypher "MATCH (f:Function {name: 'runIngest'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
cartograph cypher "MATCH (f:Function {name: 'probePluginInfo'}) RETURN f.name, f.filePath, f.startLine, f.endLine"
```

For methods:

```sh
cartograph cypher "MATCH (m:Method {name: 'Run'}) WHERE m.filePath = 'cmd/plugin.go' RETURN m.name, m.filePath, m.startLine, m.endLine ORDER BY m.startLine"
```

This narrows the review to the exact source slices that matter.

## Step 6: Read only the slices that prove or disprove a risk

Use `cartograph cat` with line ranges instead of reading whole files:

```sh
cartograph cat cmd/plugin.go -l 38-107
cartograph cat cmd/plugin.go -l 114-157
cartograph cat cmd/plugin.go -l 203-340
cartograph cat cmd/plugin.go -l 385-408

cartograph cat internal/plugin/security.go -l 23-80
cartograph cat internal/plugin/process.go -l 107-192
cartograph cat internal/plugin/process.go -l 234-317
cartograph cat internal/plugin/lifecycle.go -l 56-218
cartograph cat internal/plugin/lifecycle.go -l 278-313
cartograph cat internal/plugin/limits.go -l 1-160
cartograph cat internal/cloudgraph/config.go -l 1-141
cartograph cat plugin/sdk.go -l 1-120
cartograph cat plugin/sdk.go -l 163-259
```

This is the confirmation stage. At this point you should stop searching for
more symbols and decide whether each candidate risk is real.

## How to reason about the findings

Use the CAPEC framing to convert code behavior into attack vectors.

### 1. Unverified plugin execution

Questions to ask:

- Is checksum verification enforced at install time?
- Is checksum verification enforced every time a plugin is launched?
- Are there code paths that launch plugins without any integrity check?

What this review found:

- install-time verification is optional
- ingestion verifies only when config provides a checksum
- plugin listing and probing execute binaries without a checksum gate

That maps cleanly to CAPEC-style supply-chain and malicious-component risks.

### 2. Environment and secret exposure

Questions to ask:

- Does the host pass `os.Environ()` through to the plugin process?
- Can a plugin read unrelated shell secrets?
- Is the cookie or handshake a trust control, or only a protocol check?

What this review found:

- the plugin process inherits the host environment
- the magic cookie is not a trust anchor
- a malicious but protocol-conforming plugin can still read inherited env vars

That maps to credential capture and hostile component execution patterns.

### 3. Path manipulation

Questions to ask:

- Are plugin names normalized before `filepath.Join`?
- Are `bin` overrides validated?
- Can remove/install/data-dir paths be influenced with path-shaped names?

What this review found:

- plugin names and `bin` overrides are joined into paths
- config validation does not meaningfully constrain plugin binary names
- no visible normalization was found in the reviewed path helpers

That makes path traversal or arbitrary file targeting a credible attack path.

### 4. Resource exhaustion

Questions to ask:

- Are there default time, node, and edge limits?
- Can a plugin emit unlimited graph data by default?

What this review found:

- the runtime has default limits
- timeout, node, and edge caps are enforced in the lifecycle layer

That does not eliminate denial-of-service risk, but it does mean this area has
real defensive controls already in place.

## Recommended command sequence

If you want to repeat the same review quickly, use this sequence:

```sh
cartograph serve start
cartograph status
cartograph plugin list
cartograph plugin ingest mitre-capec

cartograph query "plugin install checksum binary ingest" -l 12
cartograph query "plugin handshake execution list probe" -l 12

cartograph context VerifyChecksum --depth 2 --content
cartograph context LaunchPlugin --depth 3 --content
cartograph context runIngest --depth 2 --content
cartograph context probePluginInfo --depth 2 --content

cartograph cat cmd/plugin.go -l 38-107
cartograph cat internal/plugin/process.go -l 107-192
cartograph cat internal/plugin/lifecycle.go -l 115-218
cartograph cat internal/plugin/limits.go -l 1-160
```

## Limits of this workflow

Two important caveats:

1. The CAPEC plugin is most useful here as a threat-model source. The core
   repo code still has to be validated through Cartograph's repo graph.
2. Not every CAPEC-style possibility is a real bug. Only report issues that
   are supported by an execution path, a missing guard, or a clearly reachable
   unsafe state in the code.

## Output format for a final report

A good final report for this workflow should contain:

1. the attack vector
2. the code path that enables it
3. the missing or weak control
4. the practical impact
5. whether there is an existing mitigation

Example:

```text
Attack vector: unverified plugin execution
Code path: plugin install -> runIngest -> PluginDataSource.Ingest -> LaunchPlugin
Weak control: checksum verification is optional, not mandatory
Impact: a tampered plugin binary can execute during normal plugin operations
Mitigation present: partial; checksum support exists but is not enforced
```

## Summary

For this use-case, the most effective process is:

1. ingest CAPEC data
2. query Cartograph for plugin-system entry points
3. trace checksum, launch, handshake, config, and limit paths
4. read only the exact slices that prove the risk
5. report only attack vectors backed by the code graph and source

That keeps the review fast, reproducible, and grounded in real execution paths
instead of generic plugin-security assumptions.
