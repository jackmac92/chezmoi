package cmd

import (
	"os"

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
	slug := chezmoi.ExternalSlug(externalRelPath)
	// getConfigFileAbsPath errors are ignored: this helper is only invoked after config resolution, so any resolution error has already surfaced.
	parentConfigFile, _ := c.getConfigFileAbsPath()
	parentConfigDir := parentConfigFile.Dir()

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

// chezmoiBinaryPath returns the path to the chezmoi binary to invoke for
// chezmoi-type externals. Falls back to "chezmoi" in PATH if os.Executable
// fails.
func (c *Config) chezmoiBinaryPath() string {
	if path, err := os.Executable(); err == nil {
		return path
	}
	return "chezmoi"
}

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
