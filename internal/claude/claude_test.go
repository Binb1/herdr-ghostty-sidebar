package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

type call struct {
	pane, source string
	value        *string
	ttl          time.Duration
}

type fake struct{ calls []call }

func (f *fake) ReportPaneTokensTTL(pane, source string, t map[string]*string, ttl time.Duration) error {
	f.calls = append(f.calls, call{pane, source, t[tokens.Worker], ttl})
	return nil
}

func (f *fake) last(t *testing.T) call {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("no report")
	}
	return f.calls[len(f.calls)-1]
}

func send(t *testing.T, f *fake, dir string, now time.Time, body string) {
	t.Helper()
	env := Env{HerdrEnv: "1", PaneID: "w1:p1", StateDir: dir, Now: now}
	if err := Handle(strings.NewReader(body), env, f); err != nil {
		t.Fatal(err)
	}
}

func pre(desc string) string {
	return `{"hook_event_name":"PreToolUse","tool_name":"Task","tool_input":{"description":"` + desc + `"}}`
}

func TestHookLifecycle(t *testing.T) {
	dir, f, now := t.TempDir(), &fake{}, time.Now()
	send(t, f, dir, now, pre("scan repo"))
	c := f.last(t)
	if c.value == nil || *c.value != tokens.Indent("└ ✳ scan repo") || c.ttl != 15*time.Minute || c.source != tokens.WorkerSource || c.pane != "w1:p1" {
		t.Fatalf("bad report %+v", c)
	}
	send(t, f, dir, now, strings.Replace(pre("write docs"), "Task", "Agent", 1))
	if v := *f.last(t).value; v != tokens.Indent("└ ✳ write docs +1") {
		t.Fatalf("got %q", v)
	}
	send(t, f, dir, now, `{"hook_event_name":"SubagentStop","agent_id":"a1"}`)
	if v := *f.last(t).value; v != tokens.Indent("└ ✳ write docs") {
		t.Fatalf("got %q", v)
	}
	send(t, f, dir, now, `{"hook_event_name":"SubagentStop","agent_id":"a2"}`)
	if f.last(t).value != nil {
		t.Fatal("line should be cleared after last subagent")
	}
	send(t, f, dir, now, pre("x"))
	send(t, f, dir, now, pre("y"))
	send(t, f, dir, now, `{"hook_event_name":"Stop"}`)
	if f.last(t).value != nil {
		t.Fatal("Stop should clear")
	}
	send(t, f, dir, now, pre("x"))
	send(t, f, dir, now, `{"hook_event_name":"SessionEnd"}`)
	if f.last(t).value != nil {
		t.Fatal("SessionEnd should clear")
	}
}

func TestHookDefaultsAndFilters(t *testing.T) {
	dir, f, now := t.TempDir(), &fake{}, time.Now()
	send(t, f, dir, now, `{"hook_event_name":"PreToolUse","tool_name":"Task","tool_input":{}}`)
	if v := *f.last(t).value; v != tokens.Indent("└ ✳ subagent") {
		t.Fatalf("got %q", v)
	}
	n := len(f.calls)
	for _, b := range []string{
		`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"description":"ls"}}`,
		`{"hook_event_name":"SessionStart"}`,
		`{"hook_event_name":"PreToolUse","tool_name":"Task","agent_id":"a1","tool_input":{"description":"nested"}}`,
		`{"hook_event_name":"Stop","agent_id":"a1"}`,
	} {
		send(t, f, dir, now, b)
	}
	if len(f.calls) != n {
		t.Fatalf("ignored events reported: %+v", f.calls[n:])
	}
	// Wrong environment: nothing happens, not even state.
	env := Env{HerdrEnv: "", PaneID: "p", StateDir: dir, Now: now}
	if err := Handle(strings.NewReader(pre("z")), env, f); err != nil || len(f.calls) != n {
		t.Fatalf("acted outside herdr: %v", err)
	}
}

func TestHookExpiresStaleAndTruncates(t *testing.T) {
	dir, f, now := t.TempDir(), &fake{}, time.Now()
	send(t, f, dir, now.Add(-20*time.Minute), pre("old"))
	send(t, f, dir, now, pre(strings.Repeat("é", 100)+"\\n"))
	v := *f.last(t).value
	if strings.Contains(v, "+1") || strings.Contains(v, "old") || !strings.HasSuffix(v, "…") {
		t.Fatalf("got %q", v)
	}
}

const fixture = `{
  "theme": "auto",
  "tui": "fullscreen",
  "env": {"A": "<b>&"},
  "hooks": {"SessionStart":[{"matcher":"^(startup|resume)$","hooks":[{"type":"command","command":"bash '/h/.claude/hooks/herdr-agent-state.sh' session","timeout":10}]}],
    "Stop": [{"hooks":[{"type":"command","command":"bash '/h/.claude/hooks/herdr-agent-state.sh' stop"}]}],
    "PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"echo hi"}]}]},
  "zzz": [1, 2]
}
`

func TestMergeUnmergeSettings(t *testing.T) {
	cmd := "bash '/h/.claude/hooks/" + scriptName + "'"
	m1, err := mergeSettings(fixture, cmd)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := mergeSettings(m1, cmd)
	if err != nil || m2 != m1 {
		t.Fatalf("not idempotent: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(m1), &d); err != nil {
		t.Fatal(err)
	}
	h := d["hooks"].(map[string]any)
	for _, ev := range []string{"PreToolUse", "SubagentStop", "Stop", "SessionEnd"} {
		if strings.Count(m1, `"`+ev+`"`) != 1 {
			t.Fatalf("event %s count", ev)
		}
	}
	pt := h["PreToolUse"].([]any)
	if len(pt) != 2 || pt[0].(map[string]any)["matcher"] != "Bash" || pt[1].(map[string]any)["matcher"] != "Task|Agent" {
		t.Fatalf("PreToolUse order: %v", pt)
	}
	if st := h["Stop"].([]any); len(st) != 2 || !strings.Contains(st[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string), "herdr-agent-state") {
		t.Fatalf("Stop: %v", st)
	}
	// Key order and unknown keys survive; no HTML escaping.
	if !(strings.Index(m1, `"theme"`) < strings.Index(m1, `"tui"`) && strings.Index(m1, `"tui"`) < strings.Index(m1, `"env"`) && strings.Index(m1, `"hooks"`) < strings.Index(m1, `"zzz"`)) {
		t.Fatalf("key order changed:\n%s", m1)
	}
	if !strings.Contains(m1, `"<b>&"`) {
		t.Fatal("string was rewritten")
	}
	u, err := unmergeSettings(m1)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal([]byte(fixture), &want)
	json.Unmarshal([]byte(u), &got)
	if jw, _ := json.Marshal(want); string(jw) != func() string { b, _ := json.Marshal(got); return string(b) }() {
		t.Fatalf("unmerge differs:\n%s", u)
	}
	if u2, _ := unmergeSettings(u); u2 != u {
		t.Fatal("unmerge not idempotent")
	}
}

func TestMergeEmptyAndInvalid(t *testing.T) {
	m, err := mergeSettings("", "bash 'x/"+scriptName+"'")
	if err != nil || !strings.Contains(m, "SessionEnd") {
		t.Fatalf("%v %s", err, m)
	}
	u, err := unmergeSettings(m)
	if err != nil || strings.TrimSpace(u) != "{}" {
		t.Fatalf("%v %q", err, u)
	}
	if _, err := mergeSettings(`{"hooks": [}`, "c"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := mergeSettings(`{"hooks": []}`, "c"); err == nil {
		t.Fatal("non-object hooks accepted")
	}
}

func TestInstallUninstallFiles(t *testing.T) {
	home := t.TempDir()
	cdir := filepath.Join(home, ".claude")
	os.MkdirAll(filepath.Join(cdir, "hooks"), 0o755)
	os.WriteFile(filepath.Join(cdir, "settings.json"), []byte(fixture), 0o600)
	p := Paths{Home: home, PluginRoot: "/opt/it's/plugin"}
	if err := Install(p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.script())
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("script: %v", err)
	}
	b, _ := os.ReadFile(p.script())
	if !strings.Contains(string(b), `/opt/it'\''s/plugin/bin/herdr-ghostty-sidebar`) || !strings.Contains(string(b), "claude-hook") {
		t.Fatalf("script:\n%s", b)
	}
	bak, err := os.ReadFile(p.settings() + backupSufx)
	if err != nil || string(bak) != fixture {
		t.Fatalf("backup: %v", err)
	}
	first, _ := os.ReadFile(p.settings())
	if err := Install(p); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(p.settings())
	if string(first) != string(again) {
		t.Fatal("reinstall changed settings")
	}
	if fi, _ := os.Stat(p.settings()); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.script()); !os.IsNotExist(err) {
		t.Fatal("script not removed")
	}
	left, _ := os.ReadFile(p.settings())
	if strings.Contains(string(left), scriptName) {
		t.Fatal("entries remain")
	}
	if err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshScriptFollowsPluginRoot(t *testing.T) {
	home := t.TempDir()
	// Not installed: nothing written.
	if changed, err := RefreshScript(Paths{Home: home, PluginRoot: "/new"}); err != nil || changed {
		t.Fatalf("not installed: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(Paths{Home: home}.script()); !os.IsNotExist(err) {
		t.Fatalf("script created without install: %v", err)
	}
	if err := Install(Paths{Home: home, PluginRoot: "/old"}); err != nil {
		t.Fatal(err)
	}
	p := Paths{Home: home, PluginRoot: "/new"}
	if changed, err := RefreshScript(p); err != nil || !changed {
		t.Fatalf("stale root: changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(p.script())
	if string(got) != scriptBody("/new") {
		t.Fatalf("script not refreshed:\n%s", got)
	}
	if changed, _ := RefreshScript(p); changed {
		t.Fatal("current script rewritten")
	}
}
