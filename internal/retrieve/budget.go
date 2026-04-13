package retrieve

import (
	"github.com/steelboltgames/heb/internal/store"
)

// Result is the full retrieve output: the data backing both Block 1
// (human-readable) and Block 2 (contract:recall>reflect JSON).
type Result struct {
	SessionID   string         `json:"session_id"`
	Project     string         `json:"project"`
	TokenBudget int            `json:"token_budget"`
	TokensUsed  int            `json:"tokens_used"`
	Memories    []store.Scored `json:"memories"`
	GitRefs     []GitRef       `json:"git_refs"`
	Beads       []BeadRef      `json:"beads"`
}

// isHardConstraint reports whether a memory is pinned:
// subject beginning with "!" cannot be trimmed.
func isHardConstraint(m store.Scored) bool {
	return len(m.Subject) > 0 && m.Subject[0] == '!'
}

// measureFunc returns the approximate token count for the current
// result state.
type measureFunc func(*Result) int

// charsToTokens returns ceil(chars / 4) — the spec's approximation.
func charsToTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

// trimToBudget caps git refs and beads refs to their configured floors,
// then measures the final token usage. Memory trimming is handled by
// Recall() itself (hard cap at RecallLimit=16).
func trimToBudget(r *Result, cfg Config, measure measureFunc) {
	// Cap git refs.
	if len(r.GitRefs) > cfg.GitCap {
		r.GitRefs = r.GitRefs[:cfg.GitCap]
	}

	// Cap beads refs.
	beadsCap := cfg.BeadsResults
	if beadsCap <= 0 {
		beadsCap = 2
	}
	if len(r.Beads) > beadsCap {
		r.Beads = r.Beads[:beadsCap]
	}

	// Measure final token usage for reporting.
	if measure != nil {
		r.TokensUsed = measure(r)
	}
}
