package memory

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		// Basic
		{"hello world", []string{"hello", "world"}},
		{"Hello WORLD", []string{"hello", "world"}},
		{"", nil},
		{"a b c", nil}, // all single chars

		// Delimiter splitting (any non-alphanumeric)
		{"heb_cli invoke_as bare_heb", []string{"heb", "cli", "invok", "as", "bare", "heb"}},
		{"internal/retrieve/", []string{"intern", "retriev"}},
		{"subject·predicate·object", []string{"subject", "predic", "object"}},
		{"a-b.c/d_e", nil}, // all single chars after split

		// Punctuation stripping
		{"combat, state; rendering.", []string{"combat", "state", "render"}},
		{"(foo) [bar] {baz}", []string{"foo", "bar", "baz"}},
		{"it's a test", []string{"it", "test"}}, // apostrophe splits, single chars dropped

		// CamelCase / PascalCase
		{"CombatScreen", []string{"combat", "screen"}},
		{"RunState", []string{"run", "state"}},
		{"getHTTPResponse", []string{"get", "http", "respons"}},
		{"XMLParser", []string{"xml", "parser"}},
		{"myURLHandler", []string{"my", "url", "handler"}},
		{"PlayerController rewrite", []string{"player", "control", "rewrit"}},

		// Digit boundaries
		{"vec3", []string{"vec"}},           // "3" is single char, dropped
		{"int32", []string{"int", "32"}},    // digit-only tokens pass through stemmer untouched
		{"player2controller", []string{"player", "control"}}, // "2" dropped
		{"BM25_IDF_memory", []string{"bm", "25", "idf", "memori"}},

		// Mixed real-world identifiers
		{"forEach", []string{"for", "each"}},
		{"useState", []string{"use", "state"}},
		{"__init__", []string{"init"}},
		{"$scope.apply()", []string{"scope", "appli"}},
		{"@dataclass", []string{"dataclass"}},
		{"std::vector<int>", []string{"std", "vector", "int"}},

		// Stemming symmetry: morphological variants collapse to same stem.
		// This is why stemming exists — "interaction" and "interactions"
		// must match, as must "track/tracks/tracking", etc.
		{"interaction", []string{"interact"}},
		{"interactions", []string{"interact"}},
		{"track tracks tracking tracked", []string{"track", "track", "track", "track"}},
		{"run runs running", []string{"run", "run", "run"}},
		{"memory memories", []string{"memori", "memori"}},
	}
	for _, tt := range tests {
		got := Tokenize(tt.input)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestID(t *testing.T) {
	// Deterministic
	id1 := ID("hello world")
	id2 := ID("hello world")
	if id1 != id2 {
		t.Errorf("ID not deterministic: %s != %s", id1, id2)
	}
	// Case insensitive
	id3 := ID("Hello World")
	if id1 != id3 {
		t.Errorf("ID not case-insensitive: %s != %s", id1, id3)
	}
	// Trims whitespace
	id4 := ID("  hello world  ")
	if id1 != id4 {
		t.Errorf("ID not trimming: %s != %s", id1, id4)
	}
	// Different bodies → different IDs
	id5 := ID("goodbye world")
	if id1 == id5 {
		t.Errorf("different bodies should have different IDs")
	}
}

func TestVerbosityCost(t *testing.T) {
	tests := []struct {
		tokens int
		want   float64
	}{
		{1, 1.0},                             // well under cap
		{AtomTokenCap, 1.0},                  // exactly at cap
		{AtomTokenCap * 2, 0.5},              // double cap → halved
		{AtomTokenCap * 3, 1.0 / 3.0},        // triple cap
		{AtomTokenCap + 1, float64(AtomTokenCap) / float64(AtomTokenCap+1)}, // just over
	}
	for _, tt := range tests {
		got := VerbosityCost(tt.tokens)
		if got != tt.want {
			t.Errorf("VerbosityCost(%d) = %v, want %v", tt.tokens, got, tt.want)
		}
	}
}

func TestTokenizeEdgeCases(t *testing.T) {
	// Middle dot is a delimiter ("pipeline" stems to "pipelin")
	got := Tokenize("heb_pipeline·must_not·stop")
	want := []string{"heb", "pipelin", "must", "not", "stop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize middle-dot = %v, want %v", got, want)
	}

	// Slash is a delimiter ("pipeline" stems to "pipelin")
	got = Tokenize("cmd/heb/pipeline.go")
	want = []string{"cmd", "heb", "pipelin", "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize slash = %v, want %v", got, want)
	}

	// Pure-digit tokens pass through stemmer unchanged.
	got = Tokenize("version 42 build 128")
	want = []string{"version", "42", "build", "128"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize digit-tokens = %v, want %v", got, want)
	}
}
