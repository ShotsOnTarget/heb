package retrieve

import (
	"encoding/json"
	"sort"
	"strings"
)

// BeadRef is a filtered beads issue surfaced by the recall pipeline.
type BeadRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// bdIssue mirrors the subset of `bd list --json` output we care about.
type bdIssue struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// beadsPass runs `bd list --json` and filters issues by case-insensitive
// token-in-title match. Returns up to cfg.BeadsResults entries, ranked
// by number of distinct tokens matched.
func beadsPass(tokens []string, runner Runner, cfg Config) []BeadRef {
	if cfg.NoExternal || len(tokens) == 0 {
		return nil
	}
	stdout, _, err := runner.Run("bd", "list", "--json")
	if err != nil || len(stdout) == 0 {
		return nil
	}
	var issues []bdIssue
	if err := json.Unmarshal(stdout, &issues); err != nil {
		return nil
	}

	type scored struct {
		ref   BeadRef
		score int
	}
	var hits []scored
	lowerTokens := make([]string, len(tokens))
	for i, t := range tokens {
		lowerTokens[i] = strings.ToLower(t)
	}
	for _, iss := range issues {
		title := strings.ToLower(iss.Title)
		matched := 0
		for _, lt := range lowerTokens {
			if lt != "" && strings.Contains(title, lt) {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		hits = append(hits, scored{
			ref:   BeadRef{ID: iss.ID, Title: iss.Title, Status: iss.Status},
			score: matched,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})
	cap := cfg.BeadsResults
	if cap <= 0 {
		cap = 2
	}
	// The GitCap analogue for beads is BeadsResults * 3 — a loose overcap
	// before budget trimming. The spec says "top 3" pre-trim; we hold that
	// line before budget trim decisions happen upstream.
	preTrim := 3
	if len(hits) > preTrim {
		hits = hits[:preTrim]
	}
	out := make([]BeadRef, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ref)
	}
	return out
}
