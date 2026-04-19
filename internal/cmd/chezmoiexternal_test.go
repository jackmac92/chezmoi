package cmd

import (
	"testing"

	"github.com/alecthomas/assert/v2"

	"chezmoi.io/chezmoi/internal/chezmoi"
)

func TestExternalChezmoiPaths_Defaults(t *testing.T) {
	c := &Config{
		customConfigFileAbsPath: chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
	}
	c.CacheDirAbsPath = chezmoi.NewAbsPath("/home/u/.cache/chezmoi")
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	ext := &chezmoi.External{}
	configFile, stateFile, cacheDir := c.externalChezmoiPaths(relPath, ext)
	assert.Equal(t, "/home/u/.config/chezmoi/externals/.local_share_work-dots/chezmoi.toml", configFile.String())
	assert.Equal(t, "/home/u/.config/chezmoi/externals/.local_share_work-dots/chezmoistate.boltdb", stateFile.String())
	assert.Equal(t, "/home/u/.cache/chezmoi/externals/.local_share_work-dots", cacheDir.String())
}

func TestExternalChezmoiPaths_Override(t *testing.T) {
	c := &Config{
		customConfigFileAbsPath: chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
	}
	c.CacheDirAbsPath = chezmoi.NewAbsPath("/home/u/.cache/chezmoi")
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

func TestExternalChezmoiPaths_PartialOverride(t *testing.T) {
	c := &Config{
		customConfigFileAbsPath: chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
	}
	c.CacheDirAbsPath = chezmoi.NewAbsPath("/home/u/.cache/chezmoi")
	relPath := chezmoi.NewRelPath("work-dots")
	ext := &chezmoi.External{
		Chezmoi: chezmoi.ExternalChezmoi{
			ConfigFile: chezmoi.NewAbsPath("/custom/config.toml"),
		},
	}
	configFile, stateFile, cacheDir := c.externalChezmoiPaths(relPath, ext)
	assert.Equal(t, "/custom/config.toml", configFile.String())
	assert.Equal(t, "/home/u/.config/chezmoi/externals/work-dots/chezmoistate.boltdb", stateFile.String())
	assert.Equal(t, "/home/u/.cache/chezmoi/externals/work-dots", cacheDir.String())
}

func TestChezmoiBinaryPath_NonEmpty(t *testing.T) {
	c := &Config{}
	got := c.chezmoiBinaryPath()
	assert.NotEqual(t, "", got)
}
