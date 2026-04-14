package consolidate

import (
	"fmt"
	"sort"

	"github.com/steelboltgames/heb/internal/memory"
)

// buildMemoryDeltas translates lessons → memoryDelta entries, enforcing the
// session energy budget. Lessons are accepted in confidence-descending order
// until the token budget is exhausted.
//
// Lessons are skipped if:
//   - confidence < MinConfidence
//   - body is empty
//   - session energy budget is exhausted
func buildMemoryDeltas(c LearnResult, cfg Config) ([]MemoryDelta, []SkippedTuple) {
	type cand struct {
		body       string
		confidence float64
		reason     string
		tokens     int
	}
	var candidates []cand
	var skipped []SkippedTuple

	// Collect regular lessons
	for _, lesson := range c.Lessons {
		if lesson.Confidence < cfg.MinConfidence {
			continue
		}
		if lesson.Body == "" {
			continue
		}
		reason := fmt.Sprintf("lesson confidence %.2f", lesson.Confidence)
		if lesson.Scope == "universal_candidate" {
			reason = "[universal_candidate] " + reason
		}
		candidates = append(candidates, cand{
			body:       lesson.Body,
			confidence: lesson.Confidence,
			reason:     reason,
			tokens:     memory.TokenCount(lesson.Body),
		})
	}

	// Collect prediction contradiction lessons
	if c.PredictionReconciliation != nil && !c.PredictionReconciliation.ColdStart {
		for _, elem := range c.PredictionReconciliation.Elements {
			if elem.Event != "prediction_contradicted" || elem.Lesson == "" {
				continue
			}
			const predictionConfidence = 0.85
			candidates = append(candidates, cand{
				body:       elem.Lesson,
				confidence: predictionConfidence,
				reason:     fmt.Sprintf("prediction contradiction: %s was %s", elem.Element, elem.Result),
				tokens:     memory.TokenCount(elem.Lesson),
			})
		}
	}

	// Apply verbosity penalty: verbose atoms compete worse mechanically.
	for i := range candidates {
		candidates[i].confidence *= memory.VerbosityCost(candidates[i].tokens)
	}

	// Sort by (penalised) confidence descending for energy budget allocation
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].confidence > candidates[j].confidence
	})

	// Apply energy budget
	var deltas []MemoryDelta
	budgetUsed := 0

	for _, c := range candidates {
		if budgetUsed+c.tokens > memory.SessionEnergyBudget {
			skipped = append(skipped, SkippedTuple{
				Tuple:  c.body,
				Reason: fmt.Sprintf("exceeds session energy budget (%d+%d > %d)", budgetUsed, c.tokens, memory.SessionEnergyBudget),
			})
			continue
		}
		budgetUsed += c.tokens
		deltas = append(deltas, MemoryDelta{
			Body:           c.body,
			Event:          "session_reinforced",
			DeltaNew:       c.confidence * cfg.NewGain,
			DeltaReinforce: c.confidence * cfg.ReinforceGain,
			Reason:         c.reason,
		})
	}

	// Also skip low-confidence lessons (these were filtered before budget)
	for _, lesson := range c.Lessons {
		if lesson.Confidence < cfg.MinConfidence && lesson.Body != "" {
			skipped = append(skipped, SkippedTuple{
				Tuple:  lesson.Body,
				Reason: fmt.Sprintf("below confidence %.2f < %.2f", lesson.Confidence, cfg.MinConfidence),
			})
		}
	}

	return deltas, skipped
}
