package store

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/memory"
)

// Memory is a weighted text pattern (cell assembly).
type Memory struct {
	ID          string  `json:"id"`
	Body        string  `json:"body"`
	Weight      float64 `json:"weight"`
	Status      string  `json:"status"`
	TopicTokens string  `json:"topic_tokens,omitempty"` // comma-separated sense tokens from the session that created/last reinforced this memory
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

// Scored is a memory with a recall score attached.
type Scored struct {
	Memory
	Score  float64 `json:"score"`
	Source string  `json:"source"` // "match" or "edge"
}

// TupleString returns the body text (kept for API compatibility).
func (m Memory) TupleString() string {
	return m.Body
}

// MemoryID returns the deterministic content-address for a body.
func MemoryID(body string) string {
	return memory.ID(body)
}

// MemoryIDLegacy computes ID the old way for migration comparison.
// Deprecated: use MemoryID(body) instead.
func MemoryIDLegacy(subject, predicate, object string) string {
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
func ApplyMemoryEvent(tx *sql.Tx, body, kind, reason, sessionID, beadID, topicTokens, commitHash string, deltaNew, deltaReinforce float64) (id string, newWeight float64, wasNew bool, err error) {
	id = memory.ID(body)
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
		INSERT INTO events(memory_id, kind, delta, reason, session_id, bead_id, commit_hash, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	`, id, eventKind, delta, nullIfEmpty(reason), nullIfEmpty(sessionID), nullIfEmpty(beadID), nullIfEmpty(commitHash), now)
	if err != nil {
		return "", 0, false, fmt.Errorf("append event: %w", err)
	}

	return id, newWeight, wasNew, nil
}

// AppendEvent writes a single event row without touching memory weight.
// Used for audit-only event kinds (e.g. prediction_confirmed logged
// before the reinforcement pathway is wired up, or traceability rows
// that reference a memory without modifying it).
//
// Returns sql.ErrNoRows if the memory_id does not exist — the FK would
// fail anyway; callers that want to skip missing memories should check
// existence first and not call this.
func AppendEvent(tx *sql.Tx, memoryID, kind, reason, sessionID, beadID, commitHash string, delta float64) error {
	now := time.Now().UTC().Unix()
	_, err := tx.Exec(`
		INSERT INTO events(memory_id, kind, delta, reason, session_id, bead_id, commit_hash, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	`, memoryID, kind, delta, nullIfEmpty(reason), nullIfEmpty(sessionID), nullIfEmpty(beadID), nullIfEmpty(commitHash), now)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

// MemoryExists reports whether a memory row exists for the given ID.
// Used before AppendEvent to skip source_tuples that don't resolve to a
// real memory (reflect sometimes emits bodies that never made it into
// the graph).
func MemoryExists(tx *sql.Tx, memoryID string) (bool, error) {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM memories WHERE id = ?`, memoryID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check memory: %w", err)
	}
	return true, nil
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
const RecallLimit = 16

// BM25 scoring is delegated to memory.BM25Rank. Constants live in
// the memory package: BM25K1, BM25B, RecencyHalfLifeDays, RecencyInfluence.


// attentionMinGap is the minimum score ratio between consecutive results
// that counts as a "significant drop." The attention filter cuts at the
// last gap >= this value, mimicking competitive inhibition in recall.
const attentionMinGap = 1.20

// attentionMinKeep is the minimum number of results the attention filter
// will return, even if every gap is significant.
const attentionMinKeep = 2

// Recall ranks memories against a token set using BM25 scoring and
// an attention filter that drops low-relevance tail results.
//
// BM25 scoring (delegated to memory.BM25Rank):
//   - IDF: ln((N - df + 0.5) / (df + 0.5) + 1)  — rare tokens dominate
//   - TF saturation: tf*(k1+1) / (tf + k1*(1-b+b*dl/avgdl))
//   - Combined: bm25_relevance × (0.7 + 0.3 × weight) × (0.85 + 0.15 × recency)
//     Weight is a mild tiebreaker (±30%), not the dominant signal.
//   - Recency: 1/(1 + ageDays/30) — uses UpdatedAt so reinforced memories
//     stay fresh. ±15% influence; breaks ties toward recent memories.
//
// Attention filter: scans ranked results for gaps where score[i-1]/score[i]
// >= attentionMinGap. Cuts at the last such gap. Returns at least
// attentionMinKeep results even if every gap qualifies.
//
// Spreading activation: after the attention filter, one-hop edge
// expansion adds neighbours not already in the result set.
// Recall returns scored memories plus the theoretical score ceiling for
// the query (Σ IDF × (K1+1), skipping zero-df tokens). The ceiling lets
// callers normalise Score into a stable [0, ~0.5] practical range so
// downstream consumers (LLMs) have a query-independent frame of reference.
func Recall(db *sql.DB, tokens []string, limit int, project string) ([]Scored, float64, error) {
	if limit <= 0 || limit > RecallLimit {
		limit = RecallLimit
	}

	// Filter memories by project via provenance table.
	var rows *sql.Rows
	var err error
	if project != "" {
		rows, err = db.Query(`
			SELECT DISTINCT m.id, m.body, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at
			FROM memories m
			JOIN provenance p ON m.id = p.memory_id
			WHERE m.status = 'active' AND p.project = ?
		`, project)
	} else {
		rows, err = db.Query(`SELECT id, body, weight, status, topic_tokens, created_at, updated_at FROM memories WHERE status = 'active'`)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("scan memories: %w", err)
	}
	defer rows.Close()

	// Load ALL active memories into Doc slice for BM25Rank.
	now := float64(time.Now().Unix())
	var mems []Memory
	var docs []memory.Doc

	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, 0, err
		}
		ageDays := (now - float64(m.UpdatedAt)) / 86400
		if ageDays < 0 {
			ageDays = 0
		}
		mems = append(mems, m)
		docs = append(docs, memory.Doc{
			Words:   memory.Tokenize(m.Body),
			Weight:  m.Weight,
			AgeDays: ageDays,
		})
	}

	// BM25 + weight + recency scoring via shared scorer.
	ranked := memory.BM25Rank(docs, tokens)
	// Theoretical score ceiling for this query against the corpus.
	maxPossible := memory.BM25MaxPossible(docs, tokens)

	scored := make([]Scored, 0, len(ranked))
	for _, r := range ranked {
		scored = append(scored, Scored{Memory: mems[r.Index], Score: r.Score, Source: "match"})
	}

	// Attention filter: cut at the last significant gap.
	scored = attentionFilter(scored, limit)

	// Spreading activation: one hop from surviving results.
	seen := make(map[string]bool, len(scored))
	for _, s := range scored {
		seen[s.ID] = true
	}
	var neighbours []Scored
	for _, s := range scored {
		var nrows *sql.Rows
		if project != "" {
			nrows, err = db.Query(`
				SELECT m.id, m.body, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at, e.strength
				FROM edges e
				JOIN memories m ON m.id = CASE WHEN e.a_id = ? THEN e.b_id ELSE e.a_id END
				JOIN provenance p ON m.id = p.memory_id AND p.project = ?
				WHERE (e.a_id = ? OR e.b_id = ?) AND e.strength > 0 AND m.status = 'active'
			`, s.ID, project, s.ID, s.ID)
		} else {
			nrows, err = db.Query(`
				SELECT m.id, m.body, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at, e.strength
				FROM edges e
				JOIN memories m ON m.id = CASE WHEN e.a_id = ? THEN e.b_id ELSE e.a_id END
				WHERE (e.a_id = ? OR e.b_id = ?) AND e.strength > 0 AND m.status = 'active'
			`, s.ID, s.ID, s.ID)
		}
		if err != nil {
			return nil, 0, fmt.Errorf("edge expand: %w", err)
		}
		for nrows.Next() {
			var m Memory
			var strength float64
			if err := nrows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt, &strength); err != nil {
				nrows.Close()
				return nil, 0, err
			}
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			neighbours = append(neighbours, Scored{
				Memory: m,
				Score:  m.Weight*0.5 + strength*0.5,
				Source: "edge",
			})
		}
		nrows.Close()
	}
	sort.Slice(neighbours, func(i, j int) bool { return neighbours[i].Score > neighbours[j].Score })

	// Merge match + edge results, cap at limit.
	combined := append(scored, neighbours...)
	if len(combined) > limit {
		var pinned, rest []Scored
		for _, s := range combined {
			if strings.HasPrefix(s.Body, "!") {
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
	return combined, maxPossible, nil
}

// attentionFilter trims a sorted score list at the last "significant gap"
// — a point where score[i-1]/score[i] >= attentionMinGap. This mimics
// competitive inhibition: memories compete for attention and only the
// cluster of strong activations survives.
//
// Always returns at least attentionMinKeep results and at most limit.
func attentionFilter(scored []Scored, limit int) []Scored {
	if len(scored) <= attentionMinKeep {
		return scored
	}
	cap := len(scored)
	if cap > limit {
		cap = limit
	}

	// Find the last gap >= attentionMinGap within the cap.
	cutAt := cap // default: keep everything up to limit
	for i := attentionMinKeep; i < cap; i++ {
		if scored[i].Score <= 0 {
			cutAt = i
			break
		}
		if scored[i-1].Score/scored[i].Score >= attentionMinGap {
			cutAt = i
		}
	}
	return scored[:cutAt]
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

// ListMemories returns all active memories, ordered by weight desc then
// updated_at desc. If project is non-empty, results are filtered to
// memories whose provenance includes that project.
func ListMemories(db *sql.DB, project string) ([]Memory, error) {
	var rows *sql.Rows
	var err error
	if project != "" {
		rows, err = db.Query(`
			SELECT DISTINCT m.id, m.body, m.weight, m.status, m.topic_tokens, m.created_at, m.updated_at
			FROM memories m
			JOIN provenance p ON m.id = p.memory_id
			WHERE m.status = 'active' AND p.project = ?
			ORDER BY m.weight DESC, m.updated_at DESC
		`, project)
	} else {
		rows, err = db.Query(`
			SELECT id, body, weight, status, topic_tokens, created_at, updated_at
			FROM memories
			WHERE status = 'active'
			ORDER BY weight DESC, updated_at DESC
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
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
		SELECT m.id, m.body, m.weight, m.status,
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
		if err := rows.Scan(&m.ID, &m.Body, &m.Weight, &m.Status, &m.TopicTokens, &m.CreatedAt, &m.UpdatedAt); err != nil {
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
		SELECT m1.id, m1.body, m2.id, m2.body
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
		var aID, aBody, bID, bBody string
		if err := rows.Scan(&aID, &aBody, &bID, &bBody); err != nil {
			return nil, err
		}
		result = append(result, DreamPair{
			AID:    aID,
			BID:    bID,
			ATuple: aBody,
			BTuple: bBody,
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
