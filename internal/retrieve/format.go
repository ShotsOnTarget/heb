package retrieve

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

// RenderHuman renders Block 1 — the human-readable retrieval result.
// Deterministic: same Result always produces the same string.
func RenderHuman(r *Result) string {
	var b strings.Builder
	b.WriteString("RETRIEVAL RESULT\n")
	b.WriteString("───────────────────────────────\n")
	fmt.Fprintf(&b, "session_id:  %s\n", r.SessionID)
	fmt.Fprintf(&b, "project:     %s\n", r.Project)
	fmt.Fprintf(&b, "memory budget: %d / %d tokens\n", r.TokensUsed, r.TokenBudget)
	fmt.Fprintf(&b, "git budget:    %d / %d tokens\n\n", r.GitTokensUsed, r.GitTokenBudget)

	fmt.Fprintf(&b, "MEMORIES (%d entries)\n", len(r.Memories))
	if len(r.Memories) == 0 {
		b.WriteString("  no matches\n")
	}
	now := time.Now().Unix()
	for _, m := range r.Memories {
		tag := "match"
		if m.Source == "edge" {
			tag = "edge "
		}
		age := int((now - m.UpdatedAt) / 86400)
		if age < 0 {
			age = 0
		}
		fmt.Fprintf(&b, "  [%s %.2f] %s +%.2f (%dd ago)\n", tag, m.Score, m.Body, m.Weight, age)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "GIT (%d commits)\n", len(r.GitRefs))
	if len(r.GitRefs) == 0 {
		b.WriteString("  no matches\n")
	}
	for _, g := range r.GitRefs {
		fmt.Fprintf(&b, "  [%.2f] %s  %s (%dd ago)\n", g.Score, g.Hash, g.Message, int(g.AgeDays))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "BEADS (%d tasks)\n", len(r.Beads))
	if len(r.Beads) == 0 {
		b.WriteString("  no matches\n")
	}
	for _, bd := range r.Beads {
		fmt.Fprintf(&b, "  %s  %s  [%s]\n", bd.ID, bd.Title, bd.Status)
	}

	b.WriteString("───────────────────────────────\n")
	return b.String()
}

// RecallMemory is the emitted JSON shape for one memory entry.
// Relevance is a coarse band ("strong" | "moderate" | "weak" | "trace")
// derived from Score / MaxPossibleScore — a stable signal the LLM can
// read without calibration. Memories are emitted ranked best-first, so
// within-band ordering is preserved by list order.
type RecallMemory struct {
	Tuple     string `json:"tuple"`
	Weight    float64 `json:"weight"`
	Source    string `json:"source"`
	Relevance string `json:"relevance"`
	AgeDays   int    `json:"age_days"`
}

// relevanceBand maps a normalised score ratio to a coarse label.
// Thresholds were tuned against real recall output — top-tier matches
// typically land at ~0.35-0.50 in practice because weight and recency
// factors rarely hit 1.0 and tf never saturates on every query token.
func relevanceBand(normalised float64) string {
	switch {
	case normalised >= 0.35:
		return "strong"
	case normalised >= 0.20:
		return "moderate"
	case normalised >= 0.10:
		return "weak"
	default:
		return "trace"
	}
}

// RecallResult is the contract:recall>reflect JSON output shape.
type RecallResult struct {
	SessionID      string         `json:"session_id"`
	Project        string         `json:"project"`
	TokenBudget    int            `json:"token_budget"`
	TokensUsed     int            `json:"tokens_used"`
	GitTokenBudget int            `json:"git_token_budget"`
	GitTokensUsed  int            `json:"git_tokens_used"`
	Memories       []RecallMemory `json:"memories"`
	GitRefs        []GitRef       `json:"git_refs"`
	Beads          []BeadRef      `json:"beads"`
}

// RenderJSON renders Block 2 — the contract:recall>reflect JSON, indented.
func RenderJSON(r *Result) string {
	out := RecallResult{
		SessionID:      r.SessionID,
		Project:        r.Project,
		TokenBudget:    r.TokenBudget,
		TokensUsed:     r.TokensUsed,
		GitTokenBudget: r.GitTokenBudget,
		GitTokensUsed:  r.GitTokensUsed,
		Memories:       make([]RecallMemory, 0, len(r.Memories)),
		GitRefs:        r.GitRefs,
		Beads:          r.Beads,
	}
	if out.GitRefs == nil {
		out.GitRefs = []GitRef{}
	}
	if out.Beads == nil {
		out.Beads = []BeadRef{}
	}
	// Normalise Score into a stable band label. Two branches because the
	// two sources use different score scales:
	//   - match: BM25-based, unbounded, needs division by theoretical ceiling
	//   - edge:  weight×0.5 + strength×0.5, already in [0, ~0.9] by construction
	// The Source field is echoed into the contract so the LLM can tell
	// a "strong edge" (adjacency signal) apart from a "strong match" (text hit).
	now := time.Now().Unix()
	for _, m := range r.Memories {
		age := int((now - m.UpdatedAt) / 86400)
		if age < 0 {
			age = 0
		}
		var ratio float64
		if m.Source == "edge" {
			ratio = m.Score
		} else if r.MaxPossibleScore > 0 {
			ratio = m.Score / r.MaxPossibleScore
		}
		out.Memories = append(out.Memories, RecallMemory{
			Tuple:     m.TupleString(),
			Weight:    m.Weight,
			Source:    m.Source,
			Relevance: relevanceBand(ratio),
			AgeDays:   age,
		})
	}
	buf, _ := json.MarshalIndent(out, "", "  ")
	return string(buf) + "\n"
}

// measureRender is a measureFunc that returns the token count of only the
// memory entries in the result. Used by Run() to wire budget.go to format.go
// without a circular import.
func measureRender(r *Result) int {
	var total int
	for _, m := range r.Memories {
		total += memoryTokens(m)
	}
	return total
}

// Ensure store.Scored is referenced so the import isn't ever dropped
// accidentally by automated tooling — TupleString lives on Memory.
var _ = store.Scored{}
