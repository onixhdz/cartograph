# Cartograph

**Build a nervous system for your codebase.** Cartograph indexes any repository into a knowledge graph — every function, call chain, dependency, and execution flow — then exposes it through smart tools so AI agents never miss code.

> Even smaller models get full architectural context, making them compete with frontier models on code tasks.

**TL;DR:** Point it at a repo, get a complete map. Use the CLI tools to search, trace impact, and explore — or connect it to your AI editor via MCP so Cursor, Claude Code, and friends stop missing dependencies and shipping blind edits.

---

## Quick Start

```bash
# Index your repo (run from repo root)
cartograph analyze

# Search for execution flows
cartograph query "authentication middleware"

# See everything about a symbol — callers, callees, processes
cartograph context UserService

# What breaks if you change something?
cartograph impact validateUser
```

That's it. The graph is built, persisted locally, and ready to query.

---

## CI Mode

Cartograph runs without a background server — no sockets, lockfiles, or idle timers. Pass `--ci` to run all queries in-process:

```bash
# Analyze once, then query in CI
cartograph analyze .
cartograph --ci query "authentication" -r myrepo
cartograph --ci impact validateUser -r myrepo
cartograph --ci context UserService -r myrepo
```

You can also set the environment variable instead of repeating the flag:

```yaml
# GitHub Actions
env:
  CARTOGRAPH_CI: "1"
steps:
  - run: cartograph analyze .
  - run: cartograph query "authentication" -r myrepo
  - run: cartograph impact validateUser -r myrepo
```

---

## What You Get

### Smart Tools

| Command           | What It Does                                        |
| ----------------- | --------------------------------------------------- |
| `analyze [path]`  | Index a repository into a knowledge graph           |
| `query <search>`  | Search for execution flows and symbols              |
| `context <name>`  | 360° view of a symbol — callers, callees, processes |
| `impact <target>` | Blast radius — what breaks if you change a symbol   |
| `cypher <query>`  | Raw graph queries for precise structural questions  |
| `wiki [path]`     | Generate repository documentation from the graph    |
| `source <files>`  | Retrieve source code from indexed repos             |
| `setup`           | Configure skills for Cursor, Claude Code, OpenCode  |
| `list`            | List all indexed repositories                       |
| `status`          | Show index status for current repo                  |
| `clean`           | Delete index for current repo                       |

### AI Editor Integration

Run `cartograph setup` once to configure MCP for your editors:

| Editor          | Support      |
| --------------- | ------------ |
| **Claude Code** | MCP + Skills |
| **Cursor**      | MCP + Skills |
| **OpenCode**    | MCP + Skills |

---

## Language Support

Cartograph detects **206 languages** via tree-sitter grammars. Extraction depth depends on the language:

### Tier 1 — Full Extraction (13 languages)

Go · TypeScript · JavaScript · Python · Java · Rust · C++ · C · Ruby · PHP · Kotlin · Swift · C#

| Feature         | Description                                                                           |
| --------------- | ------------------------------------------------------------------------------------- |
| **Symbols**     | Functions, classes, methods, structs, interfaces, enums, traits, modules, and more    |
| **Imports**     | Cross-file import resolution with config awareness (go.mod, tsconfig, etc.)           |
| **Calls**       | Function/method calls resolved across files with type-aware scoring                   |
| **Heritage**    | Inheritance, interfaces, traits, and mixins with full MRO computation                 |
| **Types**       | Static type inference from declarations, constructors, pattern matching, and comments |
| **Assignments** | Field write tracking for mutation analysis                                            |

### Tier 2 — Inferred Extraction (56+ languages)

Lua · Dart · Scala · Julia · R · Elixir · Bash · SQL · Haskell · OCaml · Zig · Nim · and more

A grammar-agnostic engine walks raw ASTs to extract symbols, imports, calls, and inheritance — no hand-crafted queries needed.

---

## How It Works

Cartograph builds a complete knowledge graph through a multi-phase pipeline:

1. **Structure** — Walks the file tree and maps folder/file relationships
2. **Parsing** — Extracts symbols using tree-sitter with hand-crafted queries per language
3. **Resolution** — Resolves imports, calls, and inheritance across files with language-aware logic
4. **Clustering** — Groups related symbols into functional communities (Leiden algorithm)
5. **Processes** — Traces execution flows from entry points through call chains
6. **Search** — Builds indexes for fast retrieval

### The Graph

| Nodes                                                                                                                  | Edges                                                                                                     |
| ---------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Function, Method, Class, Interface, Struct, Enum, Trait, Module, Namespace, File, Folder, Community, Process, and more | CALLS, IMPORTS, DEFINES, CONTAINS, EXTENDS, IMPLEMENTS, OVERRIDES, HAS_METHOD, MEMBER_OF, STEP_IN_PROCESS |

Everything is persisted locally using an embedded database — no external services needed.

---

## Extraction Depth

Cartograph goes deeper than typical code search — it understands how your code fits together.

- **28 node types** — functions, classes, methods, interfaces, structs, enums, traits, modules, and more
- **17 relationship types** — calls, imports, inheritance, overrides, containment, process flows, and more
- **Import resolution** for 9 ecosystems — Go (`go.mod`), TypeScript/JS (`tsconfig.json`), Python, Java, Kotlin, Rust, C#, PHP (`composer.json`), Ruby, and Swift (`Package.swift`)
- **Type-aware call resolution** — resolves method calls through receiver types, import scopes, and aliases across files
- **Community detection** — automatically groups related code using the Leiden algorithm
- **Inheritance analysis** — full linearization for multiple inheritance, interfaces, traits, and mixins
- **Entry point detection** — identifies HTTP handlers, main functions, and other framework-specific entry points
- **56+ fallback languages** — grammar-agnostic extraction for languages without hand-crafted support

---

## Tool Examples

### Impact Analysis

```
$ cartograph impact UserService

TARGET: Class UserService (src/services/user.ts)

DOWNSTREAM (what breaks):
  Depth 1:
    handleLogin       [CALLS]  → src/api/auth.ts:45
    handleRegister    [CALLS]  → src/api/auth.ts:78
    UserController    [CALLS]  → src/controllers/user.ts:12
  Depth 2:
    authRouter        [IMPORTS] → src/routes/auth.ts
```

### 360° Context

```
$ cartograph context validateUser

Symbol: Function validateUser (src/auth/validate.ts:15)

Incoming:
  calls: handleLogin, handleRegister, UserController

Outgoing:
  calls: checkPassword, createSession

Processes:
  LoginFlow (step 2/7), RegistrationFlow (step 3/5)
```

### Cypher Queries

```bash
# Find all callers of a function
cartograph cypher "MATCH (s)-[r:CodeRelation]->(t)
  WHERE r.type = 'CALLS' AND t.name = 'handleLogin'
  RETURN s.name, s.filePath"
```

---

## Security & Privacy

Everything runs locally. No code leaves your machine. The index is stored in `~/.local/share/cartograph/` (or `$XDG_DATA_HOME/cartograph/`) and can be deleted at any time with `cartograph clean`.

---

## License

[MIT](LICENSE)
