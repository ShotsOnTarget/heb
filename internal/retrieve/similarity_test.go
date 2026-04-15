package retrieve

import (
	"math"
	"testing"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

func mem(body string, weight float64, daysAgo int) store.Scored {
	now := time.Now().Unix()
	return store.Scored{
		Memory: store.Memory{
			Body: body, Weight: weight, Status: "active",
			CreatedAt: now - int64(daysAgo)*86400,
			UpdatedAt: now - int64(daysAgo)*86400,
		},
		Score: weight, Source: "match",
	}
}

func TestFindSimilarPairs_ConflictingValues(t *testing.T) {
	memories := []store.Scored{
		mem("ShopEncounter costs 4", 0.80, 5),
		mem("ShopEncounter costs 6", 0.75, 1),
		mem("CombatScreen syncs combat state", 0.60, 5),
	}

	pairs := FindSimilarPairs(memories, 0.70)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 similar pair, got %d", len(pairs))
	}

	p := pairs[0]
	if p.Older.Body != "ShopEncounter costs 4" {
		t.Errorf("expected older='ShopEncounter costs 4', got %q", p.Older.Body)
	}
	if p.Newer.Body != "ShopEncounter costs 6" {
		t.Errorf("expected newer='ShopEncounter costs 6', got %q", p.Newer.Body)
	}
	// Tokenizer drops single-char "4" and "6", so tokens are identical → Jaccard 1.0
	if math.Abs(p.Jaccard-1.0) > 0.01 {
		t.Errorf("expected Jaccard ~1.0, got %.3f", p.Jaccard)
	}
	t.Logf("pair: %q (%.2f, %dd) superseded by %q (%.2f, %dd) jaccard=%.2f",
		p.Older.Body, p.Older.Weight, 5, p.Newer.Body, p.Newer.Weight, 1, p.Jaccard)
}

func TestFindSimilarPairs_NoOverlap(t *testing.T) {
	memories := []store.Scored{
		mem("ShopEncounter costs 4", 0.80, 5),
		mem("CombatScreen syncs combat state", 0.60, 5),
	}

	pairs := FindSimilarPairs(memories, 0.70)
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for unrelated memories, got %d", len(pairs))
	}
}

func TestFindSimilarPairs_SameBodySkipped(t *testing.T) {
	memories := []store.Scored{
		mem("ShopEncounter costs 4", 0.80, 5),
		mem("ShopEncounter costs 4", 0.80, 5), // duplicate (match + edge)
	}

	pairs := FindSimilarPairs(memories, 0.70)
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for identical bodies, got %d", len(pairs))
	}
}

func TestFindSimilarPairs_BelowThreshold(t *testing.T) {
	memories := []store.Scored{
		mem("frigate cargo bays hold items", 0.70, 3),
		mem("frigate engine speed fast", 0.65, 1),
	}

	pairs := FindSimilarPairs(memories, 0.70)
	// "frigate" overlaps, but most tokens differ → low Jaccard
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs below threshold, got %d (jaccard=%.3f)",
			len(pairs), pairs[0].Jaccard)
	}
}

func TestFindSimilarPairs_MultiplePairs(t *testing.T) {
	memories := []store.Scored{
		mem("ShopEncounter costs 4", 0.80, 10),
		mem("ShopEncounter costs 6", 0.75, 3),
		mem("elite card reward is 3", 0.70, 8),
		mem("elite card reward is 5", 0.65, 1),
		mem("CombatScreen syncs combat state", 0.60, 5),
	}

	pairs := FindSimilarPairs(memories, 0.70)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 similar pairs, got %d", len(pairs))
	}

	for _, p := range pairs {
		t.Logf("pair: %q → %q (jaccard=%.2f)", p.Older.Body, p.Newer.Body, p.Jaccard)
	}
}

func TestFindSimilarPairs_SingleMemory(t *testing.T) {
	pairs := FindSimilarPairs([]store.Scored{mem("foo", 0.5, 1)}, 0.70)
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for single memory, got %d", len(pairs))
	}
}

func TestFindSimilarPairs_Empty(t *testing.T) {
	pairs := FindSimilarPairs(nil, 0.70)
	if pairs != nil {
		t.Fatalf("expected nil for empty input, got %v", pairs)
	}
}
