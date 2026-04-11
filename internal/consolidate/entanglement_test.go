package consolidate

import "testing"

func TestEntanglementGatedBySurpriseTouches(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "derived_by", Object: "type_lookup"},
	}
	c := Contract4{
		CorrectionCount: 2,
		PeakIntensity:   0.6,
		Implementation:  Implementation{SurpriseTouches: nil},
	}
	if out := buildEntanglementDeltas(written, c, cfg); out != nil {
		t.Errorf("empty surprise_touches should yield nil, got %v", out)
	}
}

func TestEntanglementGatedByCorrectionCount(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "derived_by", Object: "type_lookup"},
	}
	c := Contract4{
		CorrectionCount: 0,
		PeakIntensity:   0.6,
		Implementation:  Implementation{SurpriseTouches: []string{"game/drone_stats.gd"}},
	}
	if out := buildEntanglementDeltas(written, c, cfg); out != nil {
		t.Errorf("correction_count == 0 should yield nil, got %v", out)
	}
}

func TestEntanglementMatch(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "derived_by", Object: "type_lookup"},
	}
	c := Contract4{
		CorrectionCount: 1,
		PeakIntensity:   0.6,
		Implementation:  Implementation{SurpriseTouches: []string{"game/DRONE_STATS.gd"}},
	}
	out := buildEntanglementDeltas(written, c, cfg)
	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
	if out[0].Event != "entanglement_signal" {
		t.Errorf("event = %q", out[0].Event)
	}
	// signal = -(0.6 * 0.05) = -0.03, in range [-0.08, -0.02]
	if !approxEqual(out[0].DeltaNew, -0.03) {
		t.Errorf("delta_new = %v, want -0.03", out[0].DeltaNew)
	}
	if !approxEqual(out[0].DeltaReinforce, -0.03) {
		t.Errorf("delta_reinforce = %v, want -0.03", out[0].DeltaReinforce)
	}
	if !contains(out[0].Reason, "game/DRONE_STATS.gd") {
		t.Errorf("reason = %q", out[0].Reason)
	}
}

func TestEntanglementNoMatch(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "derived_by", Object: "type_lookup"},
	}
	c := Contract4{
		CorrectionCount: 1,
		PeakIntensity:   0.6,
		Implementation:  Implementation{SurpriseTouches: []string{"game/inventory.gd"}},
	}
	out := buildEntanglementDeltas(written, c, cfg)
	if len(out) != 0 {
		t.Errorf("got %d, want 0", len(out))
	}
}

func TestEntanglementClampMin(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "a", Predicate: "b", Object: "c"},
	}
	c := Contract4{
		CorrectionCount: 1,
		PeakIntensity:   10.0, // signal would be -0.50, clamped to -0.08
		Implementation:  Implementation{SurpriseTouches: []string{"a"}},
	}
	out := buildEntanglementDeltas(written, c, cfg)
	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
	if !approxEqual(out[0].DeltaNew, -0.08) {
		t.Errorf("delta_new = %v, want -0.08 (clamp min)", out[0].DeltaNew)
	}
}

func TestEntanglementClampMax(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "a", Predicate: "b", Object: "c"},
	}
	c := Contract4{
		CorrectionCount: 1,
		PeakIntensity:   0.1, // signal would be -0.005, clamped to -0.02
		Implementation:  Implementation{SurpriseTouches: []string{"a"}},
	}
	out := buildEntanglementDeltas(written, c, cfg)
	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
	if !approxEqual(out[0].DeltaNew, -0.02) {
		t.Errorf("delta_new = %v, want -0.02 (clamp max)", out[0].DeltaNew)
	}
}

// Critical interaction test from the yx0 proposal §6: a lesson reinforces
// a tuple that also matches a surprise_touch in a corrective session.
// The reinforcement and entanglement signals both hit the same tuple,
// emitted as TWO separate deltas (§3.4: "appended, not merged").
func TestEntanglementInteractionWithReinforcement(t *testing.T) {
	cfg := DefaultConfig()
	c := Contract4{
		CorrectionCount: 1,
		PeakIntensity:   0.6,
		Lessons: []Lesson{
			{Observation: "drone_stats·derived_by·type_lookup", Confidence: 0.80},
		},
		Implementation: Implementation{
			SurpriseTouches: []string{"game/drone_stats.gd"},
		},
	}
	written, _ := buildMemoryDeltas(c, cfg)
	if len(written) != 1 {
		t.Fatalf("written = %d, want 1", len(written))
	}
	// Reinforcement: 0.80 * 0.08 = 0.064
	if !approxEqual(written[0].DeltaReinforce, 0.064) {
		t.Errorf("reinforce = %v, want 0.064", written[0].DeltaReinforce)
	}

	ent := buildEntanglementDeltas(written, c, cfg)
	if len(ent) != 1 {
		t.Fatalf("entanglement = %d, want 1", len(ent))
	}
	// Entanglement: -(0.6 * 0.05) = -0.03
	if !approxEqual(ent[0].DeltaReinforce, -0.03) {
		t.Errorf("entanglement = %v, want -0.03", ent[0].DeltaReinforce)
	}

	// Net when store applies them sequentially: 0.064 + (-0.03) = 0.034
	net := written[0].DeltaReinforce + ent[0].DeltaReinforce
	if !approxEqual(net, 0.034) {
		t.Errorf("net = %v, want 0.034", net)
	}

	// Confirm both deltas reference the same tuple.
	if written[0].Subject != ent[0].Subject || written[0].Predicate != ent[0].Predicate || written[0].Object != ent[0].Object {
		t.Errorf("deltas address different tuples: %+v vs %+v", written[0], ent[0])
	}
}
