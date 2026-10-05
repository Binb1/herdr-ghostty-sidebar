// Package tokens names the pane and workspace metadata tokens the render step
// reports and the sidebar layout block displays. Both sides import these so
// the names can't drift apart.
package tokens

import (
	"fmt"
	"strings"
	"time"
)

// Source is the metadata source id every report uses.
const Source = "binb1.ghostty-sidebar"

// BrandVendors are the agents with a brand colour. Their working marks get
// one token each so the colour can differ; every other agent shares "other".
var BrandVendors = []string{"claude", "codex"}

// SpaceTabRows is how many tab rows a Spaces entry shows under its name.
const SpaceTabRows = 3

// Pane tokens (Agents panel). Exactly one title token is set per pane, so
// the single non-empty cell carries the state colour with no extra ` · `.
const (
	Group      = "gs_group"       // workspace label; only on the first agent of a workspace
	GroupStale = "gs_group_stale" // same, when every agent in the workspace is long idle
	Split      = "gs_split"       // indented "└"; second and later agent in a tab
	Logo       = "gs_logo"        // agent logo glyph (font codepoint) or text fallback
	LogoStale  = "gs_logo_stale"  // same glyph for a long-idle agent (dimmed)

	TitleWorking = "gs_title_working" // "<frame> title" in the vendor colour
	TitleDone    = "gs_title_done"    // "✓ title"
	TitleBlocked = "gs_title_blocked" // "? title"
	TitleIdle    = "gs_title_idle"    // "title"
	TitleStale   = "gs_title_stale"   // "title", dimmed
)

// PaneTokens lists every pane token in display order.
var PaneTokens = []string{
	Group, GroupStale, Split, Logo, LogoStale,
	TitleWorking, TitleDone, TitleBlocked, TitleIdle, TitleStale,
}

// Workspace tokens (Spaces panel).
const (
	SpaceLabel   = "gs_sp_label"   // workspace label
	SpaceBlocked = "gs_sp_blocked" // aggregate marks; exactly one is set
	SpaceDone    = "gs_sp_done"
	SpaceParked  = "gs_sp_parked" // agents exist but none working/blocked/done
	SpaceEmpty   = "gs_sp_empty"  // no agents, or all long idle
)

// SpaceWorking is the aggregate working mark for a vendor ("other" for
// agents without a brand colour).
func SpaceWorking(vendor string) string { return "gs_sp_working_" + vendor }

// Per-tab tokens: row i (1-based) of a workspace entry.
func SpaceTabLogo(i int) string              { return fmt.Sprintf("gs_t%d_logo", i) }
func SpaceTabWorking(i int, v string) string { return fmt.Sprintf("gs_t%d_working_%s", i, v) }
func SpaceTabDone(i int) string              { return fmt.Sprintf("gs_t%d_done", i) }
func SpaceTabBlocked(i int) string           { return fmt.Sprintf("gs_t%d_blocked", i) }
func SpaceTabIdle(i int) string              { return fmt.Sprintf("gs_t%d_idle", i) }

// Vendors returns BrandVendors plus "other".
func Vendors() []string { return append(append([]string{}, BrandVendors...), "other") }

// SpaceTokens lists every workspace token.
func SpaceTokens() []string {
	out := []string{SpaceLabel, SpaceBlocked, SpaceDone, SpaceParked, SpaceEmpty}
	for _, v := range Vendors() {
		out = append(out, SpaceWorking(v))
	}
	for i := 1; i <= SpaceTabRows; i++ {
		out = append(out, SpaceTabLogo(i), SpaceTabDone(i), SpaceTabBlocked(i), SpaceTabIdle(i))
		for _, v := range Vendors() {
			out = append(out, SpaceTabWorking(i, v))
		}
	}
	return out
}

// Legacy lists tokens earlier versions reported on panes. They are no longer
// displayed and are cleared wherever they are still found.
var Legacy = []string{
	"gs_ws", "gs_tab", "gs_title", "gs_working", "gs_blocked", "gs_done", "gs_idle",
}

// Glyphs.
const (
	MarkDone    = "✓"
	MarkBlocked = "?"
	MarkParked  = "○"
)

// FrameInterval is how long one spinner frame shows.
const FrameInterval = 125 * time.Millisecond

// Frames is the working spinner, one glyph per FrameInterval.
var Frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// FrameAt is the spinner frame index for a wall-clock time. Render and
// animate both derive it from the clock so they always agree.
func FrameAt(t time.Time) int {
	return int((t.UnixMilli() / FrameInterval.Milliseconds()) % int64(len(Frames)))
}

// Frame is the glyph for a frame index.
func Frame(i int) string { return Frames[((i%len(Frames))+len(Frames))%len(Frames)] }

// StripFrame removes a leading spinner glyph from s, so values that differ
// only in the animation frame compare equal.
func StripFrame(s string) (string, bool) {
	for _, f := range Frames {
		if strings.HasPrefix(s, f) {
			return strings.TrimPrefix(s, f), true
		}
	}
	return s, false
}

// IsWorking reports whether a token name is a working-state token (the only
// ones that carry the spinner).
func IsWorking(name string) bool { return strings.Contains(name, "_working") }
