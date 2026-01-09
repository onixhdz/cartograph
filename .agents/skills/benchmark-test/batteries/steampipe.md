# Steampipe Query Battery — Grounded Expected Symbols

All symbols verified against turbot/steampipe source on GitHub (2026-04-05).

## Investigation 1: Query execution (7 symbols)

Query keyword: `"execute query SQL statement result session"`
Query intent: `"how does steampipe execute a SQL query"`

Expected symbols:
- `ExecuteQuery` — pkg/db/db_common/execute.go — entry point, streams results via ResultStreamer
- `Execute` — pkg/db/db_client/db_client_execute.go — acquires session, calls ExecuteInSession
- `ExecuteInSession` — pkg/db/db_client/db_client_execute.go — core async executor, streams rows
- `ExecuteSyncInSession` — pkg/db/db_client/db_client_execute.go — sync wrapper
- `executeQuery` — pkg/query/queryexecute/execute.go — private, handles snapshots/exports/display
- `ExecuteSqlInTransaction` — pkg/db/db_local/execute.go — raw SQL in pgx transaction
- `DatabaseSession` — pkg/db/db_common/db_session.go — wraps connection, stores PID/search path

## Investigation 2: Plugin management (8 symbols)

Query keyword: `"plugin install uninstall update list grpc"`
Query intent: `"how are plugins installed and managed"`

Expected symbols:
- `pluginInstallCmd` — cmd/plugin.go — Cobra command for `steampipe plugin install`
- `runPluginInstallCmd` — cmd/plugin.go — handler executing install workflow
- `Install` — pkg/plugin/actions.go — downloads/installs plugin via OCI installer
- `pluginUpdateCmd` — cmd/plugin.go — Cobra command for `steampipe plugin update`
- `runPluginUpdateCmd` — cmd/plugin.go — handler executing update workflow
- `pluginUninstallCmd` — cmd/plugin.go — Cobra command for `steampipe plugin uninstall`
- `runPluginUninstallCmd` — cmd/plugin.go — handler executing uninstall workflow
- `PluginManager` — pkg/pluginmanager_service/plugin_manager.go — gRPC manager for running plugins

## Investigation 3: Database lifecycle (9 symbols)

Query keyword: `"database start stop service postgres local"`
Query intent: `"how does the embedded database start and stop"`

Expected symbols:
- `StartServices` — pkg/db/db_local/start_services.go — main entry, starts embedded postgres
- `startDB` — pkg/db/db_local/start_services.go — spawns actual postgres process
- `StopServices` — pkg/db/db_local/stop_services.go — SIGTERM→SIGINT→SIGQUIT shutdown
- `ShutdownService` — pkg/db/db_local/stop_services.go — conditional graceful shutdown
- `GetLocalClient` — pkg/db/db_local/local_db_client.go — public API for db client
- `newLocalClient` — pkg/db/db_local/local_db_client.go — internal connection setup
- `EnsureDBInstalled` — pkg/db/db_local/install.go — downloads/installs postgres + FDW
- `serviceStartCmd` — cmd/service.go — Cobra command for `steampipe service start`
- `serviceStopCmd` — cmd/service.go — Cobra command for `steampipe service stop`

## Investigation 4: Connection configuration (8 symbols)

Query keyword: `"connection config schema refresh state plugin"`
Query intent: `"how are plugin connections configured and refreshed"`

Expected symbols:
- `CreateConnectionPlugins` — pkg/steampipeconfig/connection_plugin.go — instantiates plugins, fetches schemas
- `ConnectionState` — pkg/steampipeconfig/connection_state.go — struct with name/plugin/state/schema
- `refreshConnections` — pkg/connection/refresh_connections_state.go — core orchestration
- `ConnectionSchemaMap` — pkg/steampipeconfig/connection_schemas.go — type alias map[string][]string
- `GetSchemaFromDB` — pkg/db/db_client/db_client.go — retrieves schemas for all connections
- `loadConfig` — pkg/steampipeconfig/load_config.go — parses HCL config files
- `initializeConnectionStateTable` — pkg/db/db_local/internal.go — sets up state table
- `ConnectionPlugin` — pkg/steampipeconfig/connection_plugin.go — struct for plugin+connections

## Investigation 5: Interactive console (8 symbols)

Query keyword: `"interactive prompt console input autocomplete metaquery"`
Query intent: `"how does the interactive query console work"`

Expected symbols:
- `InteractivePrompt` — pkg/interactive/interactive_client.go — main prompt loop method
- `RunInteractivePrompt` — pkg/interactive/run.go — entry point, creates client + goroutine
- `runInteractivePrompt` — pkg/interactive/interactive_client.go — configures go-prompt executor
- `InteractiveClient` — pkg/interactive/interactive_client.go — struct wrapping client+prompt
- `queryCompleter` — pkg/interactive/interactive_client.go — autocomplete suggestions
- `Handle` — pkg/interactive/metaquery/handlers.go — routes metaquery commands
- `Complete` — pkg/interactive/metaquery/completers.go — metaquery autocomplete
- `runQueryCmd` — cmd/query.go — Cobra handler, detects interactive vs batch mode
