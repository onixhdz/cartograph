# Dart Frog Query Battery — Grounded Expected Symbols

All symbols verified against dart-frog-dev/dart_frog source on GitHub (2026-05-19).

## Investigation 1: Request routing and path matching (9 symbols)

Query keyword: `"Router RouterEntry mount params match handler"`
Query intent: `"how does dart_frog route HTTP requests by method and URL path and extract route parameters"`

Expected symbols:
- `Router` — packages/dart_frog/lib/src/router.dart — dispatches requests to registered handlers by verb and path pattern
- `add` — packages/dart_frog/lib/src/router.dart — registers a handler for an HTTP verb and route string
- `mount` — packages/dart_frog/lib/src/router.dart — mounts a sub-handler under a path prefix while rewriting the effective URL
- `all` — packages/dart_frog/lib/src/router.dart — registers a handler that matches all HTTP methods for a route
- `RouterEntry` — packages/dart_frog/lib/src/router.dart — one route record with verb, compiled pattern, parameter names, and middleware
- `match` — packages/dart_frog/lib/src/router.dart — returns path parameters when a route pattern matches a request path
- `invoke` — packages/dart_frog/lib/src/router.dart — calls a route handler with context enriched by matched parameters
- `RouterParams` — packages/dart_frog/lib/src/router.dart — extension exposing captured URL parameters on shelf requests
- `routeNotFound` — packages/dart_frog/lib/src/router.dart — default 404 response used when no route matches

## Investigation 2: Middleware pipeline composition (9 symbols)

Query keyword: `"Middleware Pipeline HandlerUse provider Cascade use"`
Query intent: `"how does dart_frog compose middleware and inject typed values into request context"`

Expected symbols:
- `Handler` — packages/dart_frog/lib/src/handler.dart — typedef for the fundamental request-processing function
- `Middleware` — packages/dart_frog/lib/src/middleware.dart — typedef for functions that wrap handlers
- `HandlerUse` — packages/dart_frog/lib/src/middleware.dart — extension that adds fluent `.use` middleware composition to handlers
- `Pipeline` — packages/dart_frog/lib/src/pipeline.dart — immutable builder that sequences middleware before a terminal handler
- `addMiddleware` — packages/dart_frog/lib/src/pipeline.dart — appends middleware to a pipeline
- `addHandler` — packages/dart_frog/lib/src/pipeline.dart — terminates a pipeline and returns the composed handler
- `provider` — packages/dart_frog/lib/src/provider.dart — creates middleware that lazily injects typed values into request context
- `Cascade` — packages/dart_frog/lib/src/cascade.dart — tries handlers in sequence until one returns a non-404/non-405 response
- `requestLogger` — packages/dart_frog/lib/src/request_logger.dart — built-in middleware that logs method, path, status, and elapsed time

## Investigation 3: Development server and hot reload (9 symbols)

Query keyword: `"DevServerRunner hotReload watcher codegen snapshot reload"`
Query intent: `"how does dart_frog start the development server watch files regenerate code and hot reload"`

Expected symbols:
- `hotReload` — packages/dart_frog/lib/src/hot_reload.dart — wraps an HTTP server initializer with VM hot reload support
- `DevServerRunner` — packages/dart_frog_cli/lib/src/dev_server_runner/dev_server_runner.dart — orchestrates codegen, server process, file watching, reloads, and teardown
- `start` — packages/dart_frog_cli/lib/src/dev_server_runner/dev_server_runner.dart — generates code, spawns the Dart process, and starts file watching
- `stop` — packages/dart_frog_cli/lib/src/dev_server_runner/dev_server_runner.dart — cancels watching, kills the server process, and completes exit handling
- `reload` — packages/dart_frog_cli/lib/src/dev_server_runner/dev_server_runner.dart — triggers code generation and hot reload for a running server
- `DartFrogDevServerException` — packages/dart_frog_cli/lib/src/dev_server_runner/dev_server_runner.dart — exception for invalid dev server lifecycle operations
- `RestorableDirectoryGeneratorTarget` — packages/dart_frog_cli/lib/src/dev_server_runner/restorable_directory_generator_target.dart — generator target that snapshots generated files for rollback
- `cacheLatestSnapshot` — packages/dart_frog_cli/lib/src/dev_server_runner/restorable_directory_generator_target.dart — stores the latest generated file state in the snapshot queue
- `rollback` — packages/dart_frog_cli/lib/src/dev_server_runner/restorable_directory_generator_target.dart — restores the previous generated file snapshot after an error

## Investigation 4: CLI commands and daemon protocol (9 symbols)

Query keyword: `"DartFrogCommandRunner BuildCommand DevCommand DaemonServer protocol"`
Query intent: `"how does dart_frog register CLI commands and exchange daemon request response event messages"`

Expected symbols:
- `DartFrogCommandRunner` — packages/dart_frog_cli/lib/src/command_runner.dart — command runner that registers subcommands, handles version output, and checks updates
- `BuildCommand` — packages/dart_frog_cli/lib/src/commands/build/build.dart — CLI command that generates a production-ready server bundle
- `DevCommand` — packages/dart_frog_cli/lib/src/commands/dev/dev.dart — CLI command that starts and manages an interactive development server
- `DaemonServer` — packages/dart_frog_cli/lib/src/daemon/daemon_server.dart — persistent daemon that registers domains and routes incoming requests
- `DaemonMessage` — packages/dart_frog_cli/lib/src/daemon/protocol.dart — sealed base class for daemon wire messages parsed from JSON
- `DaemonRequest` — packages/dart_frog_cli/lib/src/daemon/protocol.dart — request message with id, domain, method, and optional params
- `DaemonResponse` — packages/dart_frog_cli/lib/src/daemon/protocol.dart — success-or-error reply keyed by request id
- `DaemonEvent` — packages/dart_frog_cli/lib/src/daemon/protocol.dart — server-to-client notification not tied to a request
- `DomainBase` — packages/dart_frog_cli/lib/src/daemon/domain/domain_base.dart — base class that maps daemon method names to handlers

## Investigation 5: Request, response, and context model (9 symbols)

Query keyword: `"Request Response RequestContext HttpMethod provide read"`
Query intent: `"how does dart_frog model HTTP requests responses and typed per-request context"`

Expected symbols:
- `Request` — packages/dart_frog/lib/src/request.dart — immutable HTTP request with named constructors and body accessors
- `copyWith` — packages/dart_frog/lib/src/request.dart — returns a request with selected fields replaced
- `Response` — packages/dart_frog/lib/src/response.dart — HTTP response with constructors for text, JSON, bytes, streams, and redirects
- `json` — packages/dart_frog/lib/src/response.dart — creates an application/json response from an encodable body
- `stream` — packages/dart_frog/lib/src/response.dart — creates a response from a raw byte stream
- `RequestContext` — packages/dart_frog/lib/src/context.dart — per-request object carrying the request and typed context store
- `provide` — packages/dart_frog/lib/src/context.dart — registers a lazy typed value factory in the request context
- `read` — packages/dart_frog/lib/src/context.dart — retrieves a typed value from the request context
- `HttpMethod` — packages/dart_frog/lib/src/http_method.dart — enum of supported HTTP methods and their string values
