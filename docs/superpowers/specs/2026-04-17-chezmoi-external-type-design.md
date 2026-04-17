# Chezmoi External Type — Design

Status: Approved for implementation planning
Date: 2026-04-17

## Summary

Add a new external type `chezmoi` to chezmoi's externals system. A chezmoi-type
external declares a secondary chezmoi source repository that is managed by
invoking the `chezmoi` binary as a subprocess, so a user can compose multiple
chezmoi-managed dotfile repositories into a single home directory.

The secondary repo has its own source directory, config file, persistent state
file, and cache directory — all isolated from the parent. Explicit CLI flags
from the parent are passed through; path flags are derived/overridden for
isolation.

## Motivation

Users today cannot compose independent chezmoi repositories — e.g. a personal
dotfiles repo and a separate work dotfiles repo that manage overlapping areas
of `$HOME`. Workarounds (shell wrappers, manually running a second `chezmoi
apply`) bypass chezmoi's apply lifecycle, refresh semantics, and dry-run
guarantees.

## Goals

- Add `type = "chezmoi"` externals declared in `.chezmoiexternal.toml` /
  `.chezmoiexternals/*`.
- Support both first-time `chezmoi init --apply <url>` and subsequent
  `chezmoi apply` for the secondary repo.
- Isolate secondary state, config, and cache per-external (separate BoltDB,
  separate lockfile, separate config file).
- Pass through explicitly set parent CLI flags to the secondary invocation.
- Respect `refreshPeriod` and `--refresh-externals` flag semantics the same
  way git-repo externals do.
- Prevent infinite nesting via recursion guard.

## Non-goals

- No support for remote chezmoi config (secondary must bring its own config
  template or use `init --apply` to create one).
- No in-process nested `Config` instance. Subprocess isolation only.
- No migration of existing git-repo externals to the new type.
- No UI for managing a collection of secondary repos beyond declaring them in
  externals files.

## Architecture

### Overview

A chezmoi-type external is processed like a git-repo external: it emits a
`SourceStateCommand` entry whose `cmdFunc` returns an `*exec.Cmd` that invokes
the parent `chezmoi` binary with a curated argument list against an isolated
source/config/state/cache layout. The existing
`TargetStateModifyDirWithCmd.Apply()` / `SkipApply()` machinery drives the
subprocess at parent apply time, records completion in a dedicated state
bucket, and throttles re-runs via `refreshPeriod` and `forceRefresh`.

### Data flow per parent apply

```
parent chezmoi apply
  → SourceState.Read() builds entries
    → chezmoi-type externals loop emits SourceStateCommand
      (cmdFunc captures external, source dir, derived paths)
  → for each entry: TargetStateModifyDirWithCmd.Apply()
    → SkipApply() checks ChezmoiExternalStateBucket + refreshPeriod
    → if not skipped:
        → sourceExists = stat(sourceDir)
        → cmd = newChezmoiExternalCmd(...)
        → exec.Cmd runs subprocess chezmoi
            (subprocess does init-if-missing + apply against own paths)
        → on success: write {Name, RunAt} to bucket
```

### Identity

The subprocess is invoked using `os.Executable()` as argv[0] so the secondary
runs the same binary as the parent. Falls back to `"chezmoi"` in `PATH` if
`os.Executable()` fails.

### Isolation guarantees

Subprocess receives explicit `--config`, `--persistent-state`, `--source`,
`--destination`, `--cache` flags that point into per-external paths. Parent's
BoltDB is never touched by child. Child's BoltDB holds its own file lock.
Parent waits for subprocess exit before continuing.

## Schema

Extend `External` struct in `internal/chezmoi/sourcestate.go`:

```go
type External struct {
    // ... existing fields ...

    // Chezmoi-type external fields.
    Chezmoi ExternalChezmoi `json:"chezmoi" toml:"chezmoi" yaml:"chezmoi"`
}

type ExternalChezmoi struct {
    Init       ExternalChezmoiInit  `json:"init"       toml:"init"       yaml:"init"`
    Apply      ExternalChezmoiApply `json:"apply"      toml:"apply"      yaml:"apply"`
    ConfigFile AbsPath              `json:"configFile" toml:"configFile" yaml:"configFile"`
    StateFile  AbsPath              `json:"stateFile"  toml:"stateFile"  yaml:"stateFile"`
    CacheDir   AbsPath              `json:"cacheDir"   toml:"cacheDir"   yaml:"cacheDir"`
}

type ExternalChezmoiInit struct {
    Args []string `json:"args" toml:"args" yaml:"args"`
}

type ExternalChezmoiApply struct {
    Args []string `json:"args" toml:"args" yaml:"args"`
}
```

Reused existing fields on `External`:

- `URL` — required; passed to `chezmoi init <url>`.
- `RefreshPeriod` — throttle between re-runs.

New constant in `internal/chezmoi/sourcestate.go`:

```go
ExternalTypeChezmoi ExternalType = "chezmoi"
```

### Example `.chezmoiexternal.toml`

```toml
[".local/share/work-dots"]
    type = "chezmoi"
    url = "https://github.com/example/work-dotfiles.git"
    refreshPeriod = "168h"
    [".local/share/work-dots".chezmoi.init]
        args = ["--branch", "main"]
    [".local/share/work-dots".chezmoi.apply]
        args = []
    # optional overrides:
    # [".local/share/work-dots".chezmoi]
    #     configFile = "~/.config/chezmoi-work/chezmoi.toml"
    #     stateFile  = "~/.config/chezmoi-work/chezmoistate.boltdb"
```

### Validation

At external-parse time, warn (not error) if fields incompatible with type
`chezmoi` are set:

- `Encrypted`, `Exact`, `Executable`, `Private`, `ReadOnly`
- `Format`, `ArchivePath`, `StripComponents`, `Decompress`, `Archive`
- `Filter`, `Exclude`, `Include`, `Checksum`, `URLs`, `TargetPath`
- `Clone`, `Pull` (these belong to git-repo type)

Error if `URL` is empty.

## Path Derivation

### Slug

```go
// externalSlug returns a filesystem-safe identifier derived from an external
// relative path. Slashes, backslashes, and colons are replaced with
// underscores.
func externalSlug(relPath RelPath) string {
    s := relPath.String()
    s = strings.ReplaceAll(s, "/", "_")
    s = strings.ReplaceAll(s, `\`, "_")
    s = strings.ReplaceAll(s, ":", "_")
    return s
}
```

Example: `.local/share/work-dots` → `.local_share_work-dots`.

### Defaults

| Artifact      | Default path                                                     |
|---------------|------------------------------------------------------------------|
| Source dir    | `<parentDestDir>/<externalRelPath>` (unchanged, like git-repo)   |
| Config file   | `<parentConfigDir>/externals/<slug>/chezmoi.toml`                |
| State file    | `<parentConfigDir>/externals/<slug>/chezmoistate.boltdb`         |
| Cache dir     | `<parentCacheDir>/externals/<slug>/`                             |

`parentConfigDir = dir(c.configFile)`, `parentCacheDir = c.CacheDirAbsPath`.

### Overrides

`ExternalChezmoi.{ConfigFile,StateFile,CacheDir}` win if non-empty.
Relative overrides resolve against `parentConfigDir` (config/state) or
`parentCacheDir` (cache). Tilde expansion handled by existing
`chezmoi.AbsPath` unmarshaler.

### Directory creation

Before first subprocess invocation for a given external, parent ensures these
paths exist via `baseSystem.MkdirAll`:

- Parent directory of source dir (so `chezmoi init` can create the source dir
  itself).
- Parent directory of config file.
- Parent directory of state file.
- The cache dir itself.

Perms `0o700` on the per-external config/state directory since the state
file may be sensitive. Source dir itself is created by `chezmoi init`.

### New accessor

```go
// externalChezmoiPaths returns the derived config, state, and cache paths
// for a chezmoi-type external, honoring any per-external overrides.
func (c *Config) externalChezmoiPaths(
    externalRelPath chezmoi.RelPath,
    external *chezmoi.External,
) (configFile, stateFile, cacheDir chezmoi.AbsPath)
```

Lives in new file `internal/cmd/chezmoiexternal.go`.

## Subprocess Command Construction

### Binary resolution

```go
// chezmoiBinaryPath returns the path to the chezmoi binary to invoke for
// chezmoi-type externals.
func (c *Config) chezmoiBinaryPath() string {
    if path, err := os.Executable(); err == nil {
        return path
    }
    return "chezmoi"
}
```

### Per-external command

```go
func (c *Config) newChezmoiExternalCmd(
    externalRelPath chezmoi.RelPath,
    external *chezmoi.External,
    sourceExists bool,
) *exec.Cmd {
    sourceDir := c.DestDirAbsPath.Join(externalRelPath)
    configFile, stateFile, cacheDir := c.externalChezmoiPaths(externalRelPath, external)

    args := []string{
        "--source", sourceDir.String(),
        "--destination", c.DestDirAbsPath.String(),
        "--config", configFile.String(),
        "--persistent-state", stateFile.String(),
        "--cache", cacheDir.String(),
    }
    args = append(args, c.passthroughFlags()...)

    if !sourceExists {
        args = append(args, "init", "--apply", external.URL)
        args = append(args, external.Chezmoi.Init.Args...)
    } else {
        args = append(args, "apply")
        args = append(args, external.Chezmoi.Apply.Args...)
    }

    cmd := exec.Command(c.chezmoiBinaryPath(), args...)
    cmd.Stdin = c.stdin
    cmd.Stdout = c.stdout
    cmd.Stderr = c.stderr
    cmd.Env = append(os.Environ(), "CHEZMOI_EXTERNAL=1")
    return cmd
}
```

### Passthrough allowlist

Only persistent flags explicitly set on parent CLI propagate. `flag.Changed`
gates inclusion so config-file defaults do not double-apply.

```go
var chezmoiExternalPassthroughFlags = []string{
    "color", "debug", "dry-run", "force", "keep-going",
    "no-tty", "refresh-externals", "verbose",
}

// passthroughFlags returns args for persistent flags that were explicitly
// set on the parent CLI.
func (c *Config) passthroughFlags() []string {
    var args []string
    for _, name := range chezmoiExternalPassthroughFlags {
        f := c.cmd.PersistentFlags().Lookup(name)
        if f == nil || !f.Changed {
            continue
        }
        args = append(args, "--"+name+"="+f.Value.String())
    }
    return args
}
```

### Recursion guard

`CHEZMOI_EXTERNAL=1` env var is set on every subprocess. If parent detects
this at startup **and** any external has `type = "chezmoi"`, it emits a hard
error during external parse:

```
chezmoi-type externals cannot be nested; offending external: <relpath>
```

Fails fast — does not wait for subprocess dispatch.

### Source existence check

`c.baseSystem.Stat(sourceDir)` at `cmdFunc` evaluation time, inside the
`sync.OnceValue` wrapper (same pattern as git-repo). Missing → init+apply.
Present → apply. No "is this a chezmoi repo?" probe; subprocess surfaces its
own error if the dir is not a valid chezmoi source.

## State Tracking

### New bucket

```go
// persistentstate.go
var (
    // ... existing buckets ...

    // ChezmoiExternalStateBucket is the bucket for recording the state of
    // chezmoi-type externals.
    ChezmoiExternalStateBucket = []byte("chezmoiExternalState")
)
```

### State struct

Reuse existing `ModifyDirWithCmdState{Name, RunAt}`. No new struct.

### Dispatcher changes

Current `TargetStateModifyDirWithCmd` hardcodes `GitRepoExternalStateBucket`.
Add `stateBucket []byte` field:

```go
type TargetStateModifyDirWithCmd struct {
    cmdFunc       func() *exec.Cmd
    forceRefresh  bool
    refreshPeriod Duration
    sourceAttr    SourceAttr
    stateBucket   []byte
}
```

Apply/SkipApply use `t.stateBucket` instead of the hardcoded bucket.
`SourceStateCommand` gains matching field; both code paths (git-repo and
chezmoi) populate it. Git-repo continues to use `GitRepoExternalStateBucket`
for backwards compatibility with existing on-disk state.

### Refresh semantics

- `refreshPeriod == 0` → run once, never re-run until
  `--refresh-externals=always`.
- `refreshPeriod > 0` → re-run if `time.Since(RunAt) >= refreshPeriod`.
- `--refresh-externals=always` on parent → `forceRefresh=true` → always run.

Matches git-repo external behavior.

### State key

`externalRelPath.String()` (absolute path of source dir). Same shape as
git-repo external state keys.

## Error Handling

### Subprocess failures

`system.RunCmd(cmd)` returns `*exec.ExitError` on non-zero exit. Existing
wrapping at `TargetStateModifyDirWithCmd.Apply()`:

```go
return false, fmt.Errorf("%s: %w", actualStateEntry.Path(), err)
```

Stderr streams directly to parent's stderr via `cmd.Stderr = c.stderr` so
secondary errors are visible inline.

### `--keep-going`

Parent's existing `c.keepGoing` governs whether a failing entry aborts the
rest. Chezmoi externals inherit this without special-casing.

### First-time init without TTY

`chezmoi init --apply` may prompt for config values. With `--no-tty`
propagated, prompts are disabled and init fails loudly unless the secondary
ships a `.chezmoi.toml.tmpl` that needs no input. This constraint is
documented in the user guide.

### Partial subprocess state

If the subprocess crashes mid-apply, its own state file records partial
progress (same failure mode as any chezmoi crash). Parent state bucket writes
only on success → next parent run retries the subprocess.

### Recursion guard

See "Subprocess Command Construction" above. Hard error fails fast at parse
time.

## Testing

### Txtar integration tests

New files under `internal/cmd/testdata/scripts/`:

| File                                | Scenario                                              |
|-------------------------------------|-------------------------------------------------------|
| `externalchezmoi.txtar`             | Happy path: init+apply then apply-only on re-run      |
| `externalchezmoirefresh.txtar`      | `refreshPeriod` throttle + `--refresh-externals=always` override |
| `externalchezmoinest.txtar`         | Nested external rejected via `CHEZMOI_EXTERNAL=1`     |
| `externalchezmoioverride.txtar`     | Per-external `configFile` / `stateFile` overrides     |
| `externalchezmoipassthrough.txtar`  | `--dry-run`, `--verbose` propagate; path flags do not |

Secondary repos in tests use `file://` URLs only. Existing `mksourcedir` and
`mkhomedir` helpers set up the second source dir.

### Unit tests

- `externalSlug` in `internal/chezmoi/sourcestate_test.go`
- `passthroughFlags` in `internal/cmd/chezmoiexternal_test.go` — mock cobra
  command with flags set via `Changed=true`, assert arg list.
- `externalChezmoiPaths` — override precedence table test.

### Out of scope for tests

Exercising real remote git URLs. Tests use `file://` only.

## Documentation

- `assets/chezmoi.io/docs/reference/source-state-attributes.md` — new external
  type section describing `type = "chezmoi"`, lifecycle, and passthrough
  semantics.
- `assets/chezmoi.io/docs/reference/special-files-and-directories/chezmoi-format.md`
  — `chezmoi.init.args`, `chezmoi.apply.args`, `chezmoi.configFile`,
  `chezmoi.stateFile`, `chezmoi.cacheDir` config fields.
- `assets/chezmoi.io/docs/user-guide/include-files-from-elsewhere.md` —
  example composing two chezmoi repos.

## Files Touched

New:

- `internal/cmd/chezmoiexternal.go` — path derivation, cmd construction,
  passthrough flag collection, binary resolution.
- `internal/cmd/chezmoiexternal_test.go` — unit tests.
- `internal/cmd/testdata/scripts/externalchezmoi*.txtar` — integration tests.
- `docs/superpowers/specs/2026-04-17-chezmoi-external-type-design.md` — this
  document.

Modified:

- `internal/chezmoi/sourcestate.go` — `ExternalTypeChezmoi` constant,
  `External.Chezmoi` field, `ExternalChezmoi*` structs, chezmoi-type loop in
  `Read()` alongside git-repo loop, `readExternal` switch case, validation
  warnings, recursion guard.
- `internal/chezmoi/sourcestateentry.go` — `SourceStateCommand.stateBucket`
  field, plumbing.
- `internal/chezmoi/targetstateentry.go` — `TargetStateModifyDirWithCmd.stateBucket`
  field, use it in `Apply`/`SkipApply`.
- `internal/chezmoi/persistentstate.go` — `ChezmoiExternalStateBucket`.
- `assets/chezmoi.io/docs/**` — docs updates per above.

## Open Questions

None outstanding. Proceed to implementation plan.

## Risks & Mitigations

| Risk                                                                 | Mitigation                                                                 |
|----------------------------------------------------------------------|----------------------------------------------------------------------------|
| Subprocess inherits unexpected env → different behavior from parent  | `cmd.Env = append(os.Environ(), "CHEZMOI_EXTERNAL=1")` preserves env; tests assert.|
| Two secondary repos writing the same target file → silent overwrite  | Document precedence (externals process in sorted rel-path order); rely on user discipline. Future: collision detection pass.|
| Secondary chezmoi binary version mismatch after parent upgrade       | `os.Executable()` ensures same binary. Doc: upgrade affects all nested repos.|
| `init --apply` prompts hang under `--no-tty`                         | Documented constraint; `.chezmoi.toml.tmpl` in secondary is required for unattended use.|
| State bucket collision if user switches external from git-repo → chezmoi on same rel path | Separate buckets; old entry remains but is harmless. Doc: no automatic migration.|
