# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```sh
# Build
go build .

# Run all tests (run twice with different umask values — both are required)
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o022" ./...
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o002" ./...

# Run a single package's tests
go test ./internal/chezmoi/...
go test ./internal/cmd/...

# Run a single txtar script test (filter by name)
go test ./internal/cmd/ -run TestScript -filter=apply

# Lint (requires golangci-lint in ./bin/)
make ensure-golangci-lint
./bin/golangci-lint run --config=.config/golangci.yml

# Format
./bin/golangci-lint fmt
find . -name \*.txtar | xargs go tool lint-txtar -w

# Code generation (e.g., after adding new commands/template funcs)
CHEZMOIDEV=ignoreflags=1,ignorehelp=1 go generate

# Build and run chezmoi directly
go tool chezmoi --version
```

## Architecture

The codebase has two main layers:

### `internal/chezmoi` — Core Logic

Central types and interfaces:

- **`System` interface** (`system.go`) — abstraction over all filesystem/script operations. Multiple implementations exist: `RealSystem` (production), `DryRunSystem`, `DumpSystem`, `DebugSystem`, `GitDiffSystem`, `ExternalDiffSystem`, etc. Commands compose these via wrapping.
- **`SourceState`** (`sourcestate.go`) — reads the chezmoi source directory, parses file naming conventions (prefixes like `dot_`, `exact_`, `run_once_`, etc. and the `.tmpl` suffix), and builds a tree of `SourceStateEntry` objects.
- **`SourceStateEntry` / `TargetStateEntry`** (`sourcestateentry.go`, `targetstateentry.go`) — represent what chezmoi *wants* a file/dir to be. `Apply()` on a `TargetStateEntry` reconciles it against the actual filesystem via a `System`.
- **`PersistentState`** (`persistentstate.go`, `boltpersistentstate.go`) — BoltDB-backed store for run-once/run-onchange script state.
- **File naming prefixes** are defined as constants in `chezmoi.go`; parsing happens in `attr.go` and `sourcerelpath.go`.

### `internal/cmd` — CLI Layer

- **`config.go`** — the `Config` struct; holds all config, flags, and top-level state. This is the main entry point for command execution.
- One file per command: `applycmd.go`, `addcmd.go`, `diffcmd.go`, etc.
- Template functions are split by secret manager: `bitwardentemplatefuncs.go`, `awssecretsmanagertemplatefuncs.go`, `githubtemplatefuncs.go`, etc. Core template funcs in `templatefuncs.go`.
- **Integration tests** use `testscript` (rogpeppe/go-internal) with `.txtar` scripts in `internal/cmd/testdata/scripts/`. Each `.txtar` file is a self-contained test scenario with setup helpers like `mkhomedir`, `mksourcedir`.

### Supporting Packages

| Package | Purpose |
|---------|---------|
| `chezmoierrors` | Typed error helpers |
| `chezmoigit` | go-git integration |
| `chezmoilog` | slog-based structured logging |
| `chezmoiset` | Generic set type |
| `chezmoitest` | Test helpers (umask control, vfs setup) |
| `archivetest` | Test helpers for archive operations |
| `internal/cmds/` | Internal Go tool commands (linters, code generators) registered in `go.mod` as `tool` directives |

## Key Conventions

- File naming prefix parsing is the core of chezmoi's behavior — when adding new prefixes, update `chezmoi.go` constants, `attr.go` parsing, and `sourcerelpath.go`.
- New commands: add `*cmd.go` in `internal/cmd/`, register in `cmd.go`, add txtar integration tests in `testdata/scripts/`.
- New template functions: add to the appropriate `*templatefuncs.go` file, then re-run `go generate`.
- The `System` interface is the boundary for testability — commands should only touch the filesystem via `System`, never directly.
- Commit messages are linted by `lint-commit-messages` against upstream master; conventional commits format required.
