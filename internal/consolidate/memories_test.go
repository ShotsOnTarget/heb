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
	c := Contract4{
		Lessons: []Lesson{
			{Observation: "drone_stats·derived_by·type_lookup", Scope: "project", Confidence: 0.80},
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
	if d.Subject != "drone_stats" || d.Predicate != "derived_by" || d.Object != "type_lookup" {
		t.Errorf("split wrong: %+v", d)
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
	if d.Reason != "lesson confidence 0.80" {
		t.Errorf("reason = %q", d.Reason)
	}
}

func TestBuildMemoryDeltasBelowConfidence(t *testing.T) {
	cfg := DefaultConfig()
	c := Contract4{
		Lessons: []Lesson{
			{Observation: "a·b·c", Confidence: 0.49}, // just below
			{Observation: "d·e·f", Confidence: 0.50}, // at threshold — kept
			{Observation: "g·h·i", Confidence: 0.51}, // above
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas) != 2 {
		t.Errorf("deltas = %d, want 2 (0.50 and 0.51)", len(deltas))
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %d, want 1", len(skipped))
	}
	if skipped[0].Tuple != "a·b·c" {
		t.Errorf("skipped tuple = %q", skipped[0].Tuple)
	}
	if !contains(skipped[0].Reason, "below confidence") {
		t.Errorf("skipped reason = %q", skipped[0].Reason)
	}
}

func TestBuildMemoryDeltasMalformed(t *testing.T) {
	cfg := DefaultConfig()
	c := Contract4{
		Lessons: []Lesson{
			{Observation: "only·two", Confidence: 0.9},
			{Observation: "a·b·c·d", Confidence: 0.9},
			{Observation: "a··c", Confidence: 0.9}, // empty middle
			{Observation: "valid·tuple·here", Confidence: 0.9},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas) != 1 {
		t.Errorf("deltas = %d, want 1", len(deltas))
	}
	if len(skipped) != 3 {
		t.Errorf("skipped = %d, want 3", len(skipped))
	}
	for _, s := range skipped {
		if !contains(s.Reason, "malformed") {
			t.Errorf("skipped reason = %q, want 'malformed'", s.Reason)
		}
	}
}

func TestBuildMemoryDeltasUniversalPrefix(t *testing.T) {
	cfg := DefaultConfig()
	c := Contract4{
		Lessons: []Lesson{
			{Observation: "gd·prefer·static_vars", Scope: "universal_candidate", Confidence: 0.85},
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
