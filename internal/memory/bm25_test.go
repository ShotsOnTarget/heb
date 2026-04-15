package memory

import (
	"math"
	"testing"
)

func TestBM25Rank_MultiTokenBeatsOne(t *testing.T) {
	docs := []Doc{
		{Words: Tokenize("fix: update cargo removal tests to match hand-first jettison behavior"), Weight: 0},
		{Words: Tokenize("feat: data-driven card effects system, extract functions from main.gd"), Weight: 0},
		{Words: Tokenize("fix: Drone I gets move_energy 1, energy icons, and drag-to-map support"), Weight: 0},
	}
	query := []string{"jettison", "cargo", "card", "deck", "elite"}

	results := BM25Rank(docs, query)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// The jettison+cargo commit should rank first.
	if results[0].Index != 0 {
		t.Errorf("expected doc 0 (jettison+cargo) first, got doc %d (score=%.3f)", results[0].Index, results[0].Score)
	}
}

func TestBM25Rank_NoMatchExcluded(t *testing.T) {
	docs := []Doc{
		{Words: Tokenize("completely unrelated commit about nothing"), Weight: 0},
		{Words: Tokenize("fix: jettison cargo from ship"), Weight: 0},
	}
	query := []string{"jettison", "cargo"}

	results := BM25Rank(docs, query)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (no-match excluded), got %d", len(results))
	}
	if results[0].Index != 1 {
		t.Errorf("expected doc 1, got doc %d", results[0].Index)
	}
}

func TestBM25Rank_EmptyInputs(t *testing.T) {
	if r := BM25Rank(nil, []string{"a"}); r != nil {
		t.Errorf("nil docs should return nil")
	}
	if r := BM25Rank([]Doc{{Words: []string{"a"}}}, nil); r != nil {
		t.Errorf("nil query should return nil")
	}
	if r := BM25Rank([]Doc{{Words: []string{"a"}}}, []string{""}); r != nil {
		t.Errorf("empty-string query should return nil")
	}
}

func TestBM25Rank_WeightTiebreaker(t *testing.T) {
	// Two docs with identical text, different weights.
	docs := []Doc{
		{Words: Tokenize("jettison cargo"), Weight: 0.5},
		{Words: Tokenize("jettison cargo"), Weight: 1.0},
	}
	query := []string{"jettison"}

	results := BM25Rank(docs, query)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Higher weight should rank first.
	if results[0].Index != 1 {
		t.Errorf("higher weight doc should rank first, got doc %d", results[0].Index)
	}
}

func TestBM25Rank_RecencyTiebreaker(t *testing.T) {
	// Two docs with identical text, different ages.
	docs := []Doc{
		{Words: Tokenize("jettison cargo"), Weight: 0.5, AgeDays: 90},
		{Words: Tokenize("jettison cargo"), Weight: 0.5, AgeDays: 1},
	}
	query := []string{"jettison"}

	results := BM25Rank(docs, query)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Newer doc should rank first.
	if results[0].Index != 1 {
		t.Errorf("newer doc should rank first, got doc %d", results[0].Index)
	}
}

func TestRecencyFactor_Values(t *testing.T) {
	tests := []struct {
		ageDays float64
		want    float64
	}{
		{0, 1.0},                                    // brand new
		{30, (1 - RecencyInfluence) + RecencyInfluence*0.5}, // at half-life
		{-5, 1.0},                                   // negative clamped
	}
	for _, tt := range tests {
		got := RecencyFactor(tt.ageDays)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("RecencyFactor(%v) = %.4f, want %.4f", tt.ageDays, got, tt.want)
		}
	}
}

func TestRecencyFactor_BandRange(t *testing.T) {
	// Factor should always be in [(1-influence), 1.0].
	lo := RecencyFactor(365 * 100) // very old
	hi := RecencyFactor(0)          // brand new
	if lo < (1-RecencyInfluence)-0.001 || lo > 1.001 {
		t.Errorf("lo = %.4f, out of band", lo)
	}
	if hi < 0.999 || hi > 1.001 {
		t.Errorf("hi = %.4f, want ~1.0", hi)
	}
}
