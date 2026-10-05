package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	scriptName = "herdr-ghostty-sidebar.sh"
	backupSufx = ".bak-ghostty-sidebar"
)

// hookEvents are the events our hook is registered for, with their matcher.
var hookEvents = []struct{ name, matcher string }{
	{"PreToolUse", "Task|Agent"},
	{"SubagentStop", ""},
	{"Stop", ""},
	{"SessionEnd", ""},
}

// Paths locates the files Install and Uninstall touch.
type Paths struct {
	Home       string // replaces $HOME
	PluginRoot string // plugin checkout holding bin/herdr-ghostty-sidebar
}

func (p Paths) claudeDir() string {
	h := p.Home
	if h == "" {
		h, _ = os.UserHomeDir()
	}
	return filepath.Join(h, ".claude")
}
func (p Paths) settings() string { return filepath.Join(p.claudeDir(), "settings.json") }
func (p Paths) script() string   { return filepath.Join(p.claudeDir(), "hooks", scriptName) }

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func scriptBody(root string) string {
	return "#!/bin/sh\n" +
		"# managed by herdr-ghostty-sidebar (claude-install); the plugin keeps the path current.\n" +
		"bin=" + shq(filepath.Join(root, "bin", "herdr-ghostty-sidebar")) + "\n" +
		"[ -x \"$bin\" ] || exit 0\n" +
		"exec \"$bin\" claude-hook\n"
}

func (p Paths) command() string { return "bash " + shq(p.script()) }

// Install writes the wrapper script and registers it in settings.json.
func Install(p Paths) error {
	if p.PluginRoot == "" {
		return errors.New("cannot locate the plugin root")
	}
	old, err := readOptional(p.settings())
	if err != nil {
		return err
	}
	updated, err := mergeSettings(old, p.command())
	if err != nil {
		return fmt.Errorf("%s: %w", p.settings(), err)
	}
	if err := os.MkdirAll(filepath.Dir(p.script()), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(p.script(), scriptBody(p.PluginRoot), 0o755); err != nil {
		return err
	}
	if updated == old {
		return nil
	}
	if err := backupOnce(p.settings()); err != nil {
		return err
	}
	return writeAtomic(p.settings(), updated, 0o644)
}

// RefreshScript points an installed wrapper script at the current plugin
// root, so a plugin update or reinstall never needs claude-install again.
// It does nothing when the hook isn't installed (no script) or is current.
func RefreshScript(p Paths) (changed bool, err error) {
	if p.PluginRoot == "" {
		return false, nil
	}
	cur, err := os.ReadFile(p.script())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	want := scriptBody(p.PluginRoot)
	if string(cur) == want {
		return false, nil
	}
	return true, writeAtomic(p.script(), want, 0o755)
}

// Uninstall removes our settings entries and the wrapper script.
func Uninstall(p Paths) error {
	old, err := readOptional(p.settings())
	if err != nil {
		return err
	}
	if old != "" {
		updated, err := unmergeSettings(old)
		if err != nil {
			return fmt.Errorf("%s: %w", p.settings(), err)
		}
		if updated != old {
			if err := backupOnce(p.settings()); err != nil {
				return err
			}
			if err := writeAtomic(p.settings(), updated, 0o644); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(p.script()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func readOptional(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func backupOnce(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	bak := path + backupSufx
	if _, err := os.Stat(bak); err == nil {
		return nil
	}
	return os.WriteFile(bak, b, 0o644)
}

func writeAtomic(path, content string, defMode fs.FileMode) error {
	mode := defMode
	if st, err := os.Stat(path); err == nil && defMode != 0o755 {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// --- order-preserving JSON editing ---------------------------------------

type member struct {
	key string
	val json.RawMessage
}

type object []member

func parseObject(raw []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var o object
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, member{k.(string), v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return o, nil
}

func (o object) index(k string) int {
	for i, m := range o {
		if m.key == k {
			return i
		}
	}
	return -1
}

func (o object) encode() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func parseArray(raw []byte) ([]json.RawMessage, error) {
	var a []json.RawMessage
	err := json.Unmarshal(raw, &a)
	return a, err
}

func encodeArray(a []json.RawMessage) []byte {
	b := []byte{'['}
	for i, v := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, v...)
	}
	return append(b, ']')
}

func pretty(raw []byte) (string, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return "", err
	}
	out.WriteByte('\n')
	return out.String(), nil
}

// isOurs reports whether a hook group (raw) contains our command, and how
// many hooks it has in total.
func ourHooks(group object) (hooks []json.RawMessage, ours []bool, err error) {
	i := group.index("hooks")
	if i < 0 {
		return nil, nil, nil
	}
	hooks, err = parseArray(group[i].val)
	if err != nil {
		return nil, nil, nil // unknown shape: leave alone
	}
	for _, h := range hooks {
		var c struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(h, &c)
		ours = append(ours, strings.Contains(c.Command, scriptName))
	}
	return hooks, ours, nil
}

// mergeSettings adds our hook to each event, keeping everything else as is.
func mergeSettings(src, command string) (string, error) {
	var top object
	if strings.TrimSpace(src) != "" {
		var err error
		if top, err = parseObject([]byte(src)); err != nil {
			return "", err
		}
	}
	hi := top.index("hooks")
	var hooks object
	if hi >= 0 {
		var err error
		if hooks, err = parseObject(top[hi].val); err != nil {
			return "", errors.New(`"hooks" is not an object`)
		}
	}
	for _, ev := range hookEvents {
		ei := hooks.index(ev.name)
		var groups []json.RawMessage
		if ei >= 0 {
			var err error
			if groups, err = parseArray(hooks[ei].val); err != nil {
				return "", fmt.Errorf("hooks.%s is not an array", ev.name)
			}
		}
		present := false
		for _, g := range groups {
			go_, err := parseObject(g)
			if err != nil {
				continue
			}
			if _, ours, _ := ourHooks(go_); anyTrue(ours) {
				present = true
				break
			}
		}
		if !present {
			groups = append(groups, newGroup(ev.matcher, command))
		}
		if ei >= 0 {
			hooks[ei].val = encodeArray(groups)
		} else {
			hooks = append(hooks, member{ev.name, encodeArray(groups)})
		}
	}
	if hi >= 0 {
		top[hi].val = hooks.encode()
	} else {
		top = append(top, member{"hooks", hooks.encode()})
	}
	return pretty(top.encode())
}

func anyTrue(b []bool) bool {
	for _, v := range b {
		if v {
			return true
		}
	}
	return false
}

func newGroup(matcher, command string) json.RawMessage {
	h, _ := json.Marshal(struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}{"command", command, 10})
	var g object
	if matcher != "" {
		m, _ := json.Marshal(matcher)
		g = append(g, member{"matcher", m})
	}
	g = append(g, member{"hooks", encodeArray([]json.RawMessage{h})})
	return g.encode()
}

// unmergeSettings removes only our hooks, dropping groups, events and the
// "hooks" key when that leaves them empty.
func unmergeSettings(src string) (string, error) {
	top, err := parseObject([]byte(src))
	if err != nil {
		return "", err
	}
	hi := top.index("hooks")
	if hi < 0 {
		return src, nil
	}
	hooks, err := parseObject(top[hi].val)
	if err != nil {
		return src, nil
	}
	changed := false
	var keptEvents object
	for _, ev := range hooks {
		groups, err := parseArray(ev.val)
		if err != nil {
			keptEvents = append(keptEvents, ev)
			continue
		}
		var keptGroups []json.RawMessage
		evChanged := false
		for _, g := range groups {
			go_, err := parseObject(g)
			if err != nil {
				keptGroups = append(keptGroups, g)
				continue
			}
			list, ours, _ := ourHooks(go_)
			if !anyTrue(ours) {
				keptGroups = append(keptGroups, g)
				continue
			}
			changed, evChanged = true, true
			var rest []json.RawMessage
			for i, h := range list {
				if !ours[i] {
					rest = append(rest, h)
				}
			}
			if len(rest) == 0 {
				continue
			}
			go_[go_.index("hooks")].val = encodeArray(rest)
			keptGroups = append(keptGroups, go_.encode())
		}
		if len(keptGroups) == 0 && evChanged {
			continue
		}
		keptEvents = append(keptEvents, member{ev.key, encodeArray(keptGroups)})
	}
	if !changed {
		return src, nil
	}
	if len(keptEvents) == 0 {
		top = append(top[:hi:hi], top[hi+1:]...)
	} else {
		top[hi].val = keptEvents.encode()
	}
	return pretty(top.encode())
}
