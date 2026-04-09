# mdBook Query Battery — Grounded Expected Symbols

All symbols verified against rust-lang/mdBook source on GitHub (2026-05-05).

## Investigation 1: CLI command dispatch (8 symbols)

Query keyword: `"clap command subcommand arg root dest watcher"`
Query intent: `"how does mdBook define shared CLI arguments and dispatch top-level commands"`

Expected symbols:
- `main` — src/main.rs — top-level CLI entry point that parses args and dispatches subcommands
- `create_clap_command` — src/main.rs — builds the root clap command and registers subcommands
- `get_book_dir` — src/main.rs — resolves the effective book root from CLI args
- `CommandExt` — src/cmd/command_prelude.rs — shared trait adding common command arguments like root dir and dest dir
- `arg_root_dir` — src/cmd/command_prelude.rs — adds the shared root-directory argument used by multiple commands
- `arg_dest_dir` — src/cmd/command_prelude.rs — adds the shared destination-directory override flag
- `arg_watcher` — src/cmd/command_prelude.rs — adds the watcher backend selection flag for watch/serve
- `set_dest_dir` — src/cmd/command_prelude.rs — applies `--dest-dir` CLI overrides to the loaded book config

## Investigation 2: Book loading and build pipeline (8 symbols)

Query keyword: `"SUMMARY chapter load preprocess render html tree"`
Query intent: `"how does mdBook load chapters from SUMMARY and render them into HTML pages"`

Expected symbols:
- `load_with_config` — crates/mdbook-driver/src/mdbook.rs — `MDBook` method that loads chapters from disk and resolves configured renderers and preprocessors
- `load_book` — crates/mdbook-driver/src/load.rs — loads `SUMMARY.md`, creates missing chapters, and starts building the in-memory book
- `load_book_from_disk` — crates/mdbook-driver/src/load.rs — walks parsed summary items into a `Book`
- `load_summary_item` — crates/mdbook-driver/src/load.rs — converts parsed summary nodes into book items recursively
- `load_chapter` — crates/mdbook-driver/src/load.rs — reads a chapter file and constructs its in-memory representation
- `execute_build_process` — crates/mdbook-driver/src/mdbook.rs — `MDBook` method that runs preprocessors, builds render context, and invokes a renderer
- `build_trees` — crates/mdbook-html/src/html/mod.rs — builds chapter navigation trees used by the HTML renderer
- `render_chapter` — crates/mdbook-html/src/html_handlebars/hbs_renderer.rs — renders one chapter page with navigation and template context

## Investigation 3: Preprocessor system (9 symbols)

Query keyword: `"preprocessor order command stdin json link replace renderer"`
Query intent: `"how does mdBook decide which preprocessors run and how external preprocessors communicate"`

Expected symbols:
- `PreprocessorContext` — crates/mdbook-preprocessor/src/lib.rs — serializable context passed into preprocessors
- `parse_input` — crates/mdbook-preprocessor/src/lib.rs — parses the stdin JSON protocol for external preprocessors
- `determine_preprocessors` — crates/mdbook-driver/src/mdbook.rs — resolves built-in and configured preprocessors and orders them
- `preprocess_book` — crates/mdbook-driver/src/mdbook.rs — `MDBook` method that runs compatible preprocessors in order for a renderer
- `preprocessor_should_run` — crates/mdbook-driver/src/mdbook.rs — checks config and renderer support before executing a preprocessor
- `CmdPreprocessor` — crates/mdbook-driver/src/builtin_preprocessors/cmd.rs — adapter for third-party preprocessors launched as commands
- `write_input_to_child` — crates/mdbook-driver/src/builtin_preprocessors/cmd.rs — sends serialized context and book data to an external preprocessor process
- `LinkPreprocessor` — crates/mdbook-driver/src/builtin_preprocessors/links.rs — built-in preprocessor expanding include, playground, and title helpers
- `replace_all` — crates/mdbook-driver/src/builtin_preprocessors/links.rs — recursively expands helper directives inside chapter content

## Investigation 4: Config loading and book initialization (8 symbols)

Query keyword: `"book.toml config env override scaffold stub gitignore theme"`
Query intent: `"how does mdBook load config defaults and scaffold a new book project"`

Expected symbols:
- `Config` — crates/mdbook-core/src/config.rs — in-memory representation of `book.toml`
- `update_from_env` — crates/mdbook-core/src/config.rs — applies `MDBOOK_*` environment overrides to config
- `BookConfig` — crates/mdbook-core/src/config.rs — typed `[book]` section with metadata and source directory settings
- `BuildConfig` — crates/mdbook-core/src/config.rs — typed `[build]` section with output and watch defaults
- `BookBuilder` — crates/mdbook-driver/src/init.rs — helper that scaffolds a new book directory and files
- `write_book_toml` — crates/mdbook-driver/src/init.rs — serializes and writes the generated `book.toml`
- `create_stub_files` — crates/mdbook-driver/src/init.rs — creates the initial `SUMMARY.md` and chapter stub files
- `create_directory_structure` — crates/mdbook-driver/src/init.rs — creates the root, source, and output directories for a new book

## Investigation 5: Serve, watch, and live reload (10 symbols)

Query keyword: `"serve websocket watch gitignore poller live reload scan"`
Query intent: `"how does mdBook watch source files and trigger browser live reload"`

Expected symbols:
- `LIVE_RELOAD_ENDPOINT` — src/cmd/serve.rs — websocket endpoint constant injected into served HTML for live reload
- `serve` — src/cmd/serve.rs — starts the Axum HTTP server and serves generated files plus the websocket route
- `websocket_connection` — src/cmd/serve.rs — sends reload messages to connected websocket clients after rebuilds
- `WatcherKind` — src/cmd/watch.rs — selects between poll-based and native file watching backends
- `find_gitignore` — src/cmd/watch.rs — locates the nearest `.gitignore` that should filter watched paths
- `remove_ignored_files` — src/cmd/watch/native.rs — filters changed native-watch paths using gitignore rules
- `filter_ignored_files` — src/cmd/watch/native.rs — applies compiled gitignore rules to a batch of changed paths
- `Watcher` — src/cmd/watch/poller.rs — poll-based watcher state tracking watched roots and file metadata
- `set_roots` — src/cmd/watch/poller.rs — selects which roots and extra assets the poller should scan
- `scan` — src/cmd/watch/poller.rs — recursive diff scan that detects changed files for rebuilds
