package consolidate

import "github.com/steelboltgames/heb/internal/memory"

// buildEdgeDeltas emits one CoActivationBoost edge delta for every
// unordered pair of distinct memories in the written set that share at
// least one token. This prevents coincidental pairs (lessons that
// happened to be learned in the same session but are about unrelated
// topics) from creating noise edges.
//
// Token overlap is checked on the full body text using the shared
// tokenizer.
//
// The written set is derived from the memory deltas produced by
// buildMemoryDeltas. Entanglement-signal memory deltas must NOT be in
// that input — only session_reinforced entries count.
//
// If fewer than 2 written tuples: returns nil.
func buildEdgeDeltas(written []MemoryDelta, cfg Config) []EdgeDelta {
	if len(written) < 2 {
		return nil
	}

	// Pre-compute token sets for each written memory.
	type tokenSet map[string]bool
	sets := make([]tokenSet, len(written))
	for i, m := range written {
		set := make(tokenSet)
		for _, tok := range memory.Tokenize(m.Body) {
			set[tok] = true
		}
		sets[i] = set
	}

	var out []EdgeDelta
	for i := 0; i < len(written); i++ {
		for j := i + 1; j < len(written); j++ {
			if !tokensOverlap(sets[i], sets[j]) {
				continue
			}
			out = append(out, EdgeDelta{
				ABody:        written[i].Body,
				BBody:        written[j].Body,
				Delta:        cfg.CoActivationBoost,
				CoActivation: true,
			})
		}
	}
	return out
}

// tokensOverlap returns true if the two token sets share at least one token.
func tokensOverlap(a, b map[string]bool) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for tok := range a {
		if b[tok] {
			return true
		}
	}
	return false
}
