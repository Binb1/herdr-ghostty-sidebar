package herdr

// Snapshot is the subset of session.snapshot the plugin reads.
type Snapshot struct {
	Workspaces []Workspace `json:"workspaces"`
	Tabs       []Tab       `json:"tabs"`
	Panes      []Pane      `json:"panes"`
	Agents     []Agent     `json:"agents"`

	FocusedPaneID string `json:"focused_pane_id"`
}

type Workspace struct {
	ID     string `json:"workspace_id"`
	Number int    `json:"number"`
	Label  string `json:"label"`

	Tokens map[string]string `json:"tokens"`
}

type Tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
}

type Pane struct {
	ID          string            `json:"pane_id"`
	WorkspaceID string            `json:"workspace_id"`
	TabID       string            `json:"tab_id"`
	Tokens      map[string]string `json:"tokens"`
}

// Agent is one pane running a detected agent.
type Agent struct {
	PaneID                string            `json:"pane_id"`
	WorkspaceID           string            `json:"workspace_id"`
	TabID                 string            `json:"tab_id"`
	Agent                 string            `json:"agent"`
	Name                  string            `json:"name"`
	Status                string            `json:"agent_status"`
	TerminalTitle         string            `json:"terminal_title"`
	TerminalTitleStripped string            `json:"terminal_title_stripped"`
	Cwd                   string            `json:"cwd"`
	ForegroundCwd         string            `json:"foreground_cwd"`
	Focused               bool              `json:"focused"`
	StateChangeSeq        uint64            `json:"state_change_seq"`
	Tokens                map[string]string `json:"tokens"`
}
