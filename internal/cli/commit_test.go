package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/splitleafhq/cairn/internal/store"
)

// writeConfig replaces the tracker's config.yml with text, and leaves the change uncommitted.
func (tr *tracker) writeConfig(text string) {
	tr.t.Helper()
	if err := os.WriteFile(filepath.Join(tr.dir, ".cairn", "config.yml"), []byte(text), 0o644); err != nil {
		tr.t.Fatal(err)
	}
}

const commitOn = "---\ncommit: true\n---\n"

// commitTracker is a gitTracker in the folder sub with commit on write turned on, and that change committed.
func commitTracker(t *testing.T, sub string) *tracker {
	t.Helper()
	tr := gitTracker(t, "main", sub)
	tr.writeConfig(commitOn)
	tr.commit(t0.Add(-day), "commit on")
	return tr
}

// write runs a write that must succeed with nothing on stderr, and returns its stdout without the trailing newline.
func (tr *tracker) write(args ...string) string {
	tr.t.Helper()
	code, out, errOut := tr.run(args...)
	if code != 0 || errOut != "" {
		tr.t.Fatalf("%q: exit %d, stderr %q", args, code, errOut)
	}
	return strings.TrimSuffix(out, "\n")
}

// head is HEAD's id, its subject, and its changes as "<status>\t<path>" lines, with paths relative to the
// repository's top folder.
func (tr *tracker) head() (commit, subject string, changes []string) {
	tr.t.Helper()
	lines := strings.Split(strings.TrimSpace(tr.git("show", "--name-status", "--format=%H%n%s", "HEAD")), "\n")
	for _, l := range lines[2:] {
		if l != "" {
			changes = append(changes, l)
		}
	}
	return lines[0], lines[1], changes
}

// headCommit is HEAD's id.
func (tr *tracker) headCommit() string {
	tr.t.Helper()
	commit, _, _ := tr.head()
	return commit
}

// status is git status in short form for the whole repository, untracked files included, one line per file, sorted.
func (tr *tracker) status() []string {
	tr.t.Helper()
	out := strings.TrimSuffix(tr.git("status", "--porcelain", "--untracked-files=all", ":/"), "\n")
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	slices.Sort(lines)
	return lines
}

// Each write makes one commit of what it changed, with the command, the id and the actor in its subject. The tracker
// sits below the repository's top folder, so git is given paths relative to the tracker's folder.
func TestCommitOnWrite(t *testing.T) {
	tr := commitTracker(t, "tools/tracker")
	const dir = "tools/tracker/.cairn/"
	parent := tr.headCommit()
	check := func(subject string, changes ...string) {
		t.Helper()
		commit, gotSubject, gotChanges := tr.head()
		if gotSubject != subject || !reflect.DeepEqual(gotChanges, changes) {
			t.Fatalf("HEAD: %q %q, want %q %q", gotSubject, gotChanges, subject, changes)
		}
		if got := strings.TrimSpace(tr.git("rev-parse", commit+"^")); got != parent {
			t.Fatalf("%s: more than one commit", subject)
		}
		if st := tr.status(); st != nil {
			t.Fatalf("%s: left uncommitted: %q", subject, st)
		}
		parent = commit
	}

	a := tr.write("new", "A title", "--body", "Body")
	issueFile := dir + "issues/" + a + "/issue.md"
	check("chore(cairn): new "+a+" (by alice)", "A\t"+issueFile)

	tr.write("edit", a, "--status", "open", "--as", "bob")
	check("chore(cairn): edit "+a+" (by bob)", "M\t"+issueFile)

	if i := tr.issue("edit", a, "--title", "Retitled"); i.Title != "Retitled" {
		t.Fatalf("edit --json: %+v", i)
	}
	check("chore(cairn): edit "+a+" (by alice)", "M\t"+issueFile)

	cm := tr.write("comment", a, "--kind", "decision", "A note")
	commentFile := dir + "issues/" + a + "/" + cm + ".md"
	check("chore(cairn): comment "+a+" (by alice)", "A\t"+commentFile)

	tr.write("retract", a, cm, "--reason", "wrong")
	check("chore(cairn): retract "+a+" (by alice)", "M\t"+commentFile)

	tr.write("purge", a, cm)
	check("chore(cairn): purge "+a+" (by alice)", "D\t"+commentFile)

	tr.write("milestone", "new", "v1", "Version 1")
	check("chore(cairn): milestone new v1 (by alice)", "A\t"+dir+"milestones/v1.md")

	tr.write("milestone", "edit", "v1", "--status", "open")
	check("chore(cairn): milestone edit v1 (by alice)", "M\t"+dir+"milestones/v1.md")

	tr.write("rm", a)
	check("chore(cairn): rm "+a+" (by alice)", "D\t"+issueFile)
}

// A write commits its issue's whole folder, so another writer's temporary file in that folder, half written, must not
// go in with it (splitleafhq/cairn#32). The tracker's .gitignore keeps git from seeing those files at all.
func TestCommitSkipsTemporaryFiles(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "A title")
	temp := filepath.Join(tr.dir, ".cairn", "issues", a, ".cairn-tmp-123")
	if err := os.WriteFile(temp, []byte("half of another writer's file"), 0o644); err != nil {
		t.Fatal(err)
	}
	cm := tr.write("comment", a, "--kind", "decision", "A note")
	if _, _, changes := tr.head(); !reflect.DeepEqual(changes, []string{"A\t.cairn/issues/" + a + "/" + cm + ".md"}) {
		t.Fatalf("HEAD's changes %q", changes)
	}
	if st := tr.status(); st != nil {
		t.Fatalf("git sees the temporary file: %q", st)
	}
}

// appendTo adds text to the end of the file at p.
func appendTo(t *testing.T, p, text string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A write commits its issue's folder and nothing else: work staged before it stays staged and out of the commit, and
// changes to other issues and other files stay uncommitted. A change to the same issue that was never committed goes
// in with the write.
func TestCommitTakesOnlyItsPaths(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "a")
	b := tr.write("new", "b")
	for name, text := range map[string]string{"staged.txt": "staged\n", "untracked.txt": "untracked\n"} {
		if err := os.WriteFile(filepath.Join(tr.dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr.git("add", "staged.txt")
	issues := filepath.Join(tr.dir, ".cairn", "issues")
	appendTo(t, filepath.Join(issues, a, "issue.md"), "Edited by hand.\n")
	appendTo(t, filepath.Join(issues, b, "issue.md"), "Edited by hand.\n")

	cm := tr.write("comment", a, "--kind", "decision", "A note")
	_, _, changes := tr.head()
	want := []string{"A\t.cairn/issues/" + a + "/" + cm + ".md", "M\t.cairn/issues/" + a + "/issue.md"}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("committed %q, want %q", changes, want)
	}
	wantStatus := []string{" M .cairn/issues/" + b + "/issue.md", "?? untracked.txt", "A  staged.txt"}
	slices.Sort(wantStatus)
	if st := tr.status(); !reflect.DeepEqual(st, wantStatus) {
		t.Fatalf("status %q, want %q", st, wantStatus)
	}
}

// When git fails, here because another writer holds the index lock, the write stands and the command succeeds with a
// warning, in both output modes. The next write to the same issue commits what was left.
func TestCommitFailureKeepsTheWrite(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "a")
	before := tr.headCommit()
	lock := filepath.Join(tr.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, args := range [][]string{
		{"comment", a, "--kind", "decision", "First"},
		{"comment", a, "--kind", "decision", "Second", "--json"},
	} {
		code, out, errOut := tr.run(args...)
		if code != 0 {
			t.Fatalf("%q: exit %d, stderr %q", args, code, errOut)
		}
		if lines := warningLines(t, errOut); len(lines) != 1 ||
			!strings.HasPrefix(lines[0], "cairn: warning: not committed: ") {
			t.Fatalf("%q: stderr %q", args, errOut)
		}
		if args[len(args)-1] == "--json" {
			if !strings.HasPrefix(out, `{"ok":true,`) {
				t.Fatalf("%q: stdout %q", args, out)
			}
			continue
		}
		left = append(left, strings.TrimSuffix(out, "\n"))
	}
	if tr.headCommit() != before {
		t.Fatal("committed while the index was locked")
	}
	if got := tr.files(a); len(got) != 3 {
		t.Fatalf("files: %q", got)
	}

	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	tr.write("comment", a, "--kind", "decision", "Third")
	_, _, changes := tr.head()
	if len(changes) != 3 || !slices.Contains(changes, "A\t.cairn/issues/"+a+"/"+left[0]+".md") {
		t.Fatalf("committed %q", changes)
	}
	if st := tr.status(); st != nil {
		t.Fatalf("left uncommitted: %q", st)
	}
}

// A write whose change another writer committed first, between the write and its own commit, has nothing left to
// commit: git fails, but that is not a warning. Here an edit to a and the removal of b are committed by hand before the
// commit runs.
func TestCommitAlreadyCommitted(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "a")
	b := tr.write("new", "b")
	appendTo(t, filepath.Join(tr.dir, ".cairn", "issues", a, "issue.md"), "Edited by another writer.\n")
	if err := os.RemoveAll(filepath.Join(tr.dir, ".cairn", "issues", b)); err != nil {
		t.Fatal(err)
	}
	tr.commit(t0, "another writer")
	before := tr.headCommit()
	s, err := store.Find(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []struct{ command, id string }{{"edit", a}, {"rm", b}} {
		c := &call{env: Env{Dir: tr.dir, Getenv: func(k string) string { return tr.env[k] }}}
		c.wroteIssue(s, w.id)
		c.commit(w.command)
		if c.warnings != nil {
			t.Errorf("%s %s: warnings %q", w.command, w.id, c.warnings)
		}
	}
	if tr.headCommit() != before {
		t.Fatal("committed again")
	}
}

// An ignored file in an issue's folder, such as an editor's swap file, is not a change left uncommitted: a write that
// another writer has already committed still gets no warning.
func TestCommitAlreadyCommittedWithAnIgnoredFile(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "a")
	appendTo(t, filepath.Join(tr.dir, ".git", "info", "exclude"), "*.swp\n")
	if err := os.WriteFile(filepath.Join(tr.dir, ".cairn", "issues", a, ".issue.md.swp"), []byte("swap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := tr.headCommit()
	s, err := store.Find(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	c := &call{env: Env{Dir: tr.dir, Getenv: func(k string) string { return tr.env[k] }}}
	c.wroteIssue(s, a)
	c.commit("edit")
	if c.warnings != nil {
		t.Errorf("warnings %q", c.warnings)
	}
	if tr.headCommit() != before {
		t.Fatal("committed again")
	}
}

// A hook that refuses the commit runs once, and the write stands with the warning.
func TestCommitRefusedByAHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a shell script")
	}
	tr := commitTracker(t, "")
	runs := filepath.Join(t.TempDir(), "runs")
	hook := "#!/bin/sh\necho run >> '" + runs + "'\necho 'refused' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(tr.dir, ".git", "hooks", "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	before := tr.headCommit()
	code, _, errOut := tr.run("new", "a")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if lines := warningLines(t, errOut); len(lines) != 1 ||
		!strings.HasPrefix(lines[0], "cairn: warning: not committed: ") {
		t.Fatalf("stderr %q", errOut)
	}
	if got, err := os.ReadFile(runs); err != nil || string(got) != "run\n" {
		t.Fatalf("hook runs: %q, %v", got, err)
	}
	if tr.headCommit() != before {
		t.Fatal("committed past the hook")
	}
}

// A repository that ignores the tracker refuses to stage the write. git status lists nothing at an ignored path, but
// that is not nothing to commit: the write stands with the warning.
func TestCommitIgnoredByTheRepository(t *testing.T) {
	tr := commitTracker(t, "")
	appendTo(t, filepath.Join(tr.dir, ".git", "info", "exclude"), "/.cairn\n")
	before := tr.headCommit()
	code, _, errOut := tr.run("new", "a")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if lines := warningLines(t, errOut); len(lines) != 1 ||
		!strings.HasPrefix(lines[0], "cairn: warning: not committed: ") {
		t.Fatalf("stderr %q", errOut)
	}
	if tr.headCommit() != before {
		t.Fatal("committed an ignored path")
	}
}

// Without a repository, and without git at all, a write still succeeds, with the warning.
func TestCommitWithoutARepository(t *testing.T) {
	tr := newTracker(t)
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(tr.dir))
	tr.writeConfig(commitOn)
	for _, path := range []string{os.Getenv("PATH"), t.TempDir()} {
		t.Setenv("PATH", path)
		code, out, errOut := tr.run("new", "a")
		if code != 0 {
			t.Fatalf("PATH %q: exit %d, stderr %q", path, code, errOut)
		}
		if lines := warningLines(t, errOut); len(lines) != 1 ||
			!strings.HasPrefix(lines[0], "cairn: warning: not committed: ") {
			t.Fatalf("PATH %q: stderr %q", path, errOut)
		}
		if code, _, errOut := tr.run("show", strings.TrimSuffix(out, "\n")); code != 0 {
			t.Fatalf("PATH %q: show: exit %d, stderr %q", path, code, errOut)
		}
	}
}

// With commit absent or false, writes never commit.
func TestCommitOff(t *testing.T) {
	for _, config := range []string{"", "---\ncommit: false\n---\n"} {
		tr := gitTracker(t, "main", "")
		if config != "" {
			tr.writeConfig(config)
			tr.commit(t0.Add(-day), "commit off")
		}
		before := tr.headCommit()
		a := tr.write("new", "a")
		tr.write("comment", a, "--kind", "decision", "A note")
		if tr.headCommit() != before {
			t.Fatalf("config %q: committed", config)
		}
	}
}

// rm, retract and purge do not read the config, so a broken one does not stop them. It stops the commit, with the
// warning.
func TestCommitBrokenConfig(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "a")
	before := tr.headCommit()
	tr.writeConfig("---\ncommit: maybe\n---\n")
	code, _, errOut := tr.run("rm", a)
	if code != 0 {
		t.Fatalf("rm: exit %d, stderr %q", code, errOut)
	}
	if lines := warningLines(t, errOut); len(lines) != 1 ||
		!strings.HasPrefix(lines[0], "cairn: warning: not committed: ") {
		t.Fatalf("stderr %q", errOut)
	}
	if _, err := os.Stat(filepath.Join(tr.dir, ".cairn", "issues", a)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("rm left the folder: %v", err)
	}
	if tr.headCommit() != before {
		t.Fatal("committed with a broken config")
	}
}

// A write that fails commits nothing.
func TestFailedWriteCommitsNothing(t *testing.T) {
	tr := commitTracker(t, "")
	a := tr.write("new", "a")
	before := tr.headCommit()
	appendTo(t, filepath.Join(tr.dir, ".cairn", "issues", a, "issue.md"), "Edited by hand.\n")
	tr.wantError(2, "usage", "edit", a, "--status", "nope")
	tr.wantError(2, "usage", "comment", a, "--kind", "evidence", "No source")
	tr.wantError(4, "not_found", "retract", a, "c-zzzz", "--reason", "wrong")
	if tr.headCommit() != before {
		t.Fatal("a failed write committed")
	}
}

// With --json, the envelope holds each warning too, as "warnings": the messages stderr gets, without their prefix. A
// command that gives no warnings has no "warnings" key.
func TestWarningsInJSON(t *testing.T) {
	envelope := func(t *testing.T, out string) (warnings []string, keys []string) {
		t.Helper()
		var top map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &top); err != nil {
			t.Fatalf("stdout %q: %v", out, err)
		}
		for k := range top {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if w, ok := top["warnings"]; ok {
			if err := json.Unmarshal(w, &warnings); err != nil {
				t.Fatalf("warnings %s: %v", w, err)
			}
		}
		return warnings, keys
	}
	messages := func(t *testing.T, errOut string) []string {
		t.Helper()
		var m []string
		for _, l := range warningLines(t, errOut) {
			m = append(m, strings.TrimPrefix(l, "cairn: warning: "))
		}
		return m
	}

	tr := commitTracker(t, "")
	lock := filepath.Join(tr.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := tr.run("new", "a", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	got, _ := envelope(t, out)
	if want := messages(t, errOut); len(got) != 1 || !strings.HasPrefix(got[0], "not committed: ") ||
		!reflect.DeepEqual(got, want) {
		t.Fatalf("warnings %q, stderr %q", got, errOut)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = tr.run("new", "b", "--json")
	if _, keys := envelope(t, out); code != 0 || errOut != "" || !reflect.DeepEqual(keys, []string{"data", "ok"}) {
		t.Fatalf("no warnings: exit %d, keys %q, stderr %q", code, keys, errOut)
	}

	// Not only commit on write: a comment that matches a retracted one names it there too.
	tr = newTracker(t)
	tr.issue("new", "a")
	tr.comment("comment", "0123", "--kind", "evidence", "--source", "log.txt", "a b c d") // c-4567
	tr.retract("0123", "c-4567", "--reason", "old log")
	code, out, errOut = tr.run("comment", "0123", "--kind", "result", "a b c e", "--json")
	got, _ = envelope(t, out)
	if want := messages(t, errOut); code != 0 || len(got) != 1 || !strings.Contains(got[0], "c-4567") ||
		!reflect.DeepEqual(got, want) {
		t.Fatalf("exit %d, warnings %q, stderr %q", code, got, errOut)
	}
}

// A milestone id with glob characters commits its own file only: git takes the path as it is, not as a pattern that
// also matches other milestones' uncommitted files. A pathspec setting in the user's environment changes neither.
func TestCommitTakesAGlobIDAsItIs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`a file name can't hold "*"`)
	}
	for _, env := range []string{"", "GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS",
		"GIT_ICASE_PATHSPECS"} {
		t.Run(env, func(t *testing.T) {
			tr := commitTracker(t, "")
			tr.writeMilestone("v1", "---\nid: v1\ntitle: One\nstatus: backlog\n---\n")
			if env != "" { // for cairn's write only: the test's own git calls take it too
				t.Setenv(env, "1")
			}
			tr.write("milestone", "new", "v*", "Any")
			os.Unsetenv(env)
			_, _, changes := tr.head()
			if want := []string{"A\t.cairn/milestones/v*.md"}; !reflect.DeepEqual(changes, want) {
				t.Fatalf("committed %q, want %q", changes, want)
			}
			if st, want := tr.status(), []string{"?? .cairn/milestones/v1.md"}; !reflect.DeepEqual(st, want) {
				t.Fatalf("status %q, want %q", st, want)
			}
		})
	}
}
