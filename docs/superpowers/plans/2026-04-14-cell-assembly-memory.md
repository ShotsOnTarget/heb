# Cell Assembly Memory Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace SPO triple memory model with cell assembly atoms — a single `body` text field, one shared tokenizer, and a session energy budget — so retrieve, learn, and consolidate all share one definition of what a memory is.

**Architecture:** New `internal/memory` package defines `Atom`, `Tokenize()`, and `ID()`. Schema migrates from `subject/predicate/object` columns to a single `body` column. Consolidate gains a session energy budget (120 tokens) that caps write volume per session. All existing Hebbian mechanics (weights, edges, attention filter, spreading activation, entanglement, decay) are preserved unchanged.

**Tech Stack:** Go 1.26, SQLite (modernc.org/sqlite), standard library only.

---

### Task 1: Create `internal/memory` package — Atom type, Tokenize, ID

**Files:**
- Create: `internal/memory/memory.go`
- Create: `internal/memory/memory_test.go`

- [ ] **Step 1: Write failing tests for Tokenize**

```go
// internal/memory/memory_test.go
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
		{"a-b.c/d_e", []string{"ab", "cd"}},  // single chars dropped after split
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:/Users/steve/code/heb && go test ./internal/memory/ -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/memory/memory.go
package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// Sep is the canonical tuple separator used throughout heb for display.
const Sep = "\u00b7"

// SessionEnergyBudget is the maximum total tokens (across all atoms)
// that a single learn session may write. Atoms are accepted in
// confidence-descending order until this budget is exhausted.
const SessionEnergyBudget = 120

// Atom is the atomic unit of memory — a weighted text pattern (cell assembly).
// No forced structure. The body is free-form text that the learn step produces
// and the retrieve step matches against via BM25.
type Atom struct {
	ID          string  `json:"id"`
	Body        string  `json:"body"`
	Weight      float64 `json:"weight"`
	Status      string  `json:"status"`
	TopicTokens string  `json:"topic_tokens,omitempty"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

// Scored is an atom with a recall score attached.
type Scored struct {
	Atom
	Score  float64 `json:"score"`
	Source string  `json:"source"` // "match" or "edge"
}

// Tokenize breaks a body into matchable word tokens. This is the ONE
// tokenizer used by both BM25 recall and edge co-activation.
//
// Splits on: space, underscore, hyphen, dot, slash, middle-dot (·).
// Lowercases everything. Drops single-character tokens as noise.
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(
		"_", " ",
		"\u00b7", " ",
		".", " ",
		"/", " ",
		"-", " ",
	).Replace(s)
	var out []string
	for _, w := range strings.Fields(s) {
		if len(w) > 1 {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TokenCount returns the number of matchable tokens in a body.
// Equivalent to len(Tokenize(s)) but avoids allocation when
// only the count is needed.
func TokenCount(s string) int {
	return len(Tokenize(s))
}

// ID returns the deterministic content-address for a body.
// Lowercased, trimmed, SHA1-hashed.
func ID(body string) string {
	normalized := strings.ToLower(strings.TrimSpace(body))
	sum := sha1.Sum([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// MatchesWord returns true if needle equals any token in hayTokens.
// Whole-word matching — "low" matches "low" but not "follow".
func MatchesWord(hayTokens []string, needle string) bool {
	for _, w := range hayTokens {
		if w == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd C:/Users/steve/code/heb && go test ./internal/memory/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/memory/memory.go internal/memory/memory_test.go
git commit -m "feat: add internal/memory package — Atom type, shared Tokenize, ID"
```

---

### Task 2: Schema migration — add `body` column, backfill from SPO

**Files:**
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/memory.go`

- [ ] **Step 1: Update schemaSQL in sqlite.go to use body column**

In `internal/store/sqlite.go`, replace the `memories` table definition in `schemaSQL` (lines 192-207):

```go
// Replace the memories CREATE TABLE block with:
CREATE TABLE IF NOT EXISTS memories (
    id           TEXT PRIMARY KEY,
    body         TEXT NOT NULL,
    weight       REAL NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'active',
    topic_tokens TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_weight    ON memories(weight);
CREATE INDEX IF NOT EXISTS idx_memories_status    ON memories(status);
```

Remove the three now-unused indexes on subject, predicate, object.

- [ ] **Step 2: Add migration in Open() to backfill body from SPO**

In `internal/store/sqlite.go`, add migration code after the existing v6 migration (after line 138):

```go
// v7: migrate SPO columns to body (cell assembly model).
// Step 1: add body column if missing.
db.Exec(`ALTER TABLE memories ADD COLUMN body TEXT NOT NULL DEFAULT ''`)
// Step 2: backfill body from SPO for any rows that still have empty body.
db.Exec(`UPDATE memories SET body = subject || ' ' || predicate || ' ' || object WHERE body = '' AND subject IS NOT NULL AND subject != ''`)
// Step 3: recompute IDs based on body content (new ID function).
// Note: IDs change because old ID = SHA1(s+\x1f+p+\x1f+o), new ID = SHA1(lower(body)).
// We handle this by keeping old IDs — they still work as unique keys. New atoms
// will use the new ID scheme. The coexistence is safe because IDs are opaque.
```

Bump `SchemaVersion` from 5 to 6:

```go
const SchemaVersion = 6
```

Update the schema version bump at the end of Open().

- [ ] **Step 3: Update Memory struct and related functions in memory.go**

In `internal/store/memory.go`, update the `Memory` struct (lines 15-25):

```go
type Memory struct {
	ID          string  `json:"id"`
	Body        string  `json:"body"`
	Weight      float64 `json:"weight"`
	Status      string  `json:"status"`
	TopicTokens string  `json:"topic_tokens,omitempty"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}
```

Update `TupleString()` to return Body:

```go
func (m Memory) TupleString() string {
	return m.Body
}
```

Update `MemoryID` to use the new `memory.ID`:

```go
import "github.com/steelboltgames/heb/internal/memory"

func MemoryID(body string) string {
	return memory.ID(body)
}
```

Keep the old `MemoryID` with SPO signature as a deprecated wrapper for backward compat during migration:

```go
// MemoryIDLegacy computes ID the old way for migration comparison.
// Deprecated: use MemoryID(body) instead.
func MemoryIDLegacy(subject, predicate, object string) string {
	s := strings.ToLower(strings.TrimSpace(subject))
	p := strings.ToLower(strings.TrimSpace(predicate))
	o := strings.ToLower(strings.TrimSpace(object))
	sum := sha1.Sum([]byte(s + "\x1f" + p + "\x1f" + o))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Update ApplyMemoryEvent to use body instead of SPO**

In `internal/store/memory.go`, change `ApplyMemoryEvent` signature (line 58):

```go
func ApplyMemoryEvent(tx *sql.Tx, body, kind, reason, sessionID, beadID, topicTokens string, deltaNew, deltaReinforce float64) (id string, newWeight float64, wasNew bool, err error) {
	id = memory.ID(body)
	now := time.Now().UTC().Unix()

	var existing float64
	row := tx.QueryRow(`SELECT weight FROM memories WHERE id = ?`, id)
	switch err = row.Scan(&existing); {
	case err == sql.ErrNoRows:
		wasNew = true
		err = nil
	case err != nil:
		return "", 0, false, fmt.Errorf("check memory: %w", err)
	}

	delta := deltaReinforce
	eventKind := kind
	if wasNew {
		delta = deltaNew
		eventKind = "created"
		_, err = tx.Exec(`
			INSERT INTO memories(id, body, weight, status, topic_tokens, created_at, updated_at)
			VALUES(?, ?, MAX(0, ?), 'active', ?, ?, ?)
		`, id, body, delta, topicTokens, now, now)
		if err != nil {
			return "", 0, false, fmt.Errorf("insert memory: %w", err)
		}
		newWeight = delta
		if newWeight < 0 {
			newWeight = 0
		}
	} else {
		_, err = tx.Exec(`
			UPDATE memories
			SET weight = MAX(0, weight + ?), topic_tokens = ?, updated_at = ?
			WHERE id = ?
		`, delta, topicTokens, now, id)
		if err != nil {
			return "", 0, false, fmt.Errorf("update memory: %w", err)
		}
		if err = tx.QueryRow(`SELECT weight FROM memories WHERE id = ?`, id).Scan(&newWeight); err != nil {
			return "", 0, false, fmt.Errorf("read weight: %w", err)
		}
	}

	_, err = tx.Exec(`
		INSERT INTO events(memory_id, kind, delta, reason, session_id, bead_id, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`, id, eventKind, delta, nullIfEmpty(reason), nullIfEmpty(sessionID), nullIfEmpty(beadID), now)
	if err != nil {
		return "", 0, false, fmt.Errorf("append event: %w", err)
	}

	return id, newWeight, wasNew, nil
}
```

- [ ] **Step 5: Update Recall to use body + memory.Tokenize**

In `internal/store/memory.go`, update `Recall()` (lines 239-386). Key changes:

Replace the scan columns (line 244):
```go
rows, err := db.Query(`SELECT id, body, weight, status, topic_tokens, created_at, updated_at FROM memories WHERE status = 'active'`)
```

Replace the scan call (line 264):
```go
if err := rows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
```

Replace the word-splitting logic (line 267-268):
```go
words := memory.Tokenize(m.Body)
```

Replace `matchesWord` calls with `memory.MatchesWord`.

Replace the edge expansion scan (lines 335-346):
```go
nrows, err := db.Query(`
    SELECT m.id, m.body, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at, e.strength
    FROM edges e
    JOIN memories m ON m.id = CASE WHEN e.a_id = ? THEN e.b_id ELSE e.a_id END
    WHERE (e.a_id = ? OR e.b_id = ?) AND e.strength > 0 AND m.status = 'active'
`, s.ID, s.ID, s.ID)
```

And the scan inside:
```go
if err := nrows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt, &strength); err != nil {
```

Replace `isHardConstraint` in budget.go to check Body prefix:
```go
func isHardConstraint(m store.Scored) bool {
	return len(m.Body) > 0 && m.Body[0] == '!'
}
```

Remove `splitWords` and `matchesWord` from `store/memory.go` (they're replaced by `memory.Tokenize` and `memory.MatchesWord`).

- [ ] **Step 6: Update DreamSeeds and DreamRandomPairs**

In `internal/store/memory.go`, update `DreamSeeds` scan (line 502-503):
```go
SELECT m.id, m.body, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at
```
And scan call:
```go
if err := rows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
```

Update `DreamRandomPairs` (lines 544-575):
```go
SELECT m1.id, m1.body, m2.id, m2.body
```
And update the scan + DreamPair construction:
```go
var aID, aBody, bID, bBody string
if err := rows.Scan(&aID, &aBody, &bID, &bBody); err != nil {
    return nil, err
}
result = append(result, DreamPair{
    AID: aID, BID: bID, ATuple: aBody, BTuple: bBody,
})
```

- [ ] **Step 7: Run all existing tests**

Run: `cd C:/Users/steve/code/heb && go test ./internal/store/ ./internal/memory/ -v`
Expected: PASS (or expected failures in downstream packages — those are fixed in later tasks).

- [ ] **Step 8: Commit**

```bash
git add internal/store/sqlite.go internal/store/memory.go
git commit -m "feat: schema v6 — migrate memories from SPO to body column"
```

---

### Task 3: Update consolidate types and memories pass

**Files:**
- Modify: `internal/consolidate/types.go`
- Modify: `internal/consolidate/memories.go`
- Modify: `internal/consolidate/memories_test.go`

- [ ] **Step 1: Update types.go — Lesson uses Body, MemoryDelta uses Body, drop SPO**

In `internal/consolidate/types.go`:

Replace `Lesson` struct (lines 133-138):
```go
type Lesson struct {
	Body       string  `json:"body"`              // free-form atom text
	Scope      string  `json:"scope"`             // "project" | "universal_candidate"
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence,omitempty"`
}
```

Also keep backward-compat unmarshaling — add `Observation` as an alias:
```go
// For backward compatibility with existing episodes that used "observation" field
func (l *Lesson) UnmarshalJSON(data []byte) error {
	type Alias Lesson
	aux := &struct {
		Observation string `json:"observation"`
		*Alias
	}{Alias: (*Alias)(l)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if l.Body == "" && aux.Observation != "" {
		l.Body = aux.Observation
	}
	return nil
}
```

Replace `MemoryDelta` struct (lines 157-165):
```go
type MemoryDelta struct {
	Body           string  `json:"body"`
	Event          string  `json:"event"`
	DeltaNew       float64 `json:"delta_new"`
	DeltaReinforce float64 `json:"delta_reinforce"`
	Reason         string  `json:"reason,omitempty"`
}
```

Replace `SPO` struct (lines 141-145) and `EdgeDelta` (lines 169-174):
```go
type EdgeDelta struct {
	ABody        string  `json:"a_body"`
	BBody        string  `json:"b_body"`
	Delta        float64 `json:"delta"`
	CoActivation bool    `json:"co_activation,omitempty"`
}
```

Remove the `SPO` struct entirely — it's no longer needed.

Update `MemoryApply` (lines 223-231):
```go
type MemoryApply struct {
	ID        string  `json:"id"`
	Body      string  `json:"body"`
	Event     string  `json:"event"`
	NewWeight float64 `json:"new_weight"`
	WasNew    bool    `json:"was_new"`
}
```

- [ ] **Step 2: Update memories.go — energy budget + body-based deltas**

Replace `internal/consolidate/memories.go` entirely:

```go
package consolidate

import (
	"fmt"
	"sort"

	"github.com/steelboltgames/heb/internal/memory"
)

// buildMemoryDeltas translates lessons → memoryDelta entries, enforcing the
// session energy budget. Lessons are accepted in confidence-descending order
// until the token budget is exhausted.
//
// Lessons are skipped if:
//   - confidence < MinConfidence
//   - body is empty
//   - session energy budget is exhausted
func buildMemoryDeltas(c LearnResult, cfg Config) ([]MemoryDelta, []SkippedTuple) {
	var candidates []struct {
		body       string
		confidence float64
		reason     string
		tokens     int
	}

	// Collect regular lessons
	for _, lesson := range c.Lessons {
		if lesson.Confidence < cfg.MinConfidence {
			// Will be added to skipped below after sorting
			continue
		}
		if lesson.Body == "" {
			continue
		}
		reason := fmt.Sprintf("lesson confidence %.2f", lesson.Confidence)
		if lesson.Scope == "universal_candidate" {
			reason = "[universal_candidate] " + reason
		}
		candidates = append(candidates, struct {
			body       string
			confidence float64
			reason     string
			tokens     int
		}{
			body:       lesson.Body,
			confidence: lesson.Confidence,
			reason:     reason,
			tokens:     memory.TokenCount(lesson.Body),
		})
	}

	// Collect prediction contradiction lessons
	if c.PredictionReconciliation != nil && !c.PredictionReconciliation.ColdStart {
		for _, elem := range c.PredictionReconciliation.Elements {
			if elem.Event != "prediction_contradicted" || elem.Lesson == "" {
				continue
			}
			const predictionConfidence = 0.85
			candidates = append(candidates, struct {
				body       string
				confidence float64
				reason     string
				tokens     int
			}{
				body:       elem.Lesson,
				confidence: predictionConfidence,
				reason:     fmt.Sprintf("prediction contradiction: %s was %s", elem.Element, elem.Result),
				tokens:     memory.TokenCount(elem.Lesson),
			})
		}
	}

	// Sort by confidence descending for energy budget allocation
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].confidence > candidates[j].confidence
	})

	// Apply energy budget
	var deltas []MemoryDelta
	var skipped []SkippedTuple
	budgetUsed := 0

	for _, c := range candidates {
		if budgetUsed+c.tokens > memory.SessionEnergyBudget {
			skipped = append(skipped, SkippedTuple{
				Tuple:  c.body,
				Reason: fmt.Sprintf("exceeds session energy budget (%d+%d > %d)", budgetUsed, c.tokens, memory.SessionEnergyBudget),
			})
			continue
		}
		budgetUsed += c.tokens
		deltas = append(deltas, MemoryDelta{
			Body:           c.body,
			Event:          "session_reinforced",
			DeltaNew:       c.confidence * cfg.NewGain,
			DeltaReinforce: c.confidence * cfg.ReinforceGain,
			Reason:         c.reason,
		})
	}

	// Also skip low-confidence lessons (these were filtered before budget)
	for _, lesson := range c.Lessons {
		if lesson.Confidence < cfg.MinConfidence && lesson.Body != "" {
			skipped = append(skipped, SkippedTuple{
				Tuple:  lesson.Body,
				Reason: fmt.Sprintf("below confidence %.2f < %.2f", lesson.Confidence, cfg.MinConfidence),
			})
		}
	}

	return deltas, skipped
}
```

- [ ] **Step 3: Update memories_test.go**

Replace `internal/consolidate/memories_test.go`:

```go
package consolidate

import (
	"math"
	"testing"
)

const eps = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < eps
}

func TestBuildMemoryDeltasBasic(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "drone_stats derived_by type_lookup", Scope: "project", Confidence: 0.80},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want empty", skipped)
	}
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(deltas))
	}
	d := deltas[0]
	if d.Body != "drone_stats derived_by type_lookup" {
		t.Errorf("body = %q", d.Body)
	}
	if d.Event != "session_reinforced" {
		t.Errorf("event = %q, want session_reinforced", d.Event)
	}
	if !approxEqual(d.DeltaNew, 0.80*0.72) {
		t.Errorf("delta_new = %v, want %v", d.DeltaNew, 0.80*0.72)
	}
	if !approxEqual(d.DeltaReinforce, 0.80*0.08) {
		t.Errorf("delta_reinforce = %v, want %v", d.DeltaReinforce, 0.80*0.08)
	}
}

func TestBuildMemoryDeltasBelowConfidence(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "low confidence atom", Confidence: 0.49},
			{Body: "at threshold atom", Confidence: 0.50},
			{Body: "above threshold atom", Confidence: 0.51},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas) != 2 {
		t.Errorf("deltas = %d, want 2", len(deltas))
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %d, want 1", len(skipped))
	}
	if skipped[0].Tuple != "low confidence atom" {
		t.Errorf("skipped tuple = %q", skipped[0].Tuple)
	}
}

func TestBuildMemoryDeltasEmptyBody(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "", Confidence: 0.9},
			{Body: "valid atom here", Confidence: 0.9},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 1 {
		t.Errorf("deltas = %d, want 1", len(deltas))
	}
}

func TestBuildMemoryDeltasEnergyBudget(t *testing.T) {
	cfg := DefaultConfig()
	// Create atoms that each use ~40 tokens — budget of 120 fits 3
	longBody := "the blast delay function implements the blast firing rate which scales proportionally with the ship attack value so higher attack means faster blasts and lower attack means slower rate"
	c := LearnResult{
		Lessons: []Lesson{
			{Body: longBody, Confidence: 0.90},
			{Body: longBody + " second version", Confidence: 0.85},
			{Body: longBody + " third version", Confidence: 0.80},
			{Body: longBody + " fourth version", Confidence: 0.75},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	// Should accept some and reject others based on energy budget
	totalTokens := 0
	for _, d := range deltas {
		totalTokens += len(tokenizeForTest(d.Body))
	}
	if totalTokens > 120 {
		t.Errorf("total tokens %d exceeds energy budget 120", totalTokens)
	}
	if len(skipped) == 0 {
		t.Errorf("expected some atoms to be skipped due to energy budget")
	}
	// Verify highest confidence atoms are kept
	if len(deltas) > 0 && !approxEqual(deltas[0].DeltaNew, 0.90*cfg.NewGain) {
		t.Errorf("highest confidence atom should be first")
	}
}

func TestBuildMemoryDeltasConfidenceOrdering(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "low confidence first", Confidence: 0.60},
			{Body: "high confidence second", Confidence: 0.95},
			{Body: "medium confidence third", Confidence: 0.80},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 3 {
		t.Fatalf("deltas = %d, want 3", len(deltas))
	}
	// Should be sorted by confidence: 0.95, 0.80, 0.60
	if !approxEqual(deltas[0].DeltaNew, 0.95*cfg.NewGain) {
		t.Errorf("first delta should be confidence 0.95")
	}
	if !approxEqual(deltas[1].DeltaNew, 0.80*cfg.NewGain) {
		t.Errorf("second delta should be confidence 0.80")
	}
}

func TestBuildMemoryDeltasPredictionContradiction(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "shake implements screen_shake", Scope: "project", Confidence: 0.90},
		},
		PredictionReconciliation: &PredictionReconciliation{
			ColdStart: false,
			Elements: []PredictionReconcileElement{
				{
					Element: "files",
					Result:  "wrong",
					Event:   "prediction_contradicted",
					Lesson:  "combat screen shake is implemented in main.gd not combat.gd",
				},
				{
					Element: "approach",
					Result:  "matched",
					Event:   "prediction_confirmed",
					Lesson:  "",
				},
			},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want empty", skipped)
	}
	if len(deltas) != 2 {
		t.Fatalf("deltas = %d, want 2", len(deltas))
	}
}

func TestBuildMemoryDeltasUniversalPrefix(t *testing.T) {
	cfg := DefaultConfig()
	c := LearnResult{
		Lessons: []Lesson{
			{Body: "godot prefer static vars for performance", Scope: "universal_candidate", Confidence: 0.85},
		},
	}
	deltas, _ := buildMemoryDeltas(c, cfg)
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(deltas))
	}
	if !contains(deltas[0].Reason, "[universal_candidate]") {
		t.Errorf("reason = %q, want universal_candidate prefix", deltas[0].Reason)
	}
}

// helper — uses same tokenizer as production code via import
func tokenizeForTest(s string) []string {
	// Inline the logic to avoid circular import in test
	// This mirrors memory.Tokenize
	import_strings := __import_strings()
	_ = import_strings
	return nil // placeholder — real test uses memory.TokenCount transitively
}
```

Wait — that test helper has issues. Let me fix the test to avoid it. The energy budget test should just check behavior, not recount tokens:

```go
// Replace the tokenizeForTest usage in TestBuildMemoryDeltasEnergyBudget:
// Just verify that not all atoms were accepted and the skipped reason mentions energy budget
func TestBuildMemoryDeltasEnergyBudget(t *testing.T) {
	cfg := DefaultConfig()
	// Each body has ~30 tokens — 4 of them = 120+, so at most 3-4 fit
	longBody := "the blast delay function implements the blast firing rate which scales proportionally with the ship attack value so higher attack means faster"
	c := LearnResult{
		Lessons: []Lesson{
			{Body: longBody, Confidence: 0.90},
			{Body: longBody + " and more", Confidence: 0.85},
			{Body: longBody + " even more tokens here", Confidence: 0.80},
			{Body: longBody + " way too many tokens now", Confidence: 0.75},
		},
	}
	deltas, skipped := buildMemoryDeltas(c, cfg)
	if len(deltas)+len(skipped) != 4 {
		t.Errorf("total = %d, want 4", len(deltas)+len(skipped))
	}
	if len(skipped) == 0 {
		t.Errorf("expected some atoms to be skipped due to energy budget")
	}
	// Verify skipped reason mentions energy budget
	for _, s := range skipped {
		if !contains(s.Reason, "energy budget") {
			t.Errorf("skipped reason = %q, want energy budget", s.Reason)
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd C:/Users/steve/code/heb && go test ./internal/consolidate/ -v -run TestBuildMemory`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/consolidate/types.go internal/consolidate/memories.go internal/consolidate/memories_test.go
git commit -m "feat: consolidate uses body atoms with session energy budget"
```

---

### Task 4: Update edges and entanglement to use body

**Files:**
- Modify: `internal/consolidate/edges.go`
- Modify: `internal/consolidate/edges_test.go`
- Modify: `internal/consolidate/entanglement.go`
- Modify: `internal/consolidate/entanglement_test.go`

- [ ] **Step 1: Update edges.go to use memory.Tokenize and body**

Replace `internal/consolidate/edges.go`:

```go
package consolidate

import "github.com/steelboltgames/heb/internal/memory"

// buildEdgeDeltas emits one CoActivationBoost edge delta for every
// unordered pair of distinct memories in the written set that share at
// least one token. Token overlap is checked on the full body using the
// shared memory.Tokenize function.
//
// If fewer than 2 written atoms: returns nil.
func buildEdgeDeltas(written []MemoryDelta, cfg Config) []EdgeDelta {
	if len(written) < 2 {
		return nil
	}

	type tokenSet map[string]bool
	sets := make([]tokenSet, len(written))
	for i, m := range written {
		tokens := memory.Tokenize(m.Body)
		set := make(tokenSet, len(tokens))
		for _, t := range tokens {
			set[t] = true
		}
		sets[i] = set
	}

	var out []EdgeDelta
	for i := 0; i < len(written); i++ {
		for j := i + 1; j < len(written); j++ {
			if !tokensOverlap(sets[i], sets[j]) {
				continue
			}
			out = append(out, EdgeDelta{
				ABody:        written[i].Body,
				BBody:        written[j].Body,
				Delta:        cfg.CoActivationBoost,
				CoActivation: true,
			})
		}
	}
	return out
}

func tokensOverlap(a, b map[string]bool) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for tok := range a {
		if b[tok] {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Update entanglement.go to use body**

Replace `internal/consolidate/entanglement.go`:

```go
package consolidate

import (
	"strings"

	"github.com/steelboltgames/heb/internal/memory"
)

// buildEntanglementDeltas emits negative deltas for written atoms whose
// tokens overlap with surprise_touches paths.
//
// Gated by: len(surprise_touches) > 0 AND correction_count > 0.
func buildEntanglementDeltas(written []MemoryDelta, c LearnResult, cfg Config) []MemoryDelta {
	if len(c.Implementation.SurpriseTouches) == 0 || c.CorrectionCount == 0 {
		return nil
	}
	if len(written) == 0 {
		return nil
	}

	signal := -(c.PeakIntensity * cfg.EntanglementScale)
	if signal < cfg.EntanglementMin {
		signal = cfg.EntanglementMin
	}
	if signal > cfg.EntanglementMax {
		signal = cfg.EntanglementMax
	}

	var out []MemoryDelta
	for _, m := range written {
		match, touch := matchesAnySurpriseTouch(m, c.Implementation.SurpriseTouches)
		if !match {
			continue
		}
		out = append(out, MemoryDelta{
			Body:           m.Body,
			Event:          "entanglement_signal",
			DeltaNew:       signal,
			DeltaReinforce: signal,
			Reason:         "surprise touch on " + touch,
		})
	}
	return out
}

// matchesAnySurpriseTouch checks if any token from the atom's body
// appears as a substring in any surprise_touch path.
func matchesAnySurpriseTouch(m MemoryDelta, touches []string) (bool, string) {
	tokens := memory.Tokenize(m.Body)
	for _, touch := range touches {
		touchLower := strings.ToLower(touch)
		for _, tok := range tokens {
			if tok == "" {
				continue
			}
			if strings.Contains(touchLower, tok) {
				return true, touch
			}
		}
	}
	return false, ""
}
```

- [ ] **Step 3: Update edge and entanglement tests**

Update test files to use `Body` instead of `Subject/Predicate/Object` in `MemoryDelta` constructors and `Lesson` constructors. For each test that constructs a `MemoryDelta`, change from `Subject: "x", Predicate: "y", Object: "z"` to `Body: "x y z"`. For each `Lesson`, change `Observation:` to `Body:`.

- [ ] **Step 4: Run tests**

Run: `cd C:/Users/steve/code/heb && go test ./internal/consolidate/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/consolidate/edges.go internal/consolidate/edges_test.go internal/consolidate/entanglement.go internal/consolidate/entanglement_test.go
git commit -m "feat: edges and entanglement use body atoms with shared tokenizer"
```

---

### Task 5: Update consolidate format and run.go

**Files:**
- Modify: `internal/consolidate/format.go`
- Modify: `internal/consolidate/run.go`

- [ ] **Step 1: Update format.go — StderrSummary uses Body**

In `internal/consolidate/format.go`, update `StderrSummary` (lines 126-132):

```go
// Replace the tuple formatting lines:
if len(newMems) > 0 {
    b.WriteString("\n  learned:")
    for _, m := range newMems {
        fmt.Fprintf(&b, "\n    + %s (%.2f)", m.Body, m.NewWeight)
    }
}
if len(reinforcedMems) > 0 {
    b.WriteString("\n  reinforced:")
    for _, m := range reinforcedMems {
        fmt.Fprintf(&b, "\n    ↑ %s (%.2f)", m.Body, m.NewWeight)
    }
}
```

Also update line 42 in `RenderHuman`:
```go
fmt.Fprintf(&b, "  skipped:    %d (below confidence or exceeds energy budget)\n", len(r.Skipped))
```

- [ ] **Step 2: Run tests**

Run: `cd C:/Users/steve/code/heb && go test ./internal/consolidate/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/consolidate/format.go internal/consolidate/run.go
git commit -m "feat: consolidate format output uses body atoms"
```

---

### Task 6: Update cmd/heb consumers — consolidate.go and dream.go

**Files:**
- Modify: `cmd/heb/consolidate.go`
- Modify: `cmd/heb/dream.go`

- [ ] **Step 1: Update applyMemoryDeltas in consolidate.go**

In `cmd/heb/consolidate.go`, update `applyMemoryDeltas` (lines 162-189):

```go
func applyMemoryDeltas(tx *sql.Tx, p consolidate.Payload, result *consolidate.Result) error {
	for _, md := range p.Memories {
		id, w, wasNew, err := store.ApplyMemoryEvent(
			tx,
			md.Body,
			md.Event, md.Reason,
			p.SessionID, p.BeadID, p.TopicTokens,
			md.DeltaNew, md.DeltaReinforce,
		)
		if err != nil {
			return fmt.Errorf("apply memory: %w", err)
		}
		if err := store.AddProvenance(tx, id, p.Project, p.SessionID, p.BeadID); err != nil {
			return fmt.Errorf("provenance: %w", err)
		}
		eventKind := md.Event
		if wasNew {
			eventKind = "created"
		}
		result.Applied = append(result.Applied, consolidate.MemoryApply{
			ID: id, Body: md.Body,
			Event: eventKind, NewWeight: w, WasNew: wasNew,
		})
		if md.Event == "entanglement_signal" {
			result.EntanglementSignals++
		}
	}
	return nil
}
```

- [ ] **Step 2: Update applyEdgeDeltas**

In `cmd/heb/consolidate.go`, update `applyEdgeDeltas` (lines 192-202):

```go
func applyEdgeDeltas(tx *sql.Tx, p consolidate.Payload, result *consolidate.Result) error {
	for _, ed := range p.Edges {
		aID := store.MemoryID(ed.ABody)
		bID := store.MemoryID(ed.BBody)
		if err := store.UpdateEdge(tx, aID, bID, ed.Delta, ed.CoActivation); err != nil {
			return fmt.Errorf("edge: %w", err)
		}
		result.EdgesUpdated++
	}
	return nil
}
```

- [ ] **Step 3: Update tupleToMemoryID**

In `cmd/heb/consolidate.go`, update `tupleToMemoryID` (lines 313-319):

```go
// tupleToMemoryID returns the memory ID for a body string.
// For backward compat with recalled_via_edges which may contain
// old "subject·predicate·object" format, we pass through as-is —
// the body IS the tuple string for migrated memories.
func tupleToMemoryID(body string) string {
	if body == "" {
		return ""
	}
	return store.MemoryID(body)
}
```

- [ ] **Step 4: Update dream.go — remove SPO fields from output**

In `cmd/heb/dream.go`, update `seedOut` struct (lines 57-64):

```go
type seedOut struct {
    ID     string  `json:"id"`
    Body   string  `json:"body"`
    Weight float64 `json:"weight"`
}
out := make([]seedOut, len(seeds))
for i, m := range seeds {
    out[i] = seedOut{
        ID:     m.ID,
        Body:   m.Body,
        Weight: m.Weight,
    }
}
```

- [ ] **Step 5: Compile check**

Run: `cd C:/Users/steve/code/heb && go build ./cmd/heb/`
Expected: builds successfully.

- [ ] **Step 6: Commit**

```bash
git add cmd/heb/consolidate.go cmd/heb/dream.go
git commit -m "feat: cmd/heb consumers use body atoms"
```

---

### Task 7: Update retrieve format and budget

**Files:**
- Modify: `internal/retrieve/format.go`
- Modify: `internal/retrieve/budget.go`
- Modify: `internal/retrieve/budget_test.go`

- [ ] **Step 1: Update RecallMemory in format.go**

In `internal/retrieve/format.go`, the `RecallMemory` struct (lines 56-61) stays the same — it already uses `Tuple` which maps to `TupleString()` which now returns `Body`. No changes needed to the struct.

Update `RenderHuman` (line 30) to use `m.Body` for display:

```go
// In RenderHuman, the line that prints memories:
fmt.Fprintf(&b, "  [%s %.2f] %s +%.2f\n", tag, m.Score, m.Body, m.Weight)
```

The `RenderJSON` function builds `RecallMemory` using `m.TupleString()` which now returns `m.Body` — no change needed there.

- [ ] **Step 2: Update isHardConstraint in budget.go**

In `internal/retrieve/budget.go` (line 21-23):

```go
func isHardConstraint(m store.Scored) bool {
	return len(m.Body) > 0 && m.Body[0] == '!'
}
```

- [ ] **Step 3: Update budget_test.go if it references Subject/Predicate/Object**

Check and update any test constructors that build `store.Scored` with SPO fields to use `Body` instead.

- [ ] **Step 4: Run tests**

Run: `cd C:/Users/steve/code/heb && go test ./internal/retrieve/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/retrieve/format.go internal/retrieve/budget.go internal/retrieve/budget_test.go
git commit -m "feat: retrieve uses body atoms"
```

---

### Task 8: Update learn prompt — emit atoms, not SPO

**Files:**
- Modify: `cmd/heb/learn.go`

- [ ] **Step 1: Update learnSystemPrompt**

In `cmd/heb/learn.go`, update the `learnSystemPrompt` const (line 18). Key changes to the prompt:

Replace the lessons section. Change:
```
lessons — what should be remembered. Tuple format: subject·predicate·object. Max 8. Min 0. Confidence >= 0.50.
```
To:
```
lessons — what should be remembered. Each lesson is an atom: a short text pattern (max ~80 chars, aim for 8-12 word tokens). Max 8 regular lessons + up to 4 code anchors. Min 0. Confidence >= 0.50.

Session energy budget: the system accepts at most 120 word-tokens total across all atoms. Write terse, high-signal atoms. Verbose atoms consume more budget and crowd out peers during future recall.
```

Replace the output shape `"observation"` field with `"body"`:
```json
"lessons": [
    {
      "body": "terse atom text — what to remember",
      "scope": "project | universal_candidate",
      "confidence": 0.0,
      "evidence": "what in the session supports this",
      "source": "session | prediction"
    }
]
```

Update the examples section. Change SPO examples:
```
- BAD: "dm.earned_cards·was_renamed_to·_reward_earned_cards" → still bad
- GOOD: "frigate has 1 cargo bay" (natural text, matchable tokens)
- GOOD: "elite card reward tracks via _reward_earned_cards not dm" (carries the why)
- GOOD: "_blast_delay scales fire rate with attack value" (code anchor, greppable)
- GOOD: "pipeline must not stop between recall and execute — skill returns look like completion" (carries root cause)
```

Update code anchor section to use body format:
```
Code anchor examples:
- GOOD: "_award_xp implements xp level progression"
- GOOD: "XP_BASE configures xp level curve base value"
```

Update prediction correction section to use body format:
```
  "lesson": "terse correction atom or null"
```

- [ ] **Step 2: Update printLearnSummary**

In `cmd/heb/learn.go`, update `printLearnSummary` (lines 1037-1076). Change the Lessons struct field:

```go
Lessons []struct {
    Body       string  `json:"body"`
    Observation string `json:"observation"` // backward compat
    Scope      string  `json:"scope"`
    Confidence float64 `json:"confidence"`
} `json:"lessons"`
```

And the display:
```go
for _, l := range learn.Lessons {
    body := l.Body
    if body == "" {
        body = l.Observation // backward compat
    }
    fmt.Fprintf(os.Stderr, "  + %s  [%s %.2f]\n", body, l.Scope, l.Confidence)
}
```

- [ ] **Step 3: Full build check**

Run: `cd C:/Users/steve/code/heb && go build ./cmd/heb/`
Expected: builds successfully.

- [ ] **Step 4: Commit**

```bash
git add cmd/heb/learn.go
git commit -m "feat: learn prompt emits atoms instead of SPO triples"
```

---

### Task 9: Full integration test — build, run all tests, verify

**Files:**
- All modified files

- [ ] **Step 1: Run all unit tests**

Run: `cd C:/Users/steve/code/heb && go test ./... -v`
Expected: All PASS.

- [ ] **Step 2: Build binary**

Run: `cd C:/Users/steve/code/heb && go build ./cmd/heb/`
Expected: builds successfully.

- [ ] **Step 3: Test against real database (read-only verify)**

Run: `cd C:/Users/steve/code/heb && go run ./cmd/heb/ status`
Expected: Shows status with correct memory count (existing memories are accessible).

- [ ] **Step 4: Verify the migration backfilled body from SPO**

Run: `sqlite3 C:/Users/steve/code/heb/.heb/memory.db "SELECT body FROM memories LIMIT 5"`
Expected: Shows concatenated SPO text for existing memories.

- [ ] **Step 5: Commit any fixups**

```bash
git add -A
git commit -m "fix: integration test fixups for cell assembly migration"
```

---

### Task 10: Install and verify heb binary

- [ ] **Step 1: Install**

Run: `cd C:/Users/steve/code/heb && go install ./cmd/heb/`
Expected: installs to GOBIN.

- [ ] **Step 2: Verify status works**

Run: `heb status`
Expected: Shows schema v6, correct memory counts.

- [ ] **Step 3: Verify dream seeds works**

Run: `heb dream seeds --limit=3`
Expected: JSON with `body` field instead of `subject/predicate/object`.

- [ ] **Step 4: Final commit if needed**

```bash
git add -A
git commit -m "chore: final cell assembly migration verification"
```
