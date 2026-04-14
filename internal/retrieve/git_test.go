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
func TestExample1LiteralFileMatch(t *testing.T) {
	cfg := DefaultConfig()
	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			"grep -rl PlayerController . --include=*.gd": {
				Stdout: []byte("game/player_controller.gd\n"),
			},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- game/player_controller.gd": {
				Stdout: []byte("aaa1111\x00refactor controller\x002 days ago\x00bbb2222\x00add input\x003 days ago\x00ccc3333\x00init\x001 week ago\x00"),
			},
		},
	}
	refs := gitPass([]string{"PlayerController"}, fr, cfg)
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3", len(refs))
	}
	if refs[0].Hash != "aaa1111" {
		t.Errorf("ref0 hash = %q", refs[0].Hash)
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
				Stdout: []byte("cs11\x00C# refactor\x001 day ago\x00cs22\x00C# init\x003 days ago\x00"),
			},
		},
	}
	refs := gitPass([]string{"PlayerController"}, fr, cfg)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
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
				Stdout: []byte("shared\x00shared commit\x001d\x00uniqA\x00only a\x002d\x00"),
			},
			"grep -rl beta . --include=*.gd": {Stdout: []byte("b.gd\n")},
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- b.gd": {
				Stdout: []byte("shared\x00shared commit\x001d\x00uniqB\x00only b\x003d\x00"),
			},
		},
	}
	refs := gitPass([]string{"alpha", "beta"}, fr, cfg)
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3 (shared, uniqA, uniqB)", len(refs))
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
func TestGitPassIDFOrdering(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitCap = 3 // tight cap to prove ordering matters

	fr := &FakeRunner{
		Responses: map[string]FakeResponse{
			// IDF sort phase: "feature" matches 5 files, "jettison" matches 1
			"grep -rl feature . --include=*.gd":  {Stdout: []byte("a.gd\nb.gd\nc.gd\nd.gd\ne.gd\n")},
			"grep -rl jettison . --include=*.gd": {Stdout: []byte("cargo.gd\n")},
			// jettison lookup (processed first due to IDF sort)
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- cargo.gd": {
				Stdout: []byte("jet1\x00feat: cargo jettison\x001d\x00jet2\x00fix: jettison bug\x002d\x00jet3\x00refactor: cargo bay\x003d\x00"),
			},
			// feature lookup (processed second, but cap already hit)
			"git log --format=%h%x00%s%x00%cr%x00 -z -10 --all -- a.gd b.gd c.gd d.gd e.gd": {
				Stdout: []byte("feat1\x00feat: combat system\x001d\x00feat2\x00fix: rift screen\x002d\x00"),
			},
		},
	}

	refs := gitPass([]string{"feature", "jettison"}, fr, cfg)
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3", len(refs))
	}
	// All 3 should be jettison-related since it's processed first (rarest)
	if refs[0].Hash != "jet1" {
		t.Errorf("ref0 = %q, want jet1 (jettison should be processed first)", refs[0].Hash)
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
