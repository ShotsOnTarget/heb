package retrieve

import (
	"encoding/json"
	"fmt"
	"strings"

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
	fmt.Fprintf(&b, "budget used: %d / %d tokens\n\n", r.TokensUsed, r.TokenBudget)

	fmt.Fprintf(&b, "MEMORIES (%d entries)\n", len(r.Memories))
	if len(r.Memories) == 0 {
		b.WriteString("  no matches\n")
	}
	for _, m := range r.Memories {
		tag := "match"
		if m.Source == "edge" {
			tag = "edge "
		}
		fmt.Fprintf(&b, "  [%s %.2f] %s·+%.2f\n", tag, m.Score, m.TupleString(), m.Weight)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "GIT (%d commits)\n", len(r.GitRefs))
	if len(r.GitRefs) == 0 {
		b.WriteString("  no matches\n")
	}
	for _, g := range r.GitRefs {
		fmt.Fprintf(&b, "  %s  %s\n", g.Hash, g.Message)
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
type RecallMemory struct {
	Tuple     string  `json:"tuple"`
	Weight    float64 `json:"weight"`
	Source    string  `json:"source"`
	Relevance float64 `json:"relevance"`
}

// RecallResult is the contract:recall>reflect JSON output shape.
type RecallResult struct {
	SessionID   string            `json:"session_id"`
	Project     string            `json:"project"`
	TokenBudget int               `json:"token_budget"`
	TokensUsed  int               `json:"tokens_used"`
	Memories    []RecallMemory `json:"memories"`
	GitRefs     []GitRef          `json:"git_refs"`
	Beads       []BeadRef         `json:"beads"`
}

// RenderJSON renders Block 2 — the contract:recall>reflect JSON, indented.
func RenderJSON(r *Result) string {
	out := RecallResult{
		SessionID:   r.SessionID,
		Project:     r.Project,
		TokenBudget: r.TokenBudget,
		TokensUsed:  r.TokensUsed,
		Memories:    make([]RecallMemory, 0, len(r.Memories)),
		GitRefs:     r.GitRefs,
		Beads:       r.Beads,
	}
	if out.GitRefs == nil {
		out.GitRefs = []GitRef{}
	}
	if out.Beads == nil {
		out.Beads = []BeadRef{}
	}
	for _, m := range r.Memories {
		out.Memories = append(out.Memories, RecallMemory{
			Tuple:     m.TupleString(),
			Weight:    m.Weight,
			Source:    m.Source,
			Relevance: m.Score,
		})
	}
	buf, _ := json.MarshalIndent(out, "", "  ")
	return string(buf) + "\n"
}

// measureRender is a measureFunc that returns the token count of the
// human-rendered block. Used by Run() to wire budget.go to format.go
// without a circular import.
func measureRender(r *Result) int {
	return charsToTokens(len(RenderHuman(r)))
}

// Ensure store.Scored is referenced so the import isn't ever dropped
// accidentally by automated tooling — TupleString lives on Memory.
var _ = store.Scored{}
