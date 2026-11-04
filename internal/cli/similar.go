package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/splitleafhq/cairn/internal/model"
	"github.com/splitleafhq/cairn/internal/similar"
	"github.com/splitleafhq/cairn/internal/store"
)

// indexDir is the folder in the tracker where derived data is kept. It is ignored by git.
const indexDir = ".index"

// warnSimilar warns of the existing issues nearest the title of the issue that new has just filed, as the package
// comment says. Nothing in it fails the command: every error becomes a warning.
func (c *call) warnSimilar(s *store.Store, cfg model.Config, filed model.Issue) {
	issues, err := s.ListIssues()
	if err != nil {
		c.warn("similar issues not checked: " + err.Error())
		return
	}
	var candidates []similar.Candidate
	for _, i := range issues {
		if i.ID != filed.ID && !cfg.IsArchived(i.Status) {
			candidates = append(candidates, similar.Candidate{ID: i.ID, Title: i.Title})
		}
	}
	if len(candidates) == 0 {
		return
	}
	method := similar.Words
	var hits []similar.Hit
	if cfg.EmbeddingsURL == "" {
		hits = similar.ByWords(filed.Title, candidates)
	} else {
		e := similar.Embedder{URL: cfg.EmbeddingsURL, Model: cfg.EmbeddingsModel, Prompt: cfg.EmbeddingsPrompt,
			Index: filepath.Join(s.Dir, indexDir)}
		var cacheErr error
		hits, cacheErr, err = e.ByMeaning(context.Background(), filed.Title, candidates)
		switch {
		case err != nil:
			c.warn("embeddings unavailable, ranked by word overlap: " + err.Error())
			hits = similar.ByWords(filed.Title, candidates)
		case cacheErr != nil:
			method = similar.Meaning
			c.warn("title vectors not cached: " + cacheErr.Error())
		default:
			method = similar.Meaning
		}
	}
	for _, h := range hits {
		c.warn(fmt.Sprintf("similar issue %s (%s %.2f): %s", h.ID, method, h.Score, h.Title))
	}
}
