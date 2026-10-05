// Package render turns a Herdr session snapshot into per-pane and
// per-workspace sidebar tokens and reports only what changed.
package render

import (
	"sort"
	"strings"
	"unicode"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

const (
	fallbackLg = "✳"
	maxValue   = 80

	// Herdr trims leading whitespace off token values (even NBSP). A
	// zero-width space is a format character, not whitespace, so it survives
	// the trim and protects the spaces after it.
	indent = "​  "
	corner = "└"
)

// Display states of one agent.
const (
	stWorking = "working"
	stBlocked = "blocked"
	stDone    = "done"
	stIdle    = "idle"
	stStale   = "idle_stale" // idle for a long time
)

// logos maps Herdr agent ids to codepoints in the bundled logo font.
var logos = map[string]string{
	"claude":     "\uE1A0",
	"codex":      "\uE1A1",
	"opencode":   "\uE1A2",
	"omp":        "\uE1A3",
	"cline":      "\uE1A4",
	"mastracode": "\uE1A5",
	"kimi":       "\uE1A6",
	"kilo":       "\uE1A7",
	"maki":       "\uE1A8",
	"agy":        "\uE1A9",
	"hermes":     "\uE1AA",
	"kimchi":     "\uE1AB",
}

// AllTokens lists every pane token name this plugin owns.
func AllTokens() []string { return tokens.PaneTokens }

// Values maps token name to value; an absent name means "cleared".
type Values map[string]string

// Result is everything Compute derives from one snapshot.
type Result struct {
	Panes      map[string]Values // agent pane id -> tokens
	PaneOrder  []string          // agent pane ids in display order
	Workspaces map[string]Values // workspace id -> tokens (every workspace)
}

// Compute returns the desired tokens, with working marks at spinner frame
// index frame (tokens.FrameAt). stale reports whether an idle agent
// pane has been untouched for long enough to dim.
func Compute(snap *herdr.Snapshot, stale func(paneID string) bool, frame int) *Result {
	mark := tokens.Frame(frame)
	if stale == nil {
		stale = func(string) bool { return false }
	}
	wsOrder := map[string]int{}
	wsLabel := map[string]string{}
	wsIdx := make([]int, len(snap.Workspaces))
	for i := range wsIdx {
		wsIdx[i] = i
	}
	sort.SliceStable(wsIdx, func(a, b int) bool {
		return snap.Workspaces[wsIdx[a]].Number < snap.Workspaces[wsIdx[b]].Number
	})
	for rank, i := range wsIdx {
		w := snap.Workspaces[i]
		wsOrder[w.ID] = rank
		wsLabel[w.ID] = w.Label
	}
	tabOrder := map[string]int{}
	for _, t := range snap.Tabs {
		tabOrder[t.ID] = t.Number
	}
	paneOrder := map[string]int{}
	for i, p := range snap.Panes {
		paneOrder[p.ID] = i
	}

	agents := append([]herdr.Agent(nil), snap.Agents...)
	rankOf := func(a herdr.Agent) (int, int, int) {
		w, ok := wsOrder[a.WorkspaceID]
		if !ok {
			w = len(wsOrder)
		}
		p, ok := paneOrder[a.PaneID]
		if !ok {
			p = len(paneOrder)
		}
		return w, tabOrder[a.TabID], p
	}
	sort.SliceStable(agents, func(i, j int) bool {
		wi, ti, pi := rankOf(agents[i])
		wj, tj, pj := rankOf(agents[j])
		if wi != wj {
			return wi < wj
		}
		if agents[i].WorkspaceID != agents[j].WorkspaceID {
			return agents[i].WorkspaceID < agents[j].WorkspaceID
		}
		if ti != tj {
			return ti < tj
		}
		return pi < pj
	})

	// Display state per agent, then per-workspace roll-ups.
	display := make(map[string]string, len(agents))
	hot := map[string]bool{}      // workspace has a working agent
	allStale := map[string]bool{} // every agent in the workspace is long idle
	count := map[string]int{}
	staleCount := map[string]int{}
	for _, a := range agents {
		d := displayFor(a, stale(a.PaneID))
		display[a.PaneID] = d
		count[a.WorkspaceID]++
		switch d {
		case stWorking:
			hot[a.WorkspaceID] = true
		case stStale:
			staleCount[a.WorkspaceID]++
		}
	}
	for ws, n := range count {
		allStale[ws] = n > 0 && staleCount[ws] == n
	}

	res := &Result{
		Panes:      make(map[string]Values, len(agents)),
		Workspaces: map[string]Values{},
	}
	seenWS := map[string]bool{}
	seenTab := map[string]bool{}
	for _, a := range agents {
		v := Values{}
		first := !seenWS[a.WorkspaceID]
		if first {
			seenWS[a.WorkspaceID] = true
			if allStale[a.WorkspaceID] {
				set(v, tokens.GroupStale, wsLabel[a.WorkspaceID])
			} else {
				set(v, tokens.Group, wsLabel[a.WorkspaceID])
			}
		}
		firstInTab := !seenTab[a.TabID]
		seenTab[a.TabID] = true
		// Herdr hangs its own indent on a row below a header; a row with no
		// header (every agent after the first of a workspace) needs ours.
		ind := ""
		if !first {
			ind = indent
		}
		d := display[a.PaneID]
		parked := d == stIdle || d == stStale
		paint := d
		if parked && hot[a.WorkspaceID] {
			paint = stWorking
		}
		logo := logoFor(a.Agent)
		if !firstInTab {
			v[tokens.Split] = ind + corner
			ind = ""
		}
		logoTok := tokens.Logo
		if d == stStale && paint == stStale {
			logoTok = tokens.LogoStale
		}
		v[logoTok] = ind + logo
		title := titleFor(a)
		switch paint {
		case stWorking:
			if d == stWorking {
				title = mark + " " + title
			}
			set(v, tokens.TitleWorking, title)
		case stDone:
			set(v, tokens.TitleDone, tokens.MarkDone+" "+title)
		case stBlocked:
			set(v, tokens.TitleBlocked, tokens.MarkBlocked+" "+title)
		case stStale:
			set(v, tokens.TitleStale, title)
		default:
			set(v, tokens.TitleIdle, title)
		}
		res.Panes[a.PaneID] = v
		res.PaneOrder = append(res.PaneOrder, a.PaneID)
	}

	// Spaces.
	byWS := map[string][]herdr.Agent{}
	for _, a := range agents {
		byWS[a.WorkspaceID] = append(byWS[a.WorkspaceID], a)
	}
	tabsByWS := map[string][]herdr.Tab{}
	for _, t := range snap.Tabs {
		tabsByWS[t.WorkspaceID] = append(tabsByWS[t.WorkspaceID], t)
	}
	for _, w := range snap.Workspaces {
		res.Workspaces[w.ID] = spaceTokens(w, mark, byWS[w.ID], tabsByWS[w.ID], display, hot[w.ID], allStale[w.ID])
	}
	return res
}

func displayFor(a herdr.Agent, isStale bool) string {
	switch a.Status {
	case "working":
		return stWorking
	case "blocked":
		return stBlocked
	case "done":
		return stDone
	}
	if isStale {
		return stStale
	}
	return stIdle
}

func vendorKey(agent string) string {
	for _, v := range tokens.BrandVendors {
		if v == agent {
			return v
		}
	}
	return "other"
}

// spaceTokens builds the Spaces entry of one workspace: an aggregate mark
// and label, then up to SpaceTabRows tab rows.
func spaceTokens(w herdr.Workspace, workMark string, agents []herdr.Agent, tabs []herdr.Tab, display map[string]string, hot, allStale bool) Values {
	v := Values{}
	set(v, tokens.SpaceLabel, w.Label)

	// Aggregate mark: blocked > working > done > parked/empty.
	pick := func(state string) (herdr.Agent, bool) {
		for _, a := range agents {
			if display[a.PaneID] == state {
				return a, true
			}
		}
		return herdr.Agent{}, false
	}
	switch {
	case has(pick(stBlocked)):
		v[tokens.SpaceBlocked] = tokens.MarkBlocked
	case has(pick(stWorking)):
		a, _ := pick(stWorking)
		v[tokens.SpaceWorking(vendorKey(a.Agent))] = workMark
	case has(pick(stDone)):
		v[tokens.SpaceDone] = tokens.MarkDone
	case len(agents) == 0 || allStale:
		v[tokens.SpaceEmpty] = tokens.MarkParked
	default:
		v[tokens.SpaceParked] = tokens.MarkParked
	}

	// Tab rows. A tab never named shows Herdr's default, its number: that
	// says nothing, so use the lead agent's title, or drop the row when the
	// tab has no agent.
	sort.SliceStable(tabs, func(i, j int) bool { return tabs[i].Number < tabs[j].Number })
	row := 0
	for _, t := range tabs {
		var inTab []herdr.Agent
		for _, a := range agents {
			if a.TabID == t.ID {
				inTab = append(inTab, a)
			}
		}
		unnamed := isNumber(t.Label)
		if unnamed && len(inTab) == 0 {
			continue
		}
		if row == tokens.SpaceTabRows {
			break
		}
		row++
		chosen := stIdle
		var lead *herdr.Agent
		if len(inTab) > 0 {
			lead = &inTab[0]
		}
	search:
		for _, st := range []string{stBlocked, stWorking, stDone} {
			for i := range inTab {
				if display[inTab[i].PaneID] == st {
					chosen, lead = st, &inTab[i]
					break search
				}
			}
		}
		label := t.Label
		if unnamed {
			label = titleFor(*lead)
		}
		if lead != nil {
			set(v, tokens.SpaceTabLogo(row), logoFor(lead.Agent))
		}
		paint := chosen
		if chosen == stIdle && hot {
			paint = stWorking
		}
		mark := ""
		switch chosen {
		case stWorking:
			mark = workMark + " "
		case stDone:
			mark = tokens.MarkDone + " "
		case stBlocked:
			mark = tokens.MarkBlocked + " "
		}
		text := mark + label
		switch paint {
		case stWorking:
			vendor := "other"
			if lead != nil {
				vendor = vendorKey(lead.Agent)
			}
			set(v, tokens.SpaceTabWorking(row, vendor), text)
		case stDone:
			set(v, tokens.SpaceTabDone(row), text)
		case stBlocked:
			set(v, tokens.SpaceTabBlocked(row), text)
		default:
			set(v, tokens.SpaceTabIdle(row), text)
		}
	}
	return v
}

func has(_ herdr.Agent, ok bool) bool { return ok }

func isNumber(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func logoFor(agent string) string {
	if g, ok := logos[agent]; ok {
		return g
	}
	return fallbackLg
}

func titleFor(a herdr.Agent) string {
	for _, s := range []string{a.TerminalTitleStripped, a.TerminalTitle, a.Name, a.Agent} {
		if c := clean(s); c != "" {
			return c
		}
	}
	return ""
}

func set(v Values, name, value string) {
	if c := clean(value); c != "" {
		v[name] = c
	}
}

// clean mirrors Herdr's server-side normalization (control characters
// removed, whitespace trimmed, 80 character cap) so cached values compare
// equal to what Herdr stores. A leading zero-width space is kept: it is not
// whitespace to Herdr either.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxValue {
		s = strings.TrimSpace(string(r[:maxValue]))
	}
	return s
}
