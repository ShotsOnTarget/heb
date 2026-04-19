package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steelboltgames/heb/internal/store"
)

func memBody(body string) store.Scored {
	return store.Scored{Memory: store.Memory{Body: body}}
}

func TestExtractIdentifiers_PicksCodeLikeTokens(t *testing.T) {
	mems := []store.Scored{
		memBody("_at_home_base flag gates all breach-only subsystems"),
		memBody("_apply_fitting_and_warp is the real home-base-to-breach path"),
		memBody("ship fitting bypass means _on_warp_pressed unreachable from home base"),
		memBody("RunState snapshots pre-breach for death revert"),
		memBody("Map generator uses hex-flower meta-grid with shape arrays"),
	}
	ids := ExtractIdentifiers(mems, 30)

	want := []string{"_at_home_base", "_apply_fitting_and_warp", "_on_warp_pressed", "RunState"}
	for _, w := range want {
		found := false
		for _, g := range ids {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in extracted ids; got %v", w, ids)
		}
	}
}

func TestExtractIdentifiers_SkipsShortAndStopwords(t *testing.T) {
	mems := []store.Scored{
		memBody("items obtained_via enemy_drops_and_home_base_crafting"),
		memBody("the cargo bay UI"),
	}
	ids := ExtractIdentifiers(mems, 30)
	for _, s := range ids {
		if s == "obtained_via" || s == "enemy_drops_and_home_base_crafting" {
			t.Errorf("stopword %q should have been filtered; got %v", s, ids)
		}
	}
}

func TestExtractIdentifiers_CapsAtMax(t *testing.T) {
	mems := []store.Scored{
		memBody("_a_one _b_two _c_three _d_four _e_five _f_six"),
	}
	ids := ExtractIdentifiers(mems, 3)
	if len(ids) != 3 {
		t.Errorf("expected 3 ids, got %d: %v", len(ids), ids)
	}
}

func TestResolveAnchors_FindsHitsAndFlagsMissing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package a\nfunc DoWork_Alpha() {}\n")
	writeFile(t, filepath.Join(root, "sub", "b.go"), "package sub\n// calls DoWork_Alpha here\nvar x = 1\n")
	writeFile(t, filepath.Join(root, "node_modules", "huge.js"), "const DoWork_Alpha = 1\n")

	anchors := ResolveAnchors(root, []string{"DoWork_Alpha", "Never_Exists"}, 5)

	var found *SymbolAnchors
	var missing *SymbolAnchors
	for i := range anchors {
		if anchors[i].Symbol == "DoWork_Alpha" {
			found = &anchors[i]
		}
		if anchors[i].Symbol == "Never_Exists" {
			missing = &anchors[i]
		}
	}
	if found == nil || len(found.Hits) != 2 {
		t.Fatalf("expected 2 hits for DoWork_Alpha (node_modules skipped), got %+v", found)
	}
	if missing == nil || !missing.NotFound {
		t.Errorf("expected Never_Exists flagged NotFound, got %+v", missing)
	}

	lines := map[string]int{}
	for _, h := range found.Hits {
		lines[h.File] = h.Line
	}
	if lines["a.go"] != 2 {
		t.Errorf("expected a.go:2, got %+v", lines)
	}
	if lines["sub/b.go"] != 2 {
		t.Errorf("expected sub/b.go:2 (forward slashes), got %+v", lines)
	}
}

func TestResolveAnchors_RespectsMaxHits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.go"),
		"Sym_Here\nSym_Here\nSym_Here\nSym_Here\nSym_Here\n")

	anchors := ResolveAnchors(root, []string{"Sym_Here"}, 2)
	if len(anchors) != 1 || len(anchors[0].Hits) != 2 {
		t.Fatalf("expected 2 hits cap, got %+v", anchors)
	}
}

func TestResolveAnchors_SkipsBinaryFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bin.dat"), string([]byte{0x00, 'S', 'y', 'm', '_', 'H', 'e', 'r', 'e'}))
	writeFile(t, filepath.Join(root, "ok.go"), "Sym_Here = 1\n")

	anchors := ResolveAnchors(root, []string{"Sym_Here"}, 5)
	if len(anchors[0].Hits) != 1 || anchors[0].Hits[0].File != "ok.go" {
		t.Errorf("binary file should be skipped, got %+v", anchors[0].Hits)
	}
}

func TestFormatAnchorSection_RendersHitsAndStale(t *testing.T) {
	anchors := []SymbolAnchors{
		{Symbol: "_at_home_base", Hits: []Anchor{{File: "game/main.gd", Line: 44}, {File: "game/state.gd", Line: 18}}},
		{Symbol: "RunState", NotFound: true},
	}
	out := FormatAnchorSection(anchors, 5)
	if !strings.Contains(out, "`_at_home_base`: game/main.gd:44, game/state.gd:18") {
		t.Errorf("hit formatting missing: %s", out)
	}
	if !strings.Contains(out, "`RunState`") || !strings.Contains(out, "Stale") {
		t.Errorf("stale formatting missing: %s", out)
	}
}

func TestFormatAnchorSection_EmptyReturnsEmpty(t *testing.T) {
	if FormatAnchorSection(nil, 5) != "" {
		t.Error("nil should return empty")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
