// Package model holds Cairn's issues, comments, milestones and config, and converts each to and from the
// frontmatter fields of its file.
//
// Parsing checks the shape of each value (ids, dates, times, numbers, and scalar or list) but not statuses, tags or
// kinds against the config: a config change must never stop an old file from loading. Keys the parser does not know
// are kept, in order, in Extra (nil when there are none), and written back after the known keys, so a file from a
// newer Cairn survives an edit.
//
// Dates are DateLayout. Times are RFC 3339: they parse with or without fractional seconds and with any offset, and
// are converted to UTC. FormatTime writes them, always with three fraction digits.
package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/splitleafhq/cairn/internal/doc"
	"github.com/splitleafhq/cairn/internal/id"
)

// DateLayout is how a date is written: "2006-01-02".
const DateLayout = time.DateOnly

// TimeLayout is how every time is written: UTC, with three fraction digits, so every time is 24 characters and times
// compare the same as text as they do as times.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// FormatTime writes t in TimeLayout, in UTC.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

// The comment kinds a writer may add.
const (
	KindEvidence = "evidence"
	KindDecision = "decision"
	KindResult   = "result"
)

// The frontmatter keys of every file type.
const (
	keyID              = "id"
	keyTitle           = "title"
	keyStatus          = "status"
	keyTags            = "tags"
	keyMilestone       = "milestone"
	keyDue             = "due"
	keyBy              = "by"
	keyPersona         = "persona"
	keyModel           = "model"
	keyCreated         = "created"
	keyUpdated         = "updated"
	keyGitHub          = "github"
	keyKind            = "kind"
	keyAt              = "at"
	keySource          = "source"
	keyRetractedBy     = "retracted_by"
	keyRetractedAt     = "retracted_at"
	keyRetractedReason = "retracted_reason"
	keyDisprovedBy     = "disproved_by"
	keyStatuses        = "statuses"
	keyClosed          = "closed"
	keyArchived        = "archived"
	keyTagGroups       = "tag_groups"
	keyGitHubRepo      = "github_repo"
	keyEmbeddingsURL   = "embeddings_url"
	keyRetractedMatch  = "retracted_match"
	keyCommit          = "commit"
)

// TagGroupsKey is the config.yml key that lists the tag groups.
const TagGroupsKey = keyTagGroups

// Issue is the issue.md file of an issue folder. Required keys: id, title, status, created, updated. The others are
// left out of the file when empty (GitHub when 0).
type Issue struct {
	ID        string
	Title     string
	Status    string
	Tags      []string
	Milestone string
	Due       string
	By        string // who created the issue; set once, never changed by an edit
	Persona   string
	Model     string
	Created   time.Time
	Updated   time.Time
	GitHub    int
	Extra     []doc.Field
	Body      string
}

// Comment is one c-<id>.md file in an issue folder. Required keys: id, kind, by, at. The others are left out of the
// file when empty.
type Comment struct {
	ID              string
	Kind            string
	By              string
	Persona         string
	Model           string
	At              time.Time
	Source          string
	RetractedBy     string
	RetractedAt     time.Time
	RetractedReason string
	DisprovedBy     string
	Extra           []doc.Field
	Body            string
}

// Retracted reports whether the comment has been retracted.
func (c Comment) Retracted() bool {
	return c.RetractedBy != ""
}

// What a milestone id, which is a file name in milestones/, must not contain (pathSep) or start with (hiddenPrefix).
const (
	pathSep      = "/" // it is one name, not a path
	hiddenPrefix = "." // a name that starts with it is hidden, as temporary files are
)

// ValidMilestoneID reports whether s can name a milestone: it is not empty, has no "/", and does not start with ".".
// The milestone an issue names must pass too, unless it is "", so that milestone can always get a file.
func ValidMilestoneID(s string) bool {
	return s != "" && !strings.Contains(s, pathSep) && !strings.HasPrefix(s, hiddenPrefix)
}

// Milestone is one file in milestones/. Its id is its file name without ".md" (for example "v0.4") and passes
// ValidMilestoneID. Required keys: id, title, status. Due is left out of the file when empty.
type Milestone struct {
	ID     string
	Title  string
	Status string
	Due    string
	Extra  []doc.Field
	Body   string
}

// Config is config.yml: frontmatter only, with an empty body. Every key is optional. Closed and Archived name the
// statuses that count as closed and archived, as IsClosed and IsArchived read them. Commit turns on commit on write.
type Config struct {
	Statuses       []string
	Closed         []string
	Archived       []string
	TagGroups      []string
	GitHubRepo     string
	EmbeddingsURL  string
	RetractedMatch float64
	Commit         bool
	Extra          []doc.Field
}

// DefaultConfig is the config used for absent keys: statuses [backlog, open, in-progress, done], closed [done],
// archived [archived], tag groups [severity, effort, area] and retracted_match 0.6, with GitHub sync, embeddings and
// commit on write off.
func DefaultConfig() Config {
	return Config{
		Statuses:       []string{"backlog", "open", "in-progress", "done"},
		Closed:         []string{"done"},
		Archived:       []string{"archived"},
		TagGroups:      []string{"severity", "effort", "area"},
		RetractedMatch: 0.6,
	}
}

// ParseIssue builds an Issue from a file's frontmatter and body. It returns an error for a missing required key, a
// bad id, date, time or number, or a scalar where a list belongs (or the reverse).
func ParseIssue(fields []doc.Field, body []byte) (Issue, error) {
	r := newReader(fields)
	i := Issue{
		ID:        r.text(keyID),
		Title:     r.text(keyTitle),
		Status:    r.text(keyStatus),
		Tags:      r.list(keyTags, nil),
		Milestone: r.text(keyMilestone),
		Due:       r.date(keyDue),
		By:        r.text(keyBy),
		Persona:   r.text(keyPersona),
		Model:     r.text(keyModel),
		Created:   r.time(keyCreated),
		Updated:   r.time(keyUpdated),
		GitHub:    r.number(keyGitHub),
		Body:      string(body),
	}
	if err := r.done(keyID, keyTitle, keyStatus, keyCreated, keyUpdated); err != nil {
		return Issue{}, err
	}
	if !id.Valid(i.ID) {
		return Issue{}, fmt.Errorf("model: bad issue id %q", i.ID)
	}
	i.Extra = r.extra()
	return i, nil
}

// Fields returns the issue's frontmatter in the order id, title, status, tags, milestone, due, by, persona, model,
// created, updated, github, then Extra.
func (i Issue) Fields() []doc.Field {
	var w writer
	w.text(keyID, i.ID)
	w.text(keyTitle, i.Title)
	w.text(keyStatus, i.Status)
	w.optList(keyTags, i.Tags)
	w.optText(keyMilestone, i.Milestone)
	w.optText(keyDue, i.Due)
	w.optText(keyBy, i.By)
	w.optText(keyPersona, i.Persona)
	w.optText(keyModel, i.Model)
	w.time(keyCreated, i.Created)
	w.time(keyUpdated, i.Updated)
	w.optNumber(keyGitHub, i.GitHub)
	return w.with(i.Extra)
}

// ParseComment builds a Comment from a file's frontmatter and body, with the same errors as ParseIssue. The id must
// pass id.ValidComment, and disproved_by too when present.
func ParseComment(fields []doc.Field, body []byte) (Comment, error) {
	r := newReader(fields)
	c := Comment{
		ID:              r.text(keyID),
		Kind:            r.text(keyKind),
		By:              r.text(keyBy),
		Persona:         r.text(keyPersona),
		Model:           r.text(keyModel),
		At:              r.time(keyAt),
		Source:          r.text(keySource),
		RetractedBy:     r.text(keyRetractedBy),
		RetractedAt:     r.time(keyRetractedAt),
		RetractedReason: r.text(keyRetractedReason),
		DisprovedBy:     r.text(keyDisprovedBy),
		Body:            string(body),
	}
	if err := r.done(keyID, keyKind, keyBy, keyAt); err != nil {
		return Comment{}, err
	}
	if !id.ValidComment(c.ID) {
		return Comment{}, fmt.Errorf("model: bad comment id %q", c.ID)
	}
	if c.DisprovedBy != "" && !id.ValidComment(c.DisprovedBy) {
		return Comment{}, fmt.Errorf("model: bad disproved_by id %q", c.DisprovedBy)
	}
	c.Extra = r.extra()
	return c, nil
}

// Fields returns the comment's frontmatter in the order id, kind, by, persona, model, at, source, retracted_by,
// retracted_at, retracted_reason, disproved_by, then Extra.
func (c Comment) Fields() []doc.Field {
	var w writer
	w.text(keyID, c.ID)
	w.text(keyKind, c.Kind)
	w.text(keyBy, c.By)
	w.optText(keyPersona, c.Persona)
	w.optText(keyModel, c.Model)
	w.time(keyAt, c.At)
	w.optText(keySource, c.Source)
	w.optText(keyRetractedBy, c.RetractedBy)
	w.optTime(keyRetractedAt, c.RetractedAt)
	w.optText(keyRetractedReason, c.RetractedReason)
	w.optText(keyDisprovedBy, c.DisprovedBy)
	return w.with(c.Extra)
}

// ParseMilestone builds a Milestone from a file's frontmatter and body, with the same errors as ParseIssue.
func ParseMilestone(fields []doc.Field, body []byte) (Milestone, error) {
	r := newReader(fields)
	m := Milestone{
		ID:     r.text(keyID),
		Title:  r.text(keyTitle),
		Status: r.text(keyStatus),
		Due:    r.date(keyDue),
		Body:   string(body),
	}
	if err := r.done(keyID, keyTitle, keyStatus); err != nil {
		return Milestone{}, err
	}
	if !ValidMilestoneID(m.ID) {
		return Milestone{}, fmt.Errorf("model: milestone id %q must not contain \"/\" or start with \".\"", m.ID)
	}
	m.Extra = r.extra()
	return m, nil
}

// Fields returns the milestone's frontmatter in the order id, title, status, due, then Extra.
func (m Milestone) Fields() []doc.Field {
	var w writer
	w.text(keyID, m.ID)
	w.text(keyTitle, m.Title)
	w.text(keyStatus, m.Status)
	w.optText(keyDue, m.Due)
	return w.with(m.Extra)
}

// ParseConfig builds a Config from config.yml's frontmatter, taking DefaultConfig's value for each absent key. It
// returns an error for a bad number, a commit that is not true or false, or a scalar where a list belongs.
func ParseConfig(fields []doc.Field) (Config, error) {
	def := DefaultConfig()
	r := newReader(fields)
	c := Config{
		Statuses:       r.list(keyStatuses, def.Statuses),
		Closed:         r.list(keyClosed, def.Closed),
		Archived:       r.list(keyArchived, def.Archived),
		TagGroups:      r.list(keyTagGroups, def.TagGroups),
		GitHubRepo:     r.text(keyGitHubRepo),
		EmbeddingsURL:  r.text(keyEmbeddingsURL),
		RetractedMatch: r.float(keyRetractedMatch, def.RetractedMatch),
		Commit:         r.boolean(keyCommit),
	}
	if err := r.done(); err != nil {
		return Config{}, err
	}
	c.Extra = r.extra()
	return c, nil
}

// Fields returns the config's frontmatter in the order statuses, closed, archived, tag_groups, github_repo,
// embeddings_url, retracted_match, commit, then Extra. github_repo and embeddings_url are left out when empty, and
// commit when false. Numbers are written with strconv.FormatFloat(f, 'g', -1, 64).
func (c Config) Fields() []doc.Field {
	var w writer
	w.list(keyStatuses, c.Statuses)
	w.list(keyClosed, c.Closed)
	w.list(keyArchived, c.Archived)
	w.list(keyTagGroups, c.TagGroups)
	w.optText(keyGitHubRepo, c.GitHubRepo)
	w.optText(keyEmbeddingsURL, c.EmbeddingsURL)
	w.float(keyRetractedMatch, c.RetractedMatch)
	w.optTrue(keyCommit, c.Commit)
	return w.with(c.Extra)
}

// IsArchived reports whether status is one of the config's archived statuses.
func (c Config) IsArchived(status string) bool {
	return slices.Contains(c.Archived, status)
}

// IsClosed reports whether status is one of the config's closed statuses and not an archived one: a status in both
// counts as archived.
func (c Config) IsClosed(status string) bool {
	return slices.Contains(c.Closed, status) && !c.IsArchived(status)
}
