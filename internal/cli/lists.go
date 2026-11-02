package cli

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/splitleafhq/cairn/internal/model"
	"github.com/splitleafhq/cairn/internal/words"
)

// defaultLimit is how many rows ls and search print when neither --limit nor --all is given.
const defaultLimit = 30

// limit is how many rows ls and search keep, as the package doc describes, or 0 with --all, for every row.
func (c *call) limit() (int, error) {
	v, given := c.given(flagLimit)
	switch {
	case given && c.switches[flagAll]:
		return 0, usageError("--limit and --all cannot both be given")
	case c.switches[flagAll]:
		return 0, nil
	case !given:
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(v)
	if errors.Is(err, strconv.ErrRange) && n > 0 {
		return 0, usageError(fmt.Sprintf("--limit %s is too large: use --all for every row", v))
	}
	if err != nil || n < 1 {
		return 0, usageError(fmt.Sprintf("--limit must be a whole number of 1 or more, not %q", v))
	}
	return n, nil
}

// keep is what ls or search prints of rows: every row when limit is 0 or there are no more than limit, else the
// last limit rows when fromEnd (ls) and the first limit otherwise (search). It sets the envelope's total to
// len(rows), and warns when it drops rows.
func keep[T any](c *call, rows []T, limit int, fromEnd bool) []T {
	total := len(rows)
	c.total = &total
	if limit == 0 || total <= limit {
		return rows
	}
	c.warn(fmt.Sprintf("%d of %d shown; --limit N or --all for more", limit, total))
	if fromEnd {
		return rows[total-limit:]
	}
	return rows[:limit]
}

func runLs(c *call) (output, error) {
	limit, err := c.limit()
	if err != nil {
		return output{}, err
	}
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	cfg, err := s.Config()
	if err != nil {
		return output{}, err
	}
	issues, err := s.ListIssues()
	if err != nil {
		return output{}, err
	}
	dueBefore, byDue := c.given(flagDueBefore)
	if byDue {
		if err := checkDate(dueBefore); err != nil {
			return output{}, err
		}
	}
	rows := make([]branchVersion, len(issues))
	for n, i := range issues {
		rows[n] = branchVersion{issue: i}
	}
	withBranches := c.switches[flagBranches]
	if withBranches {
		versions, err := c.branchIssues(s, "")
		if err != nil {
			return output{}, err
		}
		rows = append(rows, versions...)
	}
	status, byStatus := c.given(flagStatus)
	milestone, tags := c.value(flagMilestone), c.values[flagTag]
	closed, archived := c.switches[flagClosed], c.switches[flagArchived]
	rows = slices.DeleteFunc(rows, func(v branchVersion) bool {
		i := v.issue
		return byStatus && i.Status != status ||
			!byStatus && hidden(cfg, i.Status, closed, archived) ||
			milestone != "" && i.Milestone != milestone ||
			slices.ContainsFunc(tags, func(t string) bool { return !slices.Contains(i.Tags, t) }) ||
			byDue && (i.Due == "" || i.Due >= dueBefore)
	})
	if c.switches[flagOwed] {
		kept := rows[:0]
		for _, v := range rows {
			// A branch version carries its comments; the checkout's are read one issue at a time, and only for the
			// rows the other filters kept.
			comments := v.comments
			if v.branch == "" {
				if comments, err = s.LoadComments(v.issue.ID); err != nil {
					return output{}, err
				}
			}
			if owed(comments) {
				kept = append(kept, v)
			}
		}
		rows = kept
	}
	rows = keep(c, rows, limit, true)
	data := make([]lsJSON, len(rows))
	var text strings.Builder
	for n, v := range rows {
		data[n] = lsJSON{issueSummaryJSON: issueSummary(v.issue)}
		if withBranches {
			data[n].Branch = &v.branch
		}
		fmt.Fprintf(&text, "%s\t%s\t%s", v.issue.ID, v.issue.Status, v.issue.Title)
		if v.branch != "" {
			fmt.Fprintf(&text, "\t%s", v.branch)
		}
		text.WriteString("\n")
	}
	return output{data, text.String()}, nil
}

func runTags(c *call) (output, error) {
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	issues, err := s.ListIssues()
	if err != nil {
		return output{}, err
	}
	counts := map[string]int{}
	for _, i := range issues {
		for _, t := range slices.Compact(slices.Sorted(slices.Values(i.Tags))) {
			counts[t]++
		}
	}
	data := make([]tagJSON, 0, len(counts))
	var text strings.Builder
	for _, t := range slices.Sorted(maps.Keys(counts)) {
		data = append(data, tagJSON{Tag: t, Count: counts[t]})
		fmt.Fprintf(&text, "%s\t%d\n", t, counts[t])
	}
	return output{data, text.String()}, nil
}

func runSearch(c *call) (output, error) {
	query := strings.Join(c.args, " ")
	if len(words.Split(query)) == 0 {
		return output{}, usageError("search query has no words")
	}
	limit, err := c.limit()
	if err != nil {
		return output{}, err
	}
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	cfg, err := s.Config()
	if err != nil {
		return output{}, err
	}
	issues, err := s.ListIssues()
	if err != nil {
		return output{}, err
	}
	archived := c.switches[flagArchived]
	data := []searchJSON{}
	for _, i := range issues {
		if hidden(cfg, i.Status, true, archived) {
			continue
		}
		comments, err := s.LoadComments(i.ID)
		if err != nil {
			return output{}, err
		}
		if n := words.Shared(query, searchText(i, comments)); n > 0 {
			data = append(data, searchJSON{issueSummary(i), n})
		}
	}
	slices.SortStableFunc(data, func(a, b searchJSON) int { return cmp.Compare(b.Matches, a.Matches) })
	data = keep(c, data, limit, false)
	var text strings.Builder
	for _, hit := range data {
		fmt.Fprintf(&text, "%s\t%d\t%s\n", hit.ID, hit.Matches, hit.Title)
	}
	return output{data, text.String()}, nil
}

// owed reports whether the latest active decision among comments has no active result with a later at.
func owed(comments []model.Comment) bool {
	var decided, done time.Time
	for _, c := range comments {
		switch {
		case c.Retracted():
		case c.Kind == model.KindDecision && c.At.After(decided):
			decided = c.At
		case c.Kind == model.KindResult && c.At.After(done):
			done = c.At
		}
	}
	return !decided.IsZero() && !done.After(decided)
}

// hidden reports whether ls or search leaves out an issue with status: a closed one unless closed is true, and an
// archived one unless archived is true.
func hidden(cfg model.Config, status string, closed, archived bool) bool {
	return !closed && cfg.IsClosed(status) || !archived && cfg.IsArchived(status)
}

// searchText is the text search looks in: the issue's title and body, and the bodies and sources of its active
// comments.
func searchText(i model.Issue, comments []model.Comment) string {
	parts := []string{i.Title, i.Body}
	for _, cm := range comments {
		if !cm.Retracted() {
			parts = append(parts, cm.Body, cm.Source)
		}
	}
	return strings.Join(parts, " ")
}
