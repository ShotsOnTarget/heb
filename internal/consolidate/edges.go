package consolidate

import "strings"

// buildEdgeDeltas emits one CoActivationBoost edge delta for every
// unordered pair of distinct memories in the written set that share at
// least one token in their subject or object fields. This prevents
// coincidental pairs (lessons that happened to be learned in the same
// session but are about unrelated topics) from creating noise edges.
//
// Token overlap is checked on subject and object only — predicates are
// excluded because verbs like "defined_in" or "has" would match
// everything.
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

	// Pre-compute token sets for each written memory (subject + object only).
	type tokenSet map[string]bool
	sets := make([]tokenSet, len(written))
	for i, m := range written {
		sets[i] = extractTokens(m.Subject, m.Object)
	}

	var out []EdgeDelta
	for i := 0; i < len(written); i++ {
		for j := i + 1; j < len(written); j++ {
			if !tokensOverlap(sets[i], sets[j]) {
				continue
			}
			out = append(out, EdgeDelta{
				A:            SPO{Subject: written[i].Subject, Predicate: written[i].Predicate, Object: written[i].Object},
				B:            SPO{Subject: written[j].Subject, Predicate: written[j].Predicate, Object: written[j].Object},
				Delta:        cfg.CoActivationBoost,
				CoActivation: true,
			})
		}
	}
	return out
}

// extractTokens splits subject and object fields on underscores, spaces,
// and the middle-dot separator, lowercases everything, and returns a set
// of tokens. Single-character tokens are dropped as noise.
func extractTokens(fields ...string) map[string]bool {
	set := make(map[string]bool)
	for _, f := range fields {
		for _, word := range splitTokens(f) {
			if len(word) > 1 {
				set[word] = true
			}
		}
	}
	return set
}

// splitTokens breaks a string into lowercase tokens on common delimiters.
func splitTokens(s string) []string {
	s = strings.ToLower(s)
	// Replace common delimiters with spaces, then split.
	s = strings.NewReplacer(
		"_", " ",
		"\u00b7", " ", // middle dot
		".", " ",
		"/", " ",
		"-", " ",
	).Replace(s)
	return strings.Fields(s)
}

// tokensOverlap returns true if the two token sets share at least one token.
func tokensOverlap(a, b map[string]bool) bool {
	// Iterate the smaller set for efficiency.
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
