# herdr-ghostty-sidebar

A [Herdr](https://herdr.dev) plugin that restyles the Agents and Spaces sidebar panels. Each agent gets its vendor logo and a state-coloured title, and everything is coloured from your **Ghostty theme** (light and dark), so the sidebar matches your terminal. No daemon, no animation, no settings UI.

<!-- screenshot: docs/screenshot.png -->

## Install

```sh
herdr plugin install Binb1/herdr-ghostty-sidebar --yes
herdr plugin action invoke setup --plugin binb1.ghostty-sidebar
```

Then restart Ghostty (it only reads the font mapping at startup).

## What `setup` changes

Each file is backed up once to `<file>.bak-ghostty-sidebar` before the first edit. Nothing else is touched.

- **Herdr config** (`$HERDR_CONFIG_PATH`, else `~/.config/herdr/config.toml`): one marker-fenced block (`# >>> herdr-ghostty-sidebar` ... `# <<< herdr-ghostty-sidebar`) with `[ui.sidebar.agents]`, `[ui.sidebar.agents.rows_by_agent]` and `[ui.sidebar.spaces]`. Setup refuses if you already define those tables yourself, and warns if `[ui] agent_panel_sort` is not `"spaces"`.
- **Ghostty config**: one marker-fenced `font-codepoint-map = U+E1A0-U+E1AB=Herdr Sidebar Logos` line.
- **Font**: `HerdrSidebarLogos-Regular.ttf` copied to `~/Library/Fonts` (macOS) or `~/.local/share/fonts` (Linux, then `fc-cache -f`).

Herdr row styles take fixed hex colours, so the block is generated for the current appearance and rewritten (then `herdr server reload-config`) when macOS/GNOME switches between light and dark or you change your Ghostty theme. This happens on Herdr events and only writes when the text changed.

## Uninstall

```sh
herdr plugin action invoke uninstall --plugin binb1.ghostty-sidebar
herdr plugin uninstall binb1.ghostty-sidebar
```

Removes both blocks and the font and clears the reported tokens; your config is otherwise left as it was.

## What the sidebar shows

```
agents                              spaces
1. Fodmap                           ✓ · 1. Fodmap
   ✳ · ✓ ROBIN-83 recomm…              ✳ · ✓ ROBIN-83 recomm…
   └ · ✳ · MY FODMAP t…             ○ · 2. Fodmap Backend
7. moqa-aso-bot                        ✳ · Landing page
   ✳ · Claude Code                  ○ · 4. Dump-it
```

Herdr puts ` · ` between non-empty cells, so rows use as few cells as possible.

**Agents panel.** A bold, muted header row with the workspace label (dimmed when every agent in it has been idle for two hours), then one row per agent: `logo · title`. The state mark and title are a single cell coloured by state: a working glyph in the vendor colour while working, green `✓` when done, red `?` when blocked, plain foreground when idle, dim when idle for two hours. The second and later agent in a tab get a `└` corner. Rows under a header are indented with a zero-width space followed by spaces, because Herdr trims plain leading whitespace.

**Spaces panel.** Row 1 is an aggregate mark and the workspace label: working glyph (vendor colour), `✓`, `?`, a blue `○` when agents exist but are parked, a grey `○` when the workspace is empty or every agent is long idle. Below it, up to three tab rows `logo · [mark] tab name`; an unnamed tab shows its lead agent's title and is dropped when empty. The branch row is gone.

**Hot workspaces.** When one agent in a workspace is working, its parked siblings (agents and tabs) are painted in the working colour.

The plugin reports pane tokens `gs_group`, `gs_group_stale`, `gs_split`, `gs_logo`, `gs_logo_stale`, `gs_title_{working,done,blocked,idle,stale}` and workspace tokens `gs_sp_*`, `gs_t<N>_*` (source `binb1.ghostty-sidebar`). Tokens from earlier versions (`gs_ws`, `gs_tab`, `gs_title`, `gs_working`, `gs_blocked`, `gs_done`, `gs_idle`) are cleared on the next render. The "idle for two hours" clock starts when the plugin first sees an agent in its current state.

## Claude Code subagent line

While Claude Code runs subagents (the `Task`/`Agent` tool), the agent's row gets an extra muted line under it: `└ ✳ <subagent description>`, with ` +N` when several run in parallel. It disappears when the last subagent stops, when Claude stops or the session ends (and after 15 minutes at most).

```sh
herdr plugin action invoke claude-install --plugin binb1.ghostty-sidebar
```

This writes `~/.claude/hooks/herdr-ghostty-sidebar.sh` (a wrapper that runs `bin/herdr-ghostty-sidebar claude-hook`) and adds it to `~/.claude/settings.json` for `PreToolUse` (matcher `Task|Agent`), `SubagentStop`, `Stop` and `SessionEnd`. Your other hooks and settings are kept as they are (the file is re-indented with 2 spaces) and the original is backed up once to `settings.json.bak-ghostty-sidebar`. Re-run it after updating the plugin to refresh the wrapper. The hook only acts inside Herdr panes and never prints or fails. Its small per-pane state lives in `$XDG_STATE_HOME/herdr-ghostty-sidebar` (default `~/.local/state/herdr-ghostty-sidebar`).

```sh
herdr plugin action invoke claude-uninstall --plugin binb1.ghostty-sidebar
```

removes only those entries and the wrapper.

## Colours

The theme comes from `theme =` in your Ghostty config (`light:A,dark:B` or a single name), looked up in `~/.config/ghostty/themes`, then Ghostty's bundled themes. `palette`, `foreground` and `background` lines in your main config override the theme.

| Element | Colour |
| --- | --- |
| Group header, Spaces label, split corner, dimmed rows | foreground mixed 55/45 with background (muted) |
| Idle title | foreground |
| Working | vendor colour (Claude, Codex), else palette 3 |
| Blocked | palette 1 |
| Done | palette 2 |
| Parked ring | palette 4 |
| Logo | vendor colour (Claude, Codex) or foreground |

## Local development

```sh
go build -o bin/herdr-ghostty-sidebar . && herdr plugin link .
go vet ./... && go test ./...
```

## Credits

The font, logo set and the token approach come from [testy-cool/herdr-sidebar-config](https://github.com/testy-cool/herdr-sidebar-config) (MIT). Config-block handling follows ideas from herdr-radar. Logo marks belong to their owners; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). MIT licensed, see [LICENSE](LICENSE).
