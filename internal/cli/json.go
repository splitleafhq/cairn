package cli

import "github.com/splitleafhq/cairn/internal/model"

// The JSON that commands print, as the package doc describes it.

type successJSON struct {
	OK       bool     `json:"ok"`
	Data     any      `json:"data"`
	Total    *int     `json:"total,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type failureJSON struct {
	OK    bool      `json:"ok"`
	Error errorJSON `json:"error"`
}

type errorJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type versionJSON struct {
	Version string `json:"version"`
}

type helpJSON struct {
	Usage []string `json:"usage"`
	Rules []string `json:"rules"`
}

type initJSON struct {
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
}

type rmJSON struct {
	ID string `json:"id"`
}

type purgeJSON struct {
	ID      string `json:"id"`
	Comment string `json:"comment"`
}

// issueSummaryJSON is an issue without its body, as lists give it.
type issueSummaryJSON struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Tags      []string `json:"tags"`
	Milestone string   `json:"milestone"`
	Due       string   `json:"due"`
	By        string   `json:"by"`
	Persona   string   `json:"persona"`
	Model     string   `json:"model"`
	Created   string   `json:"created"`
	Updated   string   `json:"updated"`
	GitHub    int      `json:"github"`
}

type issueJSON struct {
	issueSummaryJSON
	Body string `json:"body"`
}

type showJSON struct {
	issueJSON
	Comments []commentJSON `json:"comments"`
}

// lsJSON is a row of ls. Branch is set only with --branches, to "" for the checkout's own rows.
type lsJSON struct {
	issueSummaryJSON
	Branch *string `json:"branch,omitempty"`
}

// showBranchJSON is one version of an issue that show --branches prints. Branch is "" for the checkout's version.
type showBranchJSON struct {
	showJSON
	Branch string `json:"branch"`
}

type searchJSON struct {
	issueSummaryJSON
	Matches int `json:"matches"`
}

// boardJSON is the data of board: a column for each status, in the board's order.
type boardJSON struct {
	Columns []boardColumnJSON `json:"columns"`
}

// boardColumnJSON is a column of the board. Count is how many issues the status has. Issues lists them, but is empty
// for a collapsed column, as the text leaves out its cards.
type boardColumnJSON struct {
	Status    string             `json:"status"`
	Collapsed bool               `json:"collapsed"`
	Count     int                `json:"count"`
	Issues    []issueSummaryJSON `json:"issues"`
}

// boardFileJSON is the data of board --html: the page's path and how many issues it holds.
type boardFileJSON struct {
	File   string `json:"file"`
	Issues int    `json:"issues"`
}

type commentJSON struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	By        string          `json:"by"`
	Persona   string          `json:"persona"`
	Model     string          `json:"model"`
	At        string          `json:"at"`
	Source    string          `json:"source"`
	Body      string          `json:"body"`
	Retracted *retractionJSON `json:"retracted"` // nil for an active comment
}

type retractionJSON struct {
	By          string `json:"by"`
	At          string `json:"at"`
	Reason      string `json:"reason"`
	DisprovedBy string `json:"disproved_by"`
}

type commentResultJSON struct {
	Comment          commentJSON          `json:"comment"`
	RetractedMatches []retractedMatchJSON `json:"retracted_matches"`
}

// retractedMatchJSON is a retracted comment that a new comment matches.
type retractedMatchJSON struct {
	Comment string  `json:"comment"`
	Reason  string  `json:"reason"`
	Score   float64 `json:"score"`
}

type tagJSON struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type milestoneJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Due    string `json:"due"`
}

type milestoneCountJSON struct {
	milestoneJSON
	Issues int `json:"issues"`
}

func issueSummary(i model.Issue) issueSummaryJSON {
	tags := i.Tags
	if tags == nil {
		tags = []string{}
	}
	return issueSummaryJSON{
		ID:        i.ID,
		Title:     i.Title,
		Status:    i.Status,
		Tags:      tags,
		Milestone: i.Milestone,
		Due:       i.Due,
		By:        i.By,
		Persona:   i.Persona,
		Model:     i.Model,
		Created:   model.FormatTime(i.Created),
		Updated:   model.FormatTime(i.Updated),
		GitHub:    i.GitHub,
	}
}

func issueData(i model.Issue) issueJSON {
	return issueJSON{issueSummary(i), i.Body}
}

func showData(i model.Issue, comments []model.Comment) showJSON {
	cs := make([]commentJSON, len(comments))
	for n, c := range comments {
		cs[n] = commentData(c)
	}
	return showJSON{issueData(i), cs}
}

func commentData(c model.Comment) commentJSON {
	cj := commentJSON{
		ID:      c.ID,
		Kind:    c.Kind,
		By:      c.By,
		Persona: c.Persona,
		Model:   c.Model,
		At:      model.FormatTime(c.At),
		Source:  c.Source,
		Body:    c.Body,
	}
	if c.Retracted() {
		cj.Retracted = &retractionJSON{
			By:          c.RetractedBy,
			At:          model.FormatTime(c.RetractedAt),
			Reason:      c.RetractedReason,
			DisprovedBy: c.DisprovedBy,
		}
	}
	return cj
}

func milestoneData(m model.Milestone) milestoneJSON {
	return milestoneJSON{ID: m.ID, Title: m.Title, Status: m.Status, Due: m.Due}
}
