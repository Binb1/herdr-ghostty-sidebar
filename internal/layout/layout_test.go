package layout

import (
	"strings"
	"testing"

	"github.com/Binb1/herdr-ghostty-sidebar/internal/ghostty"
	"github.com/Binb1/herdr-ghostty-sidebar/internal/tokens"
)

func pal() ghostty.Palette {
	p := ghostty.DefaultPalette()
	p.Foreground = "#eff1f8"
	p.Colors[8] = "#585b70"
	return p
}

func TestHerdrBlock(t *testing.T) {
	b := HerdrBlock(pal(), []string{"claude", "codex", "pi", "kimchi-not", "hermes"})
	muted := Muted(pal())
	for _, want := range []string{
		"[ui.sidebar.agents]", "row_gap = 0", "[ui.sidebar.agents.rows_by_agent]", "[ui.sidebar.spaces]",
		"claude = [", "codex = [", "hermes = [",
		// header: bold, muted; the stale variant is also dim
		`{ token = "$gs_group", fg = "` + muted + `", bold = true }`,
		`{ token = "$gs_group_stale", fg = "` + muted + `", bold = true, dim = true }`,
		`{ token = "$gs_logo", fg = "#D97757" }`, `{ token = "$gs_logo", fg = "#A78BFA" }`,
		`{ token = "$gs_title_working", fg = "#D97757" }`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %q in\n%s", want, b)
		}
	}
	if strings.Contains(b, "pi = [") || strings.Contains(b, "kimchi = [") {
		t.Error("emitted unaccepted agent")
	}
	if !strings.HasPrefix(b, StartMarker) || !strings.HasSuffix(b, EndMarker+"\n") {
		t.Error("markers")
	}
	for _, old := range append(append([]string{}, tokens.Legacy...), "gs_tab") {
		if strings.Contains(b, `"$`+old+`"`) {
			t.Errorf("legacy token %s still displayed", old)
		}
	}
}

// tokensOf returns the "$token" names of one rendered row, in order.
func tokensOf(row string) []string {
	var out []string
	for _, part := range strings.Split(row, `token = "$`)[1:] {
		out = append(out, part[:strings.Index(part, `"`)])
	}
	return out
}

func rowLines(block string) []string {
	var out []string
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(l, "  [{") {
			out = append(out, l)
		}
	}
	return out
}

func TestAgentRowShape(t *testing.T) {
	r := rows(pal(), "#D97757")
	lines := rowLines(r)
	if len(lines) != 4 {
		t.Fatalf("want header, agent, branch and worker rows, got %d:\n%s", len(lines), r)
	}
	if got := strings.Join(tokensOf(lines[2]), ","); got != "gs_branch,gs_branch_idle" {
		t.Errorf("branch row: %s", got)
	}
	if got := strings.Join(tokensOf(lines[3]), ","); got != "gs_worker" {
		t.Errorf("worker row: %s", got)
	}
	if !strings.Contains(lines[2], `fg = "`+pal().Colors[4]+`"`) || !strings.Contains(lines[3], "dim = true") {
		t.Errorf("branch/worker colours: %s %s", lines[2], lines[3])
	}
	if got := strings.Join(tokensOf(lines[0]), ","); got != "gs_group,gs_group_stale" {
		t.Errorf("header row: %s", got)
	}
	// Mark and title share one token per state: no separate state-dot cell.
	want := "gs_split,gs_logo,gs_logo_stale,gs_title_working,gs_title_done,gs_title_blocked,gs_title_idle,gs_title_stale"
	if got := strings.Join(tokensOf(lines[1]), ","); got != want {
		t.Errorf("agent row: %s", got)
	}
	p := pal()
	for _, w := range []string{
		`{ token = "$gs_title_done", fg = "` + p.Colors[2] + `" }`,
		`{ token = "$gs_title_blocked", fg = "` + p.Colors[1] + `" }`,
		`{ token = "$gs_title_idle", fg = "` + Muted(p) + `" }`,
		`{ token = "$gs_title_stale", fg = "` + Muted(p) + `", dim = true }`,
	} {
		if !strings.Contains(lines[1], w) {
			t.Errorf("missing %s", w)
		}
	}
	// Unbranded agents: foreground logo, yellow (palette 3) working title.
	plain := rowLines(rows(p, ""))[1]
	if !strings.Contains(plain, `{ token = "$gs_logo", fg = "`+p.Foreground+`" }`) ||
		!strings.Contains(plain, `{ token = "$gs_title_working", fg = "`+p.Colors[3]+`" }`) {
		t.Errorf("unbranded row: %s", plain)
	}
}

func TestSpaceRowShape(t *testing.T) {
	lines := rowLines(spaceRows(pal()))
	if len(lines) != 1+tokens.SpaceTabRows {
		t.Fatalf("want name row + %d tab rows, got %d", tokens.SpaceTabRows, len(lines))
	}
	head := strings.Join(tokensOf(lines[0]), ",")
	if head != "gs_sp_blocked,gs_sp_working_claude,gs_sp_working_codex,gs_sp_working_other,gs_sp_done,gs_sp_parked,gs_sp_empty,gs_sp_label" {
		t.Errorf("name row: %s", head)
	}
	p := pal()
	for _, w := range []string{
		`{ token = "$gs_sp_parked", fg = "` + p.Colors[4] + `" }`,   // parked: blue
		`{ token = "$gs_sp_empty", fg = "` + Muted(p) + `" }`,       // empty: grey
		`{ token = "$gs_sp_done", fg = "` + p.Colors[2] + `", bold`, // done: green
		`{ token = "$gs_sp_blocked", fg = "` + p.Colors[1] + `", bold`,
	} {
		if !strings.Contains(lines[0], w) {
			t.Errorf("missing %s", w)
		}
	}
	tab := strings.Join(tokensOf(lines[1]), ",")
	if tab != "gs_t1_logo,gs_t1_working_claude,gs_t1_working_codex,gs_t1_working_other,gs_t1_done,gs_t1_blocked,gs_t1_idle" {
		t.Errorf("tab row: %s", tab)
	}
	if !strings.Contains(lines[1], `rules = [{ contains = "\uE1A0", fg = "#D97757" }, { contains = "\uE1A1", fg = "#A78BFA" }]`) {
		t.Errorf("logo rules: %s", lines[1])
	}
	for _, l := range lines {
		if n := len(tokensOf(l)); n > 16 {
			t.Errorf("%d tokens in a row", n)
		}
	}
	// Every token a row shows is one the render step reports, and vice versa.
	shown := map[string]bool{}
	for _, l := range lines {
		for _, n := range tokensOf(l) {
			shown[n] = true
		}
	}
	for _, n := range tokens.SpaceTokens() {
		if !shown[n] {
			t.Errorf("reported token %s is never displayed", n)
		}
	}
	if len(shown) != len(tokens.SpaceTokens()) {
		t.Errorf("displayed %d, reported %d", len(shown), len(tokens.SpaceTokens()))
	}
}

func TestMuted(t *testing.T) {
	p := ghostty.DefaultPalette()
	p.Foreground, p.Background = "#ffffff", "#000000"
	if got := Mix("#ffffff", "#000000", 0.5); got != "#808080" {
		t.Errorf("mix = %s", got)
	}
	if got := Muted(p); got == p.Foreground || got == p.Background {
		t.Errorf("muted = %s", got)
	}
}

func TestIdempotentAndRemove(t *testing.T) {
	orig := "[ui]\nagent_panel_sort = \"spaces\"\n\n[keys]\nx = 1\n"
	block := HerdrBlock(pal(), []string{"claude"})
	once := Insert(orig, block)
	twice := Insert(once, block)
	if once != twice {
		t.Error("not idempotent")
	}
	if got := Remove(once); got != orig {
		t.Errorf("remove: %q", got)
	}
	// replace with different content
	b2 := HerdrBlock(ghostty.DefaultPalette(), []string{"claude"})
	if r := Insert(once, b2); strings.Count(r, StartMarker) != 1 || r == once {
		t.Error("replace failed")
	}
	if Remove(Insert("", block)) != "" {
		t.Error("empty roundtrip")
	}
	// content after the block is preserved
	mid := Insert(orig, block) + "[more]\ny = 2\n"
	if !strings.HasSuffix(Remove(mid), "[more]\ny = 2\n") {
		t.Error("tail lost")
	}
}

func TestGhosttyBlockRoundTrip(t *testing.T) {
	orig := "theme = a\nfont-size = 12\n"
	b := GhosttyBlock()
	if !strings.Contains(b, "font-codepoint-map = U+E1A0-U+E1AB=Herdr Sidebar Logos") {
		t.Error(b)
	}
	once := Insert(orig, b)
	if Insert(once, b) != once || Remove(once) != orig {
		t.Error("roundtrip")
	}
}

func TestCheckHerdr(t *testing.T) {
	if _, err := CheckHerdr("[ui.sidebar.agents]\nrows = []\n"); err == nil {
		t.Error("expected refusal")
	}
	if _, err := CheckHerdr("[ui.sidebar.agents.rows_by_agent]\n"); err == nil {
		t.Error("expected refusal for subtable")
	}
	block := HerdrBlock(pal(), []string{"claude"})
	w, err := CheckHerdr(Insert("[ui]\nagent_panel_sort = \"spaces\"\n", block))
	if err != nil || len(w) != 0 {
		t.Errorf("own block should pass: %v %v", w, err)
	}
	w, err = CheckHerdr("[ui]\nagent_panel_sort = \"attention\"\n")
	if err != nil || len(w) != 1 {
		t.Errorf("want warning: %v %v", w, err)
	}
	if _, err := CheckHerdr("[ui.sidebar.spaces]\nrows = []\n"); err == nil {
		t.Error("expected refusal for spaces table")
	}
	w, _ = CheckHerdr("")
	if len(w) != 1 {
		t.Error("missing sort should warn")
	}
}

func TestBranchAndWorkerRowsEverywhere(t *testing.T) {
	b := HerdrBlock(pal(), []string{"claude", "codex"})
	// default rows + one rows_by_agent entry per accepted agent
	if n := strings.Count(b, `"$gs_branch"`); n != 3 {
		t.Errorf("branch row count %d, want 3", n)
	}
	if n := strings.Count(b, `"$gs_worker"`); n != 3 {
		t.Errorf("worker row count %d, want 3", n)
	}
}
