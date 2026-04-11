package retrieve

import (
	"testing"

	"github.com/steelboltgames/heb/internal/store"
)

// fakeMeasure returns 1 token per memory + 1 per git ref + 1 per bead.
// Plus a 10-token header. Deterministic and easy to reason about.
func fakeMeasure(r *Result) int {
	return 10 + len(r.Memories) + len(r.GitRefs) + len(r.Beads)
}

func mkMem(subject string, score float64, source string) store.Scored {
	return store.Scored{
		Memory: store.Memory{Subject: subject, Predicate: "p", Object: "o"},
		Score:  score,
		Source: source,
	}
}

func TestTrimUnderBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 100
	r := &Result{
		Memories: []store.Scored{mkMem("a", 0.5, "match")},
		GitRefs:  []GitRef{{Hash: "h1"}},
		Beads:    []BeadRef{{ID: "b1"}},
	}
	trimToBudget(r, cfg, fakeMeasure)
	// 13 tokens, budget 100 — nothing trimmed.
	if len(r.Memories) != 1 || len(r.GitRefs) != 1 || len(r.Beads) != 1 {
		t.Errorf("nothing should be trimmed: memories=%d git=%d beads=%d",
			len(r.Memories), len(r.GitRefs), len(r.Beads))
	}
	if r.TokensUsed != 13 {
		t.Errorf("tokens_used = %d, want 13", r.TokensUsed)
	}
}

func TestTrimBeadsFirst(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 14
	cfg.BeadsResults = 2
	cfg.GitResults = 3
	r := &Result{
		Memories: []store.Scored{mkMem("a", 0.5, "match")},
		GitRefs:  []GitRef{{Hash: "g1"}, {Hash: "g2"}, {Hash: "g3"}},
		Beads:    []BeadRef{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}, {ID: "b4"}},
	}
	// Start: 10+1+3+4 = 18. Budget 14. Need to drop 4.
	// Drop beads from 4 → 2 (floor) = 16. Still over.
	// Drop git from 3 → 3 (floor) — can't, floor reached.
	// Drop memory: 1 match score 0.5. Wait — we go 6,5,4,3,2.
	// After beads at floor (2), measure = 10+1+3+2=16. Still over.
	// Priority 5: git already at floor (3). Skip.
	// Priority 4: no edge memories.
	// Priority 3: drop "a" (score 0.5 match). measure=10+0+3+2=15. Still over.
	// Priority 2: no score >= 1.0 memories. Stop.
	// Final: memories=0, git=3, beads=2, tokens=15.
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Beads) != 2 {
		t.Errorf("beads = %d, want 2 (floor)", len(r.Beads))
	}
	if len(r.GitRefs) != 3 {
		t.Errorf("git = %d, want 3 (floor, not trimmed)", len(r.GitRefs))
	}
	if len(r.Memories) != 0 {
		t.Errorf("memories = %d, want 0", len(r.Memories))
	}
}

func TestTrimPreservesHardConstraint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 12
	cfg.BeadsResults = 0
	cfg.GitResults = 0
	r := &Result{
		Memories: []store.Scored{
			mkMem("!pinned", 0.5, "match"),
			mkMem("normal", 0.9, "match"),
			mkMem("edge1", 0.4, "edge"),
		},
	}
	// Start: 10+3 = 13. Budget 12. Over by 1.
	// Priority 6: beads empty. Priority 5: git empty.
	// Priority 4: drop edge1 → 12. Under budget. Stop.
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Memories) != 2 {
		t.Fatalf("memories = %d, want 2", len(r.Memories))
	}
	foundPinned := false
	for _, m := range r.Memories {
		if m.Subject == "!pinned" {
			foundPinned = true
		}
		if m.Source == "edge" {
			t.Errorf("edge memory should have been trimmed first")
		}
	}
	if !foundPinned {
		t.Errorf("hard constraint '!pinned' was trimmed")
	}
}

func TestTrimHardFloorExceeded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 10
	cfg.BeadsResults = 0
	cfg.GitResults = 0
	r := &Result{
		Memories: []store.Scored{
			mkMem("!a", 0.5, "match"),
			mkMem("!b", 0.5, "match"),
			mkMem("!c", 0.5, "match"),
		},
	}
	// Start: 10+3 = 13. Budget 10. All memories are hard constraints.
	// Nothing can be trimmed. TokensUsed should reflect final state.
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Memories) != 3 {
		t.Errorf("hard constraints should survive: %d", len(r.Memories))
	}
	if r.TokensUsed != 13 {
		t.Errorf("tokens_used = %d, want 13 (over budget is allowed)", r.TokensUsed)
	}
}

func TestTrimLowestScoreFirst(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 12
	cfg.BeadsResults = 0
	cfg.GitResults = 0
	r := &Result{
		Memories: []store.Scored{
			mkMem("hi", 0.9, "match"),
			mkMem("lo", 0.2, "match"),
			mkMem("mid", 0.5, "match"),
		},
	}
	// Start: 10+3 = 13. Budget 12. Drop one lowest-score (<1.0) match.
	// "lo" (0.2) should go.
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Memories) != 2 {
		t.Fatalf("memories = %d, want 2", len(r.Memories))
	}
	for _, m := range r.Memories {
		if m.Subject == "lo" {
			t.Errorf("'lo' (lowest score) should have been trimmed first")
		}
	}
}

func TestTrimPriorityEdgeBeforeMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 12
	cfg.BeadsResults = 0
	cfg.GitResults = 0
	r := &Result{
		Memories: []store.Scored{
			mkMem("m1", 0.3, "match"),
			mkMem("e1", 0.9, "edge"),
		},
	}
	// Start: 10+2 = 12. Exactly on budget. No trim.
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Memories) != 2 {
		t.Errorf("at budget = no trim, got %d", len(r.Memories))
	}

	// Now force it over.
	cfg.TokenBudget = 11
	r2 := &Result{
		Memories: []store.Scored{
			mkMem("m1", 0.3, "match"),
			mkMem("e1", 0.9, "edge"),
		},
	}
	trimToBudget(r2, cfg, fakeMeasure)
	// Edge should go first even though it has a higher score.
	if len(r2.Memories) != 1 {
		t.Fatalf("memories = %d, want 1", len(r2.Memories))
	}
	if r2.Memories[0].Source == "edge" {
		t.Errorf("edge memory should have been trimmed before match")
	}
}
