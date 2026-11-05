package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// codeClone is a clone, code, of a bare repository, origin.git, with one commit on main pushed, both in one temp
// folder with symlinks resolved, and a tracker whose folder is code, with no .cairn yet.
func codeClone(t *testing.T) *tracker {
	t.Helper()
	isolateGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tr := &tracker{t: t, dir: root, now: t0, env: map[string]string{"CAIRN_ACTOR": "alice"}, rnd: &seq{}}
	tr.git("init", "-q", "--bare", "-b", "main", "origin.git")
	tr.git("clone", "-q", "origin.git", "code")
	tr.dir = filepath.Join(root, "code")
	tr.gitAt(t0.Add(-2*day), "commit", "-q", "--allow-empty", "-m", "start")
	tr.git("push", "-q", "origin", "main")
	return tr
}

// at is a tracker like tr in the folder dir.
func (tr *tracker) at(dir string) *tracker {
	return &tracker{t: tr.t, dir: dir, now: tr.now, env: tr.env, rnd: tr.rnd}
}

// noCairn fails the test when dir holds a .cairn.
func noCairn(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, ".cairn")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s holds a .cairn: %v", dir, err)
	}
}

// excludes reports whether the repository at tr's folder lists pattern in .git/info/exclude.
func (tr *tracker) excludes(pattern string) bool {
	tr.t.Helper()
	text, _ := os.ReadFile(filepath.Join(tr.dir, ".git", "info", "exclude"))
	return slices.Contains(strings.Split(string(text), "\n"), pattern)
}

// init --own-branch makes .cairn a git repository of its own on the branch cairn, with commit on write on and that
// first config committed, origin's URL as its remote and origin/cairn as its upstream, and the code repository
// ignoring it. Writes then commit in the tracker's repository, and the code repository sees none of it.
func TestInitOwnBranch(t *testing.T) {
	tr := codeClone(t)
	dir := filepath.Join(tr.dir, ".cairn")
	code, data, errCode := tr.json("init", "--own-branch")
	var got map[string]string
	if code != 0 || json.Unmarshal(data, &got) != nil || got["dir"] != dir || got["branch"] != "cairn" {
		t.Fatalf("exit %d (%s), data %s", code, errCode, data)
	}
	if b := tr.git("-C", ".cairn", "symbolic-ref", "--short", "HEAD"); b != "cairn\n" {
		t.Errorf("tracker's branch %q", b)
	}
	if log := tr.git("-C", ".cairn", "log", "--format=%s"); log != "chore(cairn): init\n" {
		t.Errorf("tracker's log %q", log)
	}
	if files := tr.git("-C", ".cairn", "ls-files"); files != ".gitignore\nconfig.yml\n" {
		t.Errorf("tracker's files %q", files)
	}
	if cfg, _ := os.ReadFile(filepath.Join(dir, "config.yml")); !strings.Contains(string(cfg), "\ncommit: true\n") {
		t.Errorf("config.yml %q", cfg)
	}
	for key, want := range map[string]string{
		"remote.origin.url":   tr.git("remote", "get-url", "origin"),
		"branch.cairn.remote": "origin\n",
		"branch.cairn.merge":  "refs/heads/cairn\n",
	} {
		if v := tr.git("-C", ".cairn", "config", key); v != want {
			t.Errorf("%s = %q, want %q", key, v, want)
		}
	}
	if !tr.excludes("/.cairn") {
		t.Error(".git/info/exclude does not list /.cairn")
	}
	if st := tr.git("status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("code repository status %q", st)
	}
	issueID := tr.write("new", "first")
	if s := tr.git("-C", ".cairn", "log", "-1", "--format=%s"); s != "chore(cairn): new "+issueID+" (by alice)\n" {
		t.Errorf("the write's commit %q", s)
	}
	if n := tr.git("rev-list", "--count", "HEAD"); n != "1\n" {
		t.Errorf("code repository has %q commits, want 1", n)
	}
	if st := tr.git("status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("code repository status after a write %q", st)
	}
}

// Once the branch is pushed, init in a fresh clone refuses to start a second tracker and names the command that
// attaches the first, and init --own-branch attaches it: a clone of origin/cairn, ignored by the code repository.
func TestInitOwnBranchAttach(t *testing.T) {
	a := codeClone(t)
	code, out, errOut := a.run("init", "--own-branch")
	if want := "initialized " + filepath.Join(a.dir, ".cairn") + " on branch cairn\n"; code != 0 || out != want || errOut != "" {
		t.Fatalf("init --own-branch: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	issueID := a.write("new", "from a")
	a.git("-C", ".cairn", "push", "-q")

	b := a.at(filepath.Join(filepath.Dir(a.dir), "b"))
	a.git("clone", "-q", filepath.Join(filepath.Dir(a.dir), "origin.git"), b.dir)
	code, out, errOut = b.run("init")
	if code != 1 || out != "" || !strings.Contains(errOut, "cairn init --own-branch") {
		t.Fatalf("init in a fresh clone: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	b.wantError(1, "exists", "init")
	noCairn(t, b.dir)

	code, out, errOut = b.run("init", "--own-branch")
	if want := "attached " + filepath.Join(b.dir, ".cairn") + " to origin/cairn\n"; code != 0 || out != want || errOut != "" {
		t.Fatalf("attach: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if got := b.write("ls"); !strings.HasPrefix(got, issueID+"\t") {
		t.Errorf("ls after attach %q, want %s first", got, issueID)
	}
	if up := b.git("-C", ".cairn", "rev-parse", "--abbrev-ref", "cairn@{upstream}"); up != "origin/cairn\n" {
		t.Errorf("attached tracker's upstream %q", up)
	}
	if !b.excludes("/.cairn") {
		t.Error("the fresh clone's .git/info/exclude does not list /.cairn")
	}
	if st := b.git("status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("fresh clone's status %q", st)
	}
}

// A worktree outside the main checkout uses the main checkout's tracker, and init there, with or without
// --own-branch, refuses to start another one and names the tracker that serves it.
func TestOwnBranchWorktree(t *testing.T) {
	tr := codeClone(t)
	tr.write("init", "--own-branch")
	first := tr.write("new", "from the main checkout")
	wt := tr.at(filepath.Join(filepath.Dir(tr.dir), "wt"))
	tr.git("worktree", "add", "-q", "-b", "wt", wt.dir)
	if got := wt.write("ls"); !strings.HasPrefix(got, first+"\t") {
		t.Errorf("ls in the worktree %q, want %s first", got, first)
	}
	second := wt.write("new", "worktree issue")
	if s := tr.git("-C", ".cairn", "log", "-1", "--format=%s"); s != "chore(cairn): new "+second+" (by alice)\n" {
		t.Errorf("the worktree's write committed %q in the main checkout's tracker", s)
	}
	for _, args := range [][]string{{"init"}, {"init", "--own-branch"}} {
		wt.wantError(1, "exists", args...)
		if _, _, errOut := wt.run(args...); !strings.Contains(errOut, filepath.Join(tr.dir, ".cairn")) {
			t.Errorf("%q: stderr %q does not name the main checkout's tracker", args, errOut)
		}
	}
	noCairn(t, wt.dir)
}

// init --own-branch runs in a main checkout's top folder, and refuses an origin/cairn that is not a tracker. A
// refusal leaves no .cairn and no exclude line. Plain init takes such a branch for what it is, a code branch.
func TestInitOwnBranchRefused(t *testing.T) {
	t.Run("not a repository", func(t *testing.T) {
		isolateGit(t)
		tr := &tracker{t: t, dir: t.TempDir(), now: t0, rnd: &seq{}}
		tr.wantError(1, "error", "init", "--own-branch")
		noCairn(t, tr.dir)
	})
	t.Run("a subfolder", func(t *testing.T) {
		tr := codeClone(t)
		sub := tr.at(filepath.Join(tr.dir, "sub"))
		if err := os.Mkdir(sub.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		sub.wantError(1, "error", "init", "--own-branch")
		noCairn(t, sub.dir)
		if tr.excludes("/sub/.cairn") || tr.excludes("/.cairn") {
			t.Error("a refused init wrote an exclude line")
		}
	})
	t.Run("a linked worktree", func(t *testing.T) {
		tr := codeClone(t)
		wt := tr.at(filepath.Join(filepath.Dir(tr.dir), "wt"))
		tr.git("worktree", "add", "-q", "-b", "wt", wt.dir)
		wt.wantError(1, "error", "init", "--own-branch")
		noCairn(t, wt.dir)
		if tr.excludes("/.cairn") {
			t.Error("a refused init wrote an exclude line")
		}
	})
	t.Run(".cairn already there", func(t *testing.T) {
		tr := codeClone(t)
		tr.write("init")
		tr.wantError(1, "exists", "init", "--own-branch")
		if _, err := os.Lstat(filepath.Join(tr.dir, ".cairn", ".git")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the tracker in the tree got a .git: %v", err)
		}
	})
	t.Run("origin/cairn is a code branch", func(t *testing.T) {
		tr := codeClone(t)
		tr.git("switch", "-q", "-c", "cairn")
		tr.gitAt(t0.Add(-day), "commit", "-q", "--allow-empty", "-m", "code")
		tr.git("push", "-q", "origin", "cairn")
		tr.git("switch", "-q", "main")
		tr.wantError(1, "error", "init", "--own-branch")
		noCairn(t, tr.dir)
		if tr.excludes("/.cairn") {
			t.Error("a refused init wrote an exclude line")
		}
		code, data, errCode := tr.json("init")
		var got map[string]string
		if code != 0 || json.Unmarshal(data, &got) != nil || got["branch"] != "" {
			t.Fatalf("plain init: exit %d (%s), data %s", code, errCode, data)
		}
	})
	t.Run("only init takes it", func(t *testing.T) {
		tr := newTracker(t)
		tr.wantError(2, "usage", "ls", "--own-branch")
	})
}

// A tracker on its own branch has no other branches to read, so --branches adds nothing and is not an error.
func TestOwnBranchBranches(t *testing.T) {
	tr := codeClone(t)
	tr.write("init", "--own-branch")
	issueID := tr.write("new", "first")
	if got := tr.write("ls", "--branches"); got != issueID+"\tbacklog\tfirst" {
		t.Errorf("ls --branches %q", got)
	}
	code, data, errCode := tr.json("show", issueID, "--branches")
	var versions []map[string]any
	if code != 0 || json.Unmarshal(data, &versions) != nil || len(versions) != 1 || versions[0]["branch"] != "" {
		t.Fatalf("show --branches: exit %d (%s), data %s", code, errCode, data)
	}
}

// A setup step that fails in the tracker's repository, here git init, is an error that leaves no .cairn behind, so
// init --own-branch runs again once the cause is gone.
func TestInitOwnBranchSetupFails(t *testing.T) {
	tr := codeClone(t)
	root := filepath.Dir(tr.dir)
	bad, global := filepath.Join(root, "bad"), filepath.Join(root, "global")
	// A config that git reads only in a repository at .cairn, and cannot parse, so git init there fails.
	if err := os.WriteFile(bad, []byte("[\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte("[includeIf \"gitdir:**/.cairn/\"]\n\tpath = "+bad+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	tr.wantError(1, "error", "init", "--own-branch")
	noCairn(t, tr.dir)

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	if code, out, errOut := tr.run("init", "--own-branch"); code != 0 || errOut != "" {
		t.Fatalf("init --own-branch again: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// A first commit that fails leaves the tracker made, uncommitted, with the warning "not committed: …".
func TestInitOwnBranchCommitFails(t *testing.T) {
	tr := codeClone(t)
	t.Setenv("GIT_AUTHOR_NAME", "") // git refuses an empty author name
	code, out, errOut := tr.run("init", "--own-branch")
	if code != 0 || !strings.HasPrefix(out, "initialized ") || !strings.HasPrefix(errOut, "cairn: warning: not committed: ") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(tr.dir, ".cairn", ".git")); err != nil {
		t.Fatal(err)
	}
}
