package consolidate

import "testing"

func TestBuildEdgeDeltasEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if refs := buildEdgeDeltas(nil, cfg); refs != nil {
		t.Errorf("nil input should yield nil, got %v", refs)
	}
	one := []MemoryDelta{{Subject: "a", Predicate: "b", Object: "c"}}
	if refs := buildEdgeDeltas(one, cfg); refs != nil {
		t.Errorf("single tuple should yield nil, got %v", refs)
	}
}

func TestBuildEdgeDeltasPairs(t *testing.T) {
	cfg := DefaultConfig()
	written := []MemoryDelta{
		{Subject: "a", Predicate: "p", Object: "x"},
		{Subject: "b", Predicate: "p", Object: "x"},
		{Subject: "c", Predicate: "p", Object: "x"},
	}
	refs := buildEdgeDeltas(written, cfg)
	// 3-choose-2 = 3 pairs.
	if len(refs) != 3 {
		t.Fatalf("got %d pairs, want 3", len(refs))
	}
	// Every pair uses co-activation-boost.
	for _, e := range refs {
		if e.Delta != cfg.CoActivationBoost {
			t.Errorf("delta = %v, want %v", e.Delta, cfg.CoActivationBoost)
		}
	}
	// Verify the expected pair set (unordered, a<b<c).
	wantPairs := map[string]bool{
		"a→b": true, "a→c": true, "b→c": true,
	}
	for _, e := range refs {
		key := e.A.Subject + "→" + e.B.Subject
		if !wantPairs[key] {
			t.Errorf("unexpected pair %s", key)
		}
		delete(wantPairs, key)
	}
	if len(wantPairs) != 0 {
		t.Errorf("missing pairs: %v", wantPairs)
	}
}

func TestBuildEdgeDeltasCustomBoost(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CoActivationBoost = 0.10
	written := []MemoryDelta{
		{Subject: "a", Predicate: "p", Object: "x"},
		{Subject: "b", Predicate: "p", Object: "x"},
	}
	refs := buildEdgeDeltas(written, cfg)
	if len(refs) != 1 {
		t.Fatalf("got %d, want 1", len(refs))
	}
	if refs[0].Delta != 0.10 {
		t.Errorf("delta = %v, want 0.10", refs[0].Delta)
	}
}
