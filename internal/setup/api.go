// Package setup owns everything that writes user config: the sidebar layout
// block in Herdr's config.toml, the font mapping block in Ghostty's config,
// and the logo font file.
package setup

import (
	"io"
)

// NoReload as HerdrBin skips `herdr server reload-config`.
const NoReload = "-"

// Options carries paths; empty fields mean "detect the default".
type Options struct {
	HerdrConfig   string // Herdr config.toml
	GhosttyConfig string // Ghostty config file
	FontDir       string // where the logo font is installed
	PluginRoot    string // plugin checkout (for assets/)
	HerdrBin      string // herdr binary used for `server reload-config`; NoReload skips
	DryRun        bool   // print planned changes, write nothing

	// Optional overrides, mainly for tests.
	Home           string    // replaces $HOME for default paths
	Appearance     string    // "dark"/"light"; empty detects from the OS
	StateDir       string    // Herdr state dir holding agent-detection/remote
	AcceptedAgents []string  // rows_by_agent ids Herdr accepts; overrides StateDir lookup
	Out            io.Writer // where plans/warnings go (default os.Stdout)
}

// SyncLayout regenerates the Herdr layout block for the current macOS/Linux
// appearance and Ghostty theme. It writes and reloads Herdr's config only
// when the block content changed, and reports whether it did. Cheap enough
// to call on every event. It only maintains an existing block: until Setup
// has installed one it does nothing.
func SyncLayout(opts Options) (changed bool, err error) { return syncLayout(opts) }

// Setup installs the font, writes the Ghostty font block and the Herdr
// layout block (with backups), then reloads Herdr's config.
func Setup(opts Options) error { return setup(opts) }

// Uninstall removes both managed blocks and the font.
func Uninstall(opts Options) error { return uninstall(opts) }
