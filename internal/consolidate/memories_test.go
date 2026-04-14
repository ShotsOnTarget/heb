package consolidate

import (
	"math"
	"testing"
)

const eps = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < eps
}

func TestBuildMemoryDeltasBasic(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "drone_stats derived_by type_lookup", Scope: "project", Confidence: 0.80},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want empty", skipped)
	}
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(deltas))
	}
	d := deltas[0]
	if d.Body != "drone_stats derived_by type_lookup" {
		t.Errorf("body = %q", d.Body)
	}
	if d.Event != "session_reinforced" {
		t.Errorf("event = %q, want session_reinforced", d.Event)
	}
	if !approxEqual(d.DeltaNew, 0.80*0.72) {
		t.Errorf("delta_new = %v, want %v", d.DeltaNew, 0.80*0.72)
	}
	if !approxEqual(d.DeltaReinforce, 0.80*0.08) {
		t.Errorf("delta_reinforce = %v, want %v", d.DeltaReinforce, 0.80*0.08)
	}
}

func TestBuildMemoryDeltasBelowConfidence(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "low confidence atom", Confidence: 0.49},
			{Body: "at threshold atom", Confidence: 0.50},
			{Body: "above threshold atom", Confidence: 0.51},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas) != 2 {
		t.Errorf("deltas = %d, want 2", len(deltas))
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %d, want 1", len(skipped))
	}
	if skipped[0].Tuple != "low confidence atom" {
		t.Errorf("skipped tuple = %q", skipped[0].Tuple)
	}
}

func TestBuildMemoryDeltasEmptyBody(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "", Confidence: 0.9},
			{Body: "valid atom here", Confidence: 0.9},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 1 {
		t.Errorf("deltas = %d, want 1", len(deltas))
	}
}

func TestBuildMemoryDeltasEnergyBudget(t *testing.T) {
	cfg := DefaultConfig()
	// Use 12-token bodies (at AtomTokenCap) so verbosity penalty doesn't interfere.
	// Each body = 12 tokens. 11 atoms × 12 tokens = 132 > 120 budget.
	mkBody := func(prefix string) string {
		return prefix + " blast delay scales with ship attack value extra words here"
	}
	c := LearnResult{
		Lessons: []Lesson{
			{Body: mkBody("alpha"), Confidence: 0.90},
			{Body: mkBody("bravo"), Confidence: 0.89},
			{Body: mkBody("charlie"), Confidence: 0.88},
			{Body: mkBody("delta"), Confidence: 0.87},
			{Body: mkBody("echo"), Confidence: 0.86},
			{Body: mkBody("foxtrot"), Confidence: 0.85},
			{Body: mkBody("golf"), Confidence: 0.84},
			{Body: mkBody("hotel"), Confidence: 0.83},
			{Body: mkBody("india"), Confidence: 0.82},
			{Body: mkBody("juliet"), Confidence: 0.81},
			{Body: mkBody("kilo"), Confidence: 0.80},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas)+len(skipped) != 11 {
		t.Errorf("total = %d, want 11", len(deltas)+len(skipped))
	}
	if len(skipped) == 0 {
		t.Errorf("expected some atoms to be skipped due to energy budget")
	}
	// Verify at least one skipped reason mentions energy budget
	found := false
	for _, s := range skipped {
		if contains(s.Reason, "energy budget") {
			found = true
		}
	}
	if !found {
		t.Errorf("no skipped atom mentions energy budget")
	}
}

func TestBuildMemoryDeltasConfidenceOrdering(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "low confidence first", Confidence: 0.60},
			{Body: "high confidence second", Confidence: 0.95},
			{Body: "medium confidence third", Confidence: 0.80},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 3 {
		t.Fatalf("deltas = %d, want 3", len(deltas))
	}
	// Should be sorted by confidence: 0.95, 0.80, 0.60
	if !approxEqual(deltas[0].DeltaNew, 0.95*cfg.NewGain) {
		t.Errorf("first delta should be confidence 0.95")
	}
	if !approxEqual(deltas[1].DeltaNew, 0.80*cfg.NewGain) {
		t.Errorf("second delta should be confidence 0.80")
	}
}

func TestBuildMemoryDeltasVerbosityPenalty(t *testing.T) {
	cfg := DefaultConfig()
	// Terse atom: 5 tokens, confidence 0.70 → no penalty → effective 0.70
	// Verbose atom: 23 tokens, confidence 0.90 → penalty 12/23≈0.52 → effective 0.47
	// Both accepted, but terse sorts first despite lower raw confidence.
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "CombatScreen game/combat_screen.gd implements CombatScreen as a RefCounted UI/sync layer, with functions for syncing combat state, rendering, and combat phase management", Confidence: 0.90},
			{Body: "RunState owns run-level state", Confidence: 0.70},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 2 {
		t.Fatalf("deltas = %d, want 2", len(deltas))
	}
	// Terse atom should sort first despite lower raw confidence
	if deltas[0].Body != "RunState owns run-level state" {
		t.Errorf("expected terse atom first, got %q", deltas[0].Body)
	}
}

func TestBuildMemoryDeltasPredictionContradiction(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "shake implements screen_shake", Scope: "project", Confidence: 0.90},
		},
		PredictionReconciliation: &PredictionReconciliation{
			ColdStart: false,
			Elements: []PredictionReconcileElement{
				{
					Element: "files",
					Result:  "wrong",
					Event:   "prediction_contradicted",
					Lesson:  "combat screen shake is implemented in main.gd not combat.gd",
				},
				{
					Element: "approach",
					Result:  "matched",
					Event:   "prediction_confirmed",
					Lesson:  "",
				},
			},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want empty", skipped)
	}
	if len(deltas) != 2 {
		t.Fatalf("deltas = %d, want 2", len(deltas))
	}
}

func TestBuildMemoryDeltasPredictionContradictionColdStartSkipped(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		PredictionReconciliation: &PredictionReconciliation{
			ColdStart: true,
			Elements: []PredictionReconcileElement{
				{
					Element: "files",
					Result:  "wrong",
					Event:   "prediction_contradicted",
					Lesson:  "some correction lesson",
				},
			},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 0 {
		t.Errorf("deltas = %d, want 0 (cold start should skip)", len(deltas))
	}
}

func TestBuildMemoryDeltasUniversalPrefix(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "godot prefer static vars for performance", Scope: "universal_candidate", Confidence: 0.85},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(deltas))
	}
	if !contains(deltas[0].Reason, "[universal_candidate]") {
		t.Errorf("reason = %q, want universal_candidate prefix", deltas[0].Reason)
	}
}
