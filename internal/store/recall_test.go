package store

import (
	"database/sql"
	"testing"
	"time"
)

// seedMemoryFull inserts a memory with specific weight and timestamps.
func seedMemoryFull(t *testing.T, db *sql.DB, body string, weight float64, updatedAt int64) string {
	t.Helper()
	id := MemoryID(body)
	_, err := db.Exec(`INSERT INTO memories(id, body, weight, status, created_at, updated_at)
		VALUES(?, ?, ?, 'active', ?, ?)`, id, body, weight, updatedAt, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRecall_BothConflictingMemoriesSurface(t *testing.T) {
	db := testDB(t)
	now := time.Now().Unix()

	// Seed two conflicting memories about the same topic
	seedMemoryFull(t, db, "ShopEncounter costs 4", 0.80, now-5*86400) // 5 days old
	seedMemoryFull(t, db, "ShopEncounter costs 6", 0.75, now-1*86400) // 1 day old
	// Unrelated memory
	seedMemoryFull(t, db, "CombatScreen syncs combat state", 0.60, now-5*86400)

	results, _, err := Recall(db, []string{"shop", "encounter", "costs"}, 16, "")
	if err != nil {
		t.Fatal(err)
	}

	// Both conflicting memories should surface — Recall doesn't filter
	var foundOld, foundNew bool
	for _, r := range results {
		switch r.Body {
		case "ShopEncounter costs 4":
			foundOld = true
			t.Logf("OLD memory: %q score=%.3f weight=%.2f", r.Body, r.Score, r.Weight)
		case "ShopEncounter costs 6":
			foundNew = true
			t.Logf("NEW memory: %q score=%.3f weight=%.2f", r.Body, r.Score, r.Weight)
		}
	}
	if !foundOld {
		t.Error("expected old memory 'ShopEncounter costs 4' to surface from Recall")
	}
	if !foundNew {
		t.Error("expected new memory 'ShopEncounter costs 6' to surface from Recall")
	}

	// Unrelated memory should NOT match (no token overlap)
	for _, r := range results {
		if r.Body == "CombatScreen syncs combat state" {
			t.Error("unrelated memory should not match tokens [shop, encounter, costs]")
		}
	}
}

func TestRecall_HigherWeightOldMemoryCanOutscoreNewer(t *testing.T) {
	db := testDB(t)
	now := time.Now().Unix()

	// Old memory has higher weight — should score higher (no recency penalty)
	seedMemoryFull(t, db, "ShopEncounter costs 4", 0.80, now-30*86400) // 30 days old, high weight
	seedMemoryFull(t, db, "ShopEncounter costs 6", 0.50, now-1*86400)  // 1 day old, lower weight

	results, _, err := Recall(db, []string{"shop", "encounter", "costs"}, 16, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// With no recency bias, the higher-weight memory should rank first
	if results[0].Body != "ShopEncounter costs 4" {
		t.Errorf("expected higher-weight memory first, got %q (score=%.3f)", results[0].Body, results[0].Score)
	}

	t.Logf("rank 1: %q score=%.3f weight=%.2f", results[0].Body, results[0].Score, results[0].Weight)
	t.Logf("rank 2: %q score=%.3f weight=%.2f", results[1].Body, results[1].Score, results[1].Weight)
}
