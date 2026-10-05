package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/gitbranch"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

// API is the slice of the Herdr client render needs.
type API interface {
	Snapshot() (*herdr.Snapshot, error)
	ReportPaneTokens(paneID, source string, tokens map[string]*string) error
	ReportWorkspaceTokens(workspaceID, source string, tokens map[string]*string) error
}

// StaleAfter is how long an agent must sit idle before its row dims.
const StaleAfter = 2 * time.Hour

var now = time.Now

// Patch is the token update for one pane; a nil value clears the token.
type Patch map[string]*string

// Diff returns the patch turning old into desired over names, or nil if
// nothing differs. With force every name is included. Legacy tokens are
// only ever cleared, and only where old still has them.
func Diff(old, desired Values, force bool, names []string) Patch {
	p := Patch{}
	for _, name := range names {
		o, hadOld := old[name]
		d, want := desired[name]
		switch {
		case want && (force || !hadOld || !sameValue(name, o, d)):
			v := d
			p[name] = &v
		case !want && (force || hadOld):
			p[name] = nil
		}
	}
	for _, name := range tokens.Legacy {
		if _, had := old[name]; had {
			if _, want := desired[name]; !want {
				p[name] = nil
			}
		}
	}
	if len(p) == 0 {
		return nil
	}
	return p
}

// sameValue compares two values of one token. Working tokens differ every
// animation frame; the animate loop owns that, so only the rest of the text
// counts here.
func sameValue(name, a, b string) bool {
	if tokens.IsWorking(name) {
		a, _ = tokens.StripFrame(a)
		b, _ = tokens.StripFrame(b)
	}
	return a == b
}

// wsPrefix keys workspace entries in the cache apart from pane ids.
const wsPrefix = "ws:"

type cacheFile map[string]Values

func loadCache(path string) cacheFile {
	c := cacheFile{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c == nil {
		c = cacheFile{}
	}
	return c
}

func saveCache(path string, c cacheFile) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lock takes an exclusive advisory lock, waiting up to ~10s for a
// concurrent run to finish.
func lock(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "render.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, errors.New("render: another render run holds the lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ownedFromSnapshot extracts our tokens as Herdr reports them on a pane or
// workspace (legacy ones included, so they get cleared).
func ownedFromSnapshot(m map[string]string, names []string) Values {
	v := Values{}
	for _, name := range append(append([]string{}, names...), tokens.Legacy...) {
		if s, ok := m[name]; ok {
			v[name] = s
		}
	}
	return v
}

// seen tracks when each agent pane last changed state, so a long-idle pane
// can dim. Herdr's snapshot carries no timestamps.
type seen struct {
	Seq    uint64 `json:"seq"`
	Status string `json:"status"`
	At     int64  `json:"at"`
}

func loadSeen(path string) map[string]seen {
	m := map[string]seen{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if m == nil {
		m = map[string]seen{}
	}
	return m
}

func saveJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// updateSeen returns the new tracking map for the snapshot's agents.
func updateSeen(prev map[string]seen, agents []herdr.Agent, t time.Time) map[string]seen {
	next := make(map[string]seen, len(agents))
	for _, a := range agents {
		s, ok := prev[a.PaneID]
		if !ok || s.Seq != a.StateChangeSeq || s.Status != a.Status {
			s = seen{Seq: a.StateChangeSeq, Status: a.Status, At: t.Unix()}
		}
		next[a.PaneID] = s
	}
	return next
}

// Run reconciles Herdr's pane tokens with the snapshot. stateDir holds the
// cache of last-reported values. Where the snapshot exposes a pane's current
// tokens those win over the cache, so a restarted server self-heals.
func Run(api API, stateDir string, force bool) error {
	_, err := RunWorking(api, stateDir, force)
	return err
}

// RunWorking is Run, also reporting whether any agent is working.
func RunWorking(api API, stateDir string, force bool) (bool, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return false, err
	}
	unlock, err := lock(stateDir)
	if err != nil {
		return false, err
	}
	defer unlock()

	snap, err := api.Snapshot()
	if err != nil {
		return false, err
	}
	t := now()
	seenPath := filepath.Join(stateDir, "seen.json")
	seenMap := updateSeen(loadSeen(seenPath), snap.Agents, t)
	holdPath := filepath.Join(stateDir, "hold.json")
	holdMap := updateHold(loadHold(holdPath), snap)
	branches := gitbranch.NewReader()
	res := ComputeWith(applyHold(snap, holdMap), Inputs{Stale: func(id string) bool {
		s, ok := seenMap[id]
		return ok && t.Sub(time.Unix(s.At, 0)) >= StaleAfter
	}, Branch: func(a herdr.Agent) string {
		dir := a.ForegroundCwd
		if dir == "" {
			dir = a.Cwd
		}
		return branches.Branch(dir)
	}}, tokens.FrameAt(t))
	desired, order := res.Panes, res.PaneOrder
	cachePath := filepath.Join(stateDir, "tokens.json")
	cache := loadCache(cachePath)
	live := map[string]herdr.Pane{}
	for _, p := range snap.Panes {
		live[p.ID] = p
	}
	liveTokens := map[string]map[string]string{}
	for _, a := range snap.Agents {
		if a.Tokens != nil {
			liveTokens[a.PaneID] = a.Tokens
		}
	}

	next := cacheFile{}
	var errs []error
	report := func(id string, old, want Values) {
		patch := Diff(old, want, force, AllTokens())
		if patch == nil {
			next[id] = want
			return
		}
		if err := api.ReportPaneTokens(id, tokens.Source, patch); err != nil {
			errs = append(errs, fmt.Errorf("pane %s: %w", id, err))
			next[id] = old // retry next run
			return
		}
		next[id] = want
	}
	for _, id := range order {
		old := cache[id]
		if t, ok := liveTokens[id]; ok {
			old = ownedFromSnapshot(t, AllTokens())
		} else if p, ok := live[id]; ok && p.Tokens != nil {
			old = ownedFromSnapshot(p.Tokens, AllTokens())
		}
		report(id, old, desired[id])
	}
	// Panes reported before that are no longer agents: clear (if they still
	// exist; a closed pane took its tokens with it).
	stale := make([]string, 0)
	for id := range cache {
		if _, ok := desired[id]; ok || strings.HasPrefix(id, wsPrefix) {
			continue
		}
		stale = append(stale, id)
	}
	sort.Strings(stale)
	for _, id := range stale {
		if _, ok := live[id]; !ok {
			continue
		}
		old := cache[id]
		if p := live[id]; p.Tokens != nil {
			old = ownedFromSnapshot(p.Tokens, AllTokens())
		}
		report(id, old, Values{})
		if len(next[id]) == 0 {
			delete(next, id)
		}
	}

	// Workspaces: every workspace carries tokens, even without agents.
	for _, w := range snap.Workspaces {
		key := wsPrefix + w.ID
		old := cache[key]
		if w.Tokens != nil {
			old = ownedFromSnapshot(w.Tokens, tokens.SpaceTokens())
		}
		want := res.Workspaces[w.ID]
		patch := Diff(old, want, force, tokens.SpaceTokens())
		if patch == nil {
			next[key] = want
			continue
		}
		if err := api.ReportWorkspaceTokens(w.ID, tokens.Source, patch); err != nil {
			errs = append(errs, fmt.Errorf("workspace %s: %w", w.ID, err))
			next[key] = old
			continue
		}
		next[key] = want
	}
	if err := saveCache(cachePath, next); err != nil {
		errs = append(errs, err)
	}
	if err := saveJSON(seenPath, seenMap); err != nil {
		errs = append(errs, err)
	}
	if err := saveJSON(holdPath, holdMap); err != nil {
		errs = append(errs, err)
	}
	return anyWorking(snap), errors.Join(errs...)
}

// ClearAll removes every token this plugin owns from every pane and forgets
// the cache.
func ClearAll(api API, stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	unlock, err := lock(stateDir)
	if err != nil {
		return err
	}
	defer unlock()
	snap, err := api.Snapshot()
	if err != nil {
		return err
	}
	patch := Patch{}
	for _, name := range append(append([]string{}, AllTokens()...), tokens.Legacy...) {
		patch[name] = nil
	}
	wsPatch := Patch{}
	for _, name := range tokens.SpaceTokens() {
		wsPatch[name] = nil
	}
	var errs []error
	for _, p := range snap.Panes {
		if err := api.ReportPaneTokens(p.ID, tokens.Source, patch); err != nil {
			errs = append(errs, fmt.Errorf("pane %s: %w", p.ID, err))
		}
	}
	for _, w := range snap.Workspaces {
		if err := api.ReportWorkspaceTokens(w.ID, tokens.Source, wsPatch); err != nil {
			errs = append(errs, fmt.Errorf("workspace %s: %w", w.ID, err))
		}
	}
	_ = os.Remove(filepath.Join(stateDir, "tokens.json"))
	_ = os.Remove(filepath.Join(stateDir, "seen.json"))
	_ = os.Remove(filepath.Join(stateDir, "hold.json"))
	return errors.Join(errs...)
}

func anyWorking(snap *herdr.Snapshot) bool {
	for _, a := range snap.Agents {
		if a.Status == stWorking {
			return true
		}
	}
	return false
}
