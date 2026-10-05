package render

import (
	"encoding/json"
	"os"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
)

// A pane that finishes (done) or needs input (blocked) keeps that state on
// screen after Herdr flips it to idle, until the user has focused the pane.
// hold persists the held state per pane in hold.json.

// hold is the state a pane is held in: "done" or "blocked".
type hold string

func loadHold(path string) map[string]hold {
	m := map[string]hold{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if m == nil {
		m = map[string]hold{}
	}
	return m
}

func isFocused(snap *herdr.Snapshot, a herdr.Agent) bool {
	if snap.FocusedPaneID != "" {
		return a.PaneID == snap.FocusedPaneID
	}
	return a.Focused
}

// updateHold advances the state machine one snapshot:
//   - working clears the hold (working wins);
//   - done/blocked sets it, unless the pane is focused right now (the user is
//     looking, nothing to wait for);
//   - idle keeps an existing hold until the pane is focused;
//   - a focused pane never holds; vanished panes are dropped.
func updateHold(prev map[string]hold, snap *herdr.Snapshot) map[string]hold {
	next := map[string]hold{}
	for _, a := range snap.Agents {
		if isFocused(snap, a) {
			continue
		}
		switch a.Status {
		case stDone, stBlocked:
			next[a.PaneID] = hold(a.Status)
		case stWorking:
		default:
			if h, ok := prev[a.PaneID]; ok {
				next[a.PaneID] = h
			}
		}
	}
	return next
}

// applyHold returns snap with held idle agents shown in their held state.
func applyHold(snap *herdr.Snapshot, h map[string]hold) *herdr.Snapshot {
	if len(h) == 0 {
		return snap
	}
	cp := *snap
	cp.Agents = append([]herdr.Agent(nil), snap.Agents...)
	for i, a := range cp.Agents {
		if a.Status == stWorking || a.Status == stDone || a.Status == stBlocked {
			continue
		}
		if s, ok := h[a.PaneID]; ok {
			cp.Agents[i].Status = string(s)
		}
	}
	return &cp
}
