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

func TestTrimCapsGitRefs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitCap = 3
	r := &Result{
		GitRefs: []GitRef{{Hash: "g1"}, {Hash: "g2"}, {Hash: "g3"}, {Hash: "g4"}, {Hash: "g5"}},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.GitRefs) != 3 {
		t.Errorf("git refs = %d, want 3 (cap)", len(r.GitRefs))
	}
}

func TestTrimCapsBeads(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BeadsResults = 2
	r := &Result{
		Beads: []BeadRef{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}, {ID: "b4"}},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Beads) != 2 {
		t.Errorf("beads = %d, want 2 (cap)", len(r.Beads))
	}
}

func TestTrimPreservesMemoriesUntouched(t *testing.T) {
	cfg := DefaultConfig()
	r := &Result{
		Memories: []store.Scored{
			mkMem("a", 0.5, "match"),
			mkMem("b", 0.9, "match"),
			mkMem("c", 0.2, "edge"),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	// trimToBudget no longer touches memories — that's Recall's job.
	if len(r.Memories) != 3 {
		t.Errorf("memories = %d, want 3 (untouched)", len(r.Memories))
	}
}

func TestTrimMeasuresTokens(t *testing.T) {
	cfg := DefaultConfig()
	r := &Result{
		Memories: []store.Scored{mkMem("a", 0.5, "match")},
		GitRefs:  []GitRef{{Hash: "h1"}},
		Beads:    []BeadRef{{ID: "b1"}},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if r.TokensUsed != 13 {
		t.Errorf("tokens_used = %d, want 13", r.TokensUsed)
	}
}

func TestTrimPreservesHardConstraint(t *testing.T) {
	cfg := DefaultConfig()
	r := &Result{
		Memories: []store.Scored{
			mkMem("!pinned", 0.5, "match"),
			mkMem("normal", 0.9, "match"),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	// All memories preserved (trimming is Recall's job now).
	foundPinned := false
	for _, m := range r.Memories {
		if m.Subject == "!pinned" {
			foundPinned = true
		}
	}
	if !foundPinned {
		t.Errorf("hard constraint '!pinned' missing")
	}
}
