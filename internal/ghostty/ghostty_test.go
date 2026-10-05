package ghostty

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseThemeSetting(t *testing.T) {
	cases := []struct{ in, l, d string }{
		{"theme = light:A,dark:B\n", "A", "B"},
		{"theme=\"Cat Mocha\"\n", "Cat Mocha", "Cat Mocha"},
		{"# theme = x\ntheme = dark:only\n", "only", "only"},
		{"theme = light:\"X Y\", dark:Z\n", "X Y", "Z"},
		{"font-size = 12\n", "", ""},
	}
	for _, c := range cases {
		l, d := ParseThemeSetting(c.in)
		if l != c.l || d != c.d {
			t.Errorf("%q: got %q/%q want %q/%q", c.in, l, d, c.l, c.d)
		}
	}
}

func TestResolveOverrides(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "ghostty", "config")
	write(t, cfg, "theme = light:L,dark:D\nforeground = #AABBCC\npalette = 3=#010203\n")
	write(t, filepath.Join(home, ".config", "ghostty", "themes", "D"),
		"background = #101010\nforeground = #ffffff\npalette = 1=#ff0000\npalette = 3=#999999\n")
	write(t, filepath.Join(home, ".config", "ghostty", "themes", "L"), "background = #fafafa\npalette = 2=#00ff00\n")
	e := Env{Home: home, XDGConfigHome: filepath.Join(home, "nope")}
	if got := e.FindConfig(); got != cfg {
		t.Fatalf("config = %s", got)
	}
	p, err := e.Resolve(Dark)
	if err != nil {
		t.Fatal(err)
	}
	if p.Colors[1] != "#ff0000" || p.Colors[3] != "#010203" || p.Foreground != "#aabbcc" || p.Background != "#101010" || p.Theme != "D" {
		t.Errorf("dark: %+v", p)
	}
	p, _ = e.Resolve(Light)
	if p.Colors[2] != "#00ff00" || p.Background != "#fafafa" || p.Theme != "L" {
		t.Errorf("light: %+v", p)
	}
}

func TestMissingThemeFallsBack(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".config", "ghostty", "config"), "theme = ghost\n")
	p, err := Env{Home: home}.Resolve(Dark)
	if err == nil {
		t.Error("expected error for missing theme")
	}
	if p.Colors[1] == "" || p.Foreground == "" {
		t.Error("defaults not returned")
	}
}

func TestPrefersConfigWithTheme(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, ".config", "ghostty", "config")
	b := filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config")
	write(t, a, "# nothing\n")
	write(t, b, "theme = X\n")
	if got := (Env{Home: home, XDGConfigHome: filepath.Join(home, "x")}).FindConfig(); got != b {
		t.Errorf("got %s want %s", got, b)
	}
}

func TestDetectAppearance(t *testing.T) {
	old := runCmd
	defer func() { runCmd = old }()
	runCmd = func(string, ...string) (string, error) { return "Dark\n", nil }
	if detectAppearance("darwin") != Dark {
		t.Error("darwin dark")
	}
	runCmd = func(string, ...string) (string, error) { return "", os.ErrNotExist }
	if detectAppearance("darwin") != Light {
		t.Error("darwin light")
	}
	runCmd = func(string, ...string) (string, error) { return "'prefer-dark'\n", nil }
	if detectAppearance("linux") != Dark {
		t.Error("linux dark")
	}
	runCmd = func(string, ...string) (string, error) { return "'default'\n", nil }
	if detectAppearance("linux") != Light {
		t.Error("linux light")
	}
	runCmd = func(string, ...string) (string, error) { return "", os.ErrNotExist }
	if detectAppearance("linux") != Dark {
		t.Error("linux fallback")
	}
}

// Copies the user's real Ghostty config and themes (read-only) into a temp dir.
func TestRealUserFilesCopy(t *testing.T) {
	home, _ := os.UserHomeDir()
	src := filepath.Join(home, ".config", "ghostty")
	cfg, err := os.ReadFile(filepath.Join(src, "config"))
	if err != nil {
		t.Skip("no real ghostty config")
	}
	tmp := t.TempDir()
	write(t, filepath.Join(tmp, ".config", "ghostty", "config"), string(cfg))
	entries, _ := os.ReadDir(filepath.Join(src, "themes"))
	for _, en := range entries {
		b, err := os.ReadFile(filepath.Join(src, "themes", en.Name()))
		if err == nil {
			write(t, filepath.Join(tmp, ".config", "ghostty", "themes", en.Name()), string(b))
		}
	}
	l, d := ParseThemeSetting(string(cfg))
	if l != "latte-custom" || d != "mocha-custom" {
		t.Skipf("user theme changed: %s/%s", l, d)
	}
	e := Env{Home: tmp, XDGConfigHome: filepath.Join(tmp, "none")}
	pd, err := e.Resolve(Dark)
	if err != nil || pd.Theme != "mocha-custom" || pd.Foreground != "#eff1f8" || pd.Colors[8] != "#585b70" || pd.Colors[1] != "#fd7298" {
		t.Errorf("dark %+v err=%v", pd, err)
	}
	pl, err := e.Resolve(Light)
	if err != nil || pl.Theme != "latte-custom" || pl.Foreground == "" || pl.Colors[8] == "" {
		t.Errorf("light %+v err=%v", pl, err)
	}
}
