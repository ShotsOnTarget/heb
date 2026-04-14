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
		{"heb_cli invoke_as bare_heb", []string{"heb", "cli", "invoke", "as", "bare", "heb"}},
		{"internal/retrieve/", []string{"internal", "retrieve"}},
		{"subject·predicate·object", []string{"subject", "predicate", "object"}},
		{"a-b.c/d_e", nil}, // all single chars after split

		// Punctuation stripping
		{"combat, state; rendering.", []string{"combat", "state", "rendering"}},
		{"(foo) [bar] {baz}", []string{"foo", "bar", "baz"}},
		{"it's a test", []string{"it", "test"}}, // apostrophe splits, single chars dropped

		// CamelCase / PascalCase
		{"CombatScreen", []string{"combat", "screen"}},
		{"RunState", []string{"run", "state"}},
		{"getHTTPResponse", []string{"get", "http", "response"}},
		{"XMLParser", []string{"xml", "parser"}},
		{"myURLHandler", []string{"my", "url", "handler"}},
		{"PlayerController rewrite", []string{"player", "controller", "rewrite"}},

		// Digit boundaries
		{"vec3", []string{"vec"}},           // "3" is single char, dropped
		{"int32", []string{"int", "32"}},
		{"player2controller", []string{"player", "controller"}}, // "2" dropped
		{"BM25_IDF_memory", []string{"bm", "25", "idf", "memory"}},

		// Mixed real-world identifiers
		{"forEach", []string{"for", "each"}},
		{"useState", []string{"use", "state"}},
		{"__init__", []string{"init"}},
		{"$scope.apply()", []string{"scope", "apply"}},
		{"@dataclass", []string{"dataclass"}},
		{"std::vector<int>", []string{"std", "vector", "int"}},
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
	// Middle dot is a delimiter
	got := Tokenize("heb_pipeline·must_not·stop")
	want := []string{"heb", "pipeline", "must", "not", "stop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize middle-dot = %v, want %v", got, want)
	}

	// Slash is a delimiter
	got = Tokenize("cmd/heb/pipeline.go")
	want = []string{"cmd", "heb", "pipeline", "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize slash = %v, want %v", got, want)
	}
}
