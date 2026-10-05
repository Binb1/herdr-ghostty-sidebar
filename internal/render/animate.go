package render

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

// AnimateOpts tunes the animate loop; zero values take the defaults.
type AnimateOpts struct {
	Interval  time.Duration // tick length (default tokens.FrameInterval)
	Idle      time.Duration // exit after this long with no working agent (3s)
	MaxRun    time.Duration // hard cap (6h)
	MaxErrors int           // consecutive failed ticks before giving up (5)
	Sleep     func(time.Duration)
	// Sync, when set, runs every SyncEvery (default 2s) while the loop
	// ticks: it re-applies the layout when the appearance or theme changed.
	Sync      func()
	SyncEvery time.Duration
	Now       func() time.Time
}

func (o *AnimateOpts) defaults() {
	if o.Interval <= 0 {
		o.Interval = tokens.FrameInterval
	}
	if o.Idle <= 0 {
		o.Idle = 3 * time.Second
	}
	if o.MaxRun <= 0 {
		o.MaxRun = 6 * time.Hour
	}
	if o.MaxErrors <= 0 {
		o.MaxErrors = 5
	}
	if o.SyncEvery <= 0 {
		o.SyncEvery = 2 * time.Second
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// TryLock takes the named lock file in dir without waiting. ok is false when
// someone else holds it.
func TryLock(dir, name string) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true, nil
}

// AnimateRunning reports whether an animate process holds its lock.
func AnimateRunning(stateDir string) bool {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return true // can't tell; don't spawn
	}
	unlock, ok, err := TryLock(stateDir, "animate.lock")
	if err != nil {
		return true
	}
	if ok {
		unlock()
		return false
	}
	return true
}

// Animate runs the spinner loop unless another one already holds the
// animate lock (then it returns nil at once).
func Animate(api API, stateDir string, o AnimateOpts) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	unlock, ok, err := TryLock(stateDir, "animate.lock")
	if err != nil || !ok {
		return err
	}
	defer unlock()
	return AnimateLoop(api, stateDir, o)
}

// AnimateLoop ticks until no agent has worked for o.Idle, the cap passes, or
// ticks keep failing. Each tick takes the snapshot, computes the same tokens
// as render for the current frame and reports only the working marks. A tick
// that finds render holding its lock is skipped, so the two never interleave
// (a stale animate write could otherwise undo render's clear).
func AnimateLoop(api API, stateDir string, o AnimateOpts) error {
	o.defaults()
	start := o.Now()
	lastWorking := start
	fails := 0
	lastSync := start
	for {
		now := o.Now()
		if o.Sync != nil && now.Sub(lastSync) >= o.SyncEvery {
			lastSync = now
			o.Sync()
		}
		if now.Sub(start) >= o.MaxRun {
			return nil
		}
		unlock, ok, err := TryLock(stateDir, "render.lock")
		if err == nil && ok {
			working, terr := tick(api, stateDir, now)
			unlock()
			err = terr
			if terr == nil && working {
				lastWorking = now
			}
		}
		if err != nil {
			fails++
			if fails >= o.MaxErrors {
				return fmt.Errorf("animate: giving up: %w", err)
			}
		} else if ok {
			fails = 0
		}
		if now.Sub(lastWorking) >= o.Idle {
			return nil
		}
		o.Sleep(o.Interval)
	}
}

// tick reports one frame. It returns whether any agent is working.
func tick(api API, stateDir string, t time.Time) (bool, error) {
	snap, err := api.Snapshot()
	if err != nil {
		return false, err
	}
	res := Compute(applyHold(snap, loadHold(filepath.Join(stateDir, "hold.json"))), nil, tokens.FrameAt(t))
	var firstErr error
	send := func(id string, v Values, names []string, report func(string, string, map[string]*string) error) {
		p := map[string]*string{}
		for _, name := range names {
			if val, ok := v[name]; ok && tokens.IsWorking(name) {
				if _, framed := tokens.StripFrame(val); framed {
					s := val
					p[name] = &s
				}
			}
		}
		if len(p) == 0 {
			return
		}
		if err := report(id, tokens.Source, p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, id := range res.PaneOrder {
		send(id, res.Panes[id], AllTokens(), api.ReportPaneTokens)
	}
	for _, w := range snap.Workspaces {
		send(w.ID, res.Workspaces[w.ID], tokens.SpaceTokens(), api.ReportWorkspaceTokens)
	}
	return anyWorking(snap), firstErr
}

var _ API = (*herdr.Client)(nil)
