# Chezmoi External Type Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `type = "chezmoi"` external — secondary chezmoi source repos managed via subprocess, with isolated source/config/state/cache paths and selective parent-flag passthrough.

**Architecture:** New `ExternalTypeChezmoi` emits a `SourceStateCommand` whose `cmdFunc` invokes the parent `chezmoi` binary (`os.Executable()`) with curated args against per-external paths. Reuses existing `TargetStateModifyDirWithCmd.Apply`/`SkipApply` flow. New `ChezmoiExternalStateBucket` for state isolation from git-repo externals. Recursion guard via `CHEZMOI_EXTERNAL=1` env var.

**Tech Stack:** Go 1.25, cobra/pflag, BoltDB (`go.etcd.io/bbolt`), testscript txtar integration tests.

**Reference spec:** `docs/superpowers/specs/2026-04-17-chezmoi-external-type-design.md`

---

## File Structure

### New files

| Path                                                              | Responsibility                                                           |
|-------------------------------------------------------------------|--------------------------------------------------------------------------|
| `internal/cmd/chezmoiexternal.go`                                 | Path derivation, cmd construction, passthrough flags, binary resolution  |
| `internal/cmd/chezmoiexternal_test.go`                            | Unit tests for helpers above                                             |
| `internal/cmd/testdata/scripts/externalchezmoi.txtar`             | Integration: happy path init+apply, then apply-only                      |
| `internal/cmd/testdata/scripts/externalchezmoirefresh.txtar`      | Integration: refreshPeriod + --refresh-externals=always                  |
| `internal/cmd/testdata/scripts/externalchezmoinest.txtar`         | Integration: nested external rejected via CHEZMOI_EXTERNAL=1             |
| `internal/cmd/testdata/scripts/externalchezmoioverride.txtar`     | Integration: per-external configFile/stateFile override                  |
| `internal/cmd/testdata/scripts/externalchezmoipassthrough.txtar`  | Integration: flag passthrough allowlist                                  |

### Modified files

| Path                                                 | Change                                                                                     |
|------------------------------------------------------|--------------------------------------------------------------------------------------------|
| `internal/chezmoi/persistentstate.go`                | Add `ChezmoiExternalStateBucket` constant                                                  |
| `internal/chezmoi/sourcestate.go`                    | Add `ExternalTypeChezmoi`, `ExternalChezmoi*` structs, loop, `readExternal` case, validation, recursion guard |
| `internal/chezmoi/sourcestateentry.go`               | Add `stateBucket []byte` field to `SourceStateCommand`, thread to `TargetStateEntry()`    |
| `internal/chezmoi/targetstateentry.go`               | Add `stateBucket []byte` to `TargetStateModifyDirWithCmd`; use in Apply/SkipApply          |
| `assets/chezmoi.io/docs/reference/source-state-attributes.md` | Document `type = "chezmoi"`                                                        |
| `assets/chezmoi.io/docs/reference/special-files-and-directories/chezmoi-format.md` | Document `chezmoi.init.args`, `chezmoi.apply.args`, etc.    |
| `assets/chezmoi.io/docs/user-guide/include-files-from-elsewhere.md` | Example section                                                              |

---

## Task 1: Add `ChezmoiExternalStateBucket` constant

**Files:**
- Modify: `internal/chezmoi/persistentstate.go`

- [ ] **Step 1: Add the constant**

Edit `internal/chezmoi/persistentstate.go`. After `GitRepoExternalStateBucket`:

```go
	// ChezmoiExternalStateBucket is the bucket for recording the state of
	// chezmoi-type externals.
	ChezmoiExternalStateBucket = []byte("chezmoiExternalState")
```

- [ ] **Step 2: Verify the package still builds**

Run: `go build ./internal/chezmoi/...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/chezmoi/persistentstate.go
git commit -m "feat: Add ChezmoiExternalStateBucket persistent state bucket"
```

---

## Task 2: Refactor `TargetStateModifyDirWithCmd` to take a state bucket

Backwards-compat refactor: existing git-repo externals keep passing `GitRepoExternalStateBucket`; later tasks pass `ChezmoiExternalStateBucket` for chezmoi externals.

**Files:**
- Modify: `internal/chezmoi/targetstateentry.go:85-148`
- Modify: `internal/chezmoi/sourcestateentry.go:39-128`

- [ ] **Step 1: Write a failing test that asserts git-repo state uses GitRepoExternalStateBucket**

Append to `internal/chezmoi/targetstateentry_test.go` (create if missing — check `ls internal/chezmoi/targetstateentry_test.go` first):

```go
func TestTargetStateModifyDirWithCmd_UsesConfiguredBucket(t *testing.T) {
	ps := NewMockPersistentState()
	target := &TargetStateModifyDirWithCmd{
		cmdFunc:       func() *exec.Cmd { return exec.Command("true") },
		stateBucket:   ChezmoiExternalStateBucket,
		forceRefresh:  false,
		refreshPeriod: 0,
	}
	actual := &ActualStateDir{absPath: NewAbsPath("/tmp/test")}
	_, err := target.Apply(NewRealSystem(vfst.NewEmptyFS()), ps, actual)
	assert.NoError(t, err)
	// Data should be in the ChezmoiExternalStateBucket, not GitRepo.
	got, err := ps.Get(ChezmoiExternalStateBucket, []byte("/tmp/test"))
	assert.NoError(t, err)
	assert.NotNil(t, got)
	gotGit, err := ps.Get(GitRepoExternalStateBucket, []byte("/tmp/test"))
	assert.NoError(t, err)
	assert.Nil(t, gotGit)
}
```

- [ ] **Step 2: Run test — expect compile failure because `stateBucket` field does not yet exist**

Run: `go test ./internal/chezmoi/ -run TestTargetStateModifyDirWithCmd_UsesConfiguredBucket`
Expected: compile error `unknown field stateBucket`.

- [ ] **Step 3: Add `stateBucket` field to `TargetStateModifyDirWithCmd`**

In `internal/chezmoi/targetstateentry.go` modify struct definition (around current line 31):

```go
// A TargetStateModifyDirWithCmd represents running a command that modifies
// a directory.
type TargetStateModifyDirWithCmd struct {
	cmdFunc       func() *exec.Cmd
	forceRefresh  bool
	refreshPeriod Duration
	sourceAttr    SourceAttr
	stateBucket   []byte
}
```

- [ ] **Step 4: Use `t.stateBucket` in Apply**

Replace the PersistentStateSet call in `Apply` (around current lines 102-108):

```go
	if err := PersistentStateSet(
		persistentState, t.stateBucket, modifyDirWithCmdStateKey, &ModifyDirWithCmdState{
			Name:  actualStateEntry.Path(),
			RunAt: runAt,
		}); err != nil {
		return false, err
	}
```

- [ ] **Step 5: Use `t.stateBucket` in SkipApply**

Replace the `persistentState.Get` call in `SkipApply` (around current line 133):

```go
	switch modifyDirWithCmdStateBytes, err := persistentState.Get(t.stateBucket, modifyDirWithCmdKey); {
```

- [ ] **Step 6: Add `stateBucket` field to `SourceStateCommand`**

In `internal/chezmoi/sourcestateentry.go` modify struct definition (around current line 40):

```go
// A SourceStateCommand represents a command that should be run.
type SourceStateCommand struct {
	cmdFunc       func() *exec.Cmd
	origin        SourceStateOrigin
	forceRefresh  bool
	refreshPeriod Duration
	sourceAttr    SourceAttr
	stateBucket   []byte
}
```

- [ ] **Step 7: Wire `stateBucket` in `SourceStateCommand.TargetStateEntry`**

In `internal/chezmoi/sourcestateentry.go`, change (around current line 121):

```go
func (s *SourceStateCommand) TargetStateEntry(destSystem System, destDirAbsPath AbsPath) (TargetStateEntry, error) {
	return &TargetStateModifyDirWithCmd{
		cmdFunc:       s.cmdFunc,
		forceRefresh:  s.forceRefresh,
		refreshPeriod: s.refreshPeriod,
		sourceAttr:    s.sourceAttr,
		stateBucket:   s.stateBucket,
	}, nil
}
```

- [ ] **Step 8: Update existing git-repo loop to set `stateBucket`**

In `internal/chezmoi/sourcestate.go` within the git-repo loop (both clone and pull branches, around current lines 1291 and 1319), add to the `SourceStateCommand` literal:

```go
				stateBucket:   GitRepoExternalStateBucket,
```

Both occurrences (the clone case and the pull case).

- [ ] **Step 9: Run tests**

Run: `go test ./internal/chezmoi/... -count=1`
Expected: PASS.

- [ ] **Step 10: Run full test suite with both umasks**

Run:
```
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o022" ./...
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o002" ./...
```
Expected: all PASS. Git-repo external txtar still passes — no behavior change.

- [ ] **Step 11: Commit**

```bash
git add internal/chezmoi/targetstateentry.go internal/chezmoi/sourcestateentry.go internal/chezmoi/sourcestate.go internal/chezmoi/targetstateentry_test.go
git commit -m "refactor: Parameterize modify-dir state bucket on TargetStateModifyDirWithCmd"
```

---

## Task 3: Add `ExternalTypeChezmoi` constant + schema structs

**Files:**
- Modify: `internal/chezmoi/sourcestate.go` (around line 44-50 for constants, ~85-118 for External struct)

- [ ] **Step 1: Add the ExternalType constant**

In `internal/chezmoi/sourcestate.go`, modify the const block (around current lines 46-50):

```go
const (
	ExternalTypeArchive     ExternalType = "archive"
	ExternalTypeArchiveFile ExternalType = "archive-file"
	ExternalTypeChezmoi     ExternalType = "chezmoi"
	ExternalTypeFile        ExternalType = "file"
	ExternalTypeGitRepo     ExternalType = "git-repo"
)
```

- [ ] **Step 2: Add the three new schema structs before the `External` struct**

In `internal/chezmoi/sourcestate.go` before `// An External is an external source.`, add:

```go
// An ExternalChezmoi holds fields specific to chezmoi-type externals.
type ExternalChezmoi struct {
	Init       ExternalChezmoiInit  `json:"init"       toml:"init"       yaml:"init"`
	Apply      ExternalChezmoiApply `json:"apply"      toml:"apply"      yaml:"apply"`
	ConfigFile AbsPath              `json:"configFile" toml:"configFile" yaml:"configFile"`
	StateFile  AbsPath              `json:"stateFile"  toml:"stateFile"  yaml:"stateFile"`
	CacheDir   AbsPath              `json:"cacheDir"   toml:"cacheDir"   yaml:"cacheDir"`
}

// An ExternalChezmoiInit holds extra args for chezmoi init on a chezmoi-type external.
type ExternalChezmoiInit struct {
	Args []string `json:"args" toml:"args" yaml:"args"`
}

// An ExternalChezmoiApply holds extra args for chezmoi apply on a chezmoi-type external.
type ExternalChezmoiApply struct {
	Args []string `json:"args" toml:"args" yaml:"args"`
}
```

- [ ] **Step 3: Add `Chezmoi` field to the `External` struct**

Modify the `External` struct (around current line 95). Add this field alphabetically after `Archive`:

```go
	Chezmoi         ExternalChezmoi   `json:"chezmoi"         toml:"chezmoi"         yaml:"chezmoi"`
```

- [ ] **Step 4: Verify the package builds**

Run: `go build ./internal/chezmoi/...`
Expected: no output, exit 0.

- [ ] **Step 5: Add the chezmoi case to `readExternal`**

In `internal/chezmoi/sourcestate.go` `readExternal` switch (around current line 2455):

```go
	case ExternalTypeArchive:
		return s.readExternalArchive(ctx, externalRelPath, parentSourceRelPath, external, options)
	case ExternalTypeArchiveFile:
		return s.readExternalArchiveFile(ctx, externalRelPath, parentSourceRelPath, external, options)
	case ExternalTypeChezmoi:
		return nil, nil
	case ExternalTypeFile:
		return s.readExternalFile(ctx, externalRelPath, parentSourceRelPath, external, options)
	case ExternalTypeGitRepo:
		return nil, nil
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/chezmoi/... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/chezmoi/sourcestate.go
git commit -m "feat: Add chezmoi external type constant and schema"
```

---

## Task 4: Add `externalSlug` helper with unit test

**Files:**
- Modify: `internal/chezmoi/sourcestate.go` (add helper at bottom of file)
- Modify: `internal/chezmoi/sourcestate_test.go` (add test)

- [ ] **Step 1: Write the failing test**

Append to `internal/chezmoi/sourcestate_test.go`:

```go
func TestExternalSlug(t *testing.T) {
	for _, tc := range []struct {
		name     string
		relPath  string
		expected string
	}{
		{name: "simple", relPath: "foo", expected: "foo"},
		{name: "slash", relPath: "a/b/c", expected: "a_b_c"},
		{name: "dot-prefix", relPath: ".local/share/dots", expected: ".local_share_dots"},
		{name: "backslash", relPath: `a\b`, expected: "a_b"},
		{name: "colon", relPath: "a:b", expected: "a_b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := externalSlug(NewRelPath(tc.relPath))
			assert.Equal(t, tc.expected, got)
		})
	}
}
```

- [ ] **Step 2: Run test — expect undefined function error**

Run: `go test ./internal/chezmoi/ -run TestExternalSlug`
Expected: compile error `undefined: externalSlug`.

- [ ] **Step 3: Add the helper**

Append to `internal/chezmoi/sourcestate.go`:

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

- [ ] **Step 4: Run test — expect pass**

Run: `go test ./internal/chezmoi/ -run TestExternalSlug`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/chezmoi/sourcestate.go internal/chezmoi/sourcestate_test.go
git commit -m "feat: Add externalSlug helper"
```

---

## Task 5: Add `externalChezmoiPaths` helper with unit test

**Files:**
- Create: `internal/cmd/chezmoiexternal.go`
- Create: `internal/cmd/chezmoiexternal_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cmd/chezmoiexternal_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/alecthomas/assert/v2"

	"chezmoi.io/chezmoi/internal/chezmoi"
)

func TestExternalChezmoiPaths_Defaults(t *testing.T) {
	c := &Config{
		configFile:       chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
		CacheDirAbsPath:  chezmoi.NewAbsPath("/home/u/.cache/chezmoi"),
	}
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	ext := &chezmoi.External{}
	configFile, stateFile, cacheDir := c.externalChezmoiPaths(relPath, ext)
	assert.Equal(t, "/home/u/.config/chezmoi/externals/.local_share_work-dots/chezmoi.toml", configFile.String())
	assert.Equal(t, "/home/u/.config/chezmoi/externals/.local_share_work-dots/chezmoistate.boltdb", stateFile.String())
	assert.Equal(t, "/home/u/.cache/chezmoi/externals/.local_share_work-dots", cacheDir.String())
}

func TestExternalChezmoiPaths_Override(t *testing.T) {
	c := &Config{
		configFile:      chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
		CacheDirAbsPath: chezmoi.NewAbsPath("/home/u/.cache/chezmoi"),
	}
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	ext := &chezmoi.External{
		Chezmoi: chezmoi.ExternalChezmoi{
			ConfigFile: chezmoi.NewAbsPath("/custom/config.toml"),
			StateFile:  chezmoi.NewAbsPath("/custom/state.boltdb"),
			CacheDir:   chezmoi.NewAbsPath("/custom/cache"),
		},
	}
	configFile, stateFile, cacheDir := c.externalChezmoiPaths(relPath, ext)
	assert.Equal(t, "/custom/config.toml", configFile.String())
	assert.Equal(t, "/custom/state.boltdb", stateFile.String())
	assert.Equal(t, "/custom/cache", cacheDir.String())
}
```

- [ ] **Step 2: Run test — expect undefined method error**

Run: `go test ./internal/cmd/ -run TestExternalChezmoiPaths`
Expected: compile error `c.externalChezmoiPaths undefined`.

- [ ] **Step 3: Create the helper file**

Create `internal/cmd/chezmoiexternal.go`:

```go
package cmd

import (
	"chezmoi.io/chezmoi/internal/chezmoi"
)

// chezmoiExternalsSubdir is the per-external subdirectory name under
// parentConfigDir/parentCacheDir.
const chezmoiExternalsSubdir = "externals"

// externalChezmoiPaths returns the derived configFile, stateFile, and cacheDir
// for a chezmoi-type external. Per-external overrides on external.Chezmoi win
// when non-empty; otherwise defaults under parentConfigDir/parentCacheDir are
// used.
func (c *Config) externalChezmoiPaths(
	externalRelPath chezmoi.RelPath,
	external *chezmoi.External,
) (configFile, stateFile, cacheDir chezmoi.AbsPath) {
	slug := externalSlug(externalRelPath)
	parentConfigDir := c.configFile.Dir()

	configFile = external.Chezmoi.ConfigFile
	if configFile.IsEmpty() {
		configFile = parentConfigDir.JoinString(chezmoiExternalsSubdir, slug, "chezmoi.toml")
	}
	stateFile = external.Chezmoi.StateFile
	if stateFile.IsEmpty() {
		stateFile = parentConfigDir.JoinString(chezmoiExternalsSubdir, slug, "chezmoistate.boltdb")
	}
	cacheDir = external.Chezmoi.CacheDir
	if cacheDir.IsEmpty() {
		cacheDir = c.CacheDirAbsPath.JoinString(chezmoiExternalsSubdir, slug)
	}
	return
}
```

Note: `externalSlug` is exported from the `chezmoi` package. **Change Task 4's helper to `ExternalSlug` (exported)** — see Task 5b below.

- [ ] **Step 4: Run test — expect pass after Task 5b completes**

Defer to next step.

---

## Task 5b: Export `externalSlug` → `ExternalSlug`

**Files:**
- Modify: `internal/chezmoi/sourcestate.go`
- Modify: `internal/chezmoi/sourcestate_test.go`

- [ ] **Step 1: Rename in sourcestate.go**

Rename function declaration and all references:

```go
// ExternalSlug returns a filesystem-safe identifier ...
func ExternalSlug(relPath RelPath) string {
```

- [ ] **Step 2: Update test**

In `sourcestate_test.go`, change `externalSlug(` → `ExternalSlug(`.

- [ ] **Step 3: Update `chezmoiexternal.go` to use `chezmoi.ExternalSlug`**

In `internal/cmd/chezmoiexternal.go`:

```go
	slug := chezmoi.ExternalSlug(externalRelPath)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/chezmoi/... ./internal/cmd/ -run "TestExternalSlug|TestExternalChezmoiPaths" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit Tasks 5 + 5b**

```bash
git add internal/chezmoi/sourcestate.go internal/chezmoi/sourcestate_test.go internal/cmd/chezmoiexternal.go internal/cmd/chezmoiexternal_test.go
git commit -m "feat: Add externalChezmoiPaths helper and export ExternalSlug"
```

---

## Task 6: Add `chezmoiBinaryPath` helper

**Files:**
- Modify: `internal/cmd/chezmoiexternal.go`
- Modify: `internal/cmd/chezmoiexternal_test.go`

- [ ] **Step 1: Write a basic test**

Append to `internal/cmd/chezmoiexternal_test.go`:

```go
func TestChezmoiBinaryPath_NonEmpty(t *testing.T) {
	c := &Config{}
	got := c.chezmoiBinaryPath()
	assert.NotEqual(t, "", got)
}
```

- [ ] **Step 2: Run test — expect undefined method error**

Run: `go test ./internal/cmd/ -run TestChezmoiBinaryPath_NonEmpty`
Expected: compile error.

- [ ] **Step 3: Add the helper**

Append to `internal/cmd/chezmoiexternal.go`:

```go
import (
	"os"

	"chezmoi.io/chezmoi/internal/chezmoi"
)

// chezmoiBinaryPath returns the path to the chezmoi binary to invoke for
// chezmoi-type externals. Falls back to "chezmoi" in PATH if os.Executable
// fails.
func (c *Config) chezmoiBinaryPath() string {
	if path, err := os.Executable(); err == nil {
		return path
	}
	return "chezmoi"
}
```

Ensure `os` is imported.

- [ ] **Step 4: Run test — expect pass**

Run: `go test ./internal/cmd/ -run TestChezmoiBinaryPath_NonEmpty`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cmd/chezmoiexternal.go internal/cmd/chezmoiexternal_test.go
git commit -m "feat: Add chezmoiBinaryPath helper"
```

---

## Task 7: Add `passthroughFlags` helper with allowlist

**Files:**
- Modify: `internal/cmd/chezmoiexternal.go`
- Modify: `internal/cmd/chezmoiexternal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cmd/chezmoiexternal_test.go`:

```go
import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestPassthroughFlags_OnlyChangedIncluded(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	fs := cmd.PersistentFlags()
	fs.Bool("dry-run", false, "")
	fs.Bool("verbose", false, "")
	fs.Bool("force", false, "")
	fs.String("color", "auto", "")
	// Mark dry-run as changed, leave others with defaults.
	must(fs.Set("dry-run", "true"))
	c := &Config{cmd: cmd}
	args := c.passthroughFlags()
	assert.Equal(t, []string{"--dry-run=true"}, args)
}

func TestPassthroughFlags_MultipleFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	fs := cmd.PersistentFlags()
	fs.BoolP("verbose", "v", false, "")
	fs.Bool("force", false, "")
	must(fs.Set("verbose", "true"))
	must(fs.Set("force", "true"))
	c := &Config{cmd: cmd}
	args := c.passthroughFlags()
	// Order matches chezmoiExternalPassthroughFlags slice order.
	assert.Equal(t, []string{"--force=true", "--verbose=true"}, args)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var _ = pflag.PrintDefaults // keep import if unused elsewhere
```

- [ ] **Step 2: Run test — expect compile error (undefined passthroughFlags)**

Run: `go test ./internal/cmd/ -run TestPassthroughFlags`
Expected: compile error.

- [ ] **Step 3: Add the helper and allowlist**

Append to `internal/cmd/chezmoiexternal.go`:

```go
// chezmoiExternalPassthroughFlags is the allowlist of persistent flags that
// propagate from parent to secondary chezmoi invocation. Ordered so the
// resulting arg list is deterministic.
var chezmoiExternalPassthroughFlags = []string{
	"color",
	"debug",
	"dry-run",
	"force",
	"keep-going",
	"no-tty",
	"refresh-externals",
	"verbose",
}

// passthroughFlags returns command-line args representing the subset of
// persistent flags in chezmoiExternalPassthroughFlags that were explicitly
// set on the parent CLI (flag.Changed == true).
func (c *Config) passthroughFlags() []string {
	if c.cmd == nil {
		return nil
	}
	fs := c.cmd.PersistentFlags()
	var args []string
	for _, name := range chezmoiExternalPassthroughFlags {
		f := fs.Lookup(name)
		if f == nil || !f.Changed {
			continue
		}
		args = append(args, "--"+name+"="+f.Value.String())
	}
	return args
}
```

- [ ] **Step 4: Run test — expect pass**

Run: `go test ./internal/cmd/ -run TestPassthroughFlags -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cmd/chezmoiexternal.go internal/cmd/chezmoiexternal_test.go
git commit -m "feat: Add passthroughFlags helper and allowlist"
```

---

## Task 8: Add `newChezmoiExternalCmd` method

**Files:**
- Modify: `internal/cmd/chezmoiexternal.go`
- Modify: `internal/cmd/chezmoiexternal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cmd/chezmoiexternal_test.go`:

```go
func TestNewChezmoiExternalCmd_Init(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	fs := cmd.PersistentFlags()
	fs.Bool("dry-run", false, "")
	must(fs.Set("dry-run", "true"))

	c := &Config{
		cmd:              cmd,
		DestDirAbsPath:   chezmoi.NewAbsPath("/home/u"),
		CacheDirAbsPath:  chezmoi.NewAbsPath("/home/u/.cache/chezmoi"),
		configFile:       chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
		stdin:            nil,
		stdout:           nil,
		stderr:           nil,
	}
	ext := &chezmoi.External{
		URL: "https://example.com/dots.git",
		Chezmoi: chezmoi.ExternalChezmoi{
			Init: chezmoi.ExternalChezmoiInit{Args: []string{"--branch", "main"}},
		},
	}
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	cobraCmd := c.newChezmoiExternalCmd(relPath, ext, false)

	// Arg prefix should contain path flags then dry-run then subcommand.
	got := cobraCmd.Args
	// First element is argv[0]; use cobraCmd.Args which excludes argv[0].
	wantContains := []string{
		"--source", "/home/u/.local/share/work-dots",
		"--destination", "/home/u",
		"--config", "/home/u/.config/chezmoi/externals/.local_share_work-dots/chezmoi.toml",
		"--persistent-state", "/home/u/.config/chezmoi/externals/.local_share_work-dots/chezmoistate.boltdb",
		"--cache", "/home/u/.cache/chezmoi/externals/.local_share_work-dots",
		"--dry-run=true",
		"init", "--apply", "https://example.com/dots.git",
		"--branch", "main",
	}
	for _, w := range wantContains {
		found := false
		for _, a := range got {
			if a == w {
				found = true
				break
			}
		}
		assert.True(t, found, "arg %q not in %v", w, got)
	}
	// Env should include CHEZMOI_EXTERNAL=1.
	hasEnv := false
	for _, e := range cobraCmd.Env {
		if e == "CHEZMOI_EXTERNAL=1" {
			hasEnv = true
			break
		}
	}
	assert.True(t, hasEnv)
}

func TestNewChezmoiExternalCmd_Apply(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	c := &Config{
		cmd:              cmd,
		DestDirAbsPath:   chezmoi.NewAbsPath("/home/u"),
		CacheDirAbsPath:  chezmoi.NewAbsPath("/home/u/.cache/chezmoi"),
		configFile:       chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
	}
	ext := &chezmoi.External{
		URL: "https://example.com/dots.git",
	}
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	cobraCmd := c.newChezmoiExternalCmd(relPath, ext, true)
	// "apply" must appear; "init" must not.
	hasApply, hasInit := false, false
	for _, a := range cobraCmd.Args {
		if a == "apply" {
			hasApply = true
		}
		if a == "init" {
			hasInit = true
		}
	}
	assert.True(t, hasApply)
	assert.False(t, hasInit)
}
```

- [ ] **Step 2: Run test — expect undefined method error**

Run: `go test ./internal/cmd/ -run TestNewChezmoiExternalCmd`
Expected: compile error.

- [ ] **Step 3: Implement `newChezmoiExternalCmd`**

Append to `internal/cmd/chezmoiexternal.go`:

```go
import (
	"os/exec"

	"chezmoi.io/chezmoi/internal/chezmoi"
)

// chezmoiExternalEnvVar is set by a parent chezmoi invocation when spawning
// a chezmoi-type external. Used as a recursion guard.
const chezmoiExternalEnvVar = "CHEZMOI_EXTERNAL"

// newChezmoiExternalCmd returns an *exec.Cmd that invokes the chezmoi binary
// to manage a chezmoi-type external. If sourceExists is false, the subprocess
// runs `chezmoi init --apply <url>`; otherwise `chezmoi apply`.
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
	cmd.Env = append(os.Environ(), chezmoiExternalEnvVar+"=1")
	return cmd
}
```

Ensure `os/exec` is imported.

- [ ] **Step 4: Run test — expect pass**

Run: `go test ./internal/cmd/ -run TestNewChezmoiExternalCmd -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cmd/chezmoiexternal.go internal/cmd/chezmoiexternal_test.go
git commit -m "feat: Add newChezmoiExternalCmd for subprocess construction"
```

---

## Task 9: Wire chezmoi-type loop into `SourceState.Read`

We need parent-side (`cmd` package) to hand a command factory to the `chezmoi` package's read path. Existing git-repo loop is inside `sourcestate.go` and inlines `exec.Command`. We cannot do that for chezmoi type because `internal/chezmoi` cannot depend on `internal/cmd`. Introduce a `SourceStateOption` that accepts a factory func.

**Files:**
- Modify: `internal/chezmoi/sourcestate.go`
- Modify: `internal/cmd/applycmd.go` (or wherever `getSourceState` lives — `config.go` actually)
- Modify: `internal/cmd/config.go`

- [ ] **Step 1: Add a factory type and option in `internal/chezmoi/sourcestate.go`**

Add near other option types (before `WithBaseSystem`):

```go
// A ChezmoiExternalCmdFunc returns an *exec.Cmd that manages a chezmoi-type
// external. sourceExists indicates whether the source dir already exists on
// disk, selecting init vs apply.
type ChezmoiExternalCmdFunc func(
	externalRelPath RelPath,
	external *External,
	sourceExists bool,
) *exec.Cmd

// WithChezmoiExternalCmdFunc sets the factory used to build subprocess
// commands for chezmoi-type externals.
func WithChezmoiExternalCmdFunc(fn ChezmoiExternalCmdFunc) SourceStateOption {
	return func(s *SourceState) {
		s.chezmoiExternalCmdFunc = fn
	}
}
```

- [ ] **Step 2: Add the field to `SourceState`**

In the `SourceState` struct (around line 122), add:

```go
	chezmoiExternalCmdFunc  ChezmoiExternalCmdFunc
```

- [ ] **Step 3: Add the chezmoi-type loop after the git-repo loop**

In `internal/chezmoi/sourcestate.go` immediately after the existing git-repo loop (around current line 1345, just before the "Check for inconsistent source entries" comment), add:

```go
	// Generate SourceStateCommands for chezmoi-type externals.
	if s.chezmoiExternalCmdFunc != nil {
		var chezmoiExternalRelPaths []RelPath
		for externalRelPath, externals := range s.externals {
			if s.Ignore(externalRelPath) {
				continue
			}
			for _, external := range externals {
				if external.Type == ExternalTypeChezmoi {
					chezmoiExternalRelPaths = append(chezmoiExternalRelPaths, externalRelPath)
				}
			}
		}
		slices.SortFunc(chezmoiExternalRelPaths, CompareRelPaths)
		for _, externalRelPath := range chezmoiExternalRelPaths {
			for _, external := range s.externals[externalRelPath] {
				sourceDir := s.destDirAbsPath.Join(externalRelPath)
				fn := s.chezmoiExternalCmdFunc
				erp := externalRelPath
				ext := external
				sourceStateCommand := &SourceStateCommand{
					cmdFunc: sync.OnceValue(func() *exec.Cmd {
						_, err := s.system.Lstat(sourceDir)
						sourceExists := err == nil
						return fn(erp, ext, sourceExists)
					}),
					origin:        external,
					forceRefresh:  options.RefreshExternals == RefreshExternalsAlways,
					refreshPeriod: external.RefreshPeriod,
					sourceAttr: SourceAttr{
						External: true,
					},
					stateBucket: ChezmoiExternalStateBucket,
				}
				allSourceStateEntries[externalRelPath] = append(
					allSourceStateEntries[externalRelPath], sourceStateCommand)
			}
		}
	}
```

- [ ] **Step 4: Wire the factory from `cmd` package**

Find where `chezmoi.NewSourceState` is called in `internal/cmd/config.go` (search for `chezmoi.WithBaseSystem`). Add to the options list:

```go
		chezmoi.WithChezmoiExternalCmdFunc(c.newChezmoiExternalCmd),
```

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 6: Run full unit test suite**

Run:
```
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o022" ./internal/chezmoi/... ./internal/cmd/... -count=1
```
Expected: PASS. Git-repo external test still passes.

- [ ] **Step 7: Commit**

```bash
git add internal/chezmoi/sourcestate.go internal/cmd/config.go
git commit -m "feat: Wire chezmoi-type externals loop with subprocess factory"
```

---

## Task 10: Directory creation before subprocess invocation

The subprocess expects parents of source/config/state/cache to exist. Add pre-creation in `newChezmoiExternalCmd`.

**Files:**
- Modify: `internal/cmd/chezmoiexternal.go`

- [ ] **Step 1: Add pre-creation logic**

Inside `newChezmoiExternalCmd`, before `cmd := exec.Command(...)`, add:

```go
	// Ensure parent dirs exist for the secondary's config/state file and the
	// cache dir itself. Source dir is created by chezmoi init.
	if sourceExists {
		// sourceDir already present; parent guaranteed.
	} else {
		_ = os.MkdirAll(sourceDir.Dir().String(), 0o700)
	}
	_ = os.MkdirAll(configFile.Dir().String(), 0o700)
	_ = os.MkdirAll(stateFile.Dir().String(), 0o700)
	_ = os.MkdirAll(cacheDir.String(), 0o700)
```

Note: we use `os.MkdirAll` directly (not `c.baseSystem`) because this runs at subprocess-invocation time on the real filesystem — not in a dry-run abstraction. Dry-run is handled by passing `--dry-run` to the child.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cmd/ -run TestNewChezmoiExternalCmd -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/cmd/chezmoiexternal.go
git commit -m "feat: Create per-external dirs before subprocess"
```

---

## Task 11: Recursion guard

Refuse chezmoi-type externals when `CHEZMOI_EXTERNAL=1` is set.

**Files:**
- Modify: `internal/chezmoi/sourcestate.go` — fail at external read-in time.

- [ ] **Step 1: Add a detection step in the chezmoi-type loop**

In `internal/chezmoi/sourcestate.go` at the top of the chezmoi-type loop added in Task 9, before the relPath collection, add:

```go
	if s.chezmoiExternalCmdFunc != nil {
		if os.Getenv("CHEZMOI_EXTERNAL") == "1" {
			// Pre-scan: if any chezmoi-type externals exist, refuse.
			for externalRelPath, externals := range s.externals {
				for _, external := range externals {
					if external.Type == ExternalTypeChezmoi {
						return fmt.Errorf(
							"%s: chezmoi-type externals cannot be nested",
							externalRelPath,
						)
					}
				}
			}
		}
		// ... (existing collection loop) ...
	}
```

Ensure `os` is imported.

Note: hoist the env check out of the per-entry loop; fail fast.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/chezmoi/sourcestate.go
git commit -m "feat: Reject nested chezmoi-type externals via CHEZMOI_EXTERNAL env var"
```

---

## Task 12: Validation warnings for incompatible fields

Warn when archive-specific / git-repo-specific fields are set on a chezmoi-type external.

**Files:**
- Modify: `internal/chezmoi/sourcestate.go`

- [ ] **Step 1: Add validation at external-parse time**

Locate `addExternal` in `internal/chezmoi/sourcestate.go` (around line 1421). After the externals map is populated, add a validation pass. Alternatively, add the pass just before external iteration in `Read` at the top of the chezmoi-type loop:

```go
	for externalRelPath, externals := range s.externals {
		for _, external := range externals {
			if external.Type != ExternalTypeChezmoi {
				continue
			}
			if external.URL == "" {
				return fmt.Errorf("%s: url is required for chezmoi-type externals", externalRelPath)
			}
			if external.Encrypted || external.Exact || external.Executable || external.Private || external.ReadOnly {
				s.warnFunc("%s: encrypted/exact/executable/private/readonly have no effect on chezmoi-type externals\n", externalRelPath)
			}
			if external.Format != "" || external.ArchivePath != "" || external.StripComponents != 0 || external.Decompress != "" {
				s.warnFunc("%s: archive-specific fields have no effect on chezmoi-type externals\n", externalRelPath)
			}
			if len(external.Clone.Args) != 0 || len(external.Pull.Args) != 0 {
				s.warnFunc("%s: clone/pull args have no effect on chezmoi-type externals (use chezmoi.init.args / chezmoi.apply.args)\n", externalRelPath)
			}
		}
	}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/chezmoi/sourcestate.go
git commit -m "feat: Validate chezmoi-type external fields and warn on incompatible settings"
```

---

## Task 13: Integration test — happy path

**Files:**
- Create: `internal/cmd/testdata/scripts/externalchezmoi.txtar`

- [ ] **Step 1: Create the txtar file**

Create `internal/cmd/testdata/scripts/externalchezmoi.txtar`:

```
[windows] skip 'UNIX only'
[!exec:git] skip 'git not found in $PATH'

mkgitconfig

# create the secondary chezmoi source as a git repo
cd $WORK/secondary
exec git init
exec git add .
exec git commit --message 'initial secondary commit'
cd $WORK

# set up the primary chezmoi source with a chezmoi-type external
expandenv $WORK/home/user/.local/share/chezmoi/.chezmoiexternal.toml

# first apply: secondary is cloned via chezmoi init --apply
exec chezmoi apply
exists $HOME/.secondaryfile
cmp $HOME/.secondaryfile golden/secondaryfile

# second apply: secondary source exists, should run apply not init
exec chezmoi apply
exists $HOME/.secondaryfile

-- golden/secondaryfile --
# contents of secondaryfile
-- home/user/.local/share/chezmoi/.chezmoiexternal.toml --
[".local/share/secondary"]
    type = "chezmoi"
    url = "file://$WORK/secondary"
-- secondary/dot_secondaryfile --
# contents of secondaryfile
```

- [ ] **Step 2: Run just this test**

Run:
```
go test ./internal/cmd/ -run TestScript -filter=externalchezmoi$
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd/testdata/scripts/externalchezmoi.txtar
git commit -m "test: Add chezmoi external type happy-path txtar test"
```

---

## Task 14: Integration test — refresh period

**Files:**
- Create: `internal/cmd/testdata/scripts/externalchezmoirefresh.txtar`

- [ ] **Step 1: Create the txtar file**

Create `internal/cmd/testdata/scripts/externalchezmoirefresh.txtar`:

```
[windows] skip 'UNIX only'
[!exec:git] skip 'git not found in $PATH'

mkgitconfig

cd $WORK/secondary
exec git init
exec git add .
exec git commit --message 'initial secondary commit'
cd $WORK

expandenv $WORK/home/user/.local/share/chezmoi/.chezmoiexternal.toml

# first apply
exec chezmoi apply
cmp $HOME/.secondaryfile golden/secondaryfile

# edit secondary
cp golden/secondaryfile2 $WORK/secondary/dot_secondaryfile
cd $WORK/secondary
exec git commit --message 'edit secondaryfile' .
cd $WORK

# within refreshPeriod — second apply should NOT pick up change
exec chezmoi apply
! grep 'version 2' $HOME/.secondaryfile

# with --refresh-externals=always — should pick up change
exec chezmoi apply --refresh-externals=always
grep 'version 2' $HOME/.secondaryfile

-- golden/secondaryfile --
# contents of secondaryfile
-- golden/secondaryfile2 --
# contents of secondaryfile — version 2
-- home/user/.local/share/chezmoi/.chezmoiexternal.toml --
[".local/share/secondary"]
    type = "chezmoi"
    url = "file://$WORK/secondary"
    refreshPeriod = "1h"
-- secondary/dot_secondaryfile --
# contents of secondaryfile
```

- [ ] **Step 2: Run test**

Run:
```
go test ./internal/cmd/ -run TestScript -filter=externalchezmoirefresh
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd/testdata/scripts/externalchezmoirefresh.txtar
git commit -m "test: Add chezmoi external refresh-period txtar test"
```

---

## Task 15: Integration test — nesting rejected

**Files:**
- Create: `internal/cmd/testdata/scripts/externalchezmoinest.txtar`

- [ ] **Step 1: Create the txtar file**

```
[windows] skip 'UNIX only'

mkgitconfig
expandenv $WORK/home/user/.local/share/chezmoi/.chezmoiexternal.toml

# simulate being invoked from a parent chezmoi
env CHEZMOI_EXTERNAL=1
! exec chezmoi apply
stderr 'chezmoi-type externals cannot be nested'

-- home/user/.local/share/chezmoi/.chezmoiexternal.toml --
[".local/share/nested"]
    type = "chezmoi"
    url = "file:///nonexistent"
```

- [ ] **Step 2: Run test**

Run:
```
go test ./internal/cmd/ -run TestScript -filter=externalchezmoinest
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd/testdata/scripts/externalchezmoinest.txtar
git commit -m "test: Add chezmoi external nesting-rejection txtar test"
```

---

## Task 16: Integration test — per-external overrides

**Files:**
- Create: `internal/cmd/testdata/scripts/externalchezmoioverride.txtar`

- [ ] **Step 1: Create the txtar file**

```
[windows] skip 'UNIX only'
[!exec:git] skip 'git not found in $PATH'

mkgitconfig

cd $WORK/secondary
exec git init
exec git add .
exec git commit --message 'initial'
cd $WORK

expandenv $WORK/home/user/.local/share/chezmoi/.chezmoiexternal.toml

exec chezmoi apply
exists $HOME/.overridefile
exists $WORK/custom/state/chezmoistate.boltdb

-- golden/overridefile --
# override contents
-- home/user/.local/share/chezmoi/.chezmoiexternal.toml --
[".local/share/secondary"]
    type = "chezmoi"
    url = "file://$WORK/secondary"
    [".local/share/secondary".chezmoi]
        stateFile = "$WORK/custom/state/chezmoistate.boltdb"
-- secondary/dot_overridefile --
# override contents
```

- [ ] **Step 2: Run test**

Run:
```
go test ./internal/cmd/ -run TestScript -filter=externalchezmoioverride
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd/testdata/scripts/externalchezmoioverride.txtar
git commit -m "test: Add chezmoi external override txtar test"
```

---

## Task 17: Integration test — passthrough flags

**Files:**
- Create: `internal/cmd/testdata/scripts/externalchezmoipassthrough.txtar`

- [ ] **Step 1: Create the txtar file**

```
[windows] skip 'UNIX only'
[!exec:git] skip 'git not found in $PATH'

mkgitconfig

cd $WORK/secondary
exec git init
exec git add .
exec git commit --message 'initial'
cd $WORK

expandenv $WORK/home/user/.local/share/chezmoi/.chezmoiexternal.toml

# --dry-run on parent must propagate to child — no file should be written
exec chezmoi apply --dry-run
! exists $HOME/.flagfile

# without --dry-run, file is created
exec chezmoi apply
exists $HOME/.flagfile

-- home/user/.local/share/chezmoi/.chezmoiexternal.toml --
[".local/share/secondary"]
    type = "chezmoi"
    url = "file://$WORK/secondary"
-- secondary/dot_flagfile --
# flagfile contents
```

- [ ] **Step 2: Run test**

Run:
```
go test ./internal/cmd/ -run TestScript -filter=externalchezmoipassthrough
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd/testdata/scripts/externalchezmoipassthrough.txtar
git commit -m "test: Add chezmoi external passthrough-flag txtar test"
```

---

## Task 18: Full test suite + lint

**Files:** none (verification only)

- [ ] **Step 1: Run both umask passes**

```
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o022" ./...
go test -ldflags="-X chezmoi.io/chezmoi/internal/chezmoitest.umaskStr=0o002" ./...
```
Expected: all PASS.

- [ ] **Step 2: Lint**

```
make ensure-golangci-lint
./bin/golangci-lint run --config=.config/golangci.yml
```
Expected: no issues. Fix any linter complaints and re-run.

- [ ] **Step 3: Format**

```
./bin/golangci-lint fmt
find . -name \*.txtar | xargs go tool lint-txtar -w
```

- [ ] **Step 4: If format changed anything, commit**

```bash
git add -u
git commit -m "style: Format per golangci-lint and lint-txtar"
```

---

## Task 19: Documentation — source-state-attributes

**Files:**
- Modify: `assets/chezmoi.io/docs/reference/source-state-attributes.md`

- [ ] **Step 1: Add a section for `type = "chezmoi"`**

Locate the existing `type = "git-repo"` section. Add below it:

```markdown
#### `type = "chezmoi"`

A chezmoi external — a secondary chezmoi source repository managed via
subprocess. On first apply, chezmoi runs `chezmoi init --apply <url>` against
the external's declared source directory; on subsequent applies, it runs
`chezmoi apply`. The secondary invocation uses an isolated config file,
persistent state file, and cache directory, but writes into the same
destination directory as the parent (typically `$HOME`).

Required fields:

- `url`: URL passed to `chezmoi init`.

Optional fields under `chezmoi.*`:

- `init.args`: extra args appended to `chezmoi init --apply <url>`.
- `apply.args`: extra args appended to `chezmoi apply`.
- `configFile`: override path for the secondary config file.
  Default: `<parentConfigDir>/externals/<slug>/chezmoi.toml`.
- `stateFile`: override path for the secondary persistent state.
  Default: `<parentConfigDir>/externals/<slug>/chezmoistate.boltdb`.
- `cacheDir`: override path for the secondary cache directory.
  Default: `<parentCacheDir>/externals/<slug>`.

`<slug>` is the external's relative path with `/`, `\`, and `:` replaced by `_`.

The following parent flags propagate to the secondary invocation when
explicitly set on the parent CLI: `--color`, `--debug`, `--dry-run`,
`--force`, `--keep-going`, `--no-tty`, `--refresh-externals`, `--verbose`.
Other flags (including `--source`, `--destination`, `--config`,
`--persistent-state`, `--cache`) are computed per-external for isolation and
are not inherited.

Chezmoi-type externals cannot be nested: a secondary chezmoi instance will
refuse to process `type = "chezmoi"` externals of its own.
```

- [ ] **Step 2: Commit**

```bash
git add assets/chezmoi.io/docs/reference/source-state-attributes.md
git commit -m "docs: Document chezmoi external type"
```

---

## Task 20: Documentation — example in user guide

**Files:**
- Modify: `assets/chezmoi.io/docs/user-guide/include-files-from-elsewhere.md`

- [ ] **Step 1: Append an example section**

Locate the existing externals examples. Append:

```markdown
### Compose multiple chezmoi repositories

Use the `chezmoi` external type to layer a secondary chezmoi repo into the
same destination directory:

```toml title="~/.local/share/chezmoi/.chezmoiexternal.toml"
[".local/share/work-dots"]
    type = "chezmoi"
    url = "https://github.com/example/work-dotfiles.git"
    refreshPeriod = "168h"
    [".local/share/work-dots".chezmoi.init]
        args = ["--branch", "main"]
```

On `chezmoi apply`, the secondary repo is cloned (first run) or applied
(subsequent runs) with its own config and state files.
```

- [ ] **Step 2: Commit**

```bash
git add assets/chezmoi.io/docs/user-guide/include-files-from-elsewhere.md
git commit -m "docs: Add example for composing multiple chezmoi repos"
```

---

## Task 21: Final verification pass

**Files:** none (verification only)

- [ ] **Step 1: Rebuild binary and smoke test**

```
go build .
./chezmoi --version
./chezmoi apply --help | grep -q 'dry-run'
```
Expected: version string prints; dry-run flag present.

- [ ] **Step 2: Run smoke-test target**

```
make smoke-test
```
Expected: PASS (or individual components pass; this runs build + test + lint + format).

- [ ] **Step 3: Confirm no stray files**

```
git status
```
Expected: clean working tree (only intentional new files from tasks).

- [ ] **Step 4: Confirm commit history is linear and descriptive**

```
git log --oneline master..HEAD
```
Expected: one commit per task (roughly 15-20 commits), all prefixed with conventional-commit type.

---

## Self-Review (writing-plans phase)

### Spec coverage

Walking each spec section:

- **Schema** → Tasks 3 (constant + structs).
- **Path derivation** → Tasks 4 (slug), 5 (paths), 5b (export), 10 (dir creation).
- **Subprocess construction** → Tasks 6 (binary), 7 (passthrough), 8 (cmd).
- **State tracking** → Tasks 1 (bucket), 2 (stateBucket field refactor), 9 (loop sets bucket).
- **Error handling** → Inherited from existing `TargetStateModifyDirWithCmd.Apply` wrapping; `--keep-going` inherited.
- **Recursion guard** → Task 11.
- **Validation warnings** → Task 12.
- **Testing** → Tasks 13-17 (one txtar per scenario from spec), Task 18 (full suite).
- **Docs** → Tasks 19-20.
- **Risks/Mitigations** → All mitigations implemented (env guard, os.Executable, separate bucket).

### Placeholder scan

No "TBD"/"TODO"/"implement later". Every code-changing step has actual code. Every command has expected output.

### Type consistency

- `ExternalTypeChezmoi` defined once (Task 3), referenced in tasks 3, 9, 11, 12.
- `ExternalChezmoi` / `ExternalChezmoiInit` / `ExternalChezmoiApply` defined in Task 3, used in Tasks 5, 8, 12.
- `ChezmoiExternalStateBucket` defined in Task 1, used in Task 9.
- `stateBucket []byte` field added to `TargetStateModifyDirWithCmd` (Task 2) and `SourceStateCommand` (Task 2), populated in Tasks 2 (git-repo backwards compat) and 9 (chezmoi type).
- `externalChezmoiPaths` signature `(relPath, ext) (configFile, stateFile, cacheDir)` consistent Tasks 5, 8.
- `newChezmoiExternalCmd(relPath, ext, sourceExists)` signature consistent Tasks 8, 9, 10.
- `chezmoiExternalCmdFunc` — SourceStateOption wires factory; signature matches `newChezmoiExternalCmd`.

### Scope

Single feature, ~21 tasks, each 2-15 minutes. One PR-sized change set.

---

## Execution notes

- Each commit should pass `go build ./...` at minimum.
- Between tasks that touch `internal/chezmoi`, run `go test ./internal/chezmoi/...` to catch refactor regressions early.
- Txtar tests (Tasks 13-17) must run on both umask passes per Makefile.
- `--filter` flag in `go test ./internal/cmd/ -run TestScript -filter=<regex>` lets you iterate on a single txtar file.
