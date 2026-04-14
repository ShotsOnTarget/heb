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
	// Each body has ~40 tokens — 3 of them = 120+, so budget of 120 fits at most 3
	longBody := "the blast delay function implements the blast firing rate which scales proportionally with the ship attack value so higher attack means faster blasts and lower attack means slower rate of fire for all weapons in the game"
	c := LearnResult{
		Lessons: []Lesson{
			{Body: longBody, Confidence: 0.90},
			{Body: longBody + " and even more tokens are added here to push over", Confidence: 0.85},
			{Body: longBody + " third version with extra words to ensure budget overflow occurs", Confidence: 0.80},
			{Body: longBody + " fourth version absolutely must be rejected by energy budget", Confidence: 0.75},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas)+len(skipped) != 4 {
		t.Errorf("total = %d, want 4", len(deltas)+len(skipped))
	}
	if len(skipped) == 0 {
		t.Errorf("expected some atoms to be skipped due to energy budget")
	}
	// Verify skipped reason mentions energy budget
	for _, s := range skipped {
		if !contains(s.Reason, "energy budget") {
			t.Errorf("skipped reason = %q, want energy budget", s.Reason)
		}
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
