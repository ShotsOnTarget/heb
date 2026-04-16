package retrieve

import (
	"fmt"

	"github.com/steelboltgames/heb/internal/store"
)

// Result is the full retrieve output: the data backing both Block 1
// (human-readable) and Block 2 (contract:recall>reflect JSON).
type Result struct {
	SessionID      string         `json:"session_id"`
	Project        string         `json:"project"`
	TokenBudget    int            `json:"token_budget"`
	TokensUsed     int            `json:"tokens_used"`
	GitTokenBudget int            `json:"git_token_budget"`
	GitTokensUsed  int            `json:"git_tokens_used"`
	Memories       []store.Scored `json:"memories"`
	GitRefs        []GitRef       `json:"git_refs"`
	Beads          []BeadRef      `json:"beads"`
}

// isHardConstraint reports whether a memory is pinned:
// body beginning with "!" cannot be trimmed.
func isHardConstraint(m store.Scored) bool {
	return len(m.Body) > 0 && m.Body[0] == '!'
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

// gitRefTokens returns the approximate token cost of a single git ref
// as it would appear in the execute prompt.
func gitRefTokens(ref GitRef) int {
	// Format: "- abc1234 feat: commit message (score: 0.85, 3d ago)\n"
	line := fmt.Sprintf("- %s %s (score: %.2f, %dd ago)\n", ref.Hash, ref.Message, ref.Score, int(ref.AgeDays))
	return charsToTokens(len(line))
}

// trimToBudget caps git refs to their token budget via metabolic cost,
// caps beads refs, then measures final token usage.
// Memory trimming is handled by Recall() itself (hard cap at RecallLimit=16).
func trimToBudget(r *Result, cfg Config, measure measureFunc) {
	// Trim git refs to their own token budget.
	gitBudget := cfg.GitTokenBudget
	if gitBudget <= 0 {
		gitBudget = 150
	}
	var gitUsed int
	gitCut := len(r.GitRefs)
	for i, ref := range r.GitRefs {
		cost := gitRefTokens(ref)
		if gitUsed+cost > gitBudget {
			gitCut = i
			break
		}
		gitUsed += cost
	}
	// Hard ceiling still applies after budget trim.
	if gitCut > cfg.GitCap {
		gitCut = cfg.GitCap
	}
	r.GitRefs = r.GitRefs[:gitCut]
	r.GitTokenBudget = gitBudget
	r.GitTokensUsed = gitUsed

	// Cap beads refs.
	beadsCap := cfg.BeadsResults
	if beadsCap <= 0 {
		beadsCap = 2
	}
	if len(r.Beads) > beadsCap {
		r.Beads = r.Beads[:beadsCap]
	}

	// Measure final memory token usage for reporting.
	if measure != nil {
		r.TokensUsed = measure(r)
	}
}
