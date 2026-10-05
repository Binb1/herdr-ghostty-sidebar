package gitbranch

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBranchKinds(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "repo", ".git", "HEAD"), "ref: refs/heads/feat/x\n")
	write(t, filepath.Join(root, "repo", "a", "b", "keep"), "")
	write(t, filepath.Join(root, "det", ".git", "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")
	// worktree: .git file pointing at an absolute gitdir; and a relative one
	write(t, filepath.Join(root, "main", ".git", "worktrees", "w", "HEAD"), "ref: refs/heads/wt\n")
	write(t, filepath.Join(root, "wt", ".git"), "gitdir: "+filepath.Join(root, "main", ".git", "worktrees", "w")+"\n")
	write(t, filepath.Join(root, "sub", ".git"), "gitdir: ../modules/sub\n")
	write(t, filepath.Join(root, "modules", "sub", "HEAD"), "ref: refs/heads/subbr\n")
	if err := os.MkdirAll(filepath.Join(root, "norepo"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"repo":           "feat/x",
		"repo/a/b":       "feat/x", // walks up
		"det":            "0123456",
		"wt":             "wt",
		"sub":            "subbr",
		"norepo":         "",
		"does/not/exist": "",
	}
	r := NewReader()
	for rel, want := range cases {
		if got := r.Branch(filepath.Join(root, rel)); got != want {
			t.Errorf("%s: got %q want %q", rel, got, want)
		}
	}
	if r.Branch("") != "" {
		t.Error("empty dir")
	}
}

func TestCacheAndHidden(t *testing.T) {
	root := t.TempDir()
	head := filepath.Join(root, ".git", "HEAD")
	write(t, head, "ref: refs/heads/one\n")
	r := NewReader()
	if r.Branch(root) != "one" {
		t.Fatal("first read")
	}
	write(t, head, "ref: refs/heads/two\n")
	if r.Branch(root) != "one" {
		t.Fatal("must be cached within a run")
	}
	if NewReader().Branch(root) != "two" {
		t.Fatal("new reader rereads")
	}
	for _, b := range []string{"", "main", "master"} {
		if !Hidden(b) {
			t.Errorf("%q should be hidden", b)
		}
	}
	if Hidden("main2") || Hidden("dev") {
		t.Error("only main/master hidden")
	}
}
