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
	slug := chezmoi.ExternalSlug(externalRelPath)
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
