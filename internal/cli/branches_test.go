package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// gitTracker is a tracker in the folder sub of a fresh git repository whose first branch is branch, with the
// tracker's files committed two days before t0. git runs with no global or system config, and Cairn's own git calls
// inherit that.
func gitTracker(t *testing.T, branch, sub string) *tracker {
	t.Helper()
	isolateGit(t)
	root := t.TempDir()
	dir := filepath.Join(root, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tr := &tracker{t: t, dir: dir, now: t0, env: map[string]string{"CAIRN_ACTOR": "alice"}, rnd: &seq{}}
	tr.git("init", "-q", "-b", branch, root)
	if code, _, errOut := tr.run("init"); code != 0 {
		t.Fatalf("init: exit %d: %s", code, errOut)
	}
	tr.commit(t0.Add(-48*time.Hour), "start")
	return tr
}

// isolateGit fails the test when git is not installed, and otherwise runs git, for the test and for Cairn, with no
// global or system config, no GIT_DIR-like overrides, and alice as author and committer.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed; these tests need it")
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "alice")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "alice@example.com")
	}
}

// git runs git in the tracker's folder and returns its output.
func (tr *tracker) git(args ...string) string {
	tr.t.Helper()
	return tr.gitAt(time.Time{}, args...)
}

func (tr *tracker) gitAt(at time.Time, args ...string) string {
	tr.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = tr.dir
	cmd.Env = os.Environ()
	if !at.IsZero() {
		stamp := fmt.Sprintf("@%d +0000", at.Unix())
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		tr.t.Fatalf("git %q: %v\n%s", args, err, out)
	}
	return string(out)
}

// commit commits everything in the repository, dated at.
func (tr *tracker) commit(at time.Time, msg string) {
	tr.t.Helper()
	tr.git("add", "-A", ":/")
	tr.gitAt(at, "commit", "-q", "-m", msg)
}

const day = 24 * time.Hour

// lsTracker has these branches, and main checked out:
//   - main: a "On main" and m "Merged" (merged from the branch merged).
//   - feat-a, 13 days old: a with status open, and b "On feat-a".
//   - zed, 2 hours old and checked out in a second worktree: e "On zed", plus an issue left uncommitted there.
//   - old (20 days old), code-only (no change under .cairn) and merged (merged into main): not active.
func lsTracker(t *testing.T) (tr *tracker, a, b, m, e string) {
	tr = gitTracker(t, "main", ".")
	tr.now = t0.Add(-47 * time.Hour)
	a = tr.issue("new", "On main").ID
	tr.commit(tr.now, "a")

	tr.git("switch", "-q", "-c", "merged")
	tr.now = t0.Add(-30 * time.Hour)
	m = tr.issue("new", "Merged").ID
	tr.commit(tr.now, "m")
	tr.git("switch", "-q", "main")
	tr.git("merge", "-q", "--ff-only", "merged")

	tr.git("switch", "-q", "-c", "feat-a")
	tr.now = t0.Add(-24 * time.Hour)
	b = tr.issue("new", "On feat-a").ID
	tr.issue("edit", a, "--status", "open")
	tr.commit(t0.Add(-13*day), "b")

	tr.git("switch", "-q", "-c", "old", "main")
	tr.issue("new", "Old")
	tr.commit(t0.Add(-20*day), "old")

	tr.git("switch", "-q", "-c", "code-only", "main")
	if err := os.WriteFile(filepath.Join(tr.dir, "code.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.commit(t0.Add(-time.Hour), "code")

	tr.git("switch", "-q", "-c", "zed", "main")
	tr.now = t0.Add(-2 * time.Hour)
	e = tr.issue("new", "On zed").ID
	tr.commit(tr.now, "e")
	tr.git("switch", "-q", "main")
	wt := filepath.Join(t.TempDir(), "zed")
	tr.git("worktree", "add", "-q", wt, "zed")
	other := &tracker{t: t, dir: wt, now: t0, env: tr.env, rnd: &seq{n: 200}}
	other.issue("new", "Not committed")

	tr.now = t0
	return tr, a, b, m, e
}

func TestLsBranches(t *testing.T) {
	tr, a, b, m, e := lsTracker(t)
	want := a + "\tbacklog\tOn main\n" + m + "\tbacklog\tMerged\n" +
		a + "\topen\tOn main\tfeat-a\n" + b + "\tbacklog\tOn feat-a\tfeat-a\n" +
		e + "\tbacklog\tOn zed\tzed\n"
	if code, out, errOut := tr.run("ls", "--branches"); code != 0 || out != want || errOut != "" {
		t.Fatalf("exit %d, %q\ngot\n%s\nwant\n%s", code, errOut, out, want)
	}
	// Without the flag, nothing changes.
	if code, out, _ := tr.run("ls"); code != 0 || out != a+"\tbacklog\tOn main\n"+m+"\tbacklog\tMerged\n" {
		t.Fatalf("ls: exit %d, %q", code, out)
	}
	// The filters apply to every row.
	if code, out, _ := tr.run("ls", "--branches", "--status", "open"); code != 0 ||
		out != a+"\topen\tOn main\tfeat-a\n" {
		t.Fatalf("--status open: exit %d, %q", code, out)
	}
}

func TestLsBranchesJSON(t *testing.T) {
	tr, a, b, m, e := lsTracker(t)
	type row struct{ ID, Branch string }
	var got []row
	for _, it := range tr.list("ls", "--branches") {
		branch, ok := it["branch"].(string)
		if !ok || len(it) != 13 {
			t.Fatalf("each item has the 12 keys of ls plus branch: %v", it)
		}
		got = append(got, row{it["id"].(string), branch})
	}
	want := []row{{a, ""}, {m, ""}, {a, "feat-a"}, {b, "feat-a"}, {e, "zed"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
	for _, it := range tr.list("ls") {
		if _, ok := it["branch"]; ok || len(it) != 12 {
			t.Fatalf("without --branches there is no branch key: %v", it)
		}
	}
}

// The checked-out branch is the checkout, never a branch row; the default branch is never one either.
func TestLsBranchesFromABranch(t *testing.T) {
	tr, a, b, m, e := lsTracker(t)
	tr.git("switch", "-q", "feat-a")
	want := a + "\topen\tOn main\n" + m + "\tbacklog\tMerged\n" + b + "\tbacklog\tOn feat-a\n" +
		e + "\tbacklog\tOn zed\tzed\n"
	if code, out, _ := tr.run("ls", "--branches"); code != 0 || out != want {
		t.Fatalf("exit %d\ngot\n%s\nwant\n%s", code, out, want)
	}
}

// showTracker has main with a "Shared", and two branches that changed it:
//   - feat-a: a set to in-progress, with a result comment and a retracted evidence comment, and b "Only on feat-a".
//   - talk: only a new comment on a; issue.md is untouched.
func showTracker(t *testing.T) (tr *tracker, a, b string) {
	tr = gitTracker(t, "main", ".")
	tr.now = t0.Add(-47 * time.Hour)
	a = tr.issue("new", "Shared", "--body", "Both branches touch this.").ID
	tr.commit(tr.now, "a")

	tr.git("switch", "-q", "-c", "feat-a")
	tr.now = t0.Add(-24 * time.Hour)
	tr.issue("edit", a, "--status", "in-progress")
	tr.comment("comment", a, "--kind", "result", "Fixed on feat-a")
	wrong := tr.comment("comment", a, "--kind", "evidence", "--source", "x.go:1", "Wrong").Comment.ID
	tr.retract(a, wrong, "--reason", "not it")
	b = tr.issue("new", "Only on feat-a").ID
	tr.commit(tr.now, "feat-a")

	tr.git("switch", "-q", "-c", "talk", "main")
	tr.now = t0.Add(-3 * time.Hour)
	tr.comment("comment", a, "--kind", "decision", `"Ship it" (bob)`)
	tr.commit(tr.now, "talk")
	tr.git("switch", "-q", "main")
	tr.now = t0
	return tr, a, b
}

// printed is what show prints for args with branch checked out.
func (tr *tracker) printed(branch string, args ...string) string {
	tr.t.Helper()
	tr.git("switch", "-q", branch)
	defer tr.git("switch", "-q", "main")
	code, out, errOut := tr.run(append([]string{"show"}, args...)...)
	if code != 0 {
		tr.t.Fatalf("show %q on %s: exit %d, %s", args, branch, code, errOut)
	}
	return out
}

func TestShowBranches(t *testing.T) {
	tr, a, b := showTracker(t)
	for _, all := range [][]string{nil, {"--retracted"}} {
		args := append([]string{"show", a}, all...)
		want := tr.printed("main", args[1:]...) + "\n=== branch feat-a ===\n" + tr.printed("feat-a", args[1:]...) +
			"\n=== branch talk ===\n" + tr.printed("talk", args[1:]...)
		code, out, errOut := tr.run(append(args, "--branches")...)
		if code != 0 || out != want || errOut != "" {
			t.Fatalf("%q: exit %d, %q\ngot\n%s\nwant\n%s", args, code, errOut, out, want)
		}
	}
	// An issue only a branch has: its version alone, still under its header.
	want := "=== branch feat-a ===\n" + tr.printed("feat-a", b)
	if code, out, _ := tr.run("show", b, "--branches"); code != 0 || out != want {
		t.Fatalf("branch only: exit %d\ngot\n%s\nwant\n%s", code, out, want)
	}
	tr.wantError(4, "not_found", "show", b)
	tr.wantError(4, "not_found", "show", "zzzz", "--branches")
	if code, out, _ := tr.run("show", a); code != 0 || out != tr.printed("main", a) {
		t.Fatalf("without --branches, show is unchanged: exit %d, %q", code, out)
	}
}

// showData is show's data for args with branch checked out, plus "branch".
func (tr *tracker) showData(branch, label string, args ...string) map[string]any {
	tr.t.Helper()
	tr.git("switch", "-q", branch)
	defer tr.git("switch", "-q", "main")
	code, data, errCode := tr.json(append([]string{"show"}, args...)...)
	if code != 0 {
		tr.t.Fatalf("show %q on %s: exit %d (%s)", args, branch, code, errCode)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		tr.t.Fatal(err)
	}
	m["branch"] = label
	return m
}

func TestShowBranchesJSON(t *testing.T) {
	tr, a, b := showTracker(t)
	versions := func(args ...string) []map[string]any {
		t.Helper()
		code, data, errCode := tr.json(append(append([]string{"show"}, args...), "--branches")...)
		if code != 0 || !strings.HasPrefix(string(data), "[") {
			t.Fatalf("%q: exit %d (%s), data must be a list: %s", args, code, errCode, data)
		}
		var got []map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	want := []map[string]any{tr.showData("main", "", a), tr.showData("feat-a", "feat-a", a),
		tr.showData("talk", "talk", a)}
	if got := versions(a); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
	if got := versions(a); len(got[1]["comments"].([]any)) != 1 {
		t.Fatalf("the retracted comment is left out without --retracted: %v", got[1]["comments"])
	}
	if got := versions(a, "--retracted"); len(got[1]["comments"].([]any)) != 2 {
		t.Fatalf("--retracted keeps it: %v", got[1]["comments"])
	}
	// One version is still a list.
	if got := versions(b); !reflect.DeepEqual(got, []map[string]any{tr.showData("feat-a", "feat-a", b)}) {
		t.Fatalf("branch only: %v", got)
	}
}

// The default branch comes from origin/HEAD when it is set. Here that is trunk, and x is merged into trunk but not
// into main, so x is not active.
func TestBranchesDefaultFromOrigin(t *testing.T) {
	tr := gitTracker(t, "trunk", ".")
	tr.git("branch", "main")
	tr.git("switch", "-q", "-c", "x")
	x := tr.issue("new", "From x").ID
	tr.commit(t0.Add(-time.Hour), "x")
	tr.git("switch", "-q", "trunk")
	tr.git("merge", "-q", "--ff-only", "x")
	tr.git("update-ref", "refs/remotes/origin/trunk", "HEAD")
	tr.git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	if code, out, errOut := tr.run("ls", "--branches"); code != 0 || out != x+"\tbacklog\tFrom x\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
}

// Without origin/HEAD, main or master, there is no default branch to compare with.
func TestBranchesNeedADefault(t *testing.T) {
	tr := gitTracker(t, "trunk", ".")
	tr.issue("new", "a")
	tr.wantError(1, "error", "ls", "--branches")
	if code, _, _ := tr.run("ls"); code != 0 {
		t.Fatalf("ls without --branches: exit %d", code)
	}
	tr.git("branch", "master")
	if code, _, errOut := tr.run("ls", "--branches"); code != 0 {
		t.Fatalf("master is the default: exit %d, %s", code, errOut)
	}
}

// --branches needs git and a repository; without the flag, Cairn never runs git.
func TestBranchesNeedGit(t *testing.T) {
	plain := newTracker(t)
	a := plain.issue("new", "a").ID
	plain.wantError(1, "error", "ls", "--branches")
	plain.wantError(1, "error", "show", a, "--branches")

	tr, onMain, _, _, _ := lsTracker(t)
	t.Setenv("PATH", t.TempDir())
	if code, _, errOut := tr.run("ls"); code != 0 {
		t.Fatalf("ls needs no git: exit %d, %s", code, errOut)
	}
	if code, _, errOut := tr.run("show", onMain); code != 0 {
		t.Fatalf("show needs no git: exit %d, %s", code, errOut)
	}
	tr.wantError(1, "error", "ls", "--branches")

	empty := &tracker{t: t, dir: t.TempDir(), now: t0, rnd: &seq{}}
	empty.wantError(1, "no_tracker", "ls", "--branches")
}

// The tracker can sit below the repository's top folder. On macOS the temporary folder is reached through a
// symlink, so a path worked out from git's top folder and the tracker's folder would not line up.
func TestBranchesTrackerInASubfolder(t *testing.T) {
	tr := gitTracker(t, "main", "tools/tracker")
	tr.git("switch", "-q", "-c", "feat")
	f := tr.issue("new", "In the subfolder").ID
	tr.commit(t0.Add(-time.Hour), "f")
	tr.git("switch", "-q", "main")
	if code, out, errOut := tr.run("ls", "--branches"); code != 0 || out != f+"\tbacklog\tIn the subfolder\tfeat\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
}

// A branch file that does not parse is left out with a warning, so one bad branch does not stop the list.
func TestBranchesBrokenFile(t *testing.T) {
	tr := gitTracker(t, "main", ".")
	tr.git("switch", "-q", "-c", "bad")
	broken := tr.issue("new", "Broken").ID
	good := tr.issue("new", "Good").ID
	path := filepath.Join(tr.dir, ".cairn", "issues", broken, "issue.md")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.commit(t0.Add(-time.Hour), "bad")
	tr.git("switch", "-q", "main")
	code, out, errOut := tr.run("ls", "--branches")
	if code != 0 || out != good+"\tbacklog\tGood\tbad\n" {
		t.Fatalf("exit %d, %q", code, out)
	}
	if !strings.HasPrefix(errOut, "cairn: warning: branch bad: issue "+broken+": ") ||
		strings.Count(errOut, "\n") != 1 {
		t.Fatalf("want one warning naming the branch and the issue: %q", errOut)
	}
}

// An issue a branch removed is left out of that branch's rows; the checkout still has it.
func TestBranchesRemovedIssue(t *testing.T) {
	tr := gitTracker(t, "main", ".")
	a := tr.issue("new", "Kept on main").ID
	tr.commit(t0.Add(-47*time.Hour), "a")
	tr.git("switch", "-q", "-c", "rm-a")
	if code, _, errOut := tr.run("rm", a); code != 0 {
		t.Fatalf("rm: exit %d, %s", code, errOut)
	}
	tr.commit(t0.Add(-time.Hour), "rm")
	tr.git("switch", "-q", "main")
	code, out, errOut := tr.run("ls", "--branches")
	if code != 0 || out != a+"\tbacklog\tKept on main\n" || errOut != "" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	if code, out, _ := tr.run("show", a, "--branches"); code != 0 || strings.Contains(out, "=== branch") {
		t.Fatalf("exit %d, %q", code, out)
	}
}

// A tag named like a branch, or like the default branch, changes nothing: Cairn names every branch to git by its full
// ref. Here the tag feat points at main, which has no f, and the tag main points at feat, which would make feat look
// merged.
func TestBranchesTagNamedLikeABranch(t *testing.T) {
	tr := gitTracker(t, "main", ".")
	tr.git("switch", "-q", "-c", "feat")
	f := tr.issue("new", "On feat").ID
	tr.commit(t0.Add(-time.Hour), "f")
	tr.git("switch", "-q", "main")
	tr.git("tag", "feat", "refs/heads/main")
	tr.git("tag", "main", "refs/heads/feat")
	code, out, errOut := tr.run("ls", "--branches")
	if code != 0 || out != f+"\tbacklog\tOn feat\tfeat\n" || errOut != "" {
		t.Fatalf("ls: exit %d, %q, %q", code, out, errOut)
	}
	want := "=== branch feat ===\n" + tr.printed("feat", f)
	if code, out, errOut = tr.run("show", f, "--branches"); code != 0 || out != want || errOut != "" {
		t.Fatalf("show: exit %d, %q\ngot\n%s\nwant\n%s", code, errOut, out, want)
	}
}

// A branch with no history in common with the default branch (gh-pages, say) has nothing to compare, so it is not
// active, even when it has a tracker of its own. It does not stop the list.
func TestBranchesUnrelatedHistory(t *testing.T) {
	tr := gitTracker(t, "main", ".")
	a := tr.issue("new", "On main").ID
	tr.commit(t0.Add(-47*time.Hour), "a")
	tr.git("switch", "-q", "--orphan", "pages")
	// The empty issues and milestones folders are not in git, so the switch leaves them. Start from nothing.
	if err := os.RemoveAll(filepath.Join(tr.dir, ".cairn")); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := tr.run("init"); code != 0 {
		t.Fatalf("init: exit %d, %s", code, errOut)
	}
	tr.issue("new", "On pages")
	tr.commit(t0.Add(-time.Hour), "pages")
	tr.git("switch", "-q", "main")
	if code, out, errOut := tr.run("ls", "--branches"); code != 0 || out != a+"\tbacklog\tOn main\n" || errOut != "" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
}

// Warnings are printed only when the command succeeds. A command that fails prints its message alone, on one line,
// even when a branch gave a warning on the way: here show reads the broken issue on bad, warns, and then finds no
// version of it.
func TestBranchesWarningsOnlyOnSuccess(t *testing.T) {
	tr := gitTracker(t, "main", ".")
	tr.git("switch", "-q", "-c", "bad")
	broken := tr.issue("new", "Broken").ID
	path := filepath.Join(tr.dir, ".cairn", "issues", broken, "issue.md")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.commit(t0.Add(-time.Hour), "bad")
	tr.git("switch", "-q", "main")
	// The broken branch does give a warning.
	if code, _, errOut := tr.run("ls", "--branches"); code != 0 || !strings.HasPrefix(errOut, "cairn: warning: ") {
		t.Fatalf("ls: exit %d, %q", code, errOut)
	}
	if code, out, errOut := tr.run("show", broken, "--branches"); code != 4 || out != "" ||
		errOut != "cairn: no issue \""+broken+"\"\n" {
		t.Fatalf("show: exit %d, %q, %q", code, out, errOut)
	}
	tr.wantError(4, "not_found", "show", broken, "--branches")
}
