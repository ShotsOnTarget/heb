package retrieve

import "testing"

func TestBeadsPassFilter(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"bd list --json": {
				Stdout: []byte(`[
					{"id": "beads-1", "title": "Add drone stats rework", "status": "open"},
					{"id": "beads-2", "title": "Unrelated task", "status": "open"},
					{"id": "beads-3", "title": "Cost formula cleanup", "status": "in_progress"},
					{"id": "beads-4", "title": "Drone cost review", "status": "open"}
				]`),
			},
		},
	}
	refs := beadsPass([]string{"drone", "cost"}, fr, cfg)
	if len(refs) == 0 {
		t.Fatal("expected matches")
	}
	// beads-4 matches both tokens, should rank first.
	if refs[0].ID != "beads-4" {
		t.Errorf("top result = %q, want beads-4", refs[0].ID)
	}
}

func TestBeadsPassNoExternal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NoExternal = true
	fr := &FakeRunner{}
	refs := beadsPass([]string{"anything"}, fr, cfg)
	if refs != nil {
		t.Errorf("NoExternal should return nil, got %v", refs)
	}
	if len(fr.Calls) != 0 {
		t.Errorf("NoExternal should not invoke runner, got %d calls", len(fr.Calls))
	}
}

func TestBeadsPassBdMissing(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{} // empty — any call returns error
	refs := beadsPass([]string{"drone"}, fr, cfg)
	if refs != nil {
		t.Errorf("missing bd should return nil, got %v", refs)
	}
}

func TestBeadsPassEmptyTokens(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{}
	refs := beadsPass(nil, fr, cfg)
	if refs != nil {
		t.Errorf("empty tokens should return nil, got %v", refs)
	}
}
