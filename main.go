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

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/render"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/setup"
)

var version = "0.1.0"

const usage = `usage: herdr-ghostty-sidebar <command>

  render [--force]   sync the layout block, then report sidebar tokens
  animate            spin the working marks until nothing works (started by render)
  setup              install font, Ghostty mapping and Herdr layout block
  uninstall          remove everything setup wrote and clear all tokens
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
		return render.Animate(client, stateDir(), render.AnimateOpts{})
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
