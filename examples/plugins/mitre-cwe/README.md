# mitre-cwe example plugin

A runnable example that fetches the MITRE CWE taxonomy from the CWE REST API and
registers it as a Cartograph plugin dataset named `mitre-cwe` through
`Client.RegisterPlugin`. It emits `CWEWeakness`, `CWECategory`, and `CWEView`
nodes linked by `HAS_MEMBER` edges.

## Prerequisites

- Network access to the CWE REST API (`https://cwe-api.mitre.org/api/v1` by default).
- A data directory Cartograph can own. If a background `cartograph serve` owns
  the default data directory, set `CARTOGRAPH_DATA_DIR` to a separate path.

## Run

```sh
CARTOGRAPH_DATA_DIR=/tmp/cartograph-cwe go run ./examples/plugins/mitre-cwe
```

On success it prints the registered dataset name, version, repo hash, and node,
edge, and resource counts.

## Configuration

The program reads optional environment variables and forwards them as plugin
config:

- `CWE_API_BASE_URL` — override the CWE REST API base URL.
- `CWE_INCLUDE_DEPRECATED=true` — include deprecated weaknesses.

## Query the result

```sh
cartograph query -p mitre-cwe "sql injection"
cartograph query -p mitre-cwe "CWE-89"
```

Plugin datasets are queried with `-p`. See `security-research.md` for how this
dataset is used in a security review.
