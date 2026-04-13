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

	// Prediction contradiction lessons: when a prediction was wrong, the
	// learn contract records a correction lesson in the reconciliation
	// element. These are directly observed facts (predicted X, actual Y)
	// so they get high confidence (0.85) and a "prediction" source marker.
	if c.PredictionReconciliation != nil && !c.PredictionReconciliation.ColdStart {
		for _, elem := range c.PredictionReconciliation.Elements {
			if elem.Event != "prediction_contradicted" || elem.Lesson == "" {
				continue
			}
			s, p, o, ok := splitTuple(elem.Lesson)
			if !ok {
				skipped = append(skipped, SkippedTuple{
					Tuple:  elem.Lesson,
					Reason: "malformed prediction correction tuple",
				})
				continue
			}
			const predictionConfidence = 0.85
			deltas = append(deltas, MemoryDelta{
				Subject:        s,
				Predicate:      p,
				Object:         o,
				Event:          "session_reinforced",
				DeltaNew:       predictionConfidence * cfg.NewGain,
				DeltaReinforce: predictionConfidence * cfg.ReinforceGain,
				Reason:         fmt.Sprintf("prediction contradiction: %s was %s", elem.Element, elem.Result),
			})
		}
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
