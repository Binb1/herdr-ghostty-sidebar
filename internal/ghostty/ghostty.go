// Package ghostty finds the user's Ghostty config, resolves the active theme
// (light or dark) and reads its palette.
package ghostty

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	Dark  = "dark"
	Light = "light"
)

// Palette is the resolved colour set. Colours are lowercase "#rrggbb".
type Palette struct {
	Colors     [16]string
	Foreground string
	Background string
	Theme      string // theme name that was resolved ("" if none)
}

// Color returns palette entry n.
func (p Palette) Color(n int) string { return p.Colors[n] }

// DefaultPalette is the fallback used for anything a theme leaves unset
// (a dark, xterm-like set).
func DefaultPalette() Palette {
	return Palette{
		Colors: [16]string{
			"#1d1f21", "#cc6666", "#b5bd68", "#f0c674", "#81a2be", "#b294bb", "#8abeb7", "#c5c8c6",
			"#666666", "#d54e53", "#b9ca4a", "#e7c547", "#7aa6da", "#c397d8", "#70c0b1", "#eaeaea",
		},
		Foreground: "#c5c8c6",
		Background: "#1d1f21",
	}
}

// Env bundles everything path-related so tests can use temp dirs.
type Env struct {
	Home          string   // default $HOME
	XDGConfigHome string   // default $XDG_CONFIG_HOME
	ConfigPath    string   // explicit config file (skips discovery)
	ThemeDirs     []string // extra theme dirs searched first
}

func (e Env) home() string {
	if e.Home != "" {
		return e.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

func (e Env) xdg() string {
	if e.XDGConfigHome != "" {
		return e.XDGConfigHome
	}
	return os.Getenv("XDG_CONFIG_HOME")
}

// ConfigCandidates lists the config locations in priority order.
func (e Env) ConfigCandidates() []string {
	var c []string
	if x := e.xdg(); x != "" {
		c = append(c, filepath.Join(x, "ghostty", "config"))
	}
	h := e.home()
	c = append(c,
		filepath.Join(h, ".config", "ghostty", "config"),
		filepath.Join(h, ".config", "ghostty", "config.ghostty"),
		filepath.Join(h, "Library", "Application Support", "com.mitchellh.ghostty", "config"),
	)
	return c
}

// FindConfig returns the Ghostty config path: the explicit one, else the
// first existing candidate containing a `theme =` line, else the first
// existing candidate, else the primary default location (which may not exist).
func (e Env) FindConfig() string {
	if e.ConfigPath != "" {
		return e.ConfigPath
	}
	cands := e.ConfigCandidates()
	firstExisting := ""
	for _, p := range cands {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if firstExisting == "" {
			firstExisting = p
		}
		if l, d := ParseThemeSetting(string(data)); l != "" || d != "" {
			return p
		}
	}
	if firstExisting != "" {
		return firstExisting
	}
	return cands[0]
}

// Pair is one parsed `key = value` line.
type pair struct{ key, value string }

func parseLines(text string) []pair {
	var out []pair
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out = append(out, pair{strings.TrimSpace(k), unquote(strings.TrimSpace(v))})
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// ParseThemeSetting returns the light and dark theme names from the last
// `theme =` line. `theme = A` yields A for both; `light:A,dark:B` is split.
// A mode missing from a light:/dark: pair falls back to the other one.
func ParseThemeSetting(text string) (light, dark string) {
	val := ""
	for _, p := range parseLines(text) {
		if p.key == "theme" {
			val = p.value
		}
	}
	if val == "" {
		return "", ""
	}
	if !strings.Contains(val, "light:") && !strings.Contains(val, "dark:") {
		return val, val
	}
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "light:"):
			light = unquote(strings.TrimSpace(strings.TrimPrefix(part, "light:")))
		case strings.HasPrefix(part, "dark:"):
			dark = unquote(strings.TrimSpace(strings.TrimPrefix(part, "dark:")))
		}
	}
	if light == "" {
		light = dark
	}
	if dark == "" {
		dark = light
	}
	return light, dark
}

var hexRe = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

func normHex(s string) (string, bool) {
	m := hexRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	return "#" + strings.ToLower(m[1]), true
}

// applyColors overlays palette/foreground/background lines onto p.
func applyColors(p *Palette, text string) {
	for _, kv := range parseLines(text) {
		switch kv.key {
		case "foreground":
			if h, ok := normHex(kv.value); ok {
				p.Foreground = h
			}
		case "background":
			if h, ok := normHex(kv.value); ok {
				p.Background = h
			}
		case "palette":
			n, c, ok := strings.Cut(kv.value, "=")
			if !ok {
				continue
			}
			idx, err := strconv.Atoi(strings.TrimSpace(n))
			if err != nil || idx < 0 || idx > 15 {
				continue
			}
			if h, ok := normHex(c); ok {
				p.Colors[idx] = h
			}
		}
	}
}

// ThemeSearchDirs returns the directories searched for theme files.
func (e Env) ThemeSearchDirs(configPath string) []string {
	dirs := append([]string{}, e.ThemeDirs...)
	if x := e.xdg(); x != "" {
		dirs = append(dirs, filepath.Join(x, "ghostty", "themes"))
	}
	dirs = append(dirs,
		filepath.Join(e.home(), ".config", "ghostty", "themes"),
		filepath.Join(filepath.Dir(configPath), "themes"),
		"/Applications/Ghostty.app/Contents/Resources/ghostty/themes",
		"/usr/share/ghostty/themes",
	)
	return dirs
}

// FindTheme locates a theme file by name (or absolute path).
func (e Env) FindTheme(name, configPath string) (string, error) {
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err != nil {
			return "", err
		}
		return name, nil
	}
	for _, d := range e.ThemeSearchDirs(configPath) {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("ghostty theme %q not found", name)
}

// Resolve reads the Ghostty config for the given appearance (Dark/Light) and
// returns its palette: defaults, then the theme file, then palette/
// foreground/background overrides from the main config. A missing config or
// theme is not an error — defaults are returned — except that a named theme
// that cannot be found returns the defaults together with an error so callers
// can warn.
func (e Env) Resolve(appearance string) (Palette, error) {
	p := DefaultPalette()
	if appearance == Light {
		p = lightDefault()
	}
	cfgPath := e.FindConfig()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return p, nil
		}
		return p, err
	}
	text := string(data)
	light, dark := ParseThemeSetting(text)
	name := dark
	if appearance == Light {
		name = light
	}
	var retErr error
	if name != "" {
		p.Theme = name
		if tp, err := e.FindTheme(name, cfgPath); err != nil {
			retErr = err
		} else if td, err := os.ReadFile(tp); err != nil {
			retErr = err
		} else {
			applyColors(&p, string(td))
		}
	}
	applyColors(&p, text)
	return p, retErr
}

func lightDefault() Palette {
	return Palette{
		Colors: [16]string{
			"#5c6370", "#c91b00", "#00a600", "#a67c00", "#0225c7", "#ca30c7", "#00a5a5", "#8a8a8a",
			"#666666", "#e0392b", "#1fa51f", "#b58900", "#3b5bdb", "#d94bd6", "#1ab5b5", "#444444",
		},
		Foreground: "#2e2e2e",
		Background: "#ffffff",
	}
}

// runCmd is swapped in tests.
var runCmd = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// DetectAppearance reports the OS appearance: macOS via AppleInterfaceStyle,
// Linux via gsettings color-scheme, else Dark.
func DetectAppearance() string { return detectAppearance(runtime.GOOS) }

func detectAppearance(goos string) string {
	switch goos {
	case "darwin":
		out, _ := runCmd("defaults", "read", "-g", "AppleInterfaceStyle")
		if strings.Contains(strings.ToLower(out), "dark") {
			return Dark
		}
		return Light // key is absent in light mode
	case "linux":
		out, err := runCmd("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
		if err != nil {
			return Dark
		}
		if strings.Contains(strings.ToLower(out), "dark") {
			return Dark
		}
		if strings.Contains(strings.ToLower(out), "light") || strings.Contains(out, "default") {
			return Light
		}
		return Dark
	}
	return Dark
}
