# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Expand the embedded Go API for remote repository analysis flows.

### Changed

- Avoid hardcoded install versions in documentation.
- Update Go and `golang.org/x/*` dependencies for release security checks.

### Security

- Address release-blocking Go standard library vulnerability checks by requiring Go 1.26.5.
- Dismiss dependency-only govulncheck module alerts for packages Cartograph does not import or call.

## [0.2.0] - 2026-06-30

### Added

- Public embedded Go API through the root `github.com/onixhdz/cartograph` package.
- In-process plugin registration with `Client.RegisterPlugin`.
- MITRE CWE and CAPEC example plugins.
- Plugin dataset query backend support.
- Embedded API documentation and examples.

### Changed

- Rename the module path from `realxen` to `onixhdz`.
- Replace the out-of-process plugin runtime with in-process plugin registration.
- Update Go and GitHub Actions dependencies.

### Removed

- Remove the JSON-RPC subprocess plugin runtime, checksum verification path, plugin CLI command, and HTTP ingest routes/jobs.

## [0.1.11] - 2026-06-01

### Changed

- Isolate native embedding from default CI jobs.
- Update Go dependencies.

### Fixed

- Build the regex index in memory to avoid Windows open-file rename failures.
- Make MCP use the service client and bound bbolt open lock wait time.
- Resolve repository paths containing spaces.

## [0.1.10] - 2026-05-28

### Added

- CodeQL and govulncheck security scanning automation.
- Documented CodeQL false-positive justifications for plugin storage and security paths.

### Fixed

- Reject local filesystem paths from the `/api/embed` service endpoint.
- Skip symlinks in local and memfs walkers.
- Resolve open CodeQL and govulncheck alerts for the release.

## [0.1.9] - 2026-05-26

### Added

- Raw source search through CLI `search` and `grep` workflows.
- Service and MCP support for raw source search.
- Context relationship output.

### Changed

- Improve analyze repository candidate guidance.
- Update installer page assets and release site content.

### Fixed

- Resolver lifecycle issues affecting search and service flows.

## [0.1.8] - 2026-05-20

### Added

- Dart source support, including Dart/Frog and Dart HTTP benchmark batteries.
- Multi-project repository discovery with repository candidate selection for analyze workflows.
- Analyze preflight support in the service API.
- Repository discovery tests, remote memfs tests, and expanded CLI/service coverage for multi-project flows.

### Changed

- Rename analyze selection concepts to repository candidates.
- Improve config loader manifest handling, import resolution, and ingestion pipeline behavior for multi-project repositories.
- Update agent guidance and CLI documentation for named-project workflows.

### Fixed

- Tighten walker ignore handling and service lazy-load behavior around repository registry entries.

## [0.1.7] - 2026-05-19

### Added

- Indexed repository tree command.
- Solidity source extraction and graph context support.
- TSX source support.

### Changed

- Limit extraction to supported source languages.
- Update Cartograph skill routing for named project workflows.
- Improve Solidity graph context and search relevance.

### Fixed

- Concurrent service tree requests.
- Concurrent service query stalls.
- Solidity import resolution.

## [0.1.6] - 2026-04-24

### Fixed

- Address lint failures in service tree tests.

## [0.1.5] - 2026-04-24

### Added

- JSON-RPC plugin system with data source abstractions, plugin host, CLI, and SDK.
- Plugin test framework with mock host, HTTP mock, and binary harness.
- MITRE CAPEC plugin backed by STIX feed ingestion.
- Native tree-sitter extractor runtime.
- Native Swift and Kotlin support.
- Fallback parser infrastructure for supported languages.
- Monorepo manifest and build-file ingestion support.
- Typed plugin emit API.
- Metadata-driven plugin dataset query support.
- Task-based CI coverage on `develop`.

### Changed

- Refactor plugin configuration and CLI UX from `sources.toml` to `config.toml`.
- Add optional plugin config and auto-ingest on install.
- Speed up symbol linking and add an analyze timing flag.
- Improve Maven manifest and module handling.
- Tighten annotation extraction and C++ detection.
- Stabilize Zig build flow and parallelize release builds.

### Fixed

- Race condition in `plugintest.BinaryResult`.
- Ambiguous symbol owner linking.
- Plugin dataset persistence hardening.

## [0.1.4] - 2026-04-09

### Added

- Java benchmark coverage.

### Changed

- Improve Java and Spring query support.

## [0.1.3] - 2026-03-09

### Added

- Shell installer script and README installation documentation.
- Wiki generation and bundling support.
- Split wiki context output and `module_tree.json`.
- Scala benchmark and ingestion support.

### Changed

- Document wiki generation CLI entries.
- Work around Scala block comment parsing behavior.
- Improve Scala scoped visibility handling.
- Add early test filtering.

### Fixed

- Ignore context cancellation in MCP flows where cancellation is expected.
- Write-query blocking at the handler layer.
- Parse timeout handling.
- Staticcheck and lint failures in tests.

## [0.1.2] - 2026-02-09

### Added

- MCP server over stdin/stdout through `cartograph mcp` for AI editor integration.
- Streamable HTTP MCP endpoint on `cartograph serve` at `/mcp`.
- `cartograph skills install --upgrade` for package manager post-install hooks.
- MCP tools for query, context, impact, Cypher, cat, schema, and status operations.

### Changed

- Restructure agent skill documentation for conciseness.
- Extract annotated tag text as release notes in the release workflow.

### Fixed

- Fetch full history and annotated tag objects in release automation.

## [0.1.1] - 2026-01-09

### Added

- Index versioning and compatibility checks.
- Inline `@ref` repository selection.
- Incremental embeddings with content hashes and cleanup.

### Changed

- Make client setup conditional through the `needs-client` tag.
- Rename `source` operations to `cat` across CLI, API, documentation, and tests.
- Skip unchanged repositories during analysis.
- Update README usage documentation.

### Fixed

- End-to-end embedding test coverage.

## [0.1.0] - 2026-01-09

### Added

- Initial Cartograph CLI, internal packages, and test suite.
- Repository indexing into a local code knowledge graph.
- Search, query, context, impact, Cypher, schema, and source inspection workflows.
- Optional embedding support for semantic search.
- Benchmark and feedback agent skills.
- VS Code devcontainer and Docker setup.
- Roadmap and acknowledgments.
- CLI version output.
- Golangci-lint, CI, and release workflows.
- Zig build setup and release builds for supported platforms, including macOS Intel.

### Fixed

- Initial golangci-lint failures before the first release.
- Registry handling in Zig build setup.

[Unreleased]: https://github.com/onixhdz/cartograph/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/onixhdz/cartograph/compare/v0.1.11...v0.2.0
[0.1.11]: https://github.com/onixhdz/cartograph/compare/v0.1.10...v0.1.11
[0.1.10]: https://github.com/onixhdz/cartograph/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/onixhdz/cartograph/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/onixhdz/cartograph/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/onixhdz/cartograph/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/onixhdz/cartograph/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/onixhdz/cartograph/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/onixhdz/cartograph/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/onixhdz/cartograph/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/onixhdz/cartograph/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/onixhdz/cartograph/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/onixhdz/cartograph/tree/v0.1.0
