package consolidate

import (
	"fmt"
	"strings"
)

// buildMemoryDeltas translates lessons → memoryDelta entries.
//
// Per §3.2: for each lesson with confidence >= MinConfidence, split the
// observation on "·" into subject·predicate·object, compute delta_new
// and delta_reinforce from confidence × gain constants, and emit a
// session_reinforced event. Universal candidates get a "[universal_candidate]"
// reason prefix.
//
// Lessons are skipped (added to skipped[]) if:
//   - confidence < MinConfidence
//   - observation does not split into exactly 3 non-empty parts
//
// Returns (deltas, skipped).
func buildMemoryDeltas(c LearnResult, cfg Config) ([]MemoryDelta, []SkippedTuple) {
	var deltas []MemoryDelta
	var skipped []SkippedTuple

	for _, lesson := range c.Lessons {
		if lesson.Confidence < cfg.MinConfidence {
			skipped = append(skipped, SkippedTuple{
				Tuple:  lesson.Observation,
				Reason: fmt.Sprintf("below confidence %.2f < %.2f", lesson.Confidence, cfg.MinConfidence),
			})
			continue
		}
		s, p, o, ok := splitTuple(lesson.Observation)
		if !ok {
			skipped = append(skipped, SkippedTuple{
				Tuple:  lesson.Observation,
				Reason: "malformed tuple",
			})
			continue
		}

		reason := fmt.Sprintf("lesson confidence %.2f", lesson.Confidence)
		if lesson.Scope == "universal_candidate" {
			reason = "[universal_candidate] " + reason
		}

		deltas = append(deltas, MemoryDelta{
			Subject:        s,
			Predicate:      p,
			Object:         o,
			Event:          "session_reinforced",
			DeltaNew:       lesson.Confidence * cfg.NewGain,
			DeltaReinforce: lesson.Confidence * cfg.ReinforceGain,
			Reason:         reason,
		})
	}

	return deltas, skipped
}

// splitTuple splits "subject·predicate·object" into its three parts.
// Returns ok == false if there are not exactly 3 non-empty parts.
// Whitespace around each part is trimmed.
func splitTuple(observation string) (string, string, string, bool) {
	parts := strings.Split(observation, "\u00b7")
	if len(parts) != 3 {
		return "", "", "", false
	}
	s := strings.TrimSpace(parts[0])
	p := strings.TrimSpace(parts[1])
	o := strings.TrimSpace(parts[2])
	if s == "" || p == "" || o == "" {
		return "", "", "", false
	}
	return s, p, o, true
}
