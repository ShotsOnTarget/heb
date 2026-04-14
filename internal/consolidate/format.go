package consolidate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderHuman produces Block 1 — the CONSOLIDATE display block.
// The post-apply fields on r (Applied, EdgesUpdated, EntanglementSignals,
// EpisodeWritten, EpisodePath) drive the counts, so this must be called
// after cmd/heb has applied the payload to the store.
func RenderHuman(r Result) string {
	var b strings.Builder

	b.WriteString("CONSOLIDATE\n")
	b.WriteString("───────────────────────────────\n")
	fmt.Fprintf(&b, "session:   %s\n", r.SessionID)
	fmt.Fprintf(&b, "project:   %s\n", r.Project)
	if r.ThresholdMet {
		fmt.Fprintf(&b, "threshold: met — %s\n", r.ThresholdReason)
	} else {
		fmt.Fprintf(&b, "threshold: not met — %s\n", r.ThresholdReason)
	}
	b.WriteString("\n")

	// Memory counts: split Applied by wasNew / event kind.
	var added, reinforced, entCount int
	for _, a := range r.Applied {
		switch {
		case a.Event == "entanglement_signal":
			entCount++
		case a.WasNew:
			added++
		default:
			reinforced++
		}
	}
	fmt.Fprintf(&b, "MEMORIES (%d total)\n", len(r.Applied))
	fmt.Fprintf(&b, "  added:      %d new entries\n", added)
	fmt.Fprintf(&b, "  reinforced: %d updated\n", reinforced)
	fmt.Fprintf(&b, "  skipped:    %d (below confidence or energy budget)\n", len(r.Skipped))
	b.WriteString("\n")

	b.WriteString("EDGES\n")
	fmt.Fprintf(&b, "  sent:       %d deltas\n", len(r.Payload.Edges))
	fmt.Fprintf(&b, "  updated:    %d\n", r.EdgesUpdated)
	fmt.Fprintf(&b, "  decayed:    %d\n", r.EdgesDecayed)
	b.WriteString("\n")

	b.WriteString("ENTANGLEMENT\n")
	if entCount == 0 && r.EntanglementSignals == 0 {
		b.WriteString("  —\n")
	} else {
		fmt.Fprintf(&b, "  signals:    %d weight reductions applied\n", r.EntanglementSignals)
	}
	b.WriteString("\n")

	b.WriteString("EPISODE\n")
	if r.EpisodeWritten {
		if r.EpisodePath != "" {
			fmt.Fprintf(&b, "  written:    %s\n", r.EpisodePath)
		} else {
			fmt.Fprintf(&b, "  written:    episode %s\n", r.SessionID)
		}
	} else {
		b.WriteString("  skipped:    already present\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "ERRORS (%d)\n", len(r.Errors))
	if len(r.Errors) == 0 {
		b.WriteString("  —\n")
	} else {
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	b.WriteString("───────────────────────────────\n")

	return b.String()
}

// RenderJSON produces Block 2 — the contract:consolidate>done result JSON. Marshals
// Result as-is (with indentation).
func RenderJSON(r Result) string {
	if r.Applied == nil {
		r.Applied = []MemoryApply{}
	}
	if r.Skipped == nil {
		r.Skipped = []SkippedTuple{}
	}
	if r.Errors == nil {
		r.Errors = []string{}
	}
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"errors":["render: %s"]}`, err.Error())
	}
	return string(out)
}

// StderrSummary produces a multi-line summary written to stderr that
// includes counts AND the actual memory tuples that were written.
func StderrSummary(r Result) string {
	var b strings.Builder
	var newMems, reinforcedMems []MemoryApply
	var entCount int
	for _, a := range r.Applied {
		switch {
		case a.Event == "entanglement_signal":
			entCount++
		case a.WasNew:
			newMems = append(newMems, a)
		default:
			reinforcedMems = append(reinforcedMems, a)
		}
	}

	fmt.Fprintf(&b, "consolidate: %d new, %d reinforced, %d edges (+%d decayed), %d entanglement, episode=%v",
		len(newMems), len(reinforcedMems), r.EdgesUpdated, r.EdgesDecayed, entCount, r.EpisodeWritten)

	if len(newMems) > 0 {
		b.WriteString("\n  learned:")
		for _, m := range newMems {
			fmt.Fprintf(&b, "\n    + %s (%.2f)", m.Body, m.NewWeight)
		}
	}
	if len(reinforcedMems) > 0 {
		b.WriteString("\n  reinforced:")
		for _, m := range reinforcedMems {
			fmt.Fprintf(&b, "\n    \u2191 %s (%.2f)", m.Body, m.NewWeight)
		}
	}
	if len(r.Skipped) > 0 {
		b.WriteString("\n  skipped:")
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "\n    \u2013 %s (%s)", s.Tuple, s.Reason)
		}
	}

	return b.String()
}
