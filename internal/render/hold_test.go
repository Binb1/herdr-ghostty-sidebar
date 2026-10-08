package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

func snapOf(focused string, agents ...herdr.Agent) *herdr.Snapshot {
	return &herdr.Snapshot{FocusedPaneID: focused, Agents: agents}
}

func ag(id, status string) herdr.Agent {
	return herdr.Agent{PaneID: id, WorkspaceID: "w", TabID: "t", Agent: "claude", Status: status, Name: "n"}
}

func TestHoldStateMachine(t *testing.T) {
	var h map[string]hold
	step := func(focused, status string) {
		h = updateHold(h, snapOf(focused, ag("p", status)))
	}
	shown := func() string { return applyHold(snapOf("", ag("p", "idle")), h).Agents[0].Status }

	step("other", "working")
	if shown() != "idle" {
		t.Fatal("working holds nothing")
	}
	step("other", "done")
	step("other", "idle") // Herdr flips to idle, unseen
	if shown() != "done" {
		t.Fatalf("unseen done must stay, got %q", shown())
	}
	step("other", "idle")
	if shown() != "done" {
		t.Fatal("still held on later renders")
	}
	step("p", "idle") // user focuses the pane
	if shown() != "idle" {
		t.Fatal("focus clears the hold")
	}
	step("other", "idle")
	if shown() != "idle" {
		t.Fatal("cleared hold must not come back")
	}
}

func TestHoldWorkingWinsAndBlocked(t *testing.T) {
	var h map[string]hold
	h = updateHold(h, snapOf("x", ag("p", "blocked")))
	h = updateHold(h, snapOf("x", ag("p", "idle")))
	if h["p"] != "blocked" {
		t.Fatalf("blocked held, got %q", h["p"])
	}
	// idle agent is shown blocked, but a working one is left alone
	if got := applyHold(snapOf("", ag("p", "working")), h).Agents[0].Status; got != "working" {
		t.Fatalf("working wins, got %q", got)
	}
	h = updateHold(h, snapOf("x", ag("p", "working")))
	if len(h) != 0 {
		t.Fatalf("working clears hold: %v", h)
	}
	h = updateHold(h, snapOf("x", ag("p", "idle")))
	if len(h) != 0 {
		t.Fatal("no hold after working->idle without done")
	}
	// done then blocked: held state follows the latest
	h = updateHold(h, snapOf("x", ag("p", "done")))
	h = updateHold(h, snapOf("x", ag("p", "blocked")))
	h = updateHold(h, snapOf("x", ag("p", "idle")))
	if h["p"] != "blocked" {
		t.Fatalf("got %q", h["p"])
	}
}

func TestHoldFocusedWhenFinishing(t *testing.T) {
	h := updateHold(nil, snapOf("p", ag("p", "done")))
	h = updateHold(h, snapOf("x", ag("p", "idle"))) // looked away afterwards
	if len(h) != 0 {
		t.Fatalf("pane focused at finish must not hold: %v", h)
	}
	// Without focused_pane_id, the agent's own focused flag decides.
	a := ag("q", "done")
	a.Focused = true
	if h := updateHold(nil, snapOf("", a)); len(h) != 0 {
		t.Fatal("focused flag fallback")
	}
}

func TestHoldDropsVanishedPanes(t *testing.T) {
	h := map[string]hold{"gone": "done", "p": "done"}
	h = updateHold(h, snapOf("x", ag("p", "idle")))
	if _, ok := h["gone"]; ok || h["p"] != "done" {
		t.Fatalf("%v", h)
	}
}

func TestHoldShownInTokensAndSpaces(t *testing.T) {
	s := fixture()
	s.Agents = []herdr.Agent{{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "claude", Status: "idle", TerminalTitleStripped: "job"}}
	h := map[string]hold{"w1:p1": "done"}
	r := compute(applyHold(s, h))
	if r.Panes["w1:p1"][tokens.TitleDone] != "✓ job" {
		t.Fatalf("pane: %v", r.Panes["w1:p1"])
	}
	ws := r.Workspaces["w1"]
	if ws[tokens.SpaceDone] != "✓" || ws[tokens.SpaceTabDone(1)] == "" {
		t.Fatalf("spaces: %v", ws)
	}
}

func TestRunPersistsHold(t *testing.T) {
	dir := t.TempDir()
	mk := func(focused, status string) *fakeAPI {
		s := snapOf(focused, ag("w1:p1", status))
		s.Workspaces = []herdr.Workspace{{ID: "w1", Number: 1, Label: "ws"}}
		return &fakeAPI{snap: s}
	}
	if _, err := RunWorking(mk("x", "done"), dir, false); err != nil {
		t.Fatal(err)
	}
	api := mk("x", "idle")
	if _, err := RunWorking(api, dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hold.json")); err != nil {
		t.Fatal(err)
	}
	for _, p := range api.reports["w1:p1"] {
		if v, ok := p[tokens.TitleIdle]; ok && v != nil {
			t.Fatalf("held pane flipped to idle: %v", p)
		}
	}
	api = mk("w1:p1", "idle") // now focused
	if _, err := RunWorking(api, dir, false); err != nil {
		t.Fatal(err)
	}
	got := api.reports["w1:p1"]
	if len(got) != 1 || got[0][tokens.TitleIdle] == nil {
		t.Fatalf("focus must release to idle: %v", got)
	}
}

func TestBranchToken(t *testing.T) {
	s := fixture()
	br := map[string]string{"w1:p1": "feat/x", "w1:p2": "main", "w1:p3": "master", "w2:p1": ""}
	r := ComputeWith(s, Inputs{Branch: func(a herdr.Agent) string { return br[a.PaneID] }}, 0)
	if got := r.Panes["w1:p1"][tokens.Branch]; got != "​  ⎇ feat/x" {
		t.Fatalf("branch value %q", got)
	}
	for _, id := range []string{"w1:p2", "w1:p3", "w2:p1"} {
		if v, ok := r.Panes[id][tokens.Branch]; ok {
			t.Fatalf("%s should hide branch, got %q", id, v)
		}
	}
	if _, ok := compute(s).Panes["w1:p1"][tokens.Branch]; ok {
		t.Fatal("no branch input, no token")
	}
}

func TestRunReportsBranchFromCwd(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/topic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := ag("w1:p1", "idle")
	a.Cwd = "/nonexistent"
	a.ForegroundCwd = repo
	s := snapOf("", a)
	s.Workspaces = []herdr.Workspace{{ID: "w", Number: 1, Label: "ws"}}
	api := &fakeAPI{snap: s}
	if _, err := RunWorking(api, t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	p := api.reports["w1:p1"]
	// An idle agent's branch goes in the muted token.
	if len(p) != 1 || p[0][tokens.Branch] != nil || p[0][tokens.BranchIdle] == nil || *p[0][tokens.BranchIdle] != "​  ⎇ topic" {
		t.Fatalf("%v", p)
	}
}
