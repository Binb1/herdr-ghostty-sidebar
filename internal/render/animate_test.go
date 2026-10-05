package render

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/herdr"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

func TestFrameAt(t *testing.T) {
	base := time.UnixMilli(125 * 10 * 1000) // frame 0
	for i := 0; i < 25; i++ {
		got := tokens.FrameAt(base.Add(time.Duration(i) * 125 * time.Millisecond))
		if got != i%10 {
			t.Fatalf("tick %d: frame %d", i, got)
		}
	}
	if tokens.FrameAt(base.Add(124*time.Millisecond)) != 0 {
		t.Fatal("frame must hold for the whole interval")
	}
	if tokens.Frame(-1) != tokens.Frames[9] || tokens.Frame(12) != tokens.Frames[2] {
		t.Fatal("Frame wraps")
	}
}

func TestComputeFrames(t *testing.T) {
	a := Compute(fixture(), nil, 0)
	b := Compute(fixture(), nil, 3)
	if a.Panes["w1:p1"][tokens.TitleWorking] != "⠋ task one" || b.Panes["w1:p1"][tokens.TitleWorking] != "⠸ task one" {
		t.Fatalf("pane frames: %q %q", a.Panes["w1:p1"][tokens.TitleWorking], b.Panes["w1:p1"][tokens.TitleWorking])
	}
	if b.Workspaces["w1"][tokens.SpaceTabWorking(1, "claude")] != "⠸ first" {
		t.Fatalf("tab frame: %v", b.Workspaces["w1"])
	}
}

func TestDiffIgnoresFrame(t *testing.T) {
	names := AllTokens()
	old := Values{tokens.TitleWorking: "⠋ task"}
	if Diff(old, Values{tokens.TitleWorking: "⠹ task"}, false, names) != nil {
		t.Fatal("frame-only change must not be reported")
	}
	if Diff(old, Values{tokens.TitleWorking: "⠹ other"}, false, names) == nil {
		t.Fatal("text change must be reported")
	}
	if Diff(old, Values{tokens.TitleWorking: "⠹ task"}, true, names) == nil {
		t.Fatal("force reports everything")
	}
	if p := Diff(old, Values{}, false, names); p == nil || p[tokens.TitleWorking] != nil {
		t.Fatal("a stopped agent's working token must be cleared")
	}
}

// loopAPI serves a scripted snapshot and counts calls.
type loopAPI struct {
	fakeAPI
	snaps     func(call int) (*herdr.Snapshot, error)
	calls     int
	paneCalls int
}

func (l *loopAPI) Snapshot() (*herdr.Snapshot, error) {
	l.calls++
	return l.snaps(l.calls)
}
func (l *loopAPI) ReportPaneTokens(id, s string, t map[string]*string) error {
	l.paneCalls++
	return l.fakeAPI.ReportPaneTokens(id, s, t)
}

// clock is a fake time source whose Sleep advances it.
type clock struct{ t time.Time }

func (c *clock) now() time.Time        { return c.t }
func (c *clock) sleep(d time.Duration) { c.t = c.t.Add(d) }
func (c *clock) opts() AnimateOpts     { return AnimateOpts{Now: c.now, Sleep: c.sleep} }
func newClock() *clock                 { return &clock{t: time.UnixMilli(1_000_000_000)} }

func TestAnimateReportsOnlyWorkingTokens(t *testing.T) {
	c := newClock()
	api := &loopAPI{snaps: func(n int) (*herdr.Snapshot, error) {
		s := fixture()
		if n > 4 {
			for i := range s.Agents {
				s.Agents[i].Status = "idle"
			}
		}
		return s, nil
	}}
	if err := AnimateLoop(api, t.TempDir(), c.opts()); err != nil {
		t.Fatal(err)
	}
	ps := api.reports["w1:p1"]
	if len(ps) != 4 {
		t.Fatalf("w1:p1 got %d patches", len(ps))
	}
	for i, p := range ps {
		if len(p) != 1 || p[tokens.TitleWorking] == nil || !strings.HasSuffix(*p[tokens.TitleWorking], " task one") {
			t.Fatalf("patch %d: %v", i, p)
		}
	}
	if *ps[0][tokens.TitleWorking] == *ps[1][tokens.TitleWorking] {
		t.Fatal("frame must advance between ticks")
	}
	if _, ok := api.reports["w1:p2"]; ok {
		t.Fatal("non-working pane must not be reported")
	}
	ws := api.wsRep["w1"]
	if len(ws) == 0 || ws[0][tokens.SpaceTabWorking(1, "claude")] == nil {
		t.Fatalf("workspace tab row missing: %v", ws)
	}
}

func TestAnimateExitsAfterIdle(t *testing.T) {
	c := newClock()
	api := &loopAPI{snaps: func(int) (*herdr.Snapshot, error) {
		return &herdr.Snapshot{Agents: []herdr.Agent{{PaneID: "p", Status: "idle"}}}, nil
	}}
	if err := AnimateLoop(api, t.TempDir(), c.opts()); err != nil {
		t.Fatal(err)
	}
	// 3s / 125ms = 24 sleeps, so 25 snapshots.
	if api.calls != 25 || api.paneCalls != 0 {
		t.Fatalf("calls=%d reports=%d", api.calls, api.paneCalls)
	}
}

func TestAnimateGivesUpOnErrors(t *testing.T) {
	c := newClock()
	api := &loopAPI{snaps: func(int) (*herdr.Snapshot, error) { return nil, errors.New("boom") }}
	err := AnimateLoop(api, t.TempDir(), c.opts())
	if err == nil || api.calls != 5 {
		t.Fatalf("err=%v calls=%d", err, api.calls)
	}
}

func TestAnimateHardCap(t *testing.T) {
	c := newClock()
	o := c.opts()
	o.MaxRun = time.Second
	api := &loopAPI{snaps: func(int) (*herdr.Snapshot, error) { return fixture(), nil }}
	if err := AnimateLoop(api, t.TempDir(), o); err != nil {
		t.Fatal(err)
	}
	if api.calls != 8 {
		t.Fatalf("calls=%d", api.calls)
	}
}

func TestAnimateSkipsTickWhileRenderRuns(t *testing.T) {
	dir := t.TempDir()
	unlock, ok, err := TryLock(dir, "render.lock")
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer unlock()
	c := newClock()
	o := c.opts()
	o.MaxRun = time.Second
	api := &loopAPI{snaps: func(int) (*herdr.Snapshot, error) { return fixture(), nil }}
	if err := AnimateLoop(api, dir, o); err != nil {
		t.Fatal(err)
	}
	if api.calls != 0 {
		t.Fatalf("must not touch Herdr while render holds the lock; calls=%d", api.calls)
	}
}

func TestAnimateLock(t *testing.T) {
	dir := t.TempDir()
	if AnimateRunning(dir) {
		t.Fatal("nothing runs yet")
	}
	unlock, ok, err := TryLock(dir, "animate.lock")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if !AnimateRunning(dir) {
		t.Fatal("lock held")
	}
	api := &loopAPI{snaps: func(int) (*herdr.Snapshot, error) { return fixture(), nil }}
	if err := Animate(api, dir, AnimateOpts{}); err != nil || api.calls != 0 {
		t.Fatalf("second animate must exit at once: err=%v calls=%d", err, api.calls)
	}
	unlock()
	if AnimateRunning(dir) {
		t.Fatal("released")
	}
}

func TestRunWorking(t *testing.T) {
	api := &fakeAPI{snap: fixture()}
	w, err := RunWorking(api, t.TempDir(), false)
	if err != nil || !w {
		t.Fatalf("w=%v err=%v", w, err)
	}
}

func TestAnimateSyncsLayoutEveryTwoSeconds(t *testing.T) {
	c := newClock()
	api := &loopAPI{snaps: func(n int) (*herdr.Snapshot, error) {
		s := fixture() // always working: runs until the hard cap
		return s, nil
	}}
	o := c.opts()
	o.MaxRun = 10*time.Second + time.Millisecond
	syncs := 0
	o.Sync = func() { syncs++ }
	if err := AnimateLoop(api, t.TempDir(), o); err != nil {
		t.Fatal(err)
	}
	if syncs != 5 {
		t.Fatalf("syncs = %d over 10s, want 5", syncs)
	}
}
