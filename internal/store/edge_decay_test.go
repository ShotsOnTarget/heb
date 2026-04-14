package store

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})
	return db
}

func seedMemory(t *testing.T, tx *sql.Tx, body string) string {
	t.Helper()
	id := MemoryID(body)
	_, err := tx.Exec(`INSERT INTO memories(id, body, weight, status, created_at, updated_at)
		VALUES(?, ?, 0.5, 'active', 1000, 1000)`, id, body)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// canonEdge returns (a, b) in canonical order (a < b).
func canonEdge(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
}

func queryEdge(t *testing.T, db *sql.DB, id1, id2 string) (float64, int) {
	t.Helper()
	a, b := canonEdge(id1, id2)
	var strength float64
	var count int
	if err := db.QueryRow(`SELECT strength, co_activation_count FROM edges WHERE a_id = ? AND b_id = ?`, a, b).Scan(&strength, &count); err != nil {
		t.Fatalf("query edge: %v", err)
	}
	return strength, count
}

func TestUpdateEdgeCoActivation(t *testing.T) {
	db := testDB(t)
	tx, _ := db.Begin()
	aID := seedMemory(t, tx, "a p x")
	bID := seedMemory(t, tx, "b p y")

	// First co-activation: creates edge with count=1
	if err := UpdateEdge(tx, aID, bID, 0.06, true); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	strength, count := queryEdge(t, db, aID, bID)
	if strength != 0.06 {
		t.Errorf("strength = %v, want 0.06", strength)
	}
	if count != 1 {
		t.Errorf("co_activation_count = %d, want 1", count)
	}

	// Second co-activation: increments count
	tx, _ = db.Begin()
	if err := UpdateEdge(tx, aID, bID, 0.06, true); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	strength, count = queryEdge(t, db, aID, bID)
	if strength != 0.12 {
		t.Errorf("strength = %v, want 0.12", strength)
	}
	if count != 2 {
		t.Errorf("co_activation_count = %d, want 2", count)
	}

	// Dream edge (not co-activation): does NOT increment count
	tx, _ = db.Begin()
	if err := UpdateEdge(tx, aID, bID, 0.02, false); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	strength, count = queryEdge(t, db, aID, bID)
	if !approxEqual(strength, 0.14) {
		t.Errorf("strength = %v, want 0.14", strength)
	}
	if count != 2 {
		t.Errorf("co_activation_count = %d, want 2 (dream should not increment)", count)
	}
}

func TestDecayEdge(t *testing.T) {
	db := testDB(t)
	tx, _ := db.Begin()
	aID := seedMemory(t, tx, "a p x")
	bID := seedMemory(t, tx, "b p y")
	if err := UpdateEdge(tx, aID, bID, 0.06, true); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	// Decay
	tx, _ = db.Begin()
	if err := DecayEdge(tx, aID, bID, -0.005); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	strength, count := queryEdge(t, db, aID, bID)
	if strength != 0.055 {
		t.Errorf("strength = %v, want 0.055", strength)
	}
	if count != 1 {
		t.Errorf("co_activation_count = %d, want 1 (decay should not change count)", count)
	}
}

func TestDecayEdgeClampsAtZero(t *testing.T) {
	db := testDB(t)
	tx, _ := db.Begin()
	aID := seedMemory(t, tx, "a p x")
	bID := seedMemory(t, tx, "b p y")
	if err := UpdateEdge(tx, aID, bID, 0.02, false); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	// Decay more than the edge strength
	tx, _ = db.Begin()
	if err := DecayEdge(tx, aID, bID, -0.05); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	strength, _ := queryEdge(t, db, aID, bID)
	if strength != 0 {
		t.Errorf("strength = %v, want 0 (should clamp at zero)", strength)
	}
}

func TestEdgesFor(t *testing.T) {
	db := testDB(t)
	tx, _ := db.Begin()
	aID := seedMemory(t, tx, "a p x")
	bID := seedMemory(t, tx, "b p y")
	cID := seedMemory(t, tx, "c p z")
	if err := UpdateEdge(tx, aID, bID, 0.06, true); err != nil {
		t.Fatal(err)
	}
	if err := UpdateEdge(tx, bID, cID, 0.02, false); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	edges, err := EdgesFor(db, bID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2", len(edges))
	}

	// Check that co_activation_count is correct
	for _, e := range edges {
		if e.AID == aID && e.BID == bID {
			if e.CoActivationCount != 1 {
				t.Errorf("a-b co_activation_count = %d, want 1", e.CoActivationCount)
			}
		}
		if e.AID == bID && e.BID == cID {
			if e.CoActivationCount != 0 {
				t.Errorf("b-c co_activation_count = %d, want 0", e.CoActivationCount)
			}
		}
	}
}

func TestDecayEdgeNonExistent(t *testing.T) {
	db := testDB(t)
	tx, _ := db.Begin()
	seedMemory(t, tx, "a p x")
	seedMemory(t, tx, "b p y")
	tx.Commit()

	// Decay a non-existent edge — should be a no-op
	tx, _ = db.Begin()
	aID := MemoryID("a p x")
	bID := MemoryID("b p y")
	if err := DecayEdge(tx, aID, bID, -0.005); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&count)
	if count != 0 {
		t.Errorf("edge count = %d, want 0 (decay should not create edges)", count)
	}
}
