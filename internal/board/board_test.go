package board

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/splitleafhq/cairn/internal/model"
)

var t0 = time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)

// issue is an issue created at t0 and last updated d later.
func issue(id, title, status string, d time.Duration, tags ...string) model.Issue {
	return model.Issue{ID: id, Title: title, Status: status, Tags: tags, Created: t0, Updated: t0.Add(d)}
}

// shape is a board's columns as the tests compare them: each status, whether it is collapsed, and its issue ids.
type shape struct {
	Status    string
	Collapsed bool
	IDs       []string
}

func shapeOf(b Board) []shape {
	var got []shape
	for _, c := range b.Columns {
		s := shape{Status: c.Status, Collapsed: c.Collapsed}
		for _, i := range c.Issues {
			s.IDs = append(s.IDs, i.ID)
		}
		got = append(got, s)
	}
	return got
}

// The columns are the config's statuses in the config's order, empty ones included. A status an issue has but the
// config lacks, as after a config change, gets a column after them, in byte order. Closed and archived statuses are
// collapsed.
func TestColumnsFollowConfig(t *testing.T) {
	cfg := model.DefaultConfig()
	cfg.Statuses = []string{"backlog", "open", "in-progress", "done", "archived"}
	issues := []model.Issue{
		issue("0001", "one", "done", 0),
		issue("0002", "two", "wontfix", 0),
		issue("0003", "three", "backlog", 0),
		issue("0004", "four", "blocked", 0),
		issue("0005", "five", "archived", 0),
	}
	want := []shape{
		{"backlog", false, []string{"0003"}},
		{"open", false, nil},
		{"in-progress", false, nil},
		{"done", true, []string{"0001"}},
		{"archived", true, []string{"0005"}},
		{"blocked", false, []string{"0004"}},
		{"wontfix", false, []string{"0002"}},
	}
	if got := shapeOf(New(cfg, issues)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// A status that the config lists twice is one column.
func TestDuplicateStatusIsOneColumn(t *testing.T) {
	cfg := model.DefaultConfig()
	cfg.Statuses = []string{"backlog", "open", "backlog", "done", "open"}
	want := []shape{{"backlog", false, []string{"0001"}}, {"open", false, nil}, {"done", true, nil}}
	if got := shapeOf(New(cfg, []model.Issue{issue("0001", "one", "backlog", 0)})); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// Which columns are collapsed comes from the config's closed and archived lists, not from the name "done".
func TestCollapsedComesFromConfig(t *testing.T) {
	cfg := model.DefaultConfig()
	cfg.Statuses = []string{"todo", "finished", "old"}
	cfg.Closed = []string{"finished"}
	cfg.Archived = []string{"old"}
	issues := []model.Issue{issue("0001", "one", "finished", 0), issue("0002", "two", "done", 0)}
	want := []shape{
		{"todo", false, nil},
		{"finished", true, []string{"0001"}},
		{"old", true, nil},
		{"done", false, []string{"0002"}},
	}
	if got := shapeOf(New(cfg, issues)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	// With empty lists nothing is closed or archived, so nothing is collapsed.
	cfg.Closed, cfg.Archived = []string{}, []string{}
	for _, c := range New(cfg, issues).Columns {
		if c.Collapsed {
			t.Errorf("%s is collapsed with no closed or archived statuses", c.Status)
		}
	}
}

// A column lists the issue updated last first, and issues updated at the same time in the order they came in, which
// is store order.
func TestCardsNewestUpdateFirst(t *testing.T) {
	cfg := model.DefaultConfig()
	issues := []model.Issue{
		issue("0001", "one", "backlog", time.Hour),
		issue("0002", "two", "backlog", 3*time.Hour),
		issue("0003", "three", "backlog", time.Hour),
		issue("0004", "four", "backlog", 2*time.Hour),
	}
	want := []string{"0002", "0004", "0001", "0003"}
	if got := shapeOf(New(cfg, issues))[0].IDs; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// New does not reorder the issues it was given.
func TestNewKeepsItsInput(t *testing.T) {
	issues := []model.Issue{issue("0001", "one", "backlog", 0), issue("0002", "two", "backlog", time.Hour)}
	New(model.DefaultConfig(), issues)
	if issues[0].ID != "0001" || issues[1].ID != "0002" {
		t.Fatalf("input reordered: %v", issues)
	}
}

func TestEmptyBoard(t *testing.T) {
	b := New(model.DefaultConfig(), nil)
	want := []shape{{"backlog", false, nil}, {"open", false, nil}, {"in-progress", false, nil}, {"done", true, nil}}
	if got := shapeOf(b); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if _, err := b.HTML("test-tracker", t0); err != nil {
		t.Fatal(err)
	}
	if got := New(model.Config{}, nil).Text(); got != "" {
		t.Fatalf("a board with no columns prints %q", got)
	}
}

// Text has a header per column, "<status> (<count>)", and under it one line per card: id, last update, tags and
// title, separated by tabs, the title last as in ls. A collapsed column prints its count and the word collapsed, and
// no cards. Columns are separated by a blank line.
func TestText(t *testing.T) {
	issues := []model.Issue{
		issue("0001", "Export drops the last row", "backlog", time.Minute, "area:export", "severity:high"),
		issue("0002", "Docs", "backlog", 2*time.Minute),
		issue("0003", "Parser", "open", time.Hour, "area:parser"),
		issue("0004", "Done thing", "done", time.Hour),
		issue("0005", "Another done thing", "done", 2*time.Hour),
	}
	want := "backlog (2)\n" +
		"\t0002\t2026-10-18T09:32:00.000Z\t\tDocs\n" +
		"\t0001\t2026-10-18T09:31:00.000Z\tarea:export, severity:high\tExport drops the last row\n" +
		"\n" +
		"open (1)\n" +
		"\t0003\t2026-10-18T10:30:00.000Z\tarea:parser\tParser\n" +
		"\n" +
		"in-progress (0)\n" +
		"\n" +
		"done (2, collapsed)\n"
	if got := New(model.DefaultConfig(), issues).Text(); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

// pageIssues has no link, so a test can look for any link the page makes.
func pageIssues() []model.Issue {
	return []model.Issue{
		issue("0001", "Export drops the last row", "backlog", time.Minute, "area:export", "severity:high"),
		issue("0002", "Parser", "open", time.Hour),
		issue("0003", "Done thing", "done", 2*time.Hour, "area:parser"),
	}
}

func page(t *testing.T, issues []model.Issue) string {
	t.Helper()
	out, err := New(model.DefaultConfig(), issues).HTML("test-tracker", t0)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

var column = regexp.MustCompile(`<details class="column" data-status="([^"]*)"( open)?>`)

// Every status is a <details> element, open unless its column is collapsed, so the page needs no script. A collapsed
// column still holds its cards.
func TestPageColumns(t *testing.T) {
	got := page(t, pageIssues())
	want := map[string]bool{"backlog": true, "open": true, "in-progress": true, "done": false}
	found := map[string]bool{}
	for _, m := range column.FindAllStringSubmatch(got, -1) {
		found[m[1]] = m[2] != ""
	}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("columns (status: open) %v, want %v", found, want)
	}
	if n := strings.Count(got, `<li class="card">`); n != 3 {
		t.Errorf("%d cards, want 3, the collapsed column's included", n)
	}
	for _, s := range []string{"0001", "Export drops the last row", "area:export", "severity:high", "0003", "Done thing",
		`datetime="2026-10-18T09:31:00.000Z"`, "2026-10-18", "test-tracker", "2026-10-18 09:30 UTC"} {
		if !strings.Contains(got, s) {
			t.Errorf("page lacks %q", s)
		}
	}
}

// The page is a fixed viewer: nothing in it loads, runs or links anything, and its policy says so.
func TestPageIsSelfContained(t *testing.T) {
	got := page(t, pageIssues())
	if !strings.HasPrefix(got, "<!doctype html>") {
		t.Errorf("page starts %q", got[:20])
	}
	for _, s := range []string{"http:", "https:", "//", "<script", "<link", "<img", "<iframe", "<form", "<a ", "src=", "href=",
		"@import", "url(", "onclick"} {
		if strings.Contains(strings.ToLower(got), s) {
			t.Errorf("page has %q", s)
		}
	}
	if !strings.Contains(got, `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">`) {
		t.Error("page lacks a policy that allows inline CSS and nothing else")
	}
}

// Titles and tags are data, and come out as text whatever they hold.
func TestPageEscapesData(t *testing.T) {
	issues := []model.Issue{issue("0001", `<script>alert(1)</script> & "q"`, "backlog", 0, `<b>x</b>`)}
	out, err := New(model.DefaultConfig(), issues).HTML(`<i>name</i>`, t0)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, s := range []string{"<script", "<b>", "<i>"} {
		if strings.Contains(got, s) {
			t.Errorf("page has %q", s)
		}
	}
	for _, s := range []string{"&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;q&#34;", "&lt;b&gt;x&lt;/b&gt;", "&lt;i&gt;name&lt;/i&gt;"} {
		if !strings.Contains(got, s) {
			t.Errorf("page lacks %q", s)
		}
	}
}

// The same issues and time give the same page, byte for byte.
func TestPageIsDeterministic(t *testing.T) {
	if a, b := page(t, pageIssues()), page(t, pageIssues()); a != b {
		t.Fatal("two renders differ")
	}
}
