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

func mkMem(body string, score float64, source string) store.Scored {
	return store.Scored{
		Memory: store.Memory{Body: body},
		Score:  score,
		Source: source,
	}
}

func mkGitRef(hash, msg string, score float64) GitRef {
	return GitRef{Hash: hash, Message: msg, Score: score, AgeDays: 1}
}

func TestTrimCapsGitRefsByBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitCap = 3
	cfg.GitTokenBudget = 150
	// Each ref is ~15 tokens, budget=150 fits all 5, but GitCap=3 limits
	r := &Result{
		GitRefs: []GitRef{
			mkGitRef("g1", "feat: add thing", 2.0),
			mkGitRef("g2", "fix: bug", 1.8),
			mkGitRef("g3", "refactor: stuff", 1.5),
			mkGitRef("g4", "docs: readme", 1.2),
			mkGitRef("g5", "chore: cleanup", 1.0),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.GitRefs) != 3 {
		t.Errorf("git refs = %d, want 3 (GitCap)", len(r.GitRefs))
	}
}

func TestTrimGitRefsByTokenBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitCap = 100         // high cap — budget should be the limiter
	cfg.GitTokenBudget = 30  // tight budget — ~2 refs fit
	r := &Result{
		GitRefs: []GitRef{
			mkGitRef("g1", "feat: add thing", 2.0),
			mkGitRef("g2", "fix: bug", 1.8),
			mkGitRef("g3", "refactor: stuff", 1.5),
			mkGitRef("g4", "docs: readme", 1.2),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.GitRefs) >= 4 {
		t.Errorf("git refs = %d, want < 4 (budget should trim)", len(r.GitRefs))
	}
	if r.GitTokensUsed > cfg.GitTokenBudget {
		t.Errorf("git tokens used %d > budget %d", r.GitTokensUsed, cfg.GitTokenBudget)
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

func TestTrimMemoriesToBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 30 // very tight — should drop some memories
	r := &Result{
		Memories: []store.Scored{
			mkMem("high score memory with some text", 0.9, "match"),
			mkMem("medium score memory with text", 0.5, "match"),
			mkMem("low score memory with more text here", 0.2, "edge"),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	// With a 30-token budget, not all 3 should fit
	if len(r.Memories) >= 3 {
		t.Errorf("memories = %d, want < 3 (budget should trim)", len(r.Memories))
	}
	// Highest-scored should be kept
	if len(r.Memories) > 0 && r.Memories[0].Score != 0.9 {
		t.Errorf("first memory score = %.2f, want 0.9 (highest first)", r.Memories[0].Score)
	}
}

func TestTrimMemoriesWithinBudgetPreserved(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 2000 // generous budget — all should fit
	r := &Result{
		Memories: []store.Scored{
			mkMem("a", 0.5, "match"),
			mkMem("b", 0.9, "match"),
			mkMem("c", 0.2, "edge"),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if len(r.Memories) != 3 {
		t.Errorf("memories = %d, want 3 (all within budget)", len(r.Memories))
	}
}

func TestTrimSetsGitBudgetFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitTokenBudget = 200
	r := &Result{
		GitRefs: []GitRef{mkGitRef("h1", "feat: test", 1.5)},
	}
	trimToBudget(r, cfg, fakeMeasure)
	if r.GitTokenBudget != 200 {
		t.Errorf("GitTokenBudget = %d, want 200", r.GitTokenBudget)
	}
	if r.GitTokensUsed <= 0 {
		t.Errorf("GitTokensUsed = %d, want > 0", r.GitTokensUsed)
	}
}

func TestTrimPreservesHardConstraintEvenOverBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenBudget = 1 // absurdly tight — but pinned memories must survive
	r := &Result{
		Memories: []store.Scored{
			mkMem("!pinned rule", 0.5, "match"),
			mkMem("normal memory", 0.9, "match"),
		},
	}
	trimToBudget(r, cfg, fakeMeasure)
	foundPinned := false
	for _, m := range r.Memories {
		if m.Body == "!pinned rule" {
			foundPinned = true
		}
	}
	if !foundPinned {
		t.Errorf("hard constraint '!pinned rule' was dropped — must be preserved")
	}
}
