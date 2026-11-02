package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/splitleafhq/cairn/internal/model"
	"github.com/splitleafhq/cairn/internal/store"
)

func runMilestones(c *call) (output, error) {
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	milestones, err := s.ListMilestones()
	if err != nil {
		return output{}, err
	}
	issues, err := s.ListIssues()
	if err != nil {
		return output{}, err
	}
	counts := map[string]int{}
	for _, i := range issues {
		if i.Milestone != "" {
			counts[i.Milestone]++
		}
	}
	data := []milestoneCountJSON{}
	hasFile := map[string]bool{}
	for _, m := range milestones {
		hasFile[m.ID] = true
		data = append(data, milestoneCountJSON{milestoneData(m), counts[m.ID]})
	}
	for milestoneID, n := range counts {
		if !hasFile[milestoneID] {
			data = append(data, milestoneCountJSON{milestoneJSON{ID: milestoneID}, n})
		}
	}
	slices.SortFunc(data, compareMilestones)
	var text strings.Builder
	for _, m := range data {
		fmt.Fprintf(&text, "%s\t%s\t%d\t%s\n", m.ID, m.Due, m.Issues, m.Title)
	}
	return output{data, text.String()}, nil
}

// compareMilestones orders milestones by due date, those without one last, then by id.
func compareMilestones(a, b milestoneCountJSON) int {
	switch {
	case a.Due == b.Due:
		return strings.Compare(a.ID, b.ID)
	case a.Due == "":
		return 1
	case b.Due == "":
		return -1
	}
	return strings.Compare(a.Due, b.Due)
}

func runMilestoneNew(c *call) (output, error) {
	milestoneID, title := c.args[0], c.args[1]
	if !model.ValidMilestoneID(milestoneID) {
		return output{}, usageError(`milestone id must not be empty, contain "/" or start with "."`)
	}
	if blank(title) {
		return output{}, usageError("milestone title must not be blank")
	}
	due := c.value(flagDue)
	if err := checkClearableDate(due); err != nil {
		return output{}, err
	}
	if _, err := c.actor(); err != nil {
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
	status, err := firstStatus(cfg)
	if err != nil {
		return output{}, err
	}
	ids, err := s.MilestoneIDs()
	if err != nil {
		return output{}, err
	}
	for _, other := range ids {
		if other != milestoneID && strings.EqualFold(other, milestoneID) {
			return output{}, existsError(fmt.Sprintf("milestone %q already exists, and %q differs from it only in case",
				other, milestoneID))
		}
	}
	switch _, err := s.LoadMilestone(milestoneID); {
	case err == nil:
		return output{}, existsError(fmt.Sprintf("milestone %q already exists", milestoneID))
	case !errors.Is(err, store.ErrNotFound):
		return output{}, err
	}
	m := model.Milestone{ID: milestoneID, Title: title, Status: status, Due: due}
	if err := s.SaveMilestone(m); err != nil {
		return output{}, err
	}
	c.wroteMilestone(s, m.ID)
	return output{milestoneData(m), m.ID + "\n"}, nil
}

func runMilestoneEdit(c *call) (output, error) {
	milestoneID := c.args[0]
	if !c.anyGiven(flagTitle, flagStatus, flagDue) {
		return output{}, usageError("milestone edit requires at least one change")
	}
	title, hasTitle := c.given(flagTitle)
	if hasTitle && blank(title) {
		return output{}, usageError("milestone title must not be blank")
	}
	due, hasDue := c.given(flagDue)
	if err := checkClearableDate(due); err != nil {
		return output{}, err
	}
	if _, err := c.actor(); err != nil {
		return output{}, err
	}
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	m, err := s.LoadMilestone(milestoneID)
	if errors.Is(err, store.ErrNotFound) {
		return output{}, notFoundError(fmt.Sprintf("no milestone %q", milestoneID))
	}
	if err != nil {
		return output{}, err
	}
	cfg, err := s.Config()
	if err != nil {
		return output{}, err
	}
	if status, ok := c.given(flagStatus); ok {
		if err := checkStatus(status, cfg); err != nil {
			return output{}, err
		}
		m.Status = status
	}
	if hasTitle {
		m.Title = title
	}
	if hasDue {
		m.Due = due
	}
	if err := s.SaveMilestone(m); err != nil {
		return output{}, err
	}
	c.wroteMilestone(s, m.ID)
	return output{milestoneData(m), m.ID + "\n"}, nil
}
