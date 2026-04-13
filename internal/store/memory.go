package store

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Memory is a weighted subject-predicate-object tuple.
type Memory struct {
	ID          string  `json:"id"`
	Subject     string  `json:"subject"`
	Predicate   string  `json:"predicate"`
	Object      string  `json:"object"`
	Weight      float64 `json:"weight"`
	Status      string  `json:"status"`
	TopicTokens string  `json:"topic_tokens,omitempty"` // comma-separated sense tokens from the session that created/last reinforced this memory
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

// Scored is a memory with a recall score attached.
type Scored struct {
	Memory
	Tuple  string  `json:"tuple"`
	Score  float64 `json:"score"`
	Source string  `json:"source"` // "match" or "edge"
}

// TupleString joins subject/predicate/object with the U+00B7 separator
// used throughout the slash command layer.
func (m Memory) TupleString() string {
	return m.Subject + "\u00b7" + m.Predicate + "\u00b7" + m.Object
}

// MemoryID is the deterministic ID for a SPO tuple. Lowercased,
// trimmed, joined with U+001F (unit separator), sha1'd.
func MemoryID(subject, predicate, object string) string {
	s := strings.ToLower(strings.TrimSpace(subject))
	p := strings.ToLower(strings.TrimSpace(predicate))
	o := strings.ToLower(strings.TrimSpace(object))
	sum := sha1.Sum([]byte(s + "\x1f" + p + "\x1f" + o))
	return hex.EncodeToString(sum[:])
}

// ApplyMemoryEvent upserts the memory, appends an event row, and
// returns the new weight. Runs in the passed transaction.
//
// If the memory is new, deltaNew is applied and the event is stamped
// "created". If the memory already exists, deltaReinforce is applied
// and the passed kind is used as-is. Callers that want a single delta
// regardless of existence can pass the same value for both.
func ApplyMemoryEvent(tx *sql.Tx, subject, predicate, object, kind, reason, sessionID, beadID, topicTokens string, deltaNew, deltaReinforce float64) (id string, newWeight float64, wasNew bool, err error) {
	id = MemoryID(subject, predicate, object)
	now := time.Now().UTC().Unix()

	// Detect existence first so we know which delta + event kind to use.
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
			INSERT INTO memories(id, subject, predicate, object, weight, status, topic_tokens, created_at, updated_at)
			VALUES(?, ?, ?, ?, MAX(0, ?), 'active', ?, ?, ?)
		`, id, subject, predicate, object, delta, topicTokens, now, now)
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

// AddProvenance adds a provenance row for a memory if not already present
// for this (memory, project, session) triple.
func AddProvenance(tx *sql.Tx, memoryID, project, sessionID, beadID string) error {
	now := time.Now().UTC().Unix()
	_, err := tx.Exec(`
		INSERT INTO provenance(memory_id, project, session_id, bead_id, created_at)
		SELECT ?, ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM provenance
			WHERE memory_id = ? AND project = ?
			  AND COALESCE(session_id,'') = COALESCE(?, '')
		)
	`, memoryID, project, nullIfEmpty(sessionID), nullIfEmpty(beadID), now,
		memoryID, project, nullIfEmpty(sessionID))
	return err
}

// UpdateEdge bumps the strength of the edge between two memories.
// Canonicalises (a,b) so smaller id is always "a". When coActivation
// is true, also increments co_activation_count (earned reinforcement).
func UpdateEdge(tx *sql.Tx, aID, bID string, delta float64, coActivation bool) error {
	if aID == bID {
		return nil
	}
	if aID > bID {
		aID, bID = bID, aID
	}
	now := time.Now().UTC().Unix()
	var countDelta int
	if coActivation {
		countDelta = 1
	}
	_, err := tx.Exec(`
		INSERT INTO edges(a_id, b_id, strength, co_activation_count, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(a_id, b_id) DO UPDATE SET
			strength            = MAX(0, edges.strength + excluded.strength),
			co_activation_count = edges.co_activation_count + excluded.co_activation_count,
			updated_at          = excluded.updated_at
	`, aID, bID, delta, countDelta, now)
	return err
}

// EdgeInfo is a lightweight edge record for decay queries.
type EdgeInfo struct {
	AID               string
	BID               string
	Strength          float64
	CoActivationCount int
}

// EdgesFor returns all edges involving the given memory ID.
func EdgesFor(db interface{ Query(string, ...any) (*sql.Rows, error) }, memoryID string) ([]EdgeInfo, error) {
	rows, err := db.Query(`
		SELECT a_id, b_id, strength, co_activation_count
		FROM edges
		WHERE (a_id = ? OR b_id = ?) AND strength > 0
	`, memoryID, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EdgeInfo
	for rows.Next() {
		var e EdgeInfo
		if err := rows.Scan(&e.AID, &e.BID, &e.Strength, &e.CoActivationCount); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// DecayEdge applies a negative delta to a specific edge. Does not
// increment co_activation_count. Only affects existing edges.
func DecayEdge(tx *sql.Tx, aID, bID string, delta float64) error {
	if aID == bID {
		return nil
	}
	if aID > bID {
		aID, bID = bID, aID
	}
	now := time.Now().UTC().Unix()
	_, err := tx.Exec(`
		UPDATE edges SET
			strength   = MAX(0, strength + ?),
			updated_at = ?
		WHERE a_id = ? AND b_id = ?
	`, delta, now, aID, bID)
	return err
}

// RecallLimit is the hard cap on memories returned by Recall.
// 16 ≈ 2× human working memory capacity (~7±2). Small enough that
// token-budget trimming is unnecessary; large enough to carry context.
const RecallLimit = 16

// Recall ranks memories against a token set and expands via edges.
//
// Scoring uses intersection bonus + IDF rarity weighting:
//   - Each token hit is weighted by rarity: 1.0 / log2(1 + memoriesContainingToken)
//   - Hits are combined with a quadratic intersection bonus: (Σ rarity)² / totalTokens
//   - Final score: weight + intersectionBonus
//
// Spreading activation: for top seeds, fetch neighbours via edges and
// add them with score = neighbour.weight * 0.5 + edge_strength * 0.5.
//
// Results are capped at RecallLimit (16). No token-budget trimming.
func Recall(db *sql.DB, tokens []string, limit int) ([]Scored, error) {
	if limit <= 0 || limit > RecallLimit {
		limit = RecallLimit
	}

	rows, err := db.Query(`SELECT id, subject, predicate, object, weight, status, topic_tokens, created_at, updated_at FROM memories WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("scan memories: %w", err)
	}
	defer rows.Close()

	// First pass: load all active memories and count token frequency for IDF.
	type candidate struct {
		mem  Memory
		hits []int // indices into tokens that matched
	}
	var all []candidate
	tokenFreq := make([]int, len(tokens)) // how many memories contain each token

	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Subject, &m.Predicate, &m.Object, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		hay := strings.ToLower(m.Subject + " " + m.Predicate + " " + m.Object)
		var hits []int
		for i, t := range tokens {
			tt := strings.ToLower(strings.TrimSpace(t))
			if tt != "" && strings.Contains(hay, tt) {
				hits = append(hits, i)
				tokenFreq[i]++
			}
		}
		if len(hits) == 0 && len(tokens) > 0 {
			continue
		}
		all = append(all, candidate{mem: m, hits: hits})
	}

	// Compute IDF weights: rare tokens score higher.
	idf := make([]float64, len(tokens))
	for i, freq := range tokenFreq {
		idf[i] = 1.0 / math.Log2(float64(2+freq)) // +2 avoids div-by-zero and log(1)=0
	}

	// Second pass: score with intersection bonus.
	totalTokens := float64(len(tokens))
	if totalTokens == 0 {
		totalTokens = 1
	}

	scored := make([]Scored, 0, len(all))
	for _, c := range all {
		var raritySum float64
		for _, idx := range c.hits {
			raritySum += idf[idx]
		}
		intersectionBonus := (raritySum * raritySum) / totalTokens
		score := c.mem.Weight + intersectionBonus
		scored = append(scored, Scored{Memory: c.mem, Tuple: c.mem.TupleString(), Score: score, Source: "match"})
	}

	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if len(scored) > limit {
		scored = scored[:limit]
	}

	// Spreading activation: one hop, top up to limit more.
	seen := make(map[string]bool, len(scored))
	for _, s := range scored {
		seen[s.ID] = true
	}
	var neighbours []Scored
	for _, s := range scored {
		nrows, err := db.Query(`
			SELECT m.id, m.subject, m.predicate, m.object, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at, e.strength
			FROM edges e
			JOIN memories m ON m.id = CASE WHEN e.a_id = ? THEN e.b_id ELSE e.a_id END
			WHERE (e.a_id = ? OR e.b_id = ?) AND e.strength > 0 AND m.status = 'active'
		`, s.ID, s.ID, s.ID)
		if err != nil {
			return nil, fmt.Errorf("edge expand: %w", err)
		}
		for nrows.Next() {
			var m Memory
			var strength float64
			if err := nrows.Scan(&m.ID, &m.Subject, &m.Predicate, &m.Object, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt, &strength); err != nil {
				nrows.Close()
				return nil, err
			}
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			neighbours = append(neighbours, Scored{
				Memory: m,
				Tuple:  m.TupleString(),
				Score:  m.Weight*0.5 + strength*0.5,
				Source: "edge",
			})
		}
		nrows.Close()
	}
	sort.Slice(neighbours, func(i, j int) bool { return neighbours[i].Score > neighbours[j].Score })

	// Cap total (match + edge) at RecallLimit.
	combined := append(scored, neighbours...)
	if len(combined) > limit {
		// Keep hard constraints, then top-scored.
		var pinned, rest []Scored
		for _, s := range combined {
			if strings.HasPrefix(s.Subject, "!") {
				pinned = append(pinned, s)
			} else {
				rest = append(rest, s)
			}
		}
		sort.Slice(rest, func(i, j int) bool { return rest[i].Score > rest[j].Score })
		remaining := limit - len(pinned)
		if remaining < 0 {
			remaining = 0
		}
		if len(rest) > remaining {
			rest = rest[:remaining]
		}
		combined = append(pinned, rest...)
	}
	return combined, nil
}


func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// WriteEpisode stores a session episode. Idempotent on session_id —
// if the episode already exists, it is left untouched.
func WriteEpisode(tx *sql.Tx, sessionID, payload string) (bool, error) {
	if sessionID == "" || payload == "" {
		return false, nil
	}
	now := time.Now().UTC().Unix()
	res, err := tx.Exec(`
		INSERT INTO episodes(session_id, payload, created_at)
		VALUES(?, ?, ?)
		ON CONFLICT(session_id) DO NOTHING
	`, sessionID, payload, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PurgeMemories deletes memories by ID. Cascades to events, provenance
// via foreign keys; edges cleaned up explicitly.
func PurgeMemories(db *sql.DB, ids []string) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	deleted := 0
	for _, id := range ids {
		// Clean up edges (FK cascade may handle, but be explicit)
		tx.Exec(`DELETE FROM edges WHERE a_id = ? OR b_id = ?`, id, id)
		// Delete memory (cascades to events, provenance via FK)
		res, err := tx.Exec(`DELETE FROM memories WHERE id = ?`, id)
		if err != nil {
			return deleted, fmt.Errorf("delete %s: %w", id, err)
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return deleted, nil
}

// DreamSeeds returns candidate seed memories for dreaming, prioritised by:
// 1. Isolated nodes (no edges) first
// 2. Low reinforcement count (few events)
// 3. Recently updated
func DreamSeeds(db *sql.DB, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := db.Query(`
		SELECT m.id, m.subject, m.predicate, m.object, m.weight, m.status,
		       m.topic_tokens, m.created_at, m.updated_at
		FROM memories m
		LEFT JOIN edges e ON m.id = e.a_id OR m.id = e.b_id
		LEFT JOIN (
			SELECT memory_id, COUNT(*) AS cnt FROM events GROUP BY memory_id
		) ev ON m.id = ev.memory_id
		WHERE m.status = 'active'
		ORDER BY (e.a_id IS NULL) DESC,
		         COALESCE(ev.cnt, 0) ASC,
		         m.updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("dream seeds: %w", err)
	}
	defer rows.Close()

	var result []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Subject, &m.Predicate, &m.Object, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, nil
}

// DreamPair is a candidate pair for random dreaming.
type DreamPair struct {
	AID    string `json:"a_id"`
	BID    string `json:"b_id"`
	ATuple string `json:"a_tuple"`
	BTuple string `json:"b_tuple"`
}

// DreamRandomPairs returns random memory pairs with no existing edge.
func DreamRandomPairs(db *sql.DB, limit int) ([]DreamPair, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := db.Query(`
		SELECT m1.id, m1.subject, m1.predicate, m1.object,
		       m2.id, m2.subject, m2.predicate, m2.object
		FROM memories m1, memories m2
		WHERE m1.id < m2.id
		  AND m1.status = 'active' AND m2.status = 'active'
		  AND NOT EXISTS (
			SELECT 1 FROM edges WHERE a_id = m1.id AND b_id = m2.id
		  )
		ORDER BY RANDOM()
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("dream random pairs: %w", err)
	}
	defer rows.Close()

	var result []DreamPair
	for rows.Next() {
		var aID, aSub, aPred, aObj string
		var bID, bSub, bPred, bObj string
		if err := rows.Scan(&aID, &aSub, &aPred, &aObj, &bID, &bSub, &bPred, &bObj); err != nil {
			return nil, err
		}
		result = append(result, DreamPair{
			AID:    aID,
			BID:    bID,
			ATuple: aSub + "\u00b7" + aPred + "\u00b7" + aObj,
			BTuple: bSub + "\u00b7" + bPred + "\u00b7" + bObj,
		})
	}
	return result, nil
}

// DreamStats returns dream-specific statistics.
type DreamStats struct {
	DreamMemories  int   `json:"dream_memories"`
	TentativeEdges int   `json:"tentative_edges"`
	LastDream      int64 `json:"last_dream"`
}

func (s *SQLiteStore) DreamStats() (DreamStats, error) {
	var ds DreamStats
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT memory_id) FROM events WHERE kind='dream_edge'`).Scan(&ds.DreamMemories)
	// Tentative edges: edges at dream-level strength (<=0.05)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE strength > 0 AND strength <= 0.05`).Scan(&ds.TentativeEdges)
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(created_at), 0) FROM events WHERE kind='dream_edge'`).Scan(&ds.LastDream)
	return ds, nil
}

// Stats summarises the store contents.
type Stats struct {
	Backend       string `json:"backend"`
	SchemaVersion int    `json:"schema_version"`
	Memories      int    `json:"memories"`
	Active        int    `json:"active"`
	Edges         int    `json:"edges"`
	Events        int    `json:"events"`
	Episodes      int    `json:"episodes"`
	LastActivity  int64  `json:"last_activity"`
}

func (s *SQLiteStore) Stats() (Stats, error) {
	st := Stats{Backend: "sqlite", SchemaVersion: s.SchemaVersion()}
	q := func(sql string) int {
		var n int
		_ = s.db.QueryRow(sql).Scan(&n)
		return n
	}
	st.Memories = q(`SELECT COUNT(*) FROM memories`)
	st.Active = q(`SELECT COUNT(*) FROM memories WHERE status='active'`)
	st.Edges = q(`SELECT COUNT(*) FROM edges`)
	st.Events = q(`SELECT COUNT(*) FROM events`)
	st.Episodes = q(`SELECT COUNT(*) FROM episodes`)
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(created_at), 0) FROM events`).Scan(&st.LastActivity)
	return st, nil
}
