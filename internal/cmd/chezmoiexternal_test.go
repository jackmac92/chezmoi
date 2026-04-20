package cmd

import (
	"slices"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/spf13/cobra"

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
	assert.NotZero(t, got)
}

func TestPassthroughFlags_OnlyChangedIncluded(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	fs := cmd.PersistentFlags()
	fs.Bool("dry-run", false, "")
	fs.Bool("verbose", false, "")
	fs.Bool("force", false, "")
	fs.String("color", "auto", "")
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
	assert.Equal(t, []string{"--force=true", "--verbose=true"}, args)
}

func TestNewChezmoiExternalCmd_Init(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	fs := cmd.PersistentFlags()
	fs.Bool("dry-run", false, "")
	must(fs.Set("dry-run", "true"))

	c := &Config{
		cmd:                     cmd,
		customConfigFileAbsPath: chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
		stdin:                   nil,
		stdout:                  nil,
		stderr:                  nil,
	}
	c.DestDirAbsPath = chezmoi.NewAbsPath("/home/u")
	c.CacheDirAbsPath = chezmoi.NewAbsPath("/home/u/.cache/chezmoi")
	ext := &chezmoi.External{
		URL: "https://example.com/dots.git",
		Chezmoi: chezmoi.ExternalChezmoi{
			Init: chezmoi.ExternalChezmoiInit{Args: []string{"--branch", "main"}},
		},
	}
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	cobraCmd := c.newChezmoiExternalCmd(relPath, ext, false)

	got := cobraCmd.Args
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
		assert.True(t, slices.Contains(got, w), "arg %q not in %v", w, got)
	}
	assert.True(t, slices.Contains(cobraCmd.Env, "CHEZMOI_EXTERNAL=1"))
}

func TestNewChezmoiExternalCmd_Apply(t *testing.T) {
	cmd := &cobra.Command{Use: "chezmoi"}
	c := &Config{
		cmd:                     cmd,
		customConfigFileAbsPath: chezmoi.NewAbsPath("/home/u/.config/chezmoi/chezmoi.toml"),
	}
	c.DestDirAbsPath = chezmoi.NewAbsPath("/home/u")
	c.CacheDirAbsPath = chezmoi.NewAbsPath("/home/u/.cache/chezmoi")
	ext := &chezmoi.External{
		URL: "https://example.com/dots.git",
	}
	relPath := chezmoi.NewRelPath(".local/share/work-dots")
	cobraCmd := c.newChezmoiExternalCmd(relPath, ext, true)
	hasUpdate, hasInit := false, false
	for _, a := range cobraCmd.Args {
		if a == "update" {
			hasUpdate = true
		}
		if a == "init" {
			hasInit = true
		}
	}
	assert.True(t, hasUpdate)
	assert.False(t, hasInit)
}
