package retrieve

import (
	"github.com/steelboltgames/heb/internal/memory"
	"github.com/steelboltgames/heb/internal/store"
)

// SimilarPair is a pair of recalled memories with high token overlap
// but different bodies, where the older one may be superseded.
type SimilarPair struct {
	Older   store.Scored `json:"older"`
	Newer   store.Scored `json:"newer"`
	Jaccard float64      `json:"jaccard"`
}

// FindSimilarPairs runs pairwise Jaccard similarity on recalled memories.
// Returns pairs where Jaccard >= threshold and the bodies differ.
// Within each pair, the memory with the older UpdatedAt is marked Older.
func FindSimilarPairs(memories []store.Scored, threshold float64) []SimilarPair {
	if len(memories) < 2 {
		return nil
	}

	// Tokenize once per memory
	type entry struct {
		scored store.Scored
		tokens map[string]bool
	}
	entries := make([]entry, len(memories))
	for i, m := range memories {
		words := memory.Tokenize(m.Body)
		set := make(map[string]bool, len(words))
		for _, w := range words {
			set[w] = true
		}
		entries[i] = entry{scored: m, tokens: set}
	}

	var pairs []SimilarPair
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a.scored.Body == b.scored.Body {
				continue // same memory (e.g. match + edge)
			}
			j := jaccard(a.tokens, b.tokens)
			if j < threshold {
				continue
			}
			older, newer := a.scored, b.scored
			if b.scored.UpdatedAt < a.scored.UpdatedAt {
				older, newer = b.scored, a.scored
			}
			pairs = append(pairs, SimilarPair{
				Older:   older,
				Newer:   newer,
				Jaccard: j,
			})
		}
	}
	return pairs
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
