package setup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/ghostty"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/layout"
)

const (
	fontFile   = "HerdrSidebarLogos-Regular.ttf"
	backupSufx = ".bak-ghostty-sidebar"
)

type paths struct {
	herdrConfig, ghosttyConfig, fontDir, pluginRoot, herdrBin string
	env                                                       ghostty.Env
	out                                                       io.Writer
}

func home(o Options) string {
	if o.Home != "" {
		return o.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

func resolve(o Options) paths {
	p := paths{herdrConfig: o.HerdrConfig, ghosttyConfig: o.GhosttyConfig, fontDir: o.FontDir, pluginRoot: o.PluginRoot, herdrBin: o.HerdrBin, out: o.Out}
	h := home(o)
	p.env = ghostty.Env{Home: o.Home, ConfigPath: o.GhosttyConfig}
	if p.out == nil {
		p.out = os.Stdout
	}
	if p.herdrConfig == "" {
		switch {
		case os.Getenv("HERDR_CONFIG_PATH") != "":
			p.herdrConfig = os.Getenv("HERDR_CONFIG_PATH")
		case os.Getenv("XDG_CONFIG_HOME") != "":
			p.herdrConfig = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "herdr", "config.toml")
		default:
			p.herdrConfig = filepath.Join(h, ".config", "herdr", "config.toml")
		}
	}
	if p.ghosttyConfig == "" {
		p.ghosttyConfig = p.env.FindConfig()
	}
	if p.fontDir == "" {
		if runtime.GOOS == "darwin" {
			p.fontDir = filepath.Join(h, "Library", "Fonts")
		} else {
			p.fontDir = filepath.Join(h, ".local", "share", "fonts")
		}
	}
	if p.pluginRoot == "" {
		p.pluginRoot = os.Getenv("HERDR_PLUGIN_ROOT")
	}
	if p.pluginRoot == "" {
		if exe, err := os.Executable(); err == nil {
			p.pluginRoot = filepath.Dir(filepath.Dir(exe)) // <root>/bin/<binary>
		}
	}
	if p.herdrBin == "" {
		p.herdrBin = os.Getenv("HERDR_BIN_PATH")
	}
	if p.herdrBin == "" {
		if lp, err := exec.LookPath("herdr"); err == nil {
			p.herdrBin = lp
		} else {
			p.herdrBin = NoReload
		}
	}
	return p
}

func acceptedAgents(o Options) []string {
	if o.AcceptedAgents != nil {
		return o.AcceptedAgents
	}
	dir := o.StateDir
	if dir == "" {
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			dir = filepath.Join(x, "herdr")
		} else {
			dir = filepath.Join(home(o), ".local", "state", "herdr")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "agent-detection", "remote"))
	var ids []string
	if err == nil {
		for _, e := range entries {
			if n := e.Name(); strings.HasSuffix(n, ".toml") && n != "status.toml" {
				ids = append(ids, strings.TrimSuffix(n, ".toml"))
			}
		}
	}
	if len(ids) == 0 {
		return layout.FallbackAccepted
	}
	return ids
}

func readOptional(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// backupOnce copies path to path+backupSufx unless that already exists.
func backupOnce(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	bak := path + backupSufx
	if _, err := os.Stat(bak); err == nil {
		return nil
	}
	return os.WriteFile(bak, data, 0o644)
}

// update writes new content to path (backing up first) or, on dry run,
// prints the diff. It returns whether content differs.
func update(p paths, dry bool, path, old, new string) (bool, error) {
	if old == new {
		return false, nil
	}
	if dry {
		fmt.Fprintf(p.out, "--- %s\n+++ %s\n%s\n", path, path, diff(old, new))
		return true, nil
	}
	if err := backupOnce(path); err != nil {
		return true, fmt.Errorf("backup %s: %w", path, err)
	}
	return true, writeFile(path, new)
}

// diff is a small line diff (LCS) with context-free +/- output.
func diff(a, b string) string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	n, m := len(x), len(y)
	l := make([][]int, n+1)
	for i := range l {
		l[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else if l[i+1][j] >= l[i][j+1] {
				l[i][j] = l[i+1][j]
			} else {
				l[i][j] = l[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			i++
			j++
		case l[i+1][j] >= l[i][j+1]:
			out = append(out, "-"+x[i])
			i++
		default:
			out = append(out, "+"+y[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "-"+x[i])
	}
	for ; j < m; j++ {
		out = append(out, "+"+y[j])
	}
	return strings.Join(out, "\n")
}

func reload(p paths) error {
	if p.herdrBin == NoReload {
		return nil
	}
	out, err := exec.Command(p.herdrBin, "server", "reload-config").CombinedOutput()
	if err != nil {
		return fmt.Errorf("herdr server reload-config: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func herdrBlock(o Options, p paths) (string, error) {
	app := o.Appearance
	if app == "" {
		app = ghostty.DetectAppearance()
	}
	pal, err := p.env.Resolve(app)
	if err != nil {
		fmt.Fprintf(p.out, "warning: %v; using fallback colours\n", err)
	}
	return layout.HerdrBlock(pal, acceptedAgents(o)), nil
}

func syncLayout(o Options) (bool, error) {
	p := resolve(o)
	p.out = io.Discard // runs on every event; stay quiet
	old, err := readOptional(p.herdrConfig)
	if err != nil {
		return false, err
	}
	if !strings.Contains(old, layout.StartMarker) {
		return false, nil // not set up
	}
	block, _ := herdrBlock(o, p)
	if _, err := layout.CheckHerdr(old); err != nil {
		return false, err
	}
	changed, err := update(p, o.DryRun, p.herdrConfig, old, layout.Insert(old, block))
	if err != nil || !changed || o.DryRun {
		return changed, err
	}
	return true, reload(p)
}

func installFont(p paths, dry bool) error {
	src := filepath.Join(p.pluginRoot, "assets", fontFile)
	dst := filepath.Join(p.fontDir, fontFile)
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("logo font: %w", err)
	}
	if cur, err := os.ReadFile(dst); err == nil && string(cur) == string(data) {
		return nil
	}
	if dry {
		fmt.Fprintf(p.out, "would copy %s -> %s\n", src, dst)
		return nil
	}
	if err := os.MkdirAll(p.fontDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		if fc, err := exec.LookPath("fc-cache"); err == nil {
			_ = exec.Command(fc, "-f", p.fontDir).Run()
		}
	}
	return nil
}

func setup(o Options) error {
	p := resolve(o)
	herdrOld, err := readOptional(p.herdrConfig)
	if err != nil {
		return err
	}
	warns, err := layout.CheckHerdr(herdrOld)
	if err != nil {
		return err
	}
	for _, w := range warns {
		fmt.Fprintf(p.out, "warning: %s\n", w)
	}
	ghOld, err := readOptional(p.ghosttyConfig)
	if err != nil {
		return err
	}
	block, _ := herdrBlock(o, p)

	if err := installFont(p, o.DryRun); err != nil {
		return err
	}
	if _, err := update(p, o.DryRun, p.ghosttyConfig, ghOld, layout.Insert(ghOld, layout.GhosttyBlock())); err != nil {
		return err
	}
	changed, err := update(p, o.DryRun, p.herdrConfig, herdrOld, layout.Insert(herdrOld, block))
	if err != nil {
		return err
	}
	if o.DryRun {
		fmt.Fprintln(p.out, "dry run: nothing written")
		return nil
	}
	fmt.Fprintf(p.out, "installed: font in %s, Ghostty block in %s, Herdr block in %s\n", p.fontDir, p.ghosttyConfig, p.herdrConfig)
	fmt.Fprintln(p.out, "restart Ghostty to pick up the font mapping")
	if changed {
		return reload(p)
	}
	return nil
}

func uninstall(o Options) error {
	p := resolve(o)
	for _, f := range []string{p.ghosttyConfig, p.herdrConfig} {
		old, err := readOptional(f)
		if err != nil {
			return err
		}
		if _, err := update(p, o.DryRun, f, old, layout.Remove(old)); err != nil {
			return err
		}
	}
	font := filepath.Join(p.fontDir, fontFile)
	if _, err := os.Stat(font); err == nil {
		if o.DryRun {
			fmt.Fprintf(p.out, "would remove %s\n", font)
		} else if err := os.Remove(font); err != nil {
			return err
		}
	}
	if o.DryRun {
		return nil
	}
	return reload(p)
}
