package retrieve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/steelboltgames/heb/internal/store"
)

func TestRunHappyPath(t *testing.T) {
	cfg := DefaultConfig()
	in := Input{
		SessionID: "2026-04-09T17:25:00Z",
		Project:   "dreadfall-0",
		Tokens:    []string{"PlayerController"},
	}
	memories := []store.Scored{
		{
			Memory: store.Memory{Body: "drone_cards extended_by subtype", Weight: 0.72},
			Score:  0.72,
			Source: "match",
		},
	}
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl PlayerController . --include=*.gd": {
				Stdout: []byte("game/player_controller.gd\n"),
			},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- game/player_controller.gd": {
				Stdout: []byte("aaa\x00refactor\x002d\x00bbb\x00init\x001w\x00"),
			},
			"bd list --json": {
				Stdout: []byte(`[{"id":"beads-1","title":"PlayerController rewrite","status":"open"}]`),
			},
		},
	}
	r := Run(in, memories, fr, cfg)
	if r.SessionID != in.SessionID || r.Project != in.Project {
		t.Errorf("session/project not echoed: %+v", r)
	}
	if len(r.Memories) != 1 {
		t.Errorf("memories = %d, want 1", len(r.Memories))
	}
	if len(r.GitRefs) != 2 {
		t.Errorf("git refs = %d, want 2", len(r.GitRefs))
	}
	if len(r.Beads) != 1 {
		t.Errorf("beads = %d, want 1", len(r.Beads))
	}
	if r.TokensUsed == 0 {
		t.Errorf("tokens_used should be set after trim")
	}

	// Human block contains each section.
	human := RenderHuman(r)
	for _, want := range []string{"RETRIEVAL RESULT", "MEMORIES", "GIT", "BEADS", "aaa", "beads-1"} {
		if !strings.Contains(human, want) {
			t.Errorf("human block missing %q", want)
		}
	}

	// JSON block parses and preserves session_id.
	jsonBlock := RenderJSON(r)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonBlock), &parsed); err != nil {
		t.Fatalf("JSON block doesn't parse: %v", err)
	}
	if parsed["session_id"] != in.SessionID {
		t.Errorf("session_id not round-tripped")
	}
}

func TestRunColdStart(t *testing.T) {
	cfg := DefaultConfig()
	in := Input{SessionID: "sid", Project: "proj", Tokens: []string{"anything"}}
	fr := &FakeRunner{} // empty — all externals fail.
	r := Run(in, nil, fr, cfg)
	if len(r.Memories) != 0 {
		t.Errorf("cold start: memories = %d, want 0", len(r.Memories))
	}
	if len(r.GitRefs) != 0 || len(r.Beads) != 0 {
		t.Errorf("cold start: externals should be empty")
	}
	// JSON block still renders, arrays non-nil.
	jsonBlock := RenderJSON(r)
	if !strings.Contains(jsonBlock, `"memories": []`) {
		t.Errorf("cold start JSON missing empty memories array")
	}
	if !strings.Contains(jsonBlock, `"git_refs": []`) {
		t.Errorf("cold start JSON missing empty git_refs array")
	}
	if !strings.Contains(jsonBlock, `"beads": []`) {
		t.Errorf("cold start JSON missing empty beads array")
	}
}

func TestRunNoExternal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NoExternal = true
	in := Input{SessionID: "sid", Project: "proj", Tokens: []string{"foo"}}
	fr := &FakeRunner{}
	r := Run(in, nil, fr, cfg)
	if len(fr.Calls) != 0 {
		t.Errorf("NoExternal should issue zero runner calls, got %d", len(fr.Calls))
	}
	if len(r.GitRefs) != 0 || len(r.Beads) != 0 {
		t.Errorf("NoExternal externals non-empty")
	}
}
