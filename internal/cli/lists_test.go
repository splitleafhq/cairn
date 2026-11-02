package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/splitleafhq/cairn/internal/model"
	"github.com/splitleafhq/cairn/internal/store"
)

// list runs a command with --json, checks that its data is a list, and returns the items as maps.
func (tr *tracker) list(args ...string) []map[string]any {
	tr.t.Helper()
	code, data, errCode := tr.json(args...)
	if code != 0 {
		tr.t.Fatalf("%q: exit %d (%s)", args, code, errCode)
	}
	if !strings.HasPrefix(string(data), "[") {
		tr.t.Fatalf("%q: data must be a list, got %s", args, data)
	}
	items := []map[string]any{}
	if err := json.Unmarshal(data, &items); err != nil {
		tr.t.Fatalf("%q: %v", args, err)
	}
	return items
}

func TestTags(t *testing.T) {
	tr := newTracker(t)
	if items := tr.list("tags"); len(items) != 0 {
		t.Fatalf("no issues: %v", items)
	}
	if code, out, _ := tr.run("tags"); code != 0 || out != "" {
		t.Fatalf("no issues, text: exit %d, %q", code, out)
	}
	tr.issue("new", "a", "--tag", "severity:high", "--tag", "area:export")
	tr.issue("new", "b", "--tag", "area:export", "--tag", "Zeta")
	tr.issue("new", "c")
	// A file can hold a tag twice; the issue still counts once.
	s, err := store.Find(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	twice := model.Issue{ID: "7f3k", Title: "d", Status: "open", Tags: []string{"area:export", "area:export"},
		Created: t0, Updated: t0}
	if err := s.SaveIssue(twice); err != nil {
		t.Fatal(err)
	}

	want := []map[string]any{
		{"tag": "Zeta", "count": float64(1)},
		{"tag": "area:export", "count": float64(3)},
		{"tag": "severity:high", "count": float64(1)},
	}
	if got := tr.list("tags"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
	if code, out, _ := tr.run("tags"); code != 0 || out != "Zeta\t1\narea:export\t3\nseverity:high\t1\n" {
		t.Fatalf("text: exit %d, %q", code, out)
	}
	tr.wantError(2, "usage", "tags", "extra")
}

func TestMilestones(t *testing.T) {
	tr := newTracker(t)
	if items := tr.list("milestones"); len(items) != 0 {
		t.Fatalf("no milestones: %v", items)
	}
	dir := filepath.Join(tr.dir, ".cairn", "milestones")
	for name, data := range map[string]string{
		"v0.4.md":   "---\nid: v0.4\ntitle: First release\nstatus: open\ndue: 2026-12-01\n---\nWhat ships.\n",
		"zulu.md":   "---\nid: zulu\ntitle: Zulu\nstatus: open\ndue: 2026-10-30\n---\n",
		"unused.md": "---\nid: unused\ntitle: Someday\nstatus: open\n---\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr.issue("new", "a", "--milestone", "v0.4")
	tr.issue("new", "b", "--milestone", "v0.4")
	tr.issue("new", "c", "--milestone", "beta") // no file for beta
	tr.issue("new", "d")

	// By due date, then the ones without one by id: zulu, v0.4, then beta and unused.
	want := []map[string]any{
		{"id": "zulu", "title": "Zulu", "status": "open", "due": "2026-10-30", "issues": float64(0)},
		{"id": "v0.4", "title": "First release", "status": "open", "due": "2026-12-01", "issues": float64(2)},
		{"id": "beta", "title": "", "status": "", "due": "", "issues": float64(1)},
		{"id": "unused", "title": "Someday", "status": "open", "due": "", "issues": float64(0)},
	}
	if got := tr.list("milestones"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
	wantText := "zulu\t2026-10-30\t0\tZulu\n" + "v0.4\t2026-12-01\t2\tFirst release\n" + "beta\t\t1\t\n" +
		"unused\t\t0\tSomeday\n"
	if code, out, _ := tr.run("milestones"); code != 0 || out != wantText {
		t.Fatalf("text: exit %d\ngot  %q\nwant %q", code, out, wantText)
	}
	tr.wantError(2, "usage", "milestones", "extra")
}

func TestMilestonesBrokenFile(t *testing.T) {
	tr := newTracker(t)
	if err := os.WriteFile(filepath.Join(tr.dir, ".cairn", "milestones", "v1.md"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.wantError(1, "error", "milestones")
}

// searchTracker holds three issues whose store order (by created) is the reverse of their id order:
// 89ab "Unrelated" at t0, 4567 "Import is slow" at t0+1m, 0123 "Export drops the last row" at t0+2m.
func searchTracker(t *testing.T) *tracker {
	t.Helper()
	tr := newTracker(t)
	tr.now = t0.Add(2 * time.Minute)
	tr.issue("new", "Export drops the last row", "--body", "Only when the file has no trailing newline.")
	tr.now = t0.Add(time.Minute)
	tr.issue("new", "Import is slow", "--body", "The parser reads every row twice.")
	tr.now = t0
	tr.issue("new", "Unrelated", "--tag", "area:export")
	tr.comment("comment", "4567", "--kind", "evidence", "--source", "parser.go:42", "Rows are copied") // c-cdef
	return tr
}

func searchIDs(t *testing.T, items []map[string]any) []string {
	t.Helper()
	ids := []string{}
	for _, it := range items {
		ids = append(ids, it["id"].(string))
	}
	return ids
}

func TestSearchRanksByMatches(t *testing.T) {
	tr := searchTracker(t)
	items := tr.list("search", "export row")
	if ids := searchIDs(t, items); !reflect.DeepEqual(ids, []string{"0123", "4567"}) {
		t.Fatalf("export row: %q", ids)
	}
	if items[0]["matches"] != float64(2) || items[1]["matches"] != float64(1) {
		t.Fatalf("matches: %v", items)
	}
	if code, out, _ := tr.run("search", "export row"); code != 0 ||
		out != "0123\t2\tExport drops the last row\n4567\t1\tImport is slow\n" {
		t.Fatalf("text: exit %d, %q", code, out)
	}
}

// Equal matches keep store order, which here is not id order.
func TestSearchTiesKeepStoreOrder(t *testing.T) {
	tr := searchTracker(t)
	if ids := searchIDs(t, tr.list("search", "row")); !reflect.DeepEqual(ids, []string{"4567", "0123"}) {
		t.Fatalf("row: %q", ids)
	}
}

func TestSearchWords(t *testing.T) {
	tr := searchTracker(t)
	// Whole words only: "rows" in the comment is not "row", and "row" in the title is not "rows".
	if items := tr.list("search", "rows"); !reflect.DeepEqual(searchIDs(t, items), []string{"4567"}) ||
		items[0]["matches"] != float64(1) {
		t.Fatalf("rows: %v", items)
	}
	// Case does not matter, and a repeated query word counts once.
	for _, it := range tr.list("search", "ROW row Row") {
		if it["matches"] != float64(1) {
			t.Fatalf("repeated word: %v", it)
		}
	}
	// Several arguments are one query.
	if items := tr.list("search", "export", "newline"); len(items) != 1 || items[0]["matches"] != float64(2) {
		t.Fatalf("two arguments: %v", items)
	}
	// Comment sources are searched: "go" is only in parser.go:42.
	if items := tr.list("search", "parser.go"); len(items) != 1 || items[0]["id"] != "4567" ||
		items[0]["matches"] != float64(2) {
		t.Fatalf("source: %v", items)
	}
	// Tags are not searched: 89ab is tagged area:export but says nothing about it.
	if ids := searchIDs(t, tr.list("search", "area")); len(ids) != 0 {
		t.Fatalf("tags searched: %q", ids)
	}
}

func TestSearchSkipsRetractedComments(t *testing.T) {
	tr := searchTracker(t)
	tr.comment("comment", "89ab", "--kind", "result", "The zebra is loose") // c-ghjk
	if ids := searchIDs(t, tr.list("search", "zebra")); !reflect.DeepEqual(ids, []string{"89ab"}) {
		t.Fatalf("active comment: %q", ids)
	}
	tr.retract("89ab", "c-ghjk", "--reason", "no zebra")
	if ids := searchIDs(t, tr.list("search", "zebra")); len(ids) != 0 {
		t.Fatalf("retracted comment matched: %q", ids)
	}
	if code, out, errOut := tr.run("search", "zebra"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("no match, text: exit %d, %q, %q", code, out, errOut)
	}
}

func TestSearchJSONShape(t *testing.T) {
	tr := searchTracker(t)
	items := tr.list("search", "row")
	keys := []string{"id", "title", "status", "tags", "milestone", "due", "by", "persona", "model", "created",
		"updated", "github", "matches"}
	for _, it := range items {
		for _, k := range keys {
			if _, ok := it[k]; !ok {
				t.Errorf("missing key %q in %v", k, it)
			}
		}
		if len(it) != len(keys) {
			t.Errorf("want exactly %d keys, got %v", len(keys), it)
		}
	}
}

func TestSearchUsage(t *testing.T) {
	tr := searchTracker(t)
	for _, args := range [][]string{
		{"search"},
		{"search", "!!!"},
		{"search", "  ", "..."},
		{"search", "--meaning", "row"},
		{"search", "row", "--branches"},
		{"search", "row", "--retracted"},
	} {
		tr.wantError(2, "usage", args...)
	}
}

func TestListCommandsNeedATracker(t *testing.T) {
	empty := &tracker{t: t, dir: t.TempDir(), now: t0, rnd: &seq{}}
	for _, cmd := range [][]string{{"tags"}, {"milestones"}, {"search", "row"}} {
		empty.wantError(1, "no_tracker", cmd...)
	}
}

func TestHelpListsListCommands(t *testing.T) {
	tr := newTracker(t)
	_, out, _ := tr.run("help")
	for _, cmd := range []string{"tags", "milestones", "search"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("help does not list %q: %q", cmd, out)
		}
	}
}
