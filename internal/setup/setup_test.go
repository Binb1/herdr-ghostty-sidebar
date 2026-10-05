package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/layout"
)

type env struct {
	opts                         Options
	herdr, ghostty, fontDir, bin string
	out                          *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{out: &bytes.Buffer{}}
	e.herdr = filepath.Join(root, "herdr", "config.toml")
	e.ghostty = filepath.Join(root, "ghostty", "config")
	e.fontDir = filepath.Join(root, "fonts")
	plugin := filepath.Join(root, "plugin")
	must(t, os.MkdirAll(filepath.Join(plugin, "assets"), 0o755))
	must(t, os.WriteFile(filepath.Join(plugin, "assets", fontFile), []byte("FONT"), 0o644))
	must(t, os.MkdirAll(filepath.Dir(e.herdr), 0o755))
	must(t, os.MkdirAll(filepath.Join(root, "ghostty", "themes"), 0o755))
	must(t, os.WriteFile(e.herdr, []byte("[ui]\nagent_panel_sort = \"spaces\"\n"), 0o644))
	must(t, os.WriteFile(e.ghostty, []byte("theme = light:L,dark:D\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "ghostty", "themes", "D"), []byte("foreground = #111111\npalette = 8=#222222\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "ghostty", "themes", "L"), []byte("foreground = #eeeeee\npalette = 8=#dddddd\n"), 0o644))
	// fake herdr that records reload calls
	e.bin = filepath.Join(root, "fake-herdr")
	log := filepath.Join(root, "reloads")
	must(t, os.WriteFile(e.bin, []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o755))
	e.opts = Options{HerdrConfig: e.herdr, GhosttyConfig: e.ghostty, FontDir: e.fontDir, PluginRoot: plugin,
		HerdrBin: e.bin, Home: root, Appearance: "dark", AcceptedAgents: []string{"claude", "codex"}, Out: e.out}
	return e
}

func (e *env) reloads() int {
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(e.bin), "reloads"))
	return strings.Count(string(b), "server reload-config")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

func TestSetupSyncUninstall(t *testing.T) {
	e := newEnv(t)
	herdrOrig, ghOrig := read(t, e.herdr), read(t, e.ghostty)

	must(t, Setup(e.opts))
	h := read(t, e.herdr)
	if !strings.Contains(h, `fg = "#111111"`) || !strings.Contains(h, "claude = [") || strings.Contains(h, "hermes = [") {
		t.Errorf("herdr block:\n%s", h)
	}
	if !strings.Contains(read(t, e.ghostty), "font-codepoint-map = U+E1A0-U+E1AB=Herdr Sidebar Logos") {
		t.Error("ghostty block missing")
	}
	if read(t, filepath.Join(e.fontDir, fontFile)) != "FONT" {
		t.Error("font not installed")
	}
	if read(t, e.herdr+backupSufx) != herdrOrig || read(t, e.ghostty+backupSufx) != ghOrig {
		t.Error("backups wrong")
	}
	if e.reloads() != 1 {
		t.Errorf("reloads = %d", e.reloads())
	}

	// Setup twice: identical, backup unchanged, no extra reload.
	must(t, Setup(e.opts))
	if read(t, e.herdr) != h || read(t, e.herdr+backupSufx) != herdrOrig || e.reloads() != 1 {
		t.Error("second setup not idempotent")
	}

	// Sync: no change when appearance same; rewrites on appearance switch.
	changed, err := SyncLayout(e.opts)
	if err != nil || changed {
		t.Errorf("sync same: %v %v", changed, err)
	}
	e.opts.Appearance = "light"
	changed, err = SyncLayout(e.opts)
	if err != nil || !changed || !strings.Contains(read(t, e.herdr), `fg = "#eeeeee"`) || e.reloads() != 2 {
		t.Errorf("sync light: %v %v reloads=%d", changed, err, e.reloads())
	}

	must(t, Uninstall(e.opts))
	if read(t, e.herdr) != herdrOrig || read(t, e.ghostty) != ghOrig {
		t.Error("uninstall did not restore")
	}
	if _, err := os.Stat(filepath.Join(e.fontDir, fontFile)); err == nil {
		t.Error("font not removed")
	}
}

func TestSyncDoesNothingBeforeSetup(t *testing.T) {
	e := newEnv(t)
	changed, err := SyncLayout(e.opts)
	if changed || err != nil || e.reloads() != 0 {
		t.Error("sync before setup should be a no-op")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.opts.DryRun = true
	must(t, Setup(e.opts))
	if read(t, e.herdr) != "[ui]\nagent_panel_sort = \"spaces\"\n" || e.reloads() != 0 {
		t.Error("dry run wrote")
	}
	if _, err := os.Stat(e.fontDir); err == nil {
		t.Error("font dir created")
	}
	if _, err := os.Stat(e.herdr + backupSufx); err == nil {
		t.Error("backup created")
	}
	if !strings.Contains(e.out.String(), "+[ui.sidebar.agents]") {
		t.Errorf("plan missing: %s", e.out)
	}
}

func TestRefusesExistingTables(t *testing.T) {
	e := newEnv(t)
	must(t, os.WriteFile(e.herdr, []byte("[ui.sidebar.agents]\nrows = []\n"), 0o644))
	if err := Setup(e.opts); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(e.fontDir, fontFile)); err == nil {
		t.Error("wrote font despite refusal")
	}
	_ = layout.StartMarker
}

func TestAcceptedAgentsFromStateDir(t *testing.T) {
	d := t.TempDir()
	r := filepath.Join(d, "agent-detection", "remote")
	must(t, os.MkdirAll(r, 0o755))
	for _, n := range []string{"claude.toml", "hermes.toml", "status.toml"} {
		must(t, os.WriteFile(filepath.Join(r, n), nil, 0o644))
	}
	got := acceptedAgents(Options{StateDir: d})
	if strings.Join(got, ",") != "claude,hermes" {
		t.Errorf("%v", got)
	}
	if strings.Join(acceptedAgents(Options{StateDir: t.TempDir()}), ",") != "claude,codex" {
		t.Error("fallback")
	}
}
