// Package herdr is a minimal client for Herdr's local JSON-over-unix-socket
// API: one request line, one response line per connection.
package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const maxReply = 32 << 20

// Client talks to one Herdr session socket.
type Client struct {
	SocketPath string
	Timeout    time.Duration
}

// SocketPath resolves the session socket: HERDR_SOCKET_PATH (injected into
// plugin commands), else the session socket under Herdr's config directory
// (HERDR_SESSION selects a named session).
func SocketPath() (string, error) {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p, nil
	}
	dir := os.Getenv("HERDR_CONFIG_PATH")
	if dir != "" {
		dir = filepath.Dir(dir)
	} else if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		dir = filepath.Join(x, "herdr")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("herdr: cannot locate socket: HERDR_SOCKET_PATH is unset and no home directory")
		}
		dir = filepath.Join(home, ".config", "herdr")
	}
	if s := os.Getenv("HERDR_SESSION"); s != "" {
		return filepath.Join(dir, "sessions", s, "herdr.sock"), nil
	}
	return filepath.Join(dir, "herdr.sock"), nil
}

// NewClient builds a client for the resolved socket path.
func NewClient() (*Client, error) {
	p, err := SocketPath()
	if err != nil {
		return nil, err
	}
	return &Client{SocketPath: p, Timeout: 5 * time.Second}, nil
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call sends one request and decodes the "result" object into out (if non-nil).
func (c *Client) Call(method string, params, out any) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := net.DialTimeout("unix", c.SocketPath, timeout)
	if err != nil {
		return fmt.Errorf("herdr: connect %s: %w", c.SocketPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	line, err := json.Marshal(request{ID: "ghostty-sidebar", Method: method, Params: params})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("herdr: %s: %w", method, err)
	}
	r := bufio.NewReaderSize(conn, 64<<10)
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return fmt.Errorf("herdr: %s: reading reply: %w", method, err)
		}
		buf = append(buf, chunk...)
		if len(buf) > maxReply {
			return fmt.Errorf("herdr: %s: reply exceeds %d bytes", method, maxReply)
		}
		if !isPrefix {
			break
		}
	}
	var resp response
	if err := json.Unmarshal(buf, &resp); err != nil {
		return fmt.Errorf("herdr: %s: bad reply: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("herdr: %s: %s", method, resp.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("herdr: %s: decoding result: %w", method, err)
		}
	}
	return nil
}

// Snapshot fetches the session snapshot (session.snapshot).
func (c *Client) Snapshot() (*Snapshot, error) {
	var res struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.Call("session.snapshot", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return &res.Snapshot, nil
}

// ReportPaneTokens patches a pane's tokens for source (pane.report_metadata).
// A nil value clears the token. At most 16 keys per call; longer patches are
// split.
func (c *Client) ReportPaneTokens(paneID, source string, tokens map[string]*string) error {
	return c.report("pane.report_metadata", "pane_id", paneID, source, tokens, 0)
}

// ReportPaneTokensTTL is ReportPaneTokens with ttl_ms set, so Herdr drops the
// tokens by itself after ttl if nothing refreshes or clears them. The patch
// must fit in one call (at most 16 keys); ttl is capped at 24h by Herdr.
func (c *Client) ReportPaneTokensTTL(paneID, source string, tokens map[string]*string, ttl time.Duration) error {
	return c.report("pane.report_metadata", "pane_id", paneID, source, tokens, ttl)
}

// ReportWorkspaceTokens is ReportPaneTokens for a workspace
// (workspace.report_metadata).
func (c *Client) ReportWorkspaceTokens(workspaceID, source string, tokens map[string]*string) error {
	return c.report("workspace.report_metadata", "workspace_id", workspaceID, source, tokens, 0)
}

func (c *Client) report(method, idKey, id, source string, tokens map[string]*string, ttl time.Duration) error {
	const maxKeys = 16
	batch := map[string]*string{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		params := map[string]any{
			idKey:    id,
			"source": source,
			"tokens": batch,
		}
		if ttl > 0 {
			params["ttl_ms"] = ttl.Milliseconds()
		}
		err := c.Call(method, params, nil)
		batch = map[string]*string{}
		return err
	}
	for k, v := range tokens {
		batch[k] = v
		if len(batch) == maxKeys {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}
