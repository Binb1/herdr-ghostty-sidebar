// Package claude connects Claude Code subagents to the sidebar: a hook
// handler that reports the "└ ✳ <description>" line, and the installer that
// registers that hook in ~/.claude/settings.json.
package claude

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

// TTL is how long a subagent line lives without a refresh; it guards against
// a missed stop event leaving the line up forever.
const TTL = 15 * time.Minute

const maxDesc = 60

// Reporter is the part of the Herdr client the hook needs.
type Reporter interface {
	ReportPaneTokensTTL(paneID, source string, tokens map[string]*string, ttl time.Duration) error
}

// Env is everything Handle needs from the process environment.
type Env struct {
	HerdrEnv string // HERDR_ENV
	PaneID   string // HERDR_PANE_ID
	StateDir string // where the per-pane counter lives
	Now      time.Time
}

type event struct {
	Name      string `json:"hook_event_name"`
	ToolName  string `json:"tool_name"`
	AgentID   string `json:"agent_id"`
	ToolInput struct {
		Description string `json:"description"`
	} `json:"tool_input"`
}

type entry struct {
	Desc string `json:"d"`
	At   int64  `json:"t"` // unix ms
}

// Handle processes one hook event. It returns an error for the caller to
// ignore or log; nothing here should ever fail the hook.
func Handle(in io.Reader, env Env, rep Reporter) error {
	if env.HerdrEnv != "1" || env.PaneID == "" {
		return nil
	}
	var ev event
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		return err
	}
	// Events fired inside a subagent's own session carry agent_id; the only
	// one we want is SubagentStop, which the parent receives with the id of
	// the subagent that stopped.
	if ev.AgentID != "" && ev.Name != "SubagentStop" {
		return nil
	}
	var op func([]entry) []entry
	switch ev.Name {
	case "PreToolUse":
		if ev.ToolName != "Task" && ev.ToolName != "Agent" {
			return nil
		}
		desc := clean(ev.ToolInput.Description)
		if desc == "" {
			desc = "subagent"
		}
		op = func(l []entry) []entry { return append(l, entry{desc, env.Now.UnixMilli()}) }
	case "SubagentStop":
		// The stopping subagent can't be matched to its PreToolUse entry
		// (no shared id), so drop the oldest.
		op = func(l []entry) []entry {
			if len(l) > 0 {
				l = l[1:]
			}
			return l
		}
	case "Stop", "SessionEnd":
		op = func([]entry) []entry { return nil }
	default:
		return nil
	}

	f, err := lockState(env.StateDir, env.PaneID)
	if err != nil {
		return err
	}
	defer f.Close() // releases the lock

	list := readState(f)
	cutoff := env.Now.Add(-TTL).UnixMilli()
	live := list[:0]
	for _, e := range list {
		if e.At > cutoff {
			live = append(live, e)
		}
	}
	list = op(live)
	writeState(f, list)

	var value *string
	if n := len(list); n > 0 {
		line := "└ ✳ " + list[n-1].Desc
		if n > 1 {
			line += " +" + strconv.Itoa(n-1)
		}
		line = tokens.Indent(line)
		value = &line
	}
	patch := map[string]*string{tokens.Worker: value}
	if value == nil {
		return rep.ReportPaneTokensTTL(env.PaneID, tokens.WorkerSource, patch, 0)
	}
	return rep.ReportPaneTokensTTL(env.PaneID, tokens.WorkerSource, patch, TTL)
}

// clean collapses whitespace and truncates to maxDesc runes.
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxDesc {
		r := []rune(s)
		s = string(r[:maxDesc-1]) + "…"
	}
	return s
}

func lockState(dir, paneID string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, paneID)
	f, err := os.OpenFile(filepath.Join(dir, "claude-"+name+".json"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func readState(f *os.File) []entry {
	b, err := io.ReadAll(f)
	var l []entry
	if err != nil || json.Unmarshal(b, &l) != nil {
		return nil
	}
	return l
}

func writeState(f *os.File, l []entry) {
	b, _ := json.Marshal(l)
	if f.Truncate(0) == nil {
		_, _ = f.WriteAt(b, 0)
	}
}
