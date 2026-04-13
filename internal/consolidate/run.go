package consolidate

import "encoding/json"

// Run is the pure entry point: take a parsed contract:learn>consolidate plus a Config
// and produce a Result. No filesystem, no sqlite. The caller (cmd/heb)
// is responsible for applying Result.Payload to the store inside its
// own transaction and filling in the post-apply fields (Applied,
// EdgesUpdated, EntanglementSignals, EpisodeWritten, EpisodePath).
//
// Pipeline:
//  1. threshold  — gate memory/edge passes on significance
//  2. memories   — lessons → memoryDelta (session_reinforced)
//  3. edges      — pair enumeration over the written set
//  4. entanglement — surprise_touches → memoryDelta (entanglement_signal)
//  5. episode    — always included, carries the full contract:learn>consolidate verbatim
func Run(c LearnResult, cfg Config) Result {
	met, reason := checkThreshold(c)

	result := Result{
		SessionID:       c.SessionID,
		Project:         c.Project,
		ThresholdMet:    met,
		ThresholdReason: reason,
		Payload: Payload{
			SessionID: c.SessionID,
			Project:   c.Project,
			BeadID:    c.BeadID,
		},
		Applied: []MemoryApply{},
		Skipped: []SkippedTuple{},
		Errors:  []string{},
	}

	if met {
		written, skipped := buildMemoryDeltas(c, cfg)
		edges := buildEdgeDeltas(written, cfg)
		ent := buildEntanglementDeltas(written, c, cfg)

		// Reinforcement deltas first, then entanglement deltas —
		// §3.4 is explicit that these are appended, not merged.
		memories := make([]MemoryDelta, 0, len(written)+len(ent))
		memories = append(memories, written...)
		memories = append(memories, ent...)

		result.Payload.Memories = memories
		result.Payload.Edges = edges
		result.Payload.Skipped = skipped
		result.Skipped = skipped
	}

	// Episode is always written, even for threshold-failed sessions.
	if len(c.Raw) > 0 {
		result.Payload.Episode = &EpisodePayload{
			SessionID: c.SessionID,
			Payload:   c.Raw,
		}
	} else {
		// Fallback: marshal the LearnResult struct itself if Raw was
		// not populated (e.g. in unit tests).
		if raw, err := json.Marshal(c); err == nil {
			result.Payload.Episode = &EpisodePayload{
				SessionID: c.SessionID,
				Payload:   raw,
			}
		}
	}

	return result
}
