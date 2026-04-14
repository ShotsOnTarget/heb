package consolidate

import "testing"

func TestBuildEdgeDeltasEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if refs := buildEdgeDeltas(nil, cfg); refs != nil {
		t.Errorf("nil input should yield nil, got %v", refs)
	}
	one := []MemoryDelta{{Body: "drone has stats"}}
	if refs := buildEdgeDeltas(one, cfg); refs != nil {
		t.Errorf("single tuple should yield nil, got %v", refs)
	}
}

func TestBuildEdgeDeltasPairsWithOverlap(t *testing.T) {
	cfg := DefaultConfig()
	// All three share "drone" — should produce 3 edges.
	written := []MemoryDelta{
		{Body: "drone_stats defined_in main.gd"},
		{Body: "drone_pool contains combat_shield"},
		{Body: "drone_baseline min_value one_for_any_stat"},
	}
	refs := buildEdgeDeltas(written, cfg)
	if len(refs) != 3 {
		t.Fatalf("got %d pairs, want 3 (all share 'drone')", len(refs))
	}
	for _, e := range refs {
		if e.Delta != cfg.CoActivationBoost {
			t.Errorf("delta = %v, want %v", e.Delta, cfg.CoActivationBoost)
		}
		if !e.CoActivation {
			t.Errorf("expected CoActivation=true")
		}
	}
}

func TestBuildEdgeDeltasNoOverlap(t *testing.T) {
	cfg := DefaultConfig()
	// These share no tokens — should produce 0 edges.
	written := []MemoryDelta{
		{Body: "drone_stats defined_in main.gd"},
		{Body: "user prefers new_files_over_monolith"},
	}
	refs := buildEdgeDeltas(written, cfg)
	if len(refs) != 0 {
		t.Fatalf("got %d pairs, want 0 (no token overlap)", len(refs))
	}
}

func TestBuildEdgeDeltasPartialOverlap(t *testing.T) {
	cfg := DefaultConfig()
	// A and B share "salvage", B and C share "combat", A and C share nothing.
	written := []MemoryDelta{
		{Body: "salvage_phase triggers_from victory"},
		{Body: "salvage_beam reuses combat_particles"},
		{Body: "combat_result shows damage_summary"},
	}
	refs := buildEdgeDeltas(written, cfg)
	if len(refs) != 2 {
		t.Fatalf("got %d pairs, want 2", len(refs))
	}
}

func TestBuildEdgeDeltasCustomBoost(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CoActivationBoost = 0.10
	written := []MemoryDelta{
		{Body: "drone_stats has baseline"},
		{Body: "drone_pool contains types"},
	}
	refs := buildEdgeDeltas(written, cfg)
	if len(refs) != 1 {
		t.Fatalf("got %d, want 1", len(refs))
	}
	if refs[0].Delta != 0.10 {
		t.Errorf("delta = %v, want 0.10", refs[0].Delta)
	}
}

func TestTokensOverlap(t *testing.T) {
	a := map[string]bool{"drone": true, "stats": true}
	b := map[string]bool{"drone": true, "pool": true}
	c := map[string]bool{"user": true, "prefers": true}

	if !tokensOverlap(a, b) {
		t.Error("a and b should overlap on 'drone'")
	}
	if tokensOverlap(a, c) {
		t.Error("a and c should not overlap")
	}
}
