package render

import (
	"strings"
	"testing"
	"time"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/layout"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

const ind = "\u200b  "

func fixture() *herdr.Snapshot {
	return &herdr.Snapshot{
		// Deliberately out of display order: numbers decide.
		Workspaces: []herdr.Workspace{
			{ID: "w3", Number: 3, Label: "3. empty"},
			{ID: "w2", Number: 2, Label: "2. beta"},
			{ID: "w1", Number: 1, Label: "1. alpha"},
		},
		Tabs: []herdr.Tab{
			{ID: "w1:t2", WorkspaceID: "w1", Number: 2, Label: "second"},
			{ID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "first"},
			{ID: "w2:t1", WorkspaceID: "w2", Number: 1, Label: "1"},
			{ID: "w3:t1", WorkspaceID: "w3", Number: 1, Label: "1"},
		},
		Panes: []herdr.Pane{
			{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1"},
			{ID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1"},
			{ID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t2"},
			{ID: "w1:p4", WorkspaceID: "w1", TabID: "w1:t2"}, // plain shell
			{ID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1"},
		},
		Agents: []herdr.Agent{
			{PaneID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1", Agent: "mystery", Status: "bogus", Name: "Fred"},
			{PaneID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t2", Agent: "codex", Status: "blocked", TerminalTitle: "◐ raw", TerminalTitleStripped: ""},
			{PaneID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Agent: "claude", Status: "done", TerminalTitleStripped: "  task two  "},
			{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "claude", Status: "working", TerminalTitleStripped: "task one"},
		},
	}
}

func compute(s *herdr.Snapshot) *Result { return Compute(s, nil, 0) }

func TestComputeOrderAndRows(t *testing.T) {
	r := compute(fixture())
	if want := []string{"w1:p1", "w1:p2", "w1:p3", "w2:p1"}; strings.Join(r.PaneOrder, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v", r.PaneOrder)
	}
	if _, ok := r.Panes["w1:p4"]; ok {
		t.Fatal("non-agent pane must not get tokens")
	}
	// First agent of workspace: header, no indent (Herdr hangs it), mark+title in one cell.
	p1 := r.Panes["w1:p1"]
	if p1[tokens.Group] != "1. alpha" || p1[tokens.Logo] != "\ue1a0" || p1[tokens.TitleWorking] != "⠋ task one" || len(p1) != 3 {
		t.Fatalf("p1: %v", p1)
	}
	// Second agent of the same tab: corner cell carries the indent, logo none.
	p2 := r.Panes["w1:p2"]
	if p2[tokens.Split] != ind+"└" || p2[tokens.Logo] != "\ue1a0" || p2[tokens.TitleDone] != "✓ task two" || len(p2) != 3 {
		t.Fatalf("p2: %v", p2)
	}
	// First agent of a second tab: indented logo, no header, no corner.
	p3 := r.Panes["w1:p3"]
	if p3[tokens.Logo] != ind+"\ue1a1" || p3[tokens.TitleBlocked] != "? ◐ raw" || len(p3) != 2 {
		t.Fatalf("p3: %v", p3)
	}
	// Unknown agent, unknown status: fallback logo, plain idle title.
	w2 := r.Panes["w2:p1"]
	if w2[tokens.Group] != "2. beta" || w2[tokens.Logo] != "✳" || w2[tokens.TitleIdle] != "Fred" || len(w2) != 3 {
		t.Fatalf("unknown agent: %v", w2)
	}
}

func TestExactlyOneTitleAndTokenBudget(t *testing.T) {
	r := compute(fixture())
	titles := []string{tokens.TitleWorking, tokens.TitleDone, tokens.TitleBlocked, tokens.TitleIdle, tokens.TitleStale}
	for id, v := range r.Panes {
		n := 0
		for _, s := range titles {
			if v[s] != "" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s has %d title tokens", id, n)
		}
		if len(v) > 16 {
			t.Fatal("too many tokens")
		}
	}
	if n := len(tokens.SpaceTokens()); n > 32 {
		t.Fatalf("%d workspace tokens, Herdr allows 32", n)
	}
}

func TestStaleAndHot(t *testing.T) {
	s := fixture()
	s.Agents = []herdr.Agent{
		{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "claude", Status: "idle", TerminalTitleStripped: "old"},
		{PaneID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Agent: "claude", Status: "idle", TerminalTitleStripped: "older"},
	}
	r := Compute(s, func(string) bool { return true }, 0)
	if r.Panes["w1:p1"][tokens.GroupStale] != "1. alpha" || r.Panes["w1:p1"][tokens.TitleStale] != "old" || r.Panes["w1:p1"][tokens.LogoStale] == "" {
		t.Fatalf("all-stale workspace must dim: %v", r.Panes["w1:p1"])
	}
	if r.Workspaces["w1"][tokens.SpaceEmpty] == "" {
		t.Fatalf("all-stale workspace is an empty ring: %v", r.Workspaces["w1"])
	}
	// One working agent makes its parked sibling take the working colour.
	s.Agents[0].Status = "working"
	r = Compute(s, func(string) bool { return true }, 0)
	if r.Panes["w1:p2"][tokens.TitleWorking] != "older" || r.Panes["w1:p2"][tokens.Logo] == "" {
		t.Fatalf("hot sibling: %v", r.Panes["w1:p2"])
	}
	if r.Panes["w1:p1"][tokens.TitleWorking] != "⠋ old" || r.Panes["w1:p1"][tokens.GroupStale] != "" {
		t.Fatalf("working agent: %v", r.Panes["w1:p1"])
	}
}

func TestSpaces(t *testing.T) {
	r := compute(fixture())
	w1 := r.Workspaces["w1"]
	// blocked beats working beats done.
	if w1[tokens.SpaceBlocked] != "?" || w1[tokens.SpaceLabel] != "1. alpha" {
		t.Fatalf("w1: %v", w1)
	}
	if w1[tokens.SpaceTabLogo(1)] != "\ue1a0" || w1[tokens.SpaceTabWorking(1, "claude")] != "⠋ first" {
		t.Fatalf("w1 tab1: %v", w1)
	}
	if w1[tokens.SpaceTabLogo(2)] != "\ue1a1" || w1[tokens.SpaceTabBlocked(2)] != "? second" {
		t.Fatalf("w1 tab2: %v", w1)
	}
	if _, ok := w1[tokens.SpaceTabLogo(3)]; ok {
		t.Fatal("only two tabs")
	}
	// Unnamed tab takes the lead agent's title; unknown agent: parked ring, idle tab.
	w2 := r.Workspaces["w2"]
	if w2[tokens.SpaceParked] != "○" || w2[tokens.SpaceTabIdle(1)] != "Fred" || w2[tokens.SpaceTabLogo(1)] != "✳" {
		t.Fatalf("w2: %v", w2)
	}
	// No agents, unnamed tab: label and empty ring only.
	w3 := r.Workspaces["w3"]
	if len(w3) != 2 || w3[tokens.SpaceEmpty] != "○" || w3[tokens.SpaceLabel] != "3. empty" {
		t.Fatalf("w3: %v", w3)
	}
	// Working aggregate takes the vendor; done is a tick.
	s := fixture()
	s.Agents = s.Agents[2:]
	r = compute(s)
	if r.Workspaces["w1"][tokens.SpaceWorking("claude")] != "⠋" {
		t.Fatalf("working mark: %v", r.Workspaces["w1"])
	}
	s.Agents = s.Agents[:1]
	if r = compute(s); r.Workspaces["w1"][tokens.SpaceDone] != "✓" {
		t.Fatalf("done mark: %v", r.Workspaces["w1"])
	}
}

func TestSpacesHotAndNamedEmptyTab(t *testing.T) {
	s := fixture()
	s.Tabs = append(s.Tabs, herdr.Tab{ID: "w1:t3", WorkspaceID: "w1", Number: 3, Label: "logs"})
	s.Agents = s.Agents[2:] // claude done + claude working in t1
	r := compute(s)
	w1 := r.Workspaces["w1"]
	// hot: the shell-only named tab is painted in the working colour (vendor "other").
	if w1[tokens.SpaceTabWorking(2, "other")] != "second" || w1[tokens.SpaceTabWorking(3, "other")] != "logs" {
		t.Fatalf("hot tabs: %v", w1)
	}
}

func TestTitleFallbacksAndClean(t *testing.T) {
	a := herdr.Agent{Agent: "claude"}
	if titleFor(a) != "claude" {
		t.Fatal(titleFor(a))
	}
	long := strings.Repeat("x", 100)
	if got := clean(long); len([]rune(got)) != 80 {
		t.Fatalf("len = %d", len([]rune(got)))
	}
	if clean(" a\x00b\n ") != "ab" {
		t.Fatal(clean(" a\x00b\n "))
	}
	// The zero-width-space indent must survive clean, like it survives Herdr.
	if clean(ind+"└") != ind+"└" {
		t.Fatal("indent trimmed")
	}
}

func TestDiff(t *testing.T) {
	names := AllTokens()
	old := Values{tokens.TitleIdle: "a", tokens.Logo: "x"}
	same := Values{tokens.TitleIdle: "a", tokens.Logo: "x"}
	if Diff(old, same, false, names) != nil {
		t.Fatal("identical values must produce no patch")
	}
	next := Values{tokens.TitleDone: "✓ b", tokens.Logo: "x"}
	p := Diff(old, next, false, names)
	if p[tokens.TitleDone] == nil || *p[tokens.TitleDone] != "✓ b" {
		t.Fatalf("done: %v", p)
	}
	if v, ok := p[tokens.TitleIdle]; !ok || v != nil {
		t.Fatal("idle must be cleared")
	}
	if _, ok := p[tokens.Logo]; ok {
		t.Fatal("untouched token must not appear")
	}
	if got := Diff(old, same, true, names); len(got) != len(names) {
		t.Fatalf("force patch has %d keys", len(got))
	}
	// Legacy tokens are cleared once, only where present, even under force.
	legacy := Values{"gs_ws": "w", "gs_working": "●", tokens.Logo: "x"}
	p = Diff(legacy, Values{tokens.Logo: "x"}, false, names)
	if len(p) != 2 || p["gs_ws"] != nil || p["gs_working"] != nil {
		t.Fatalf("legacy: %v", p)
	}
	if got := Diff(Values{}, Values{}, true, names); len(got) != len(names) {
		t.Fatalf("force without legacy has %d keys", len(got))
	}
}

type fakeAPI struct {
	snap    *herdr.Snapshot
	reports map[string][]Patch
	wsRep   map[string][]Patch
}

func (f *fakeAPI) Snapshot() (*herdr.Snapshot, error) { return f.snap, nil }
func (f *fakeAPI) ReportPaneTokens(id, source string, t map[string]*string) error {
	if source != tokens.Source {
		panic("wrong source")
	}
	if f.reports == nil {
		f.reports = map[string][]Patch{}
	}
	f.reports[id] = append(f.reports[id], Patch(t))
	return nil
}
func (f *fakeAPI) ReportWorkspaceTokens(id, source string, t map[string]*string) error {
	if source != tokens.Source {
		panic("wrong source")
	}
	if f.wsRep == nil {
		f.wsRep = map[string][]Patch{}
	}
	f.wsRep[id] = append(f.wsRep[id], Patch(t))
	return nil
}

func TestRunCachesAndClears(t *testing.T) {
	dir := t.TempDir()
	api := &fakeAPI{snap: fixture()}
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if len(api.reports) != 4 || len(api.wsRep) != 3 {
		t.Fatalf("first run reported %d panes, %d workspaces", len(api.reports), len(api.wsRep))
	}
	api.reports, api.wsRep = nil, nil
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if len(api.reports) != 0 || len(api.wsRep) != 0 {
		t.Fatalf("second run must send nothing: %v %v", api.reports, api.wsRep)
	}
	// w1:p2 stops being an agent but still exists; w2:p1 pane vanishes.
	s := fixture()
	var agents []herdr.Agent
	for _, a := range s.Agents {
		if a.PaneID != "w1:p2" && a.PaneID != "w2:p1" {
			agents = append(agents, a)
		}
	}
	s.Agents = agents
	s.Panes = s.Panes[:4]
	api.snap = s
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if len(api.reports["w2:p1"]) != 0 {
		t.Fatal("vanished pane must not be contacted")
	}
	cleared := api.reports["w1:p2"]
	if len(cleared) != 1 || len(cleared[0]) != 3 || cleared[0][tokens.TitleDone] != nil {
		t.Fatalf("p2 clear: %v", cleared)
	}
	for _, name := range []string{tokens.Split, tokens.Logo, tokens.TitleDone} {
		if v, ok := cleared[0][name]; !ok || v != nil {
			t.Fatalf("%s should be cleared: %v", name, cleared[0])
		}
	}
	// Cache forgot p2: a further run is quiet.
	api.reports, api.wsRep = nil, nil
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if len(api.reports) != 0 {
		t.Fatalf("expected quiet, got %v", api.reports)
	}
	// Force resends everything for agent panes and workspaces.
	if err := Run(api, dir, true); err != nil {
		t.Fatal(err)
	}
	if len(api.reports) != 2 || len(api.wsRep) != 3 {
		t.Fatalf("force reported %d panes, %d workspaces", len(api.reports), len(api.wsRep))
	}
}

func TestRunClearsLegacyTokensOnce(t *testing.T) {
	dir := t.TempDir()
	s := fixture()
	s.Panes[0].Tokens = map[string]string{"gs_ws": "alpha", "gs_working": "●", "gs_title": "task one", "other": "keep"}
	s.Agents[3].Tokens = s.Panes[0].Tokens
	api := &fakeAPI{snap: s}
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	p := api.reports["w1:p1"][0]
	for _, name := range []string{"gs_ws", "gs_working", "gs_title"} {
		if v, ok := p[name]; !ok || v != nil {
			t.Fatalf("%s not cleared: %v", name, p)
		}
	}
	if _, ok := p["other"]; ok {
		t.Fatal("foreign token touched")
	}
}

func TestStaleTracking(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	defer func() { now = time.Now }()
	s := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Number: 1, Label: "1. a"}},
		Panes:      []herdr.Pane{{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1"}},
		Tabs:       []herdr.Tab{{ID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "1"}},
		Agents:     []herdr.Agent{{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "claude", Status: "idle", StateChangeSeq: 3, TerminalTitleStripped: "t"}},
	}
	api := &fakeAPI{snap: s}
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if v := api.reports["w1:p1"][0][tokens.TitleIdle]; v == nil {
		t.Fatalf("fresh idle: %v", api.reports)
	}
	now = func() time.Time { return t0.Add(StaleAfter + time.Minute) }
	api.reports = nil
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	p := api.reports["w1:p1"][0]
	if p[tokens.TitleStale] == nil || p[tokens.TitleIdle] != nil || p[tokens.GroupStale] == nil {
		t.Fatalf("should dim: %v", p)
	}
	// A new state change resets the clock.
	s.Agents[0].StateChangeSeq = 4
	api.reports = nil
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if p := api.reports["w1:p1"][0]; p[tokens.TitleIdle] == nil {
		t.Fatalf("should be fresh again: %v", p)
	}
}

func TestClearAll(t *testing.T) {
	dir := t.TempDir()
	api := &fakeAPI{snap: fixture()}
	if err := ClearAll(api, dir); err != nil {
		t.Fatal(err)
	}
	if len(api.reports) != 5 || len(api.wsRep) != 3 {
		t.Fatalf("cleared %d panes, %d workspaces", len(api.reports), len(api.wsRep))
	}
	for _, v := range api.reports["w1:p4"][0] {
		if v != nil {
			t.Fatal("ClearAll must only clear")
		}
	}
	if _, ok := api.reports["w1:p4"][0]["gs_ws"]; !ok {
		t.Fatal("legacy tokens must be cleared too")
	}
}

func TestStatusChangeOnlySendsChangedTokens(t *testing.T) {
	dir := t.TempDir()
	api := &fakeAPI{snap: fixture()}
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	api.reports = nil
	api.snap.Agents[3].Status = "idle" // w1:p1 working -> idle
	if err := Run(api, dir, false); err != nil {
		t.Fatal(err)
	}
	p := api.reports["w1:p1"]
	if len(p) != 1 || len(p[0]) != 2 || p[0][tokens.TitleWorking] != nil || p[0][tokens.TitleIdle] == nil {
		t.Fatalf("patch: %v", p)
	}
}

func TestLogoGlyphsMatchLayout(t *testing.T) {
	for _, a := range layout.Logos {
		if logos[a.ID] == "" || logos[a.ID] != layout.Glyph(a.ID) {
			t.Errorf("%s: render %q, layout %q", a.ID, logos[a.ID], layout.Glyph(a.ID))
		}
	}
	var brands []string
	for _, a := range layout.Logos {
		if a.Brand != "" {
			brands = append(brands, a.ID)
		}
	}
	if strings.Join(brands, ",") != strings.Join(tokens.BrandVendors, ",") {
		t.Errorf("tokens.BrandVendors %v != layout brands %v", tokens.BrandVendors, brands)
	}
}
