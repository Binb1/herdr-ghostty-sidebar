package herdr

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeServer struct {
	path string
	mu   sync.Mutex
	reqs []map[string]any
}

func newFake(t *testing.T, reply func(req map[string]any) string) *fakeServer {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeServer{path: filepath.Join(dir, "s.sock")}
	l, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req map[string]any
				if json.Unmarshal(line, &req) != nil {
					return
				}
				f.mu.Lock()
				f.reqs = append(f.reqs, req)
				f.mu.Unlock()
				c.Write([]byte(reply(req) + "\n"))
			}()
		}
	}()
	return f
}

func TestSnapshot(t *testing.T) {
	f := newFake(t, func(req map[string]any) string {
		return `{"id":"x","result":{"type":"session_snapshot","snapshot":{"workspaces":[{"workspace_id":"w1","number":1,"label":"A"}],"tabs":[],"panes":[{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1"}],"agents":[{"pane_id":"w1:p1","agent":"claude","name":null,"agent_status":"idle","terminal_title_stripped":"hi"}]}}}`
	})
	c := &Client{SocketPath: f.path, Timeout: 2 * time.Second}
	s, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if f.reqs[0]["method"] != "session.snapshot" {
		t.Fatalf("method = %v", f.reqs[0]["method"])
	}
	if len(s.Workspaces) != 1 || s.Agents[0].TerminalTitleStripped != "hi" || s.Agents[0].Name != "" {
		t.Fatalf("bad snapshot %+v", s)
	}
}

func TestReportPaneTokens(t *testing.T) {
	f := newFake(t, func(req map[string]any) string { return `{"id":"x","result":{"type":"ok"}}` })
	c := &Client{SocketPath: f.path}
	v := "●"
	if err := c.ReportPaneTokens("w1:p1", "src", map[string]*string{"a": &v, "b": nil}); err != nil {
		t.Fatal(err)
	}
	req := f.reqs[0]
	if req["method"] != "pane.report_metadata" {
		t.Fatalf("method = %v", req["method"])
	}
	p := req["params"].(map[string]any)
	if p["pane_id"] != "w1:p1" || p["source"] != "src" {
		t.Fatalf("params = %v", p)
	}
	tk := p["tokens"].(map[string]any)
	if tk["a"] != "●" {
		t.Fatalf("tokens = %v", tk)
	}
	if val, ok := tk["b"]; !ok || val != nil {
		t.Fatalf("b should be explicit null, got %v (present=%v)", val, ok)
	}
}

func TestReportSplitsLargePatch(t *testing.T) {
	f := newFake(t, func(req map[string]any) string { return `{"id":"x","result":{}}` })
	c := &Client{SocketPath: f.path}
	patch := map[string]*string{}
	for i := 0; i < 20; i++ {
		patch[string(rune('a'+i))] = nil
	}
	if err := c.ReportPaneTokens("p", "s", patch); err != nil {
		t.Fatal(err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(f.reqs))
	}
}

func TestErrorReply(t *testing.T) {
	f := newFake(t, func(req map[string]any) string {
		return `{"id":"x","error":{"code":"pane_not_found","message":"no such pane"}}`
	})
	c := &Client{SocketPath: f.path}
	err := c.ReportPaneTokens("p", "s", map[string]*string{"a": nil})
	if err == nil || !strings.Contains(err.Error(), "no such pane") {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectError(t *testing.T) {
	c := &Client{SocketPath: filepath.Join(t.TempDir(), "none.sock")}
	if _, err := c.Snapshot(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/x/y.sock")
	if p, _ := SocketPath(); p != "/x/y.sock" {
		t.Fatal(p)
	}
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("HERDR_CONFIG_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("HERDR_SESSION", "")
	if p, _ := SocketPath(); p != "/cfg/herdr/herdr.sock" {
		t.Fatal(p)
	}
	t.Setenv("HERDR_SESSION", "dev")
	if p, _ := SocketPath(); p != "/cfg/herdr/sessions/dev/herdr.sock" {
		t.Fatal(p)
	}
}

func TestReportWorkspaceTokens(t *testing.T) {
	f := newFake(t, func(req map[string]any) string { return `{"id":"x","result":{"type":"ok"}}` })
	c := &Client{SocketPath: f.path}
	v := "✓"
	if err := c.ReportWorkspaceTokens("w1", "src", map[string]*string{"a": &v, "b": nil}); err != nil {
		t.Fatal(err)
	}
	req := f.reqs[0]
	p := req["params"].(map[string]any)
	if req["method"] != "workspace.report_metadata" || p["workspace_id"] != "w1" || p["source"] != "src" {
		t.Fatalf("request = %v", req)
	}
	if tk := p["tokens"].(map[string]any); tk["a"] != "✓" || tk["b"] != nil {
		t.Fatalf("tokens = %v", tk)
	}
}

func TestReportPaneTokensTTL(t *testing.T) {
	f := newFake(t, func(req map[string]any) string { return `{"id":"x","result":{}}` })
	c := &Client{SocketPath: f.path}
	v := "x"
	if err := c.ReportPaneTokensTTL("p", "s", map[string]*string{"a": &v}, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := f.reqs[0]["params"].(map[string]any)["ttl_ms"]; got != float64(900000) {
		t.Fatalf("ttl_ms = %v", got)
	}
}
