package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Session is a pipeline session record.
type Session struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	Status    string  `json:"status"`
	CreatedAt int64   `json:"created_at"`
	ClosedAt  *int64  `json:"closed_at,omitempty"`
	Steps     []string `json:"steps,omitempty"` // populated by ListSessions/ResumeSessions
}

// SessionContract is a step's output stored in the database.
type SessionContract struct {
	SessionID string `json:"session_id"`
	Step      string `json:"step"`
	Contract  string `json:"contract"`
	CreatedAt int64  `json:"created_at"`
}

// ValidSteps are the pipeline steps in order.
var ValidSteps = []string{"sense", "recall", "reflect", "learn", "consolidate"}

func isValidStep(step string) bool {
	for _, s := range ValidSteps {
		if s == step {
			return true
		}
	}
	return false
}

// StartSession creates a session and writes the sense contract in one call.
// Returns the session ID extracted from the contract's session_id field.
func StartSession(db *sql.DB, sessionID, project, senseContract string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id required")
	}
	if project == "" {
		return fmt.Errorf("project required")
	}
	now := time.Now().UTC().Unix()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO sessions(id, project, status, created_at)
		VALUES(?, ?, 'active', ?)
		ON CONFLICT(id) DO NOTHING
	`, sessionID, project, now)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("insert session: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO session_contracts(session_id, step, contract, created_at)
		VALUES(?, 'sense', ?, ?)
		ON CONFLICT(session_id, step) DO UPDATE SET
			contract = excluded.contract,
			created_at = excluded.created_at
	`, sessionID, senseContract, now)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("write sense contract: %w", err)
	}

	return tx.Commit()
}

// WriteContract writes a step's output contract to the session.
// Upserts: re-writing the same step overwrites.
func WriteContract(db *sql.DB, sessionID, step, contract string) error {
	if !isValidStep(step) {
		return fmt.Errorf("invalid step %q", step)
	}
	now := time.Now().UTC().Unix()

	// Verify session exists
	var status string
	err := db.QueryRow(`SELECT status FROM sessions WHERE id = ?`, sessionID).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("session %q not found", sessionID)
	}
	if err != nil {
		return fmt.Errorf("check session: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO session_contracts(session_id, step, contract, created_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(session_id, step) DO UPDATE SET
			contract = excluded.contract,
			created_at = excluded.created_at
	`, sessionID, step, contract, now)
	if err != nil {
		return fmt.Errorf("write contract: %w", err)
	}
	return nil
}

// ReadContract reads a step's contract from the session.
func ReadContract(db *sql.DB, sessionID, step string) (string, error) {
	if !isValidStep(step) {
		return "", fmt.Errorf("invalid step %q", step)
	}
	var contract string
	err := db.QueryRow(`
		SELECT contract FROM session_contracts
		WHERE session_id = ? AND step = ?
	`, sessionID, step).Scan(&contract)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no %s contract for session %q", step, sessionID)
	}
	if err != nil {
		return "", fmt.Errorf("read contract: %w", err)
	}
	return contract, nil
}

// ListSessions returns recent sessions with their completed steps.
func ListSessions(db *sql.DB, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.Query(`
		SELECT id, project, status, created_at, closed_at
		FROM sessions
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Project, &s.Status, &s.CreatedAt, &s.ClosedAt); err != nil {
			return nil, err
		}
		// Fetch completed steps in pipeline order
		stepRows, err := db.Query(`
			SELECT sc.step FROM session_contracts sc
			WHERE sc.session_id = ?
		`, s.ID)
		if err != nil {
			return nil, err
		}
		stepSet := make(map[string]bool)
		for stepRows.Next() {
			var step string
			if err := stepRows.Scan(&step); err != nil {
				stepRows.Close()
				return nil, err
			}
			stepSet[step] = true
		}
		stepRows.Close()
		// Order by pipeline sequence
		for _, step := range ValidSteps {
			if stepSet[step] {
				s.Steps = append(s.Steps, step)
			}
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// ResumeSession returns a session with its completed steps and identifies
// the next step to run.
func ResumeSession(db *sql.DB, sessionID string) (*Session, string, error) {
	var s Session
	err := db.QueryRow(`
		SELECT id, project, status, created_at, closed_at
		FROM sessions WHERE id = ?
	`, sessionID).Scan(&s.ID, &s.Project, &s.Status, &s.CreatedAt, &s.ClosedAt)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("session %q not found", sessionID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("read session: %w", err)
	}

	stepRows, err := db.Query(`
		SELECT step FROM session_contracts
		WHERE session_id = ?
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, "", err
	}
	defer stepRows.Close()

	done := make(map[string]bool)
	for stepRows.Next() {
		var step string
		if err := stepRows.Scan(&step); err != nil {
			return nil, "", err
		}
		s.Steps = append(s.Steps, step)
		done[step] = true
	}

	// Find next step
	next := ""
	for _, step := range ValidSteps {
		if !done[step] {
			next = step
			break
		}
	}

	return &s, next, nil
}

// ConfigGet reads a config value from the meta table. Keys are stored
// with a "config." prefix to avoid collisions with system meta.
func ConfigGet(db *sql.DB, key string) (string, error) {
	var val string
	err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, "config."+key).Scan(&val)
	if err != nil {
		return "", fmt.Errorf("config %q not set", key)
	}
	return val, nil
}

// ConfigSet writes a config value to the meta table.
func ConfigSet(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, "config."+key, value)
	return err
}

// SessionResponse is a turn in the session transcript (user prompt or assistant response).
type SessionResponse struct {
	ID               int64    `json:"id"`
	SessionID        string   `json:"session_id"`
	Role             string   `json:"role"` // "user" or "assistant"
	ClaudeSessionID  *string  `json:"claude_session_id,omitempty"`
	Response         string   `json:"response"`
	ResultText       *string  `json:"result_text,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	NumTurns         *int     `json:"num_turns,omitempty"`
	CreatedAt        int64    `json:"created_at"`
}

// WriteUserPrompt appends a user prompt turn to the session transcript.
func WriteUserPrompt(db *sql.DB, sessionID, prompt string) (int64, error) {
	if err := verifySession(db, sessionID); err != nil {
		return 0, err
	}
	now := time.Now().UTC().Unix()
	res, err := db.Exec(`
		INSERT INTO transcript_log(session_id, role, response, result_text, created_at)
		VALUES(?, 'user', ?, ?, ?)
	`, sessionID, prompt, prompt, now)
	if err != nil {
		return 0, fmt.Errorf("write user prompt: %w", err)
	}
	return res.LastInsertId()
}

// WriteAssistantResponse appends a Claude Code assistant response to the session transcript.
func WriteAssistantResponse(db *sql.DB, sessionID string, claudeSessionID *string, fullJSON string, resultText *string, costUSD *float64, numTurns *int) (int64, error) {
	if err := verifySession(db, sessionID); err != nil {
		return 0, err
	}
	now := time.Now().UTC().Unix()
	res, err := db.Exec(`
		INSERT INTO transcript_log(session_id, role, claude_session_id, response, result_text, cost_usd, num_turns, created_at)
		VALUES(?, 'assistant', ?, ?, ?, ?, ?, ?)
	`, sessionID, claudeSessionID, fullJSON, resultText, costUSD, numTurns, now)
	if err != nil {
		return 0, fmt.Errorf("write assistant response: %w", err)
	}
	return res.LastInsertId()
}

// LatestClaudeSessionID returns the most recent claude_session_id for a heb session.
func LatestClaudeSessionID(db *sql.DB, sessionID string) (string, error) {
	var id string
	err := db.QueryRow(`
		SELECT claude_session_id FROM transcript_log
		WHERE session_id = ? AND role = 'assistant' AND claude_session_id IS NOT NULL
		ORDER BY created_at DESC LIMIT 1
	`, sessionID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no claude session for %q", sessionID)
	}
	if err != nil {
		return "", fmt.Errorf("latest claude session: %w", err)
	}
	return id, nil
}

// ListResponses returns all transcript turns for a session in chronological order.
func ListResponses(db *sql.DB, sessionID string) ([]SessionResponse, error) {
	rows, err := db.Query(`
		SELECT id, session_id, role, claude_session_id, response, result_text, cost_usd, num_turns, created_at
		FROM transcript_log
		WHERE session_id = ?
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list responses: %w", err)
	}
	defer rows.Close()

	var out []SessionResponse
	for rows.Next() {
		var r SessionResponse
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Role, &r.ClaudeSessionID, &r.Response, &r.ResultText, &r.CostUSD, &r.NumTurns, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// verifySession checks that a session exists.
func verifySession(db *sql.DB, sessionID string) error {
	var status string
	err := db.QueryRow(`SELECT status FROM sessions WHERE id = ?`, sessionID).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("session %q not found", sessionID)
	}
	if err != nil {
		return fmt.Errorf("check session: %w", err)
	}
	return nil
}

// LatestActiveSession returns the most recently created active session.
func LatestActiveSession(db *sql.DB) (*Session, error) {
	var s Session
	err := db.QueryRow(`
		SELECT id, project, status, created_at, closed_at
		FROM sessions WHERE status = 'active'
		ORDER BY created_at DESC LIMIT 1
	`).Scan(&s.ID, &s.Project, &s.Status, &s.CreatedAt, &s.ClosedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active sessions")
	}
	if err != nil {
		return nil, fmt.Errorf("latest active session: %w", err)
	}
	return &s, nil
}

// CloseSession marks a session as complete.
func CloseSession(db *sql.DB, sessionID string) error {
	now := time.Now().UTC().Unix()
	res, err := db.Exec(`
		UPDATE sessions SET status = 'complete', closed_at = ?
		WHERE id = ? AND status = 'active'
	`, now, sessionID)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found or already closed", sessionID)
	}
	return nil
}
