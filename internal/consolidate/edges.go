package consolidate

// buildEdgeDeltas emits one CoActivationBoost edge delta for every
// unordered pair of distinct memories in the written set. Per the
// Hebbian correctness argument (§3.3 / §7.1), edges strengthen only
// when both tuples were written — retrieval alone is not a signal.
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
	out := make([]EdgeDelta, 0, len(written)*(len(written)-1)/2)
	for i := 0; i < len(written); i++ {
		for j := i + 1; j < len(written); j++ {
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
