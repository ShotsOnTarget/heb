package consolidate

import "testing"

func TestBuildEdgeDeltasEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if refs := buildEdgeDeltas(nil, cfg); refs != nil {
		t.Errorf("nil input should yield nil, got %v", refs)
	}
	one := []MemoryDelta{{Subject: "drone", Predicate: "has", Object: "stats"}}
	if refs := buildEdgeDeltas(one, cfg); refs != nil {
		t.Errorf("single tuple should yield nil, got %v", refs)
	}
}

func TestBuildEdgeDeltasPairsWithOverlap(t *testing.T) {
	cfg := DefaultConfig()
	// All three share "drone" in subject — should produce 3 edges.
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "defined_in", Object: "main.gd"},
		{Subject: "drone_pool", Predicate: "contains", Object: "combat_shield"},
		{Subject: "drone_baseline", Predicate: "min_value", Object: "one_for_any_stat"},
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
	// These share no subject/object tokens — should produce 0 edges.
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "defined_in", Object: "main.gd"},
		{Subject: "user", Predicate: "prefers", Object: "new_files_over_monolith"},
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
		{Subject: "salvage_phase", Predicate: "triggers_from", Object: "victory"},
		{Subject: "salvage_beam", Predicate: "reuses", Object: "combat_particles"},
		{Subject: "combat_result", Predicate: "shows", Object: "damage_summary"},
	}
	refs := buildEdgeDeltas(written, cfg)
	// salvage_phase ↔ salvage_beam (share "salvage") ✓
	// salvage_beam ↔ combat_result (share "combat") ✓
	// salvage_phase ↔ combat_result (no overlap) ✗
	if len(refs) != 2 {
		t.Fatalf("got %d pairs, want 2", len(refs))
	}
}

func TestBuildEdgeDeltasCustomBoost(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CoActivationBoost = 0.10
	written := []MemoryDelta{
		{Subject: "drone_stats", Predicate: "has", Object: "baseline"},
		{Subject: "drone_pool", Predicate: "contains", Object: "types"},
	}
	refs := buildEdgeDeltas(written, cfg)
	if len(refs) != 1 {
		t.Fatalf("got %d, want 1", len(refs))
	}
	if refs[0].Delta != 0.10 {
		t.Errorf("delta = %v, want 0.10", refs[0].Delta)
	}
}

func TestExtractTokens(t *testing.T) {
	got := extractTokens("drone_station_pool", "combat_shield_scout")
	want := map[string]bool{
		"drone": true, "station": true, "pool": true,
		"combat": true, "shield": true, "scout": true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(got), len(want), got)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing token %q", k)
		}
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
