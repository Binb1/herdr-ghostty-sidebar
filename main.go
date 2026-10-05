// herdr-ghostty-sidebar restyles Herdr's Agents and Spaces sidebar with vendor
// logos and state colours taken from the Ghostty theme.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/claude"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/render"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/setup"
)

var version = "0.1.1"

const usage = `usage: herdr-ghostty-sidebar <command>

  render [--force]   sync the layout block, then report sidebar tokens
  animate            spin the working marks until nothing works (started by render)
  setup              install font, Ghostty mapping and Herdr layout block
  uninstall          remove everything setup wrote and clear all tokens
  claude-hook        Claude Code hook: show the subagent line (reads hook JSON on stdin)
  claude-install     register the hook in ~/.claude/settings.json
  claude-uninstall   remove it again
  version            print the version

setup/uninstall/render accept --dry-run (setup only), --herdr-config,
--ghostty-config, --font-dir and --appearance to override detected values.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ghostty-sidebar %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "claude-hook":
		claudeHook() // never fails, never prints
		return nil
	case "claude-install":
		return claude.Install(claude.Paths{PluginRoot: pluginRoot()})
	case "claude-uninstall":
		return claude.Uninstall(claude.Paths{PluginRoot: pluginRoot()})
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	opts := setup.Options{
		HerdrConfig: os.Getenv("HERDR_CONFIG_PATH"),
		PluginRoot:  pluginRoot(),
		HerdrBin:    os.Getenv("HERDR_BIN_PATH"),
	}
	fs.StringVar(&opts.HerdrConfig, "herdr-config", opts.HerdrConfig, "Herdr config.toml")
	fs.StringVar(&opts.GhosttyConfig, "ghostty-config", "", "Ghostty config file")
	fs.StringVar(&opts.FontDir, "font-dir", "", "font install directory")
	fs.StringVar(&opts.Appearance, "appearance", "", "light or dark (default: detect from the OS)")
	force := fs.Bool("force", false, "report every token, ignoring the cache")
	dryRun := fs.Bool("dry-run", false, "print planned changes, write nothing")
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "render", "animate", "setup", "uninstall":
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts.DryRun = *dryRun

	switch cmd {
	case "render":
		if _, err := setup.SyncLayout(opts); err != nil {
			fmt.Fprintf(os.Stderr, "herdr-ghostty-sidebar: layout sync: %v\n", err)
		}
		client, err := herdr.NewClient()
		if err != nil {
			return err
		}
		working, err := render.RunWorking(client, stateDir(), *force)
		if working && !render.AnimateRunning(stateDir()) {
			if serr := spawnAnimate(); serr != nil {
				fmt.Fprintf(os.Stderr, "herdr-ghostty-sidebar: start animate: %v\n", serr)
			}
		}
		return err
	case "animate":
		client, err := herdr.NewClient()
		if err != nil {
			return err
		}
		return render.Animate(client, stateDir(), render.AnimateOpts{Sync: func() {
			_, _ = setup.SyncLayout(opts) // no-op unless appearance/theme changed
		}})
	case "setup":
		return setup.Setup(opts)
	default: // uninstall
		if err := setup.Uninstall(opts); err != nil {
			return err
		}
		if opts.DryRun {
			return nil
		}
		client, err := herdr.NewClient()
		if err != nil {
			return err
		}
		return render.ClearAll(client, stateDir())
	}
}

// claudeHook handles one Claude Code hook event. Hook output is shown to the
// user by Claude Code, so every error is swallowed.
func claudeHook() {
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_PANE_ID") == "" {
		return
	}
	client, err := herdr.NewClient()
	if err != nil {
		return
	}
	client.Timeout = 2 * time.Second
	_ = claude.Handle(os.Stdin, claude.Env{
		HerdrEnv: os.Getenv("HERDR_ENV"),
		PaneID:   os.Getenv("HERDR_PANE_ID"),
		StateDir: hookStateDir(),
		Now:      time.Now(),
	}, client)
}

// hookStateDir is stable without HERDR_PLUGIN_STATE_DIR, which Claude Code
// hooks do not get.
func hookStateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "herdr-ghostty-sidebar")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "state", "herdr-ghostty-sidebar")
	}
	return stateDir()
}

// spawnAnimate starts "animate" in its own session with stdio detached and
// does not wait for it, so the event hook returns at once.
func spawnAnimate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd := exec.Command(exe, "animate")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func stateDir() string {
	if d := os.Getenv("HERDR_PLUGIN_STATE_DIR"); d != "" {
		return d
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return filepath.Join(cache, "herdr-ghostty-sidebar")
}

// pluginRoot is HERDR_PLUGIN_ROOT, else the directory above bin/.
func pluginRoot() string {
	if r := os.Getenv("HERDR_PLUGIN_ROOT"); r != "" {
		return r
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		return filepath.Dir(filepath.Dir(exe))
	}
	return ""
}
