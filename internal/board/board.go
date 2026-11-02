// Package board lays a tracker's issues out as a board: one column per status, in the config's order. A board is
// printed as text for a terminal, or as one self-contained page for a browser.
package board

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/splitleafhq/cairn/internal/model"
)

// Column is the issues of one status, the one updated last first. Collapsed is true for a closed or archived status:
// a terminal shows its count and not its cards, and the page starts it shut.
type Column struct {
	Status    string
	Collapsed bool
	Issues    []model.Issue
}

// Board is a tracker's issues in columns.
type Board struct {
	Columns []Column
}

// New lays issues out in columns: one for each of the config's statuses in its order, empty ones included, then one
// for each other status that an issue has, in byte order, as after a change to the config. A column is collapsed when
// the config says its status is closed or archived. A column lists the issue updated last first, and issues updated at
// the same time in the order they came in. New does not change issues.
func New(cfg model.Config, issues []model.Issue) Board {
	byStatus := map[string][]model.Issue{}
	for _, i := range issues {
		byStatus[i.Status] = append(byStatus[i.Status], i)
	}
	statuses := slices.Clone(cfg.Statuses)
	for _, s := range slices.Sorted(maps.Keys(byStatus)) {
		if !slices.Contains(statuses, s) {
			statuses = append(statuses, s)
		}
	}
	var b Board
	for n, s := range statuses {
		if slices.Contains(statuses[:n], s) {
			continue // a status listed twice in the config is one column
		}
		in := byStatus[s]
		slices.SortStableFunc(in, func(x, y model.Issue) int { return y.Updated.Compare(x.Updated) })
		b.Columns = append(b.Columns, Column{Status: s, Collapsed: cfg.IsClosed(s) || cfg.IsArchived(s), Issues: in})
	}
	return b
}

// Total is how many issues the board holds, collapsed columns included.
func (b Board) Total() int {
	n := 0
	for _, c := range b.Columns {
		n += len(c.Issues)
	}
	return n
}

// Text is the board for a terminal. Each column has a header, "<status> (<count>)", with ", collapsed" after the
// count for a collapsed one, and under it one line for each card of a column that is not collapsed: a tab, then the
// id, time of the last update, tags (joined by ", ") and title, separated by tabs. The title is last, as in ls, so a
// tab in a title shifts no other field. A blank line separates columns.
func (b Board) Text() string {
	var text strings.Builder
	for n, c := range b.Columns {
		if n > 0 {
			text.WriteString("\n")
		}
		if c.Collapsed {
			fmt.Fprintf(&text, "%s (%d, collapsed)\n", c.Status, len(c.Issues))
			continue
		}
		fmt.Fprintf(&text, "%s (%d)\n", c.Status, len(c.Issues))
		for _, i := range c.Issues {
			fmt.Fprintf(&text, "\t%s\t%s\t%s\t%s\n", i.ID, model.FormatTime(i.Updated), strings.Join(i.Tags, ", "), i.Title)
		}
	}
	return text.String()
}
