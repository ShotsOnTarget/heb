package retrieve

import (
	"errors"
	"testing"
)

// TestParseNullSeparated covers §8.1 null-separator parsing including a
// subject containing a literal tab character.
func TestParseNullSeparated(t *testing.T) {
	// Two records:
	//   abc1234  "fix: drop tab	in subject"  "2 days ago"
	//   def5678  "add feature"               "1 week ago"
	data := []byte("abc1234\x00fix: drop tab\tin subject\x002 days ago\x00def5678\x00add feature\x001 week ago\x00")
	refs := parseNullSeparated(data)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	if refs[0].Hash != "abc1234" {
		t.Errorf("ref0 hash = %q", refs[0].Hash)
	}
	if refs[0].Message != "fix: drop tab\tin subject" {
		t.Errorf("ref0 message = %q (tab preserved?)", refs[0].Message)
	}
	if refs[0].Age != "2 days ago" {
		t.Errorf("ref0 age = %q", refs[0].Age)
	}
	if refs[1].Hash != "def5678" {
		t.Errorf("ref1 hash = %q", refs[1].Hash)
	}
	if refs[1].Message != "add feature" {
		t.Errorf("ref1 message = %q", refs[1].Message)
	}
}

func TestParseNullSeparatedEmpty(t *testing.T) {
	if refs := parseNullSeparated(nil); refs != nil {
		t.Errorf("empty input should yield nil, got %v", refs)
	}
	if refs := parseNullSeparated([]byte{}); refs != nil {
		t.Errorf("empty input should yield nil, got %v", refs)
	}
}

// TestExample1LiteralFileMatch mirrors Part A Example 1.
// Only commits whose messages match query tokens survive the attention filter.
func TestExample1LiteralFileMatch(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl PlayerController . --include=*.gd": {
				Stdout: []byte("game/player_controller.gd\n"),
			},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- game/player_controller.gd": {
				// All three messages contain query tokens so they score > 0.
				// "controller" and "player" both come from "PlayerController".
				Stdout: []byte("aaa1111\x00refactor player controller\x002 days ago\x00bbb2222\x00fix player controller input\x003 days ago\x00ccc3333\x00init\x001 week ago\x00"),
			},
		},
	}
	refs := gitPass([]string{"PlayerController"}, fr, cfg)
	// "init" has no query-token match → score 0 → dropped.
	// The two scored refs survive (scores are close enough to avoid gap cut).
	if len(refs) < 1 {
		t.Fatal("got 0 refs, want >= 1")
	}
	if refs[0].Hash != "aaa1111" {
		t.Errorf("ref0 hash = %q, want aaa1111", refs[0].Hash)
	}
	if refs[0].Score <= 0 {
		t.Errorf("ref0 score = %.2f, want > 0", refs[0].Score)
	}
	if refs[0].AgeDays <= 0 {
		t.Errorf("ref0 age_days = %.1f, want > 0", refs[0].AgeDays)
	}
}

// TestExample2LiteralMessageGrep mirrors Part A Example 2.
func TestExample2LiteralMessageGrep(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl refactor_auth . --include=*.gd": {
				Err: errors.New("grep: no matches"),
			},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all --grep=refactor_auth": {
				Stdout: []byte("aaa\x00auth refactor step 1\x001 day ago\x00bbb\x00auth refactor step 2\x002 hours ago\x00"),
			},
		},
	}
	refs := gitPass([]string{"refactor_auth"}, fr, cfg)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	// Both should have scores > 0 since "refactor" and "auth" are in the messages
	for i, r := range refs {
		if r.Score <= 0 {
			t.Errorf("ref[%d] score = %.2f, want > 0", i, r.Score)
		}
	}
}

// TestExample12FileGlob mirrors Part A Example 12 — verifies --file-glob
// is actually forwarded to the grep invocation.
func TestExample12FileGlob(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FileGlob = "*.cs"
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl PlayerController . --include=*.cs": {
				Stdout: []byte("Scripts/PlayerController.cs\n"),
			},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- Scripts/PlayerController.cs": {
				// Messages contain query-matching tokens
				Stdout: []byte("cs11\x00refactor player controller\x001 day ago\x00cs22\x00fix controller init\x003 days ago\x00"),
			},
		},
	}
	refs := gitPass([]string{"PlayerController"}, fr, cfg)
	if len(refs) < 1 {
		t.Fatal("got 0 refs, want >= 1")
	}
	// Verify the fake was actually called with --include=*.cs, not *.gd.
	foundCs := false
	for _, call := range fr.Calls {
		if call == "grep -rl PlayerController . --include=*.cs" {
			foundCs = true
		}
		if call == "grep -rl PlayerController . --include=*.gd" {
			t.Errorf("unexpected call with default file-glob: %s", call)
		}
	}
	if !foundCs {
		t.Errorf("--file-glob=*.cs not forwarded to grep")
	}
}

// TestGitDedupeAcrossTokens verifies that refs appearing in multiple token
// lookups only appear once in the output.
func TestGitDedupeAcrossTokens(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl alpha . --include=*.gd": {Stdout: []byte("a.gd\n")},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- a.gd": {
				// Both messages contain both query tokens so scores are similar
				Stdout: []byte("shared\x00alpha beta shared\x001d\x00uniqA\x00alpha beta unique\x001d\x00"),
			},
			"grep -rl beta . --include=*.gd": {Stdout: []byte("b.gd\n")},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- b.gd": {
				Stdout: []byte("shared\x00alpha beta shared\x001d\x00uniqB\x00alpha beta other\x001d\x00"),
			},
		},
	}
	refs := gitPass([]string{"alpha", "beta"}, fr, cfg)
	// All messages contain both query tokens, so all score similarly.
	// Shared should appear only once despite being in both lookups.
	if len(refs) < 2 {
		t.Fatalf("got %d refs, want >= 2", len(refs))
	}
	seen := map[string]int{}
	for _, r := range refs {
		seen[r.Hash]++
	}
	if seen["shared"] != 1 {
		t.Errorf("shared hash appeared %d times, want 1", seen["shared"])
	}
}

// TestIDFSortRarestFirst verifies that tokens are reordered so that tokens
// matching fewer files (more specific) come first.
func TestIDFSortRarestFirst(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			// "feature" matches 5 files (generic)
			"grep -rl feature . --include=*.gd": {Stdout: []byte("a.gd\nb.gd\nc.gd\nd.gd\ne.gd\n")},
			// "user" matches 3 files
			"grep -rl user . --include=*.gd": {Stdout: []byte("a.gd\nb.gd\nc.gd\n")},
			// "jettison" matches 1 file (specific)
			"grep -rl jettison . --include=*.gd": {Stdout: []byte("cargo.gd\n")},
			// "cargo" matches 2 files
			"grep -rl cargo . --include=*.gd": {Stdout: []byte("cargo.gd\nbay.gd\n")},
		},
	}
	sorted := idfSort([]string{"feature", "user", "jettison", "cargo"}, fr, cfg)
	if len(sorted) != 4 {
		t.Fatalf("got %d tokens, want 4", len(sorted))
	}
	// jettison (1) < cargo (2) < user (3) < feature (5)
	want := []string{"jettison", "cargo", "user", "feature"}
	for i, w := range want {
		if sorted[i] != w {
			t.Errorf("sorted[%d] = %q, want %q (full: %v)", i, sorted[i], w, sorted)
			break
		}
	}
}

// TestIDFSortStable verifies stable sort preserves original order for tokens
// with equal file grep counts.
func TestIDFSortStable(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl alpha . --include=*.gd": {Stdout: []byte("x.gd\n")},
			"grep -rl beta . --include=*.gd":  {Stdout: []byte("y.gd\n")},
			"grep -rl gamma . --include=*.gd": {Stdout: []byte("z.gd\n")},
		},
	}
	sorted := idfSort([]string{"alpha", "beta", "gamma"}, fr, cfg)
	// All have count=1, stable sort preserves input order.
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if sorted[i] != w {
			t.Errorf("sorted[%d] = %q, want %q", i, sorted[i], w)
		}
	}
}

// TestIDFSortZeroHitsFirst verifies tokens with no file matches (count=0)
// sort before tokens with matches — they'll fall through to L2 message grep
// or decomposition quickly without wasting budget.
func TestIDFSortZeroHitsFirst(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl common . --include=*.gd": {Stdout: []byte("a.gd\nb.gd\nc.gd\n")},
			"grep -rl rare . --include=*.gd":   {Err: errors.New("no match")},
		},
	}
	sorted := idfSort([]string{"common", "rare"}, fr, cfg)
	if sorted[0] != "rare" {
		t.Errorf("zero-hit token should sort first, got %v", sorted)
	}
}

// TestGitPassIDFOrdering verifies the full gitPass pipeline processes rare
// tokens before common ones, so specific commits surface instead of noise.
// IDF sort controls candidate *collection* order. When the candidate cap
// is tight, rare-token candidates get collected first and are available
// for BM25 ranking.
func TestGitPassIDFOrdering(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitCap = 3 // tight cap

	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			// IDF sort phase: "feature" matches 5 files, "jettison" matches 1
			"grep -rl feature . --include=*.gd":  {Stdout: []byte("a.gd\nb.gd\nc.gd\nd.gd\ne.gd\n")},
			"grep -rl jettison . --include=*.gd": {Stdout: []byte("cargo.gd\n")},
			// jettison lookup (processed first due to IDF sort).
			// Messages contain "jettison" — the rarer token in BM25 too.
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- cargo.gd": {
				Stdout: []byte("jet1\x00feat: cargo jettison feature\x001d\x00jet2\x00fix: jettison feature bug\x002d\x00jet3\x00refactor: jettison cargo\x003d\x00"),
			},
			// feature lookup — only contains "feature", the common token
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- a.gd b.gd c.gd d.gd e.gd": {
				Stdout: []byte("feat1\x00feat: feature combat system\x001d\x00feat2\x00fix: feature rift screen\x002d\x00"),
			},
		},
	}

	refs := gitPass([]string{"feature", "jettison"}, fr, cfg)
	if len(refs) == 0 {
		t.Fatal("got 0 refs, want > 0")
	}
	if len(refs) > 3 {
		t.Errorf("got %d refs, want <= 3 (GitCap)", len(refs))
	}
	// Jettison commits contain both tokens while feature commits only have one.
	// BM25 should rank jet commits higher.
	if refs[0].Hash != "jet1" && refs[0].Hash != "jet2" {
		t.Errorf("ref0 = %q, want jet1 or jet2 (jettison+feature should outrank feature-only)", refs[0].Hash)
	}
}

// TestGitNoExternal verifies --no-external returns nil without calling runner.
func TestGitNoExternal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NoExternal = true
	fr := &FakeRunner{}
	refs := gitPass([]string{"anything"}, fr, cfg)
	if refs != nil {
		t.Errorf("NoExternal should return nil, got %v", refs)
	}
	if len(fr.Calls) != 0 {
		t.Errorf("NoExternal should not invoke runner, got %d calls", len(fr.Calls))
	}
}

// TestGitRefsHaveScores verifies that surviving refs carry BM25 scores and age.
func TestGitRefsHaveScores(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl salvage . --include=*.gd": {
				Stdout: []byte("card_data.gd\n"),
			},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- card_data.gd": {
				Stdout: []byte("abc1\x00feat: add salvage card\x002 days ago\x00def2\x00fix: salvage effect\x005 days ago\x00"),
			},
		},
	}
	refs := gitPass([]string{"salvage"}, fr, cfg)
	if len(refs) == 0 {
		t.Fatal("expected refs, got 0")
	}
	for i, r := range refs {
		if r.Score <= 0 {
			t.Errorf("ref[%d] %s score=%.2f, want > 0", i, r.Hash, r.Score)
		}
		if r.AgeDays <= 0 {
			t.Errorf("ref[%d] %s age_days=%.1f, want > 0", i, r.Hash, r.AgeDays)
		}
	}
}

// TestAttentionFilterGitDropsIrrelevant verifies zero-score refs are dropped.
func TestAttentionFilterGitDropsIrrelevant(t *testing.T) {
	refs := []GitRef{
		{Hash: "a", Score: 2.5},
		{Hash: "b", Score: 2.4}, // close to a, no gap
		{Hash: "c", Score: 0},   // should be dropped
		{Hash: "d", Score: 0},   // should be dropped
	}
	filtered := attentionFilterGit(refs, 10)
	if len(filtered) != 2 {
		t.Fatalf("got %d refs, want 2 (zero-score dropped)", len(filtered))
	}
}

// TestAttentionFilterGitGap verifies the gap-based cutoff works.
func TestAttentionFilterGitGap(t *testing.T) {
	refs := []GitRef{
		{Hash: "a", Score: 5.0},
		{Hash: "b", Score: 4.8},
		{Hash: "c", Score: 1.0}, // big gap from b → c (4.8/1.0 = 4.8 >> 1.2)
		{Hash: "d", Score: 0.9},
	}
	filtered := attentionFilterGit(refs, 10)
	// Gap at position 2 (4.8/1.0 >= 1.2), so cut there
	if len(filtered) != 2 {
		t.Fatalf("got %d refs, want 2 (gap cutoff)", len(filtered))
	}
}
