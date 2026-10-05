// Package layout generates and edits the marker-fenced blocks this plugin
// owns: the sidebar layout in Herdr's config.toml and the font mapping in
// Ghostty's config. Everything here is pure string manipulation.
package layout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/ghostty"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

const (
	StartMarker = "# >>> herdr-ghostty-sidebar"
	EndMarker   = "# <<< herdr-ghostty-sidebar"

	// FontFamily is the family name inside HerdrSidebarLogos-Regular.ttf.
	FontFamily = "Herdr Sidebar Logos"
	// CodepointRange is the private-use range the font's logos occupy.
	CodepointRange = "U+E1A0-U+E1AB"
)

// Agent is an agent with a logo glyph in the font.
type Agent struct {
	ID    string
	Brand string // "#RRGGBB" logo colour; "" means the theme foreground
}

// Logos lists every agent in the glyph table, in codepoint order
// (U+E1A0 ...).
var Logos = []Agent{
	{"claude", "#D97757"},
	{"codex", "#A78BFA"},
	{"opencode", ""},
	{"omp", ""},
	{"cline", ""},
	{"mastracode", ""},
	{"kimi", ""},
	{"kilo", ""},
	{"maki", ""},
	{"agy", ""},
	{"hermes", ""},
	{"kimchi", ""},
}

// FallbackAccepted is used when Herdr's detector list can't be read: only
// ids the Herdr docs name explicitly. An id Herdr does not know makes the
// whole config fail to load, so unknown is never guessed.
var FallbackAccepted = []string{"claude", "codex"}

// cell renders one styled token. dim adds the terminal's dim attribute.
func cell(token, fg string, bold, dim bool) string {
	s := fmt.Sprintf(`{ token = "$%s"`, token)
	if fg != "" {
		s += fmt.Sprintf(`, fg = "%s"`, fg)
	}
	if bold {
		s += ", bold = true"
	}
	if dim {
		s += ", dim = true"
	}
	return s + " }"
}

// logoCell is a logo cell whose colour follows the glyph it carries: ink by
// default, the brand colour for a glyph with a rule. One token therefore
// serves every vendor (value rules need Herdr 0.9).
func logoCell(token, ink string) string {
	var rules []string
	for _, a := range Logos {
		if a.Brand != "" {
			rules = append(rules, fmt.Sprintf(`{ contains = "\u%04X", fg = "%s" }`, []rune(Glyph(a.ID))[0], a.Brand))
		}
	}
	return fmt.Sprintf(`{ token = "$%s", fg = "%s", rules = [%s] }`, token, ink, strings.Join(rules, ", "))
}

// Glyph returns the logo codepoint of an agent id in the font ("" if none).
func Glyph(id string) string {
	for i, a := range Logos {
		if a.ID == id {
			return string(rune(0xE1A0 + i))
		}
	}
	return ""
}

// Mix blends colour a toward b: w=1 is a, w=0 is b. Both "#rrggbb".
func Mix(a, b string, w float64) string {
	var ar, ag, ab, br, bg, bb int
	if _, err := fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab); err != nil {
		return a
	}
	if _, err := fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb); err != nil {
		return a
	}
	m := func(x, y int) int { return int(float64(x)*w + float64(y)*(1-w) + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", m(ar, br), m(ag, bg), m(ab, bb))
}

// Muted is the quiet colour of group headers, split corners and long-idle
// rows: the foreground pulled toward the background, readable on both
// light and dark themes.
func Muted(p ghostty.Palette) string { return Mix(p.Foreground, p.Background, 0.55) }

// rows renders the two Agents rows for a brand colour: the workspace header
// (bold, muted; empty and so hidden except on a workspace's first agent) and
// the agent row, `[corner] · logo · title`. Exactly one title token is set,
// so mark and title are one cell, coloured by state.
func rows(p ghostty.Palette, brand string) string {
	fg, muted := p.Foreground, Muted(p)
	logo := brand
	working := brand
	if brand == "" {
		logo = fg
		working = p.Colors[3]
	}
	row2 := []string{
		cell(tokens.Split, muted, false, true),
		cell(tokens.Logo, logo, false, false),
		cell(tokens.LogoStale, muted, false, true),
		cell(tokens.TitleWorking, working, false, false),
		cell(tokens.TitleDone, p.Colors[2], false, false),
		cell(tokens.TitleBlocked, p.Colors[1], false, false),
		cell(tokens.TitleIdle, muted, false, false),
		cell(tokens.TitleStale, muted, false, true),
	}
	return "[\n" +
		"  [" + cell(tokens.Group, muted, true, false) + ", " + cell(tokens.GroupStale, muted, true, true) + "],\n" +
		"  [" + strings.Join(row2, ", ") + "],\n]"
}

// spaceBrand returns the colour of a vendor's working marks.
func spaceBrand(p ghostty.Palette, vendor string) string {
	for _, a := range Logos {
		if a.ID == vendor && a.Brand != "" {
			return a.Brand
		}
	}
	return p.Colors[3]
}

// spaceRows renders the Spaces rows: aggregate mark + label, then one row
// per tab, `logo · [mark] tab`.
func spaceRows(p ghostty.Palette) string {
	fg, muted := p.Foreground, Muted(p)
	head := []string{cell(tokens.SpaceBlocked, p.Colors[1], true, false)}
	for _, v := range tokens.Vendors() {
		head = append(head, cell(tokens.SpaceWorking(v), spaceBrand(p, v), true, false))
	}
	head = append(head,
		cell(tokens.SpaceDone, p.Colors[2], true, false),
		cell(tokens.SpaceParked, p.Colors[4], false, false),
		cell(tokens.SpaceEmpty, muted, false, false),
		cell(tokens.SpaceLabel, muted, true, false),
	)
	out := []string{"  [" + strings.Join(head, ", ") + "]"}
	for i := 1; i <= tokens.SpaceTabRows; i++ {
		r := []string{logoCell(tokens.SpaceTabLogo(i), fg)}
		for _, v := range tokens.Vendors() {
			r = append(r, cell(tokens.SpaceTabWorking(i, v), spaceBrand(p, v), false, false))
		}
		r = append(r,
			cell(tokens.SpaceTabDone(i), p.Colors[2], false, false),
			cell(tokens.SpaceTabBlocked(i), p.Colors[1], false, false),
			cell(tokens.SpaceTabIdle(i), muted, false, false),
		)
		out = append(out, "  ["+strings.Join(r, ", ")+"]")
	}
	return "[\n" + strings.Join(out, ",\n") + ",\n]"
}

// HerdrBlock renders the managed block for one palette. accepted lists the
// agent ids Herdr accepts as rows_by_agent keys; only those that also have
// a glyph get an entry.
func HerdrBlock(p ghostty.Palette, accepted []string) string {
	ok := map[string]bool{}
	for _, a := range accepted {
		ok[a] = true
	}
	var b strings.Builder
	b.WriteString(StartMarker + " (managed - regenerated, do not edit)\n")
	b.WriteString("[ui.sidebar.agents]\nrow_gap = 0\nrows = " + rows(p, "") + "\n")
	var entries []string
	for _, a := range Logos {
		if ok[a.ID] {
			entries = append(entries, fmt.Sprintf("%s = %s", a.ID, rows(p, a.Brand)))
		}
	}
	if len(entries) > 0 {
		b.WriteString("\n[ui.sidebar.agents.rows_by_agent]\n")
		b.WriteString(strings.Join(entries, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("\n[ui.sidebar.spaces]\nrow_gap = 0\nrows = " + spaceRows(p) + "\n")
	b.WriteString(EndMarker + "\n")
	return b.String()
}

// GhosttyBlock renders the font mapping block.
func GhosttyBlock() string {
	return StartMarker + " (managed - font mapping for agent logos)\n" +
		"font-codepoint-map = " + CodepointRange + "=" + FontFamily + "\n" +
		EndMarker + "\n"
}

// findBlock returns the line range [start,end] (inclusive) of the managed
// block, or -1,-1.
func findBlock(lines []string) (int, int) {
	s := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if s < 0 && strings.HasPrefix(t, StartMarker) {
			s = i
		} else if s >= 0 && strings.HasPrefix(t, EndMarker) {
			return s, i
		}
	}
	return -1, -1
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.SplitAfter(text, "\n")
}

// Insert replaces the managed block in text with block, or appends it
// (separated by a blank line). Applying the same block twice is a no-op.
func Insert(text, block string) string {
	lines := splitLines(text)
	s, e := findBlock(lines)
	if s >= 0 {
		return strings.Join(lines[:s], "") + block + strings.Join(lines[e+1:], "")
	}
	if text == "" {
		return block
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "\n" + block
}

// Remove deletes the managed block, plus the blank separator Insert added,
// so Remove(Insert(x)) == x for any x ending in a newline.
func Remove(text string) string {
	lines := splitLines(text)
	s, e := findBlock(lines)
	if s < 0 {
		return text
	}
	if s > 0 && strings.TrimSpace(lines[s-1]) == "" {
		s--
	}
	return strings.Join(lines[:s], "") + strings.Join(lines[e+1:], "")
}

// outside returns text with the managed block cut out.
func outside(text string) []string {
	lines := splitLines(text)
	s, e := findBlock(lines)
	if s < 0 {
		return lines
	}
	return append(append([]string{}, lines[:s]...), lines[e+1:]...)
}

// CheckHerdr validates a Herdr config before the block is written. It
// returns an error if [ui.sidebar.agents...] tables already exist outside
// the block (the user owns them; two definitions would not load), and a
// warning if [ui] agent_panel_sort is not "spaces".
func CheckHerdr(text string) (warnings []string, err error) {
	table := ""
	sortVal := ""
	var clash []string
	for _, l := range outside(text) {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[") {
			end := strings.Index(t, "]")
			if end > 0 {
				table = strings.TrimSpace(t[1:end])
			}
			if table == "ui.sidebar.agents" || strings.HasPrefix(table, "ui.sidebar.agents.") || table == "ui.sidebar.spaces" {
				clash = append(clash, table)
			}
			continue
		}
		if table == "ui" {
			if k, v, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == "agent_panel_sort" {
				if i := strings.Index(v, "#"); i >= 0 {
					v = v[:i]
				}
				sortVal = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	}
	if len(clash) > 0 {
		sort.Strings(clash)
		return nil, fmt.Errorf("herdr config already defines [%s] outside the %s block; remove or merge it (refusing to write a duplicate table)", strings.Join(clash, "], ["), StartMarker)
	}
	if sortVal != "spaces" {
		warnings = append(warnings, `[ui] agent_panel_sort is not "spaces"; set agent_panel_sort = "spaces" in your Herdr config so agents group by workspace`)
	}
	return warnings, nil
}
