package consolidate

import "testing"

func TestRunThresholdFailSkipsDeltas(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		SessionID: "s1",
		Project:   "p",
		Completed: true, // no signal
	}
	r := Run(c, cfg)
	if r.ThresholdMet {
		t.Errorf("threshold should not be met")
	}
	if len(r.Payload.Memories) != 0 {
		t.Errorf("memories = %d, want 0", len(r.Payload.Memories))
	}
	if len(r.Payload.Edges) != 0 {
		t.Errorf("edges = %d, want 0", len(r.Payload.Edges))
	}
	if r.Payload.Episode == nil {
		t.Errorf("episode should still be written")
	}
}

func TestRunHappyPath(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		SessionID: "s2",
		Project:   "p",
		Completed: true,
		Lessons: []Lesson{
			{Observation: "drone_stats·defined_in·main", Confidence: 0.80},
			{Observation: "drone_pool·contains·types", Confidence: 0.80},
		},
	}
	r := Run(c, cfg)
	if !r.ThresholdMet {
		t.Fatalf("threshold should be met (has lessons)")
	}
	if len(r.Payload.Memories) != 2 {
		t.Errorf("memories = %d, want 2", len(r.Payload.Memories))
	}
	if len(r.Payload.Edges) != 1 {
		t.Errorf("edges = %d, want 1", len(r.Payload.Edges))
	}
	if r.Payload.Episode == nil {
		t.Errorf("episode missing")
	}
}

// E2E interaction test mirroring entanglement_test's interaction case:
// a corrective session with one lesson whose tuple also matches a
// surprise_touch. Result.Payload.Memories should contain 2 entries —
// one session_reinforced and one entanglement_signal on the same tuple.
func TestRunInteractionReinforcementPlusEntanglement(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		SessionID:       "s3",
		Project:         "p",
		Completed:       true,
		CorrectionCount: 1,
		PeakIntensity:   0.6,
		Lessons: []Lesson{
			{Observation: "drone_stats·derived_by·type_lookup", Confidence: 0.80},
		},
		Implementation: Implementation{
			SurpriseTouches: []string{"game/drone_stats.gd"},
		},
	}
	r := Run(c, cfg)
	if !r.ThresholdMet {
		t.Fatalf("threshold should be met")
	}
	if len(r.Payload.Memories) != 2 {
		t.Fatalf("memories = %d, want 2 (reinforcement + entanglement)", len(r.Payload.Memories))
	}
	m0 := r.Payload.Memories[0]
	m1 := r.Payload.Memories[1]
	if m0.Event != "session_reinforced" {
		t.Errorf("first event = %q, want session_reinforced", m0.Event)
	}
	if m1.Event != "entanglement_signal" {
		t.Errorf("second event = %q, want entanglement_signal", m1.Event)
	}
	if !approxEqual(m0.DeltaReinforce, 0.064) {
		t.Errorf("reinforce = %v, want 0.064", m0.DeltaReinforce)
	}
	if !approxEqual(m1.DeltaReinforce, -0.03) {
		t.Errorf("entanglement = %v, want -0.03", m1.DeltaReinforce)
	}
	net := m0.DeltaReinforce + m1.DeltaReinforce
	if !approxEqual(net, 0.034) {
		t.Errorf("net = %v, want 0.034", net)
	}
	// Edges pass: only 1 written tuple → no edges
	if len(r.Payload.Edges) != 0 {
		t.Errorf("edges = %d, want 0 (single tuple)", len(r.Payload.Edges))
	}
}

func TestRunSkippedPropagates(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		SessionID: "s4",
		Project:   "p",
		Completed: true,
		Lessons: []Lesson{
			{Observation: "good·tuple·here", Confidence: 0.80},
			{Observation: "bad", Confidence: 0.80},         // malformed
			{Observation: "low·conf·tuple", Confidence: 0.1}, // below threshold
		},
	}
	r := Run(c, cfg)
	if len(r.Payload.Memories) != 1 {
		t.Errorf("memories = %d, want 1", len(r.Payload.Memories))
	}
	if len(r.Skipped) != 2 {
		t.Errorf("skipped = %d, want 2", len(r.Skipped))
	}
}
