package retrieve

import (
	"strings"

	"github.com/steelboltgames/heb/internal/store"
)

// Result is the full retrieve output: the data backing both Block 1
// (human-readable) and Block 2 (Contract 3 JSON). Trimming mutates
// this struct in place.
type Result struct {
	SessionID   string         `json:"session_id"`
	Project     string         `json:"project"`
	TokenBudget int            `json:"token_budget"`
	TokensUsed  int            `json:"tokens_used"`
	Memories    []store.Scored `json:"memories"`
	GitRefs     []GitRef       `json:"git_refs"`
	Beads       []BeadRef      `json:"beads"`
}

// measureFunc returns the approximate token count for the current
// result state. Injected so budget.go doesn't have to depend on
// format.go directly — format.go calls trimToBudget with RenderHuman.
type measureFunc func(*Result) int

// charsToTokens returns ceil(chars / 4) — the spec's approximation.
func charsToTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

// isHardConstraint reports whether a memory is pinned (§5 priority 1):
// subject beginning with "!" cannot be trimmed.
func isHardConstraint(m store.Scored) bool {
	return strings.HasPrefix(m.Subject, "!")
}

// trimToBudget implements the Part B §5 trim algorithm.
//
// Priority order (trim from bottom of the list up):
//
//	1  hard constraints (subject starts "!")        NEVER trim
//	2  match memories score >= 1.0                  trim last (hard floor)
//	3  match memories score < 1.0                   trim lowest first
//	4  edge memories                                trim lowest first
//	5  git refs                                     trim to most recent 3 floor
//	6  beads refs                                   trim to top 2 floor
//
// Processing: start at priority 6, drop one entry, remeasure, repeat
// until under budget or priority is exhausted. Advance upward. Priority
// 1 is never touched. Hard floor: if priority 2 is exhausted and still
// over budget, stop and emit what remains.
func trimToBudget(r *Result, cfg Config, measure measureFunc) {
	if measure == nil {
		return
	}
	budget := cfg.TokenBudget
	if budget <= 0 {
		budget = 300
	}

	// Floors for git/beads come from cfg.
	gitFloor := cfg.GitResults
	if gitFloor < 0 {
		gitFloor = 0
	}
	beadsFloor := cfg.BeadsResults
	if beadsFloor < 0 {
		beadsFloor = 0
	}

	remeasure := func() bool {
		r.TokensUsed = measure(r)
		return r.TokensUsed <= budget
	}

	if remeasure() {
		return
	}

	// Priority 6 — beads (trim from bottom until floor reached).
	for len(r.Beads) > beadsFloor {
		r.Beads = r.Beads[:len(r.Beads)-1]
		if remeasure() {
			return
		}
	}

	// Priority 5 — git (trim from bottom until floor reached).
	for len(r.GitRefs) > gitFloor {
		r.GitRefs = r.GitRefs[:len(r.GitRefs)-1]
		if remeasure() {
			return
		}
	}

	// Priority 4 — edge memories, lowest score first.
	for dropLowestMemory(r, func(m store.Scored) bool {
		return !isHardConstraint(m) && m.Source == "edge"
	}) {
		if remeasure() {
			return
		}
	}

	// Priority 3 — match memories with score < 1.0, lowest first.
	for dropLowestMemory(r, func(m store.Scored) bool {
		return !isHardConstraint(m) && m.Source != "edge" && m.Score < 1.0
	}) {
		if remeasure() {
			return
		}
	}

	// Priority 2 — match memories with score >= 1.0, lowest first.
	for dropLowestMemory(r, func(m store.Scored) bool {
		return !isHardConstraint(m) && m.Source != "edge" && m.Score >= 1.0
	}) {
		if remeasure() {
			return
		}
	}

	// Hard floor: still over budget, but only hard constraints remain.
	// Stop trimming and emit what remains. Budget is a guideline.
	r.TokensUsed = measure(r)
}

// dropLowestMemory removes one memory matching the predicate with the
// lowest score. Returns true if a memory was dropped, false if none
// matched.
func dropLowestMemory(r *Result, match func(store.Scored) bool) bool {
	idx := -1
	for i, m := range r.Memories {
		if !match(m) {
			continue
		}
		if idx < 0 || r.Memories[i].Score < r.Memories[idx].Score {
			idx = i
		}
	}
	if idx < 0 {
		return false
	}
	r.Memories = append(r.Memories[:idx], r.Memories[idx+1:]...)
	return true
}
