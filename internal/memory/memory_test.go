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
		{"hello world", []string{"hello", "world"}},
		{"heb_cli invoke_as bare_heb", []string{"heb", "cli", "invoke", "as", "bare", "heb"}},
		{"internal/retrieve/", []string{"internal", "retrieve"}},
		{"BM25_IDF_memory", []string{"bm25", "idf", "memory"}},
		{"subject·predicate·object", []string{"subject", "predicate", "object"}},
		{"a-b.c/d_e", nil}, // all single chars after split → dropped
		{"", nil},
		{"a b c", nil}, // all single chars
		{"Hello WORLD", []string{"hello", "world"}}, // lowercased
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
