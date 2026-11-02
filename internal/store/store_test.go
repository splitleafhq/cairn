package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/splitleafhq/cairn/internal/doc"
	"github.com/splitleafhq/cairn/internal/id"
	"github.com/splitleafhq/cairn/internal/model"
)

var t0 = time.Date(2026, 10, 17, 19, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func issue(i string, created time.Time) model.Issue {
	return model.Issue{ID: i, Title: "title " + i, Status: "open", Created: created, Updated: created, Body: "body\n"}
}

func comment(c string, at time.Time) model.Comment {
	return model.Comment{ID: c, Kind: "evidence", By: "alice", At: at, Body: "note " + c + "\n"}
}

func save(t *testing.T, s *Store, i model.Issue, cs ...model.Comment) {
	t.Helper()
	if err := s.SaveIssue(i); err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if err := s.SaveComment(i.ID, c); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Dir != filepath.Join(dir, ".cairn") {
		t.Fatalf("Dir %q", s.Dir)
	}
	cfg, err := doc.Format(model.DefaultConfig().Fields(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"config.yml": string(cfg), ".gitignore": ".index/\n.cairn-tmp-*\n"} {
		if got, err := os.ReadFile(filepath.Join(s.Dir, name)); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"issues", "milestones"} {
		if fi, err := os.Stat(filepath.Join(s.Dir, name)); err != nil || !fi.IsDir() {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Init(dir); !errors.Is(err, ErrExists) {
		t.Fatalf("second Init: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(s.Dir, "config.yml")); string(got) != string(cfg) {
		t.Fatal("second Init changed config.yml")
	}
}

func TestFind(t *testing.T) {
	s := newStore(t)
	deep := filepath.Join(filepath.Dir(s.Dir), "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{filepath.Dir(s.Dir), deep, s.Dir} {
		got, err := Find(start)
		if err != nil || got.Dir != s.Dir {
			t.Errorf("Find(%q) = %v, %v", start, got, err)
		}
	}
}

func TestFindNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cairn"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(dir); !errors.Is(err, ErrNoTracker) {
		t.Fatalf("got %v", err)
	}
}

func TestConfig(t *testing.T) {
	s := newStore(t)
	cfg := filepath.Join(s.Dir, "config.yml")
	if err := os.WriteFile(cfg, []byte("---\nstatuses: [todo, done]\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := s.Config()
	if err != nil || !reflect.DeepEqual(c.Statuses, []string{"todo", "done"}) || c.RetractedMatch != 0.6 {
		t.Fatalf("got %+v, %v", c, err)
	}
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Config(); err != nil || !reflect.DeepEqual(c, model.DefaultConfig()) {
		t.Fatalf("missing file: %+v, %v", c, err)
	}
}

func TestSaveAndLoadIssue(t *testing.T) {
	s := newStore(t)
	i := issue("7f3k", t0)
	// c-0002 is older; c-0001 and c-0003 share a time, so the id breaks the tie.
	c1, c2, c3 := comment("c-0001", t0.Add(time.Minute)), comment("c-0002", t0), comment("c-0003", t0.Add(time.Minute))
	save(t, s, i, c3, c1, c2)
	folder := filepath.Join(s.Dir, "issues", "7f3k")
	for _, junk := range []string{".cairn-tmp-123", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(folder, junk), []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, cs, err := s.LoadIssue("7f3k")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, i) {
		t.Fatalf("issue\ngot  %+v\nwant %+v", got, i)
	}
	if want := []model.Comment{c2, c1, c3}; !reflect.DeepEqual(cs, want) {
		t.Fatalf("comments\ngot  %+v\nwant %+v", cs, want)
	}
	i.Title = "renamed"
	if err := s.SaveIssue(i); err != nil {
		t.Fatal(err)
	}
	if _, cs, err := s.LoadIssue("7f3k"); err != nil || len(cs) != 3 {
		t.Fatalf("SaveIssue must leave comments alone: %d, %v", len(cs), err)
	}
}

func TestLoadIssueNotFound(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.LoadIssue("zzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LoadIssue: %v", err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "issues", "zzzz"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadIssue("zzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LoadIssue without issue.md: %v", err)
	}
	if err := s.SaveComment("yyyy", comment("c-0001", t0)); !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveComment: %v", err)
	}
	if _, err := s.NewCommentID("yyyy", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("NewCommentID: %v", err)
	}
}

func TestLoadIssueIDMismatch(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("aaaa", t0))
	issues := filepath.Join(s.Dir, "issues")
	if err := os.Rename(filepath.Join(issues, "aaaa"), filepath.Join(issues, "bbbb")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadIssue("bbbb"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("issue id mismatch: %v", err)
	}

	save(t, s, issue("cccc", t0), comment("c-1111", t0))
	folder := filepath.Join(issues, "cccc")
	if err := os.Rename(filepath.Join(folder, "c-1111.md"), filepath.Join(folder, "c-2222.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadIssue("cccc"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("comment id mismatch: %v", err)
	}
}

// ReadIssue reads an issue folder wherever its files are; here they are in memory, as a git commit gives them. It
// reads the names the folder lists, and of those only issue.md and the comment files.
func TestReadIssue(t *testing.T) {
	i := issue("7f3k", t0)
	c1, c2 := comment("c-0001", t0.Add(time.Minute)), comment("c-0002", t0)
	files := map[string][]byte{"notes.txt": []byte("junk")}
	add := func(name string, fields []doc.Field, body string) {
		data, err := doc.Format(fields, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
	}
	add("issue.md", i.Fields(), i.Body)
	add("c-0001.md", c1.Fields(), c1.Body)
	add("c-0002.md", c2.Fields(), c2.Body)
	var read []string
	folder := func(names ...string) Folder {
		return Folder{Path: "commit:issues/7f3k", Names: names, Read: func(name string) ([]byte, error) {
			read = append(read, name)
			if data, ok := files[name]; ok && slices.Contains(names, name) {
				return data, nil
			}
			return nil, fs.ErrNotExist
		}}
	}
	got, cs, err := ReadIssue("7f3k", folder("c-0001.md", "notes.txt", "issue.md", "c-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, i) || !reflect.DeepEqual(cs, []model.Comment{c2, c1}) {
		t.Fatalf("got  %+v %+v\nwant %+v %+v", got, cs, i, []model.Comment{c2, c1})
	}
	if slices.Contains(read, "notes.txt") {
		t.Errorf("read a file that is neither issue.md nor a comment: %q", read)
	}
	if _, cs, err := ReadIssue("7f3k", folder("issue.md", "c-0001.md")); err != nil || len(cs) != 1 {
		t.Errorf("a comment the folder does not list: %d comments, %v", len(cs), err)
	}
	if _, _, err := ReadIssue("7f3k", folder("c-0001.md")); !errors.Is(err, ErrNotFound) {
		t.Errorf("no issue.md: %v", err)
	}
	if _, _, err := ReadIssue("../x", folder("issue.md")); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
	files["c-0002.md"] = []byte("nope")
	want := filepath.Join("commit:issues/7f3k", "c-0002.md")
	_, _, err = ReadIssue("7f3k", folder("issue.md", "c-0002.md"))
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), want) {
		t.Errorf("a file that does not parse is an error naming it by the folder's path: %v", err)
	}
}

// LoadComments reads an issue's comments without issue.md, for a caller that already has the issue, as search does
// after ListIssues.
func TestLoadComments(t *testing.T) {
	s := newStore(t)
	c1, c2 := comment("c-0001", t0.Add(time.Minute)), comment("c-0002", t0)
	save(t, s, issue("aaaa", t0), c1, c2)
	folder := filepath.Join(s.Dir, "issues", "aaaa")
	for name, data := range map[string]string{"issue.md": "not frontmatter", "notes.txt": "junk"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.LoadIssue("aaaa"); err == nil {
		t.Fatal("LoadIssue read an issue.md that does not parse")
	}
	cs, err := s.LoadComments("aaaa")
	if err != nil || !reflect.DeepEqual(cs, []model.Comment{c2, c1}) {
		t.Fatalf("got  %+v %v\nwant %+v", cs, err, []model.Comment{c2, c1})
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "issues", "zzzz"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, issueID := range []string{"yyyy", "zzzz", "../x"} {
		if _, err := s.LoadComments(issueID); !errors.Is(err, ErrNotFound) {
			t.Errorf("LoadComments(%q): %v", issueID, err)
		}
	}
	if err := os.WriteFile(filepath.Join(folder, "c-0002.md"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadComments("aaaa"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a comment that does not parse: %v", err)
	}
}

func TestListIssues(t *testing.T) {
	s := newStore(t)
	a, b, c := issue("aaaa", t0.Add(time.Hour)), issue("bbbb", t0), issue("cccc", t0.Add(time.Hour))
	save(t, s, c, comment("c-0001", t0))
	save(t, s, a)
	save(t, s, b)
	issues := filepath.Join(s.Dir, "issues")
	if err := os.Mkdir(filepath.Join(issues, "Not-An-Id"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issues, "README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListIssues()
	if err != nil {
		t.Fatal(err)
	}
	if want := []model.Issue{b, a, c}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	empty := &Store{Dir: t.TempDir()}
	if got, err := empty.ListIssues(); err != nil || len(got) != 0 {
		t.Fatalf("no issues folder: %v, %v", got, err)
	}
}

func TestNewIssueID(t *testing.T) {
	s := newStore(t)
	got, err := s.NewIssueID(bytes.NewReader([]byte{0, 0, 0, 0}))
	if err != nil || got != "0000" {
		t.Fatalf("empty tracker: %q, %v", got, err)
	}
	save(t, s, issue("0000", t0))
	got, err = s.NewIssueID(bytes.NewReader([]byte{0, 0, 0, 0, 1, 1, 1, 1}))
	if err != nil || got != "1111" {
		t.Fatalf("taken id: %q, %v", got, err)
	}
	// 1,000 issue folders make ids 5 characters long. Folders are enough: NewIssueID only counts them.
	issues := filepath.Join(s.Dir, "issues")
	for n := 1; n < 1000; n++ {
		name := string([]byte{'1', id.Alphabet[n/32/32%32], id.Alphabet[n/32%32], id.Alphabet[n%32]})
		if err := os.Mkdir(filepath.Join(issues, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.NewIssueID(bytes.NewReader([]byte{2, 2, 2, 2, 2}))
	if err != nil || got != "22222" {
		t.Fatalf("1,000 issues: %q, %v", got, err)
	}
}

func TestNewCommentID(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("7f3k", t0), comment("c-0000", t0))
	got, err := s.NewCommentID("7f3k", bytes.NewReader([]byte{0, 0, 0, 0, 1, 1, 1, 1}))
	if err != nil || got != "c-1111" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// A broken issue file must surface, not vanish from the list. A folder without issue.md is skipped.
func TestListIssuesErrors(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("aaaa", t0))
	if err := os.Mkdir(filepath.Join(s.Dir, "issues", "bbbb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListIssues(); err != nil || len(got) != 1 {
		t.Fatalf("folder without issue.md: %v, %v", got, err)
	}
	broken := filepath.Join(s.Dir, "issues", "bbbb", "issue.md")
	if err := os.WriteFile(broken, []byte("not frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListIssues(); err == nil {
		t.Fatal("broken issue.md: want an error")
	}
	if err := os.Rename(filepath.Join(s.Dir, "issues", "aaaa"), filepath.Join(s.Dir, "issues", "cccc")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(s.Dir, "issues", "bbbb")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListIssues(); err == nil {
		t.Fatal("id that does not match its folder: want an error")
	}
}

// Ids become paths, so an id that is not valid must never reach the file system.
func TestBadIDsNeverBecomePaths(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("7f3k", t0))
	for _, bad := range []string{"../7f3k", "7f3k/..", "", "."} {
		if _, _, err := s.LoadIssue(bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("LoadIssue(%q): %v", bad, err)
		}
		if _, err := s.NewCommentID(bad, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("NewCommentID(%q): %v", bad, err)
		}
		if err := s.SaveComment(bad, comment("c-0001", t0)); err == nil {
			t.Errorf("SaveComment(%q): want an error", bad)
		}
	}
	if err := s.SaveIssue(issue("../x", t0)); err == nil {
		t.Error("SaveIssue with a bad id: want an error")
	}
	if err := s.SaveComment("7f3k", comment("c-../../x", t0)); err == nil {
		t.Error("SaveComment with a bad comment id: want an error")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "x")); !os.IsNotExist(err) {
		t.Error("a bad id wrote outside issues/")
	}
}

func TestRemoveIssue(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("7f3k", t0), comment("c-0001", t0))
	if err := s.RemoveIssue("7f3k"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "issues", "7f3k")); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}
	for _, bad := range []string{"7f3k", "../7f3k", "", "."} {
		if err := s.RemoveIssue(bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("RemoveIssue(%q): %v", bad, err)
		}
	}
	if _, err := os.Stat(s.Dir); err != nil {
		t.Fatalf("RemoveIssue removed too much: %v", err)
	}
}

func TestRemoveComment(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("7f3k", t0), comment("c-0001", t0), comment("c-0002", t0))
	if err := s.RemoveComment("7f3k", "c-0001"); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(s.Dir, "issues", "7f3k")
	if _, err := os.Stat(filepath.Join(folder, "c-0001.md")); !os.IsNotExist(err) {
		t.Fatalf("c-0001.md still there: %v", err)
	}
	for _, bad := range [][2]string{
		{"7f3k", "c-0001"}, {"7f3k", "c-zzzz"}, {"7f3k", "../7f3k/issue"}, {"7f3k", "c-../x"}, {"7f3k", "issue"},
		{"7f3k", "c-0002.md"}, {"7f3k", ""}, {"7f3k", "c-00"},
		{"zzzz", "c-0002"}, {"../7f3k", "c-0002"}, {"", "c-0002"}, {".", "c-0002"},
	} {
		if err := s.RemoveComment(bad[0], bad[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("RemoveComment(%q, %q): %v", bad[0], bad[1], err)
		}
	}
	for _, name := range []string{"issue.md", "c-0002.md"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("RemoveComment removed too much: %v", err)
		}
	}
}

// A missing comment is ErrNoComment, which is also ErrNotFound; a missing issue is ErrNotFound alone.
func TestRemoveCommentSaysWhatIsMissing(t *testing.T) {
	s := newStore(t)
	save(t, s, issue("7f3k", t0))
	if err := s.RemoveComment("7f3k", "c-0001"); !errors.Is(err, ErrNoComment) || !errors.Is(err, ErrNotFound) {
		t.Errorf("missing comment: %v", err)
	}
	if err := s.RemoveComment("7f3k", "../x"); !errors.Is(err, ErrNoComment) {
		t.Errorf("bad comment id: %v", err)
	}
	for _, missing := range []string{"zzzz", "../7f3k"} {
		if err := s.RemoveComment(missing, "c-0001"); !errors.Is(err, ErrNotFound) || errors.Is(err, ErrNoComment) {
			t.Errorf("missing issue %q: %v", missing, err)
		}
	}
}

func TestListMilestones(t *testing.T) {
	s := newStore(t)
	if got, err := s.ListMilestones(); err != nil || len(got) != 0 {
		t.Fatalf("no milestones: %v, %v", got, err)
	}
	dir := filepath.Join(s.Dir, "milestones")
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("v0.4.md", "---\nid: v0.4\ntitle: First release\nstatus: open\ndue: 2026-12-01\n---\nWhat ships.\n")
	write("beta.md", "---\nid: beta\ntitle: Beta\nstatus: done\n---\n")
	write("notes.txt", "not a milestone")
	write(".cairn-tmp-123", "half a file")
	write(".hidden.md", "not frontmatter")
	if err := os.Mkdir(filepath.Join(dir, "folder.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListMilestones()
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Milestone{
		{ID: "beta", Title: "Beta", Status: "done", Body: ""},
		{ID: "v0.4", Title: "First release", Status: "open", Due: "2026-12-01", Body: "What ships.\n"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	write("broken.md", "not frontmatter")
	if _, err := s.ListMilestones(); err == nil {
		t.Fatal("broken milestone file: want an error")
	}
	if err := os.Remove(filepath.Join(dir, "broken.md")); err != nil {
		t.Fatal(err)
	}
	write("v0.5.md", "---\nid: v0.6\ntitle: Next\nstatus: open\n---\n")
	if _, err := s.ListMilestones(); err == nil {
		t.Fatal("id that does not match its file name: want an error")
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListMilestones(); err != nil || len(got) != 0 {
		t.Fatalf("missing folder: %v, %v", got, err)
	}
}

func TestSaveAndLoadMilestone(t *testing.T) {
	s := newStore(t)
	m := model.Milestone{ID: "v0.4", Title: "First release", Status: "open", Due: "2026-12-01",
		Extra: []doc.Field{{Key: "owner", Value: "bob"}}, Body: "What ships.\n"}
	if err := s.SaveMilestone(m); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "milestones", "v0.4.md"))
	want := "---\nid: v0.4\ntitle: First release\nstatus: open\ndue: 2026-12-01\nowner: bob\n---\nWhat ships.\n"
	if err != nil || string(data) != want {
		t.Fatalf("file: %q, %v", data, err)
	}
	if got, err := s.LoadMilestone("v0.4"); err != nil || !reflect.DeepEqual(got, m) {
		t.Fatalf("load: %+v, %v", got, err)
	}
	// The folder is made again when it is gone.
	if err := os.RemoveAll(filepath.Join(s.Dir, "milestones")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMilestone(model.Milestone{ID: "v1", Title: "One", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadMilestone("v1"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "a/b", ".hidden", ".."} {
		if err := s.SaveMilestone(model.Milestone{ID: bad, Title: "x", Status: "open"}); err == nil {
			t.Errorf("saved milestone id %q", bad)
		}
	}
}

func TestLoadMilestoneErrors(t *testing.T) {
	s := newStore(t)
	for _, missing := range []string{"v9", "", "a/b", ".hidden", "..", "../config"} {
		if _, err := s.LoadMilestone(missing); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: want ErrNotFound, got %v", missing, err)
		}
	}
	dir := filepath.Join(s.Dir, "milestones")
	for name, data := range map[string]string{
		"broken.md": "nope",
		"other.md":  "---\nid: something-else\ntitle: x\nstatus: open\n---\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"broken", "other"} {
		if _, err := s.LoadMilestone(id); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%q: want an error other than ErrNotFound, got %v", id, err)
		}
	}
}

// worktree makes the files git keeps for a linked worktree, without git: a main checkout main with a .git folder, the
// worktree's git folder main/.git/worktrees/wt holding commondir, and the worktree wt, whose .git file points there.
// gitdir and commondir are what those two files say; "" means the usual absolute gitdir and "../..".
func worktree(t *testing.T, root, main, wt, gitdir, commondir string) {
	t.Helper()
	wtGit := filepath.Join(root, main, ".git", "worktrees", "wt")
	if gitdir == "" {
		gitdir = wtGit
	}
	if commondir == "" {
		commondir = "../.."
	}
	for _, d := range []string{wtGit, filepath.Join(root, wt)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(wtGit, "commondir"): commondir + "\n",
		filepath.Join(root, wt, ".git"):   "gitdir: " + gitdir + "\n",
		filepath.Join(wtGit, "gitdir"):    filepath.Join(root, wt, ".git") + "\n",
	}
	for name, text := range files {
		if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ownBranch makes dir/.cairn a tracker on its own branch: Init, plus a .git folder of its own.
func ownBranch(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return s
}

// A linked worktree outside the main checkout finds the main checkout's tracker when it is on its own branch, from
// the worktree's top folder or below it, with the worktree's files written either way git writes them.
func TestFindWorktree(t *testing.T) {
	for name, files := range map[string]struct{ gitdir, commondir string }{
		"absolute gitdir":    {},
		"relative gitdir":    {gitdir: filepath.Join("..", "main", ".git", "worktrees", "wt")},
		"absolute commondir": {commondir: "ABS"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			commondir := files.commondir
			if commondir == "ABS" {
				commondir = filepath.Join(root, "main", ".git")
			}
			worktree(t, root, "main", "wt", files.gitdir, commondir)
			s := ownBranch(t, filepath.Join(root, "main"))
			deep := filepath.Join(root, "wt", "a", "b")
			if err := os.MkdirAll(deep, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, start := range []string{filepath.Join(root, "wt"), deep} {
				got, err := Find(start)
				if err != nil || got.Dir != s.Dir {
					t.Errorf("Find(%q) = %v, %v; want %s", start, got, err, s.Dir)
				}
			}
		})
	}
}

// A worktree's own .cairn, such as a tracker committed on its branch, comes before the main checkout's.
func TestFindWorktreeOwnTrackerFirst(t *testing.T) {
	root := t.TempDir()
	worktree(t, root, "main", "wt", "", "")
	ownBranch(t, filepath.Join(root, "main"))
	own, err := Init(filepath.Join(root, "wt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Find(filepath.Join(root, "wt")); err != nil || got.Dir != own.Dir {
		t.Fatalf("got %v, %v; want %s", got, err, own.Dir)
	}
}

// Only a link to a main checkout's tracker on its own branch counts. Anything else is not a link, and the walk goes
// on up as if the .git file were not there.
func TestFindWorktreeNotFollowed(t *testing.T) {
	t.Run("tracker in the main checkout's tree", func(t *testing.T) {
		root := t.TempDir()
		worktree(t, root, "main", "wt", "", "")
		if _, err := Init(filepath.Join(root, "main")); err != nil {
			t.Fatal(err)
		}
		if got, err := Find(filepath.Join(root, "wt")); !errors.Is(err, ErrNoTracker) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("no commondir, as in a submodule", func(t *testing.T) {
		root := t.TempDir()
		worktree(t, root, "main", "wt", "", "")
		ownBranch(t, filepath.Join(root, "main"))
		if err := os.Remove(filepath.Join(root, "main", ".git", "worktrees", "wt", "commondir")); err != nil {
			t.Fatal(err)
		}
		if got, err := Find(filepath.Join(root, "wt")); !errors.Is(err, ErrNoTracker) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("bare repository", func(t *testing.T) {
		// a/repo.git is bare, with a tracker on its own branch beside it; the worktree is b/wt, so the walk up from it
		// never passes a/.
		root := t.TempDir()
		bareWt := filepath.Join(root, "a", "repo.git", "worktrees", "wt")
		if err := os.MkdirAll(bareWt, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bareWt, "commondir"), []byte("../..\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ownBranch(t, filepath.Join(root, "a"))
		wt := filepath.Join(root, "b", "wt")
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+bareWt+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := Find(wt); !errors.Is(err, ErrNoTracker) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("main checkout moved", func(t *testing.T) {
		root := t.TempDir()
		worktree(t, root, "main", "wt", "", "")
		ownBranch(t, filepath.Join(root, "main"))
		if err := os.Rename(filepath.Join(root, "main"), filepath.Join(root, "moved")); err != nil {
			t.Fatal(err)
		}
		if got, err := Find(filepath.Join(root, "wt")); !errors.Is(err, ErrNoTracker) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run(".git file that is not a gitfile", func(t *testing.T) {
		root := t.TempDir()
		worktree(t, root, "main", "wt", "", "")
		ownBranch(t, filepath.Join(root, "main"))
		if err := os.WriteFile(filepath.Join(root, "wt", ".git"), []byte("not a gitfile\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := Find(filepath.Join(root, "wt")); !errors.Is(err, ErrNoTracker) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

func TestOwnBranch(t *testing.T) {
	s := newStore(t)
	if s.OwnBranch() {
		t.Fatal("a tracker in the tree reports its own branch")
	}
	if err := os.Mkdir(filepath.Join(s.Dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !s.OwnBranch() {
		t.Fatal("a tracker with its own .git does not report its own branch")
	}
}

func TestSaveConfig(t *testing.T) {
	s := newStore(t)
	cfg := model.DefaultConfig()
	cfg.Commit = true
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := s.Config()
	if err != nil || !reflect.DeepEqual(got, cfg) {
		t.Fatalf("got %+v, %v; want %+v", got, err, cfg)
	}
	want, err := doc.Format(cfg.Fields(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := os.ReadFile(filepath.Join(s.Dir, "config.yml")); string(text) != string(want) {
		t.Fatalf("config.yml %q, want %q", text, want)
	}
}
