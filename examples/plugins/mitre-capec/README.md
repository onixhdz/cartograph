# mitre-capec example plugin

A runnable example that fetches the MITRE CAPEC STIX bundle and registers it as a
Cartograph plugin dataset named `mitre-capec` through `Client.RegisterPlugin`. It
emits `CAPECPattern`, `CAPECMitigation`, and `CAPECCategory` nodes linked by
`CHILD_OF`, `CAN_PRECEDE`, `PEER_OF`, and `MITIGATES` edges.

## Prerequisites

- Network access to the CAPEC STIX bundle
  (`https://raw.githubusercontent.com/mitre/cti/master/capec/2.1/stix-capec.json`
  by default).
- A data directory Cartograph can own. If a background `cartograph serve` owns
  the default data directory, set `CARTOGRAPH_DATA_DIR` to a separate path.

## Run

```sh
CARTOGRAPH_DATA_DIR=/tmp/cartograph-capec go run ./examples/plugins/mitre-capec
```

On success it prints the registered dataset name, version, repo hash, and node,
edge, and resource counts.

## Configuration

The program reads optional environment variables and forwards them as plugin
config:

- `CAPEC_STIX_URL` — override the CAPEC STIX bundle URL.
- `CAPEC_INCLUDE_DEPRECATED=true` — include deprecated attack patterns.

## Query the result

```sh
cartograph query -p mitre-capec "sql injection"
cartograph query -p mitre-capec "CWE-89"
```

Plugin datasets are queried with `-p`. See `query-examples.md` for Cypher
traversal examples and `security-research.md` for how this dataset is used in a
security review.
