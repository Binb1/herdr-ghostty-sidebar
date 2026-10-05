// Package gitbranch reads the current branch of a repository straight from
// .git, with no git subprocess.
package gitbranch

import (
	"os"
	"path/filepath"
	"strings"
)

// Reader resolves branches and caches the answer per starting directory, so
// many panes sharing a cwd cost one lookup. Use one Reader per run.
type Reader struct {
	cache map[string]string
}

func NewReader() *Reader { return &Reader{cache: map[string]string{}} }

// Branch returns the branch checked out at or above dir: the branch name, a
// 7 character sha for a detached HEAD, or "" when dir is not in a repo.
func (r *Reader) Branch(dir string) string {
	if dir == "" {
		return ""
	}
	if b, ok := r.cache[dir]; ok {
		return b
	}
	b := Read(dir)
	r.cache[dir] = b
	return b
}

// Hidden reports whether a branch is too ordinary to show.
func Hidden(branch string) bool {
	return branch == "" || branch == "main" || branch == "master"
}

// Read finds the repo containing dir and returns its HEAD branch ("" if none).
func Read(dir string) string {
	dir = filepath.Clean(dir)
	for {
		if gd := gitDir(filepath.Join(dir, ".git")); gd != "" {
			return head(gd)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitDir resolves a .git entry: a directory is the git dir; a file holds
// "gitdir: <path>" (worktrees, submodules), relative to its own directory.
func gitDir(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if st.IsDir() {
		return path
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	rest, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	rest = strings.TrimSpace(rest)
	if !filepath.IsAbs(rest) {
		rest = filepath.Join(filepath.Dir(path), rest)
	}
	return rest
}

func head(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if ref, ok := strings.CutPrefix(s, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			return name
		}
		return ref
	}
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}
