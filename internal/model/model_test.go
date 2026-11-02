package model

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/splitleafhq/cairn/internal/doc"
)

const specIssue = `---
id: 7f3k
title: "Export drops the last row"
status: in-progress
tags: [severity:high, effort:2, area:export]
milestone: v0.4
due: 2026-11-01
created: 2026-10-17T19:00:00.000Z
updated: 2026-10-17T19:20:00.000Z
github: 123
---
What is wrong, why it matters, and what "done" means.
`

const specComment = `---
id: c-x9k2
kind: evidence
by: alice
persona: skeptic
model: example-model-1
at: 2026-10-17T19:20:00.000Z
source: export_test.go:42
retracted_by: bob
retracted_at: 2026-10-17T20:00:00.500Z
retracted_reason: the test was wrong
disproved_by: c-ab12
---
The last row is dropped when the file has no trailing newline.
`

func split(t *testing.T, s string) ([]doc.Field, []byte) {
	t.Helper()
	f, body, err := doc.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return f, body
}

func join(t *testing.T, f []doc.Field, body string) string {
	t.Helper()
	b, err := doc.Format(f, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func keys(f []doc.Field) string {
	var k []string
	for _, x := range f {
		k = append(k, x.Key)
	}
	return strings.Join(k, " ")
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseIssueSpecExample(t *testing.T) {
	i, err := ParseIssue(split(t, specIssue))
	if err != nil {
		t.Fatal(err)
	}
	want := Issue{ID: "7f3k", Title: "Export drops the last row", Status: "in-progress",
		Tags: []string{"severity:high", "effort:2", "area:export"}, Milestone: "v0.4", Due: "2026-11-01",
		Created: at("2026-10-17T19:00:00.000Z"), Updated: at("2026-10-17T19:20:00.000Z"), GitHub: 123,
		Body: "What is wrong, why it matters, and what \"done\" means.\n"}
	if !reflect.DeepEqual(i, want) {
		t.Fatalf("got  %+v\nwant %+v", i, want)
	}
}

func TestIssueRoundTrip(t *testing.T) {
	i, err := ParseIssue(split(t, specIssue))
	if err != nil {
		t.Fatal(err)
	}
	out := join(t, i.Fields(), i.Body)
	if !strings.Contains(out, "created: 2026-10-17T19:00:00.000Z\n") {
		t.Errorf("whole seconds should print with a fraction of .000:\n%s", out)
	}
	j, err := ParseIssue(split(t, out))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(i, j) {
		t.Fatalf("got  %+v\nwant %+v", j, i)
	}
	if again := join(t, j.Fields(), j.Body); again != out {
		t.Fatalf("not stable:\n%s\n%s", out, again)
	}
}

func TestIssueFieldsOrderAndOmitted(t *testing.T) {
	i := Issue{ID: "7f3k", Title: "t", Status: "open", Created: at("2026-10-17T19:00:00.000Z"),
		Updated: at("2026-10-17T19:00:00.000Z")}
	if got := keys(i.Fields()); got != "id title status created updated" {
		t.Errorf("minimal: %q", got)
	}
	i.Tags, i.Milestone, i.Due, i.GitHub = []string{"a"}, "v1", "2026-11-01", 7
	i.Extra = []doc.Field{{Key: "zeta", Value: "1"}, {Key: "alpha", Value: "2"}}
	if got := keys(i.Fields()); got != "id title status tags milestone due created updated github zeta alpha" {
		t.Errorf("full: %q", got)
	}
}

func TestIssueKeepsUnknownKeys(t *testing.T) {
	src := strings.Replace(specIssue, "github: 123\n", "github: 123\nweight: 3\nlabels: [x, y]\n", 1)
	i, err := ParseIssue(split(t, src))
	if err != nil {
		t.Fatal(err)
	}
	want := []doc.Field{{Key: "weight", Value: "3"}, {Key: "labels", List: []string{"x", "y"}, IsList: true}}
	if !reflect.DeepEqual(i.Extra, want) {
		t.Fatalf("Extra %+v", i.Extra)
	}
	if out := join(t, i.Fields(), i.Body); !strings.Contains(out, "github: 123\nweight: 3\nlabels: [x, y]\n---\n") {
		t.Fatalf("unknown keys not written back after the known ones:\n%s", out)
	}
}

func TestIssueTimes(t *testing.T) {
	src := strings.Replace(specIssue, "created: 2026-10-17T19:00:00.000Z", "created: 2026-10-17T15:00:00.25-04:00", 1)
	i, err := ParseIssue(split(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if !i.Created.Equal(at("2026-10-17T19:00:00.25Z")) || i.Created.Location() != time.UTC {
		t.Fatalf("created %v", i.Created)
	}
	if out := join(t, i.Fields(), i.Body); !strings.Contains(out, "created: 2026-10-17T19:00:00.250Z\n") {
		t.Fatalf("want UTC with the fraction kept:\n%s", out)
	}
}

func TestParseIssueDoesNotCheckConfig(t *testing.T) {
	src := strings.Replace(specIssue, "status: in-progress", "status: no-such-status", 1)
	src = strings.Replace(src, "severity:high", "colour:blue", 1)
	if _, err := ParseIssue(split(t, src)); err != nil {
		t.Fatal(err)
	}
}

func TestParseIssueErrors(t *testing.T) {
	for _, c := range []struct{ old, new string }{
		{"id: 7f3k\n", ""},
		{"title: \"Export drops the last row\"\n", ""},
		{"status: in-progress\n", ""},
		{"created: 2026-10-17T19:00:00.000Z\n", ""},
		{"updated: 2026-10-17T19:20:00.000Z\n", ""},
		{"id: 7f3k", "id: 7F3K"},
		{"id: 7f3k", "id: 7f3"},
		{"title: \"Export drops the last row\"", "title: [a, b]"},
		{"tags: [severity:high, effort:2, area:export]", "tags: severity:high"},
		{"due: 2026-11-01", "due: 2026-13-01"},
		{"due: 2026-11-01", "due: 2026-11-01T00:00:00Z"},
		{"created: 2026-10-17T19:00:00.000Z", "created: 2026-10-17"},
		{"github: 123", "github: abc"},
	} {
		src := strings.Replace(specIssue, c.old, c.new, 1)
		if src == specIssue {
			t.Fatalf("case %q did not change the file", c.old)
		}
		if _, err := ParseIssue(split(t, src)); err == nil {
			t.Errorf("%q -> %q: want an error", c.old, c.new)
		}
	}
}

func TestCommentRoundTrip(t *testing.T) {
	c, err := ParseComment(split(t, specComment))
	if err != nil {
		t.Fatal(err)
	}
	want := Comment{ID: "c-x9k2", Kind: "evidence", By: "alice", Persona: "skeptic", Model: "example-model-1",
		At: at("2026-10-17T19:20:00.000Z"), Source: "export_test.go:42", RetractedBy: "bob",
		RetractedAt: at("2026-10-17T20:00:00.500Z"), RetractedReason: "the test was wrong", DisprovedBy: "c-ab12",
		Body: "The last row is dropped when the file has no trailing newline.\n"}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got  %+v\nwant %+v", c, want)
	}
	if out := join(t, c.Fields(), c.Body); out != specComment {
		t.Fatalf("got\n%s\nwant\n%s", out, specComment)
	}
	bare := Comment{ID: "c-x9k2", Kind: "result", By: "bob", At: at("2026-10-17T19:20:00.000Z")}
	if got := keys(bare.Fields()); got != "id kind by at" {
		t.Errorf("minimal: %q", got)
	}
}

func TestParseCommentErrors(t *testing.T) {
	for _, c := range []struct{ old, new string }{
		{"id: c-x9k2\n", ""},
		{"kind: evidence\n", ""},
		{"by: alice\n", ""},
		{"at: 2026-10-17T19:20:00.000Z\n", ""},
		{"id: c-x9k2", "id: x9k2"},
		{"at: 2026-10-17T19:20:00.000Z", "at: yesterday"},
		{"retracted_at: 2026-10-17T20:00:00.500Z", "retracted_at: soon"},
		{"disproved_by: c-ab12", "disproved_by: ab12"},
	} {
		src := strings.Replace(specComment, c.old, c.new, 1)
		if src == specComment {
			t.Fatalf("case %q did not change the file", c.old)
		}
		if _, err := ParseComment(split(t, src)); err == nil {
			t.Errorf("%q -> %q: want an error", c.old, c.new)
		}
	}
}

func TestMilestone(t *testing.T) {
	src := "---\nid: v0.4\ntitle: First release\nstatus: open\ndue: 2026-12-01\n---\nWhat ships.\n"
	m, err := ParseMilestone(split(t, src))
	if err != nil {
		t.Fatal(err)
	}
	want := Milestone{ID: "v0.4", Title: "First release", Status: "open", Due: "2026-12-01", Body: "What ships.\n"}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %+v", m)
	}
	if out := join(t, m.Fields(), m.Body); out != src {
		t.Fatalf("got\n%s", out)
	}
	for _, bad := range []string{
		strings.Replace(src, "id: v0.4", "id: a/b", 1),
		strings.Replace(src, "title: First release\n", "", 1),
		strings.Replace(src, "due: 2026-12-01", "due: december", 1),
	} {
		if _, err := ParseMilestone(split(t, bad)); err == nil {
			t.Errorf("want an error for\n%s", bad)
		}
	}
}

// A milestone id is a file name in milestones/: not empty, no "/", and not starting with ".", which would hide it.
func TestValidMilestoneID(t *testing.T) {
	for s, want := range map[string]bool{"v0.4": true, "release-1": true, "a.b": true, "": false, "a/b": false,
		".hidden": false, "..": false} {
		if got := ValidMilestoneID(s); got != want {
			t.Errorf("ValidMilestoneID(%q) = %v, want %v", s, got, want)
		}
	}
	src := "---\nid: .hidden\ntitle: Hidden\nstatus: open\n---\n"
	if _, err := ParseMilestone(split(t, src)); err == nil {
		t.Errorf("want an error for\n%s", src)
	}
}

func TestConfigDefaults(t *testing.T) {
	c, err := ParseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Statuses: []string{"backlog", "open", "in-progress", "done"},
		Closed: []string{"done"}, Archived: []string{"archived"},
		TagGroups: []string{"severity", "effort", "area"}, RetractedMatch: 0.6}
	if !reflect.DeepEqual(c, want) || !reflect.DeepEqual(DefaultConfig(), want) {
		t.Fatalf("got %+v", c)
	}
	wantFile := "---\nstatuses: [backlog, open, in-progress, done]\nclosed: [done]\narchived: [archived]\n" +
		"tag_groups: [severity, effort, area]\nretracted_match: 0.6\n---\n"
	if out := join(t, c.Fields(), ""); out != wantFile {
		t.Fatalf("got\n%s", out)
	}
}

// dupe_text is retracted_match since step 9. A config that still has the old name keeps the line as an unknown key,
// and retracted_match takes its default.
func TestConfigDupeTextIsUnknown(t *testing.T) {
	f, _ := split(t, "---\ndupe_text: 0.9\n---\n")
	c, err := ParseConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.RetractedMatch != 0.6 || len(c.Extra) != 1 || c.Extra[0].Key != "dupe_text" || c.Extra[0].Value != "0.9" {
		t.Fatalf("retracted_match %v, extra %+v", c.RetractedMatch, c.Extra)
	}
}

// dupe_meaning went with the dupes command (issue #16). A config that still has it keeps the line as an unknown key.
func TestConfigDupeMeaningIsUnknown(t *testing.T) {
	f, _ := split(t, "---\ndupe_meaning: 0.85\n---\n")
	c, err := ParseConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Extra) != 1 || c.Extra[0].Key != "dupe_meaning" || c.Extra[0].Value != "0.85" {
		t.Fatalf("extra: %+v", c.Extra)
	}
	want := "---\nstatuses: [backlog, open, in-progress, done]\nclosed: [done]\narchived: [archived]\n" +
		"tag_groups: [severity, effort, area]\nretracted_match: 0.6\ndupe_meaning: 0.85\n---\n"
	if out := join(t, c.Fields(), ""); out != want {
		t.Fatalf("got\n%s", out)
	}
}

func TestConfigOverrides(t *testing.T) {
	src := "---\nstatuses: [todo, done]\nclosed: [done, wontfix]\narchived: []\ngithub_repo: owner/name\n" +
		"retracted_match: 0.7\n---\n"
	f, _ := split(t, src)
	c, err := ParseConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Statuses, []string{"todo", "done"}) || c.GitHubRepo != "owner/name" ||
		!reflect.DeepEqual(c.Closed, []string{"done", "wontfix"}) || c.Archived == nil || len(c.Archived) != 0 ||
		c.RetractedMatch != 0.7 || len(c.TagGroups) != 3 || c.EmbeddingsURL != "" {
		t.Fatalf("got %+v", c)
	}
	for _, bad := range []string{"---\nretracted_match: high\n---\n", "---\nstatuses: open\n---\n",
		"---\nclosed: done\n---\n", "---\narchived: old\n---\n"} {
		f, _ := split(t, bad)
		if _, err := ParseConfig(f); err == nil {
			t.Errorf("want an error for %q", bad)
		}
	}
}

// commit is true or false, and off when absent. Fields writes it only when it is on, so init's config.yml stays as
// it was.
func TestConfigCommit(t *testing.T) {
	for src, want := range map[string]bool{"---\ncommit: true\n---\n": true, "---\ncommit: false\n---\n": false} {
		f, _ := split(t, src)
		c, err := ParseConfig(f)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if c.Commit != want || len(c.Extra) != 0 {
			t.Fatalf("%q: got %+v", src, c)
		}
	}
	on := DefaultConfig()
	on.Commit = true
	wantFile := "---\nstatuses: [backlog, open, in-progress, done]\nclosed: [done]\narchived: [archived]\n" +
		"tag_groups: [severity, effort, area]\nretracted_match: 0.6\ncommit: true\n---\n"
	if out := join(t, on.Fields(), ""); out != wantFile {
		t.Fatalf("got\n%s", out)
	}
	for _, bad := range []string{"yes", "1", "True", "on", `""`, "[true]"} {
		f, _ := split(t, "---\ncommit: "+bad+"\n---\n")
		if _, err := ParseConfig(f); err == nil {
			t.Errorf("commit: %s: want an error", bad)
		}
	}
}

// Every key is either a scalar or a list; the wrong shape is an error, never a silently dropped value.
func TestParseRejectsWrongShape(t *testing.T) {
	for _, c := range []struct{ old, new string }{
		{"milestone: v0.4", "milestone: [v0.4]"},
		{"status: in-progress", "status: [in-progress]"},
	} {
		if _, err := ParseIssue(split(t, strings.Replace(specIssue, c.old, c.new, 1))); err == nil {
			t.Errorf("issue %q: want an error", c.new)
		}
	}
	for _, c := range []struct{ old, new string }{
		{"persona: skeptic", "persona: [skeptic]"},
		{"source: export_test.go:42", "source: [export_test.go:42]"},
		{"retracted_reason: the test was wrong", "retracted_reason: [x]"},
	} {
		if _, err := ParseComment(split(t, strings.Replace(specComment, c.old, c.new, 1))); err == nil {
			t.Errorf("comment %q: want an error", c.new)
		}
	}
	f, _ := split(t, "---\ngithub_repo: [owner/name]\n---\n")
	if _, err := ParseConfig(f); err == nil {
		t.Error("config github_repo list: want an error")
	}
}

func TestIssueCreator(t *testing.T) {
	i := Issue{ID: "7f3k", Title: "t", Status: "open", Due: "2026-11-01", By: "alice", Persona: "skeptic",
		Model: "example-model-1", Created: at("2026-10-17T19:00:00.000Z"), Updated: at("2026-10-17T19:00:00.000Z"),
		GitHub: 7}
	if got := keys(i.Fields()); got != "id title status due by persona model created updated github" {
		t.Errorf("order: %q", got)
	}
	j, err := ParseIssue(split(t, join(t, i.Fields(), i.Body)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(i, j) {
		t.Fatalf("got  %+v\nwant %+v", j, i)
	}
	i.Persona, i.Model = "", ""
	if got := keys(i.Fields()); got != "id title status due by created updated github" {
		t.Errorf("empty persona and model must be left out: %q", got)
	}
	for _, key := range []string{"by", "persona", "model"} {
		src := strings.Replace(specIssue, "github: 123\n", "github: 123\n"+key+": [a, b]\n", 1)
		if _, err := ParseIssue(split(t, src)); err == nil {
			t.Errorf("%s as a list: want an error", key)
		}
	}
}

// Times are written with three fraction digits, so they compare the same as text as they do as times. v0.1.0 wrote
// them without trailing zeros, and those files still load.
func TestTimesSortAsText(t *testing.T) {
	var written []string
	for _, old := range []string{"2026-10-18T20:23:44.71Z", "2026-10-18T20:23:44Z", "2026-10-18T20:23:44.7Z",
		"2026-10-18T20:23:44.693Z"} {
		c, err := ParseComment(split(t, strings.Replace(specComment, "at: 2026-10-17T19:20:00.000Z", "at: "+old, 1)))
		if err != nil {
			t.Fatalf("%s: %v", old, err)
		}
		for _, f := range c.Fields() {
			if f.Key == "at" {
				written = append(written, f.Value)
			}
		}
	}
	slices.Sort(written)
	want := []string{"2026-10-18T20:23:44.000Z", "2026-10-18T20:23:44.693Z", "2026-10-18T20:23:44.700Z",
		"2026-10-18T20:23:44.710Z"}
	if !reflect.DeepEqual(written, want) {
		t.Fatalf("got  %q\nwant %q", written, want)
	}
}
