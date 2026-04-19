package retrieve

import (
	"fmt"

	"github.com/steelboltgames/heb/internal/store"
)

// Result is the full retrieve output: the data backing both Block 1
// (human-readable) and Block 2 (contract:recall>reflect JSON).
type Result struct {
	SessionID        string         `json:"session_id"`
	Project          string         `json:"project"`
	TokenBudget      int            `json:"token_budget"`
	TokensUsed       int            `json:"tokens_used"`
	GitTokenBudget   int            `json:"git_token_budget"`
	GitTokensUsed    int            `json:"git_tokens_used"`
	Memories         []store.Scored `json:"memories"`
	MaxPossibleScore float64        `json:"-"` // set by caller; used to normalise Score → band label
	GitRefs          []GitRef       `json:"git_refs"`
	Beads            []BeadRef      `json:"beads"`
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

// memoryTokens returns the approximate token cost of a single memory
// as it would appear in the human-rendered block.
func memoryTokens(m store.Scored) int {
	// Format: "  [match 0.85] body text +1.50 (3d ago)\n"
	line := fmt.Sprintf("  [match %.2f] %s +%.2f (0d ago)\n", m.Score, m.Body, m.Weight)
	return charsToTokens(len(line))
}

// trimToBudget caps memories, git refs, and beads to their respective
// token budgets, then measures final token usage.
func trimToBudget(r *Result, cfg Config, measure measureFunc) {
	// Trim memories to their token budget, dropping lowest-scored first.
	// Hard constraints (body starting with "!") are never dropped.
	memBudget := cfg.TokenBudget
	if memBudget <= 0 {
		memBudget = 300
	}
	var memUsed int
	kept := make([]store.Scored, 0, len(r.Memories))
	// Memories arrive sorted by score descending from Recall().
	// Walk forward, accumulating cost; drop once over budget (unless pinned).
	for _, m := range r.Memories {
		cost := memoryTokens(m)
		if isHardConstraint(m) {
			kept = append(kept, m)
			memUsed += cost
			continue
		}
		if memUsed+cost > memBudget {
			continue // drop — over budget
		}
		kept = append(kept, m)
		memUsed += cost
	}
	r.Memories = kept

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
