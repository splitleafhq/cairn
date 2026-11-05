// Package similar finds the existing issues whose titles are nearest a new one, by what the titles mean when an
// embeddings endpoint is set, and by the words they share otherwise. It only ranks: it never decides that two issues
// are the same work.
package similar

import (
	"cmp"
	"slices"

	"github.com/splitleafhq/cairn/internal/words"
)

// The signals a Hit can come from, as the warning that names it says them.
const (
	Meaning = "title meaning"
	Words   = "word overlap"
)

// Max is the most hits Rank returns.
const Max = 3

// Candidate is an issue a new title is compared with.
type Candidate struct {
	ID    string
	Title string
}

// Hit is a Candidate with its score: a cosine similarity for Meaning, a words.Overlap for Words.
type Hit struct {
	Candidate
	Score float64
}

// ByWords ranks the candidates by the word overlap of their titles with title. See Rank.
func ByWords(title string, candidates []Candidate) []Hit {
	hits := make([]Hit, len(candidates))
	for i, c := range candidates {
		hits[i] = Hit{c, words.Overlap(title, c.Title)}
	}
	return Rank(hits)
}

// Rank returns the Max best hits: highest score first, ties by id. A score of 0 or less is not listed.
func Rank(hits []Hit) []Hit {
	hits = slices.DeleteFunc(slices.Clone(hits), func(h Hit) bool { return h.Score <= 0 })
	slices.SortFunc(hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.ID, b.ID))
	})
	return hits[:min(len(hits), Max)]
}
