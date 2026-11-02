package cli

import (
	"errors"
	"fmt"
	"slices"

	"github.com/splitleafhq/cairn/internal/model"
	"github.com/splitleafhq/cairn/internal/store"
	"github.com/splitleafhq/cairn/internal/words"
)

// kinds are the kinds comment takes.
var kinds = []string{model.KindEvidence, model.KindDecision, model.KindResult}

func runComment(c *call) (output, error) {
	issueID, text := c.args[0], c.args[1]
	kind := c.value(flagKind)
	if !slices.Contains(kinds, kind) {
		return output{}, usageError("kind must be evidence, decision or result")
	}
	source := c.value(flagSource)
	if kind == model.KindEvidence && blank(source) {
		return output{}, usageError("evidence requires --source")
	}
	if blank(text) {
		return output{}, usageError("comment text must not be blank")
	}
	actor, err := c.actor()
	if err != nil {
		return output{}, err
	}
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	_, comments, err := s.LoadIssue(issueID)
	if err != nil {
		return output{}, noIssue(err, issueID)
	}
	cfg, err := s.Config()
	if err != nil {
		return output{}, err
	}
	matches := []retractedMatchJSON{}
	for _, old := range comments {
		if !old.Retracted() {
			continue
		}
		if score := words.Overlap(text, old.Body); score >= cfg.RetractedMatch {
			matches = append(matches, retractedMatchJSON{Comment: old.ID, Reason: old.RetractedReason, Score: score})
			c.warn(fmt.Sprintf("matches retracted comment %s (reason %q)", old.ID, old.RetractedReason))
		}
	}
	commentID, err := s.NewCommentID(issueID, c.env.Rand)
	if err != nil {
		return output{}, noIssue(err, issueID)
	}
	cm := model.Comment{
		ID:      commentID,
		Kind:    kind,
		By:      actor,
		Persona: c.value(flagPersona),
		Model:   c.value(flagModel),
		At:      c.env.stamp(),
		Source:  source,
		Body:    withNewline(text),
	}
	if err := s.SaveComment(issueID, cm); err != nil {
		return output{}, err
	}
	c.wroteIssue(s, issueID)
	return output{commentResultJSON{Comment: commentData(cm), RetractedMatches: matches}, cm.ID + "\n"}, nil
}

func runRetract(c *call) (output, error) {
	issueID, commentID := c.args[0], c.args[1]
	reason := c.value(flagReason)
	if blank(reason) {
		return output{}, usageError("retract requires a non-blank --reason")
	}
	disprovedBy := c.value(flagDisprovedBy)
	if disprovedBy != "" && disprovedBy == commentID {
		return output{}, usageError("disproved-by must not be the same comment as the one being retracted")
	}
	actor, err := c.actor()
	if err != nil {
		return output{}, err
	}
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	_, comments, err := s.LoadIssue(issueID)
	if err != nil {
		return output{}, noIssue(err, issueID)
	}
	i := indexOf(comments, commentID)
	if i < 0 {
		return output{}, noComment(commentID, issueID)
	}
	if disprovedBy != "" && indexOf(comments, disprovedBy) < 0 {
		return output{}, noComment(disprovedBy, issueID)
	}
	cm := comments[i]
	if cm.Retracted() {
		return output{}, fmt.Errorf("comment %q is already retracted", commentID)
	}
	cm.RetractedBy = actor
	cm.RetractedAt = c.env.stamp()
	cm.RetractedReason = reason
	cm.DisprovedBy = disprovedBy
	if err := s.SaveComment(issueID, cm); err != nil {
		return output{}, err
	}
	c.wroteIssue(s, issueID)
	return output{commentData(cm), "retracted " + commentID + "\n"}, nil
}

func runPurge(c *call) (output, error) {
	issueID, commentID := c.args[0], c.args[1]
	if _, err := c.actor(); err != nil {
		return output{}, err
	}
	s, err := c.openStore()
	if err != nil {
		return output{}, err
	}
	if err := s.RemoveComment(issueID, commentID); err != nil {
		if errors.Is(err, store.ErrNoComment) {
			return output{}, noComment(commentID, issueID)
		}
		return output{}, noIssue(err, issueID)
	}
	c.wroteIssue(s, issueID)
	return output{purgeJSON{ID: issueID, Comment: commentID}, "purged " + commentID + "\n"}, nil
}

// indexOf is the index of the comment with id in comments, or -1 when there is none.
func indexOf(comments []model.Comment, id string) int {
	return slices.IndexFunc(comments, func(cm model.Comment) bool { return cm.ID == id })
}
