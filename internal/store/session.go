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
