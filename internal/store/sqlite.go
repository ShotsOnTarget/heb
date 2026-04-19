// Package store is the heb memory store.
//
// Backend-agnostic by design: the Store interface defined here is what
// the CLI commands talk to. Today there is one implementation (SQLite
// via modernc.org/sqlite, pure Go, no cgo). A Dolt implementation will
// slot in later behind the same interface.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 8

// Store is the memory store interface. Commands depend on this, not on
// any concrete backend.
type Store interface {
	Close() error
	Backend() string
	SchemaVersion() int
}

// SQLiteStore is the SQLite implementation.
type SQLiteStore struct {
	db   *sql.DB
	path string
}

func (s *SQLiteStore) Close() error    { return s.db.Close() }
func (s *SQLiteStore) Backend() string { return "sqlite" }
func (s *SQLiteStore) SchemaVersion() int {
	var v int
	row := s.db.QueryRow(`SELECT CAST(value AS INTEGER) FROM meta WHERE key='schema_version'`)
	_ = row.Scan(&v)
	return v
}

// DB exposes the underlying handle for commands that need custom queries.
// Kept internal-package-only in spirit; callers outside internal/ cannot
// import this package so this is safe.
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// HebDir returns the global .heb directory path (~/.heb).
func HebDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".heb"), nil
}

// DBPath returns the global SQLite database path (~/.heb/memory.db).
func DBPath() (string, error) {
	dir, err := HebDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "memory.db"), nil
}

// ProjectID returns a stable project identifier for the current directory.
// Uses the git repo root if in a git repo, otherwise the current working directory.
// Normalised to forward slashes for cross-platform consistency.
func ProjectID() (string, error) {
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(root), nil
}

// RepoRoot resolves the repository root by asking git. Falls back to
// the current working directory if not inside a git repo.
func RepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	return os.Getwd()
}

// Init creates ~/.heb/ and the SQLite database if they do not exist,
// applies the schema, and writes meta rows. Idempotent: re-running is
// a no-op that returns the existing store opened read/write.
func Init() (*SQLiteStore, bool, error) {
	dir, err := HebDir()
	if err != nil {
		return nil, false, err
	}
	dbPath, err := DBPath()
	if err != nil {
		return nil, false, err
	}

	created := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		created = true
	} else if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", dbPath, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, false, fmt.Errorf("open %s: %w", dbPath, err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, false, fmt.Errorf("pragmas: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, false, fmt.Errorf("schema: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO meta(key, value) VALUES
			('schema_version', ?),
			('backend', 'sqlite'),
			('created_at', ?)`,
		fmt.Sprintf("%d", SchemaVersion), now); err != nil {
		db.Close()
		return nil, false, fmt.Errorf("meta seed: %w", err)
	}

	return &SQLiteStore{db: db, path: dbPath}, created, nil
}

// Open opens the global store at ~/.heb/memory.db. Errors if the db does not exist.
func Open() (*SQLiteStore, error) {
	dbPath, err := DBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("heb not initialised: run 'heb init'")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	// Auto-migrate: run schema DDL on open. All statements use
	// IF NOT EXISTS so this is safe on already-current databases.
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("auto-migrate: %w", err)
	}
	// v4: add co_activation_count to edges for existing databases.
	db.Exec(`ALTER TABLE edges ADD COLUMN co_activation_count INTEGER NOT NULL DEFAULT 0`)
	// v5: add role to transcript_log for existing databases.
	db.Exec(`ALTER TABLE transcript_log ADD COLUMN role TEXT NOT NULL DEFAULT 'assistant'`)
	// v6: add topic_tokens to memories for edge filtering and scoped decay.
	db.Exec(`ALTER TABLE memories ADD COLUMN topic_tokens TEXT NOT NULL DEFAULT ''`)
	// v7: migrate SPO columns to body (cell assembly model).
	// Step 1: add body column if missing.
	db.Exec(`ALTER TABLE memories ADD COLUMN body TEXT NOT NULL DEFAULT ''`)
	// Step 2: backfill body from SPO for rows that still have empty body.
	db.Exec(`UPDATE memories SET body = subject || ' ' || predicate || ' ' || object WHERE body = '' AND subject IS NOT NULL AND subject != ''`)
	// Step 3: recreate table without SPO columns (SQLite has no DROP COLUMN on older versions).
	// Check if the old columns still exist before attempting migration.
	var hasSubject int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('memories') WHERE name='subject'`).Scan(&hasSubject); err == nil && hasSubject > 0 {
		db.Exec(`PRAGMA foreign_keys=OFF`)
		db.Exec(`CREATE TABLE memories_new (
			id           TEXT PRIMARY KEY,
			body         TEXT NOT NULL,
			weight       REAL NOT NULL DEFAULT 0,
			status       TEXT NOT NULL DEFAULT 'active',
			topic_tokens TEXT NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL,
			updated_at   INTEGER NOT NULL
		)`)
		db.Exec(`INSERT INTO memories_new(id, body, weight, status, topic_tokens, created_at, updated_at)
			SELECT id, body, weight, status, COALESCE(topic_tokens,''), created_at, updated_at FROM memories`)
		db.Exec(`DROP TABLE memories`)
		db.Exec(`ALTER TABLE memories_new RENAME TO memories`)
		db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_weight ON memories(weight)`)
		db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_status ON memories(status)`)
		db.Exec(`PRAGMA foreign_keys=ON`)
	}
	// v8: add commit_hash to events for git traceability.
	db.Exec(`ALTER TABLE events ADD COLUMN commit_hash TEXT`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_events_commit ON events(commit_hash)`)
	// Bump schema version if behind.
	if _, err := db.Exec(
		`UPDATE meta SET value = ? WHERE key = 'schema_version' AND CAST(value AS INTEGER) < ?`,
		fmt.Sprintf("%d", SchemaVersion), SchemaVersion,
	); err != nil {
		db.Close()
		return nil, fmt.Errorf("bump schema version: %w", err)
	}
	return &SQLiteStore{db: db, path: dbPath}, nil
}

// Path returns the on-disk path of the SQLite database.
func (s *SQLiteStore) Path() string { return s.path }

// OpenOrInit opens the global store, creating it if it doesn't exist.
// Convenience wrapper for callers that don't care about first-init semantics.
func OpenOrInit() (*SQLiteStore, error) {
	s, err := Open()
	if err != nil {
		s, _, err = Init()
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

const schemaSQL = `
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

CREATE TABLE IF NOT EXISTS provenance (
    memory_id    TEXT NOT NULL,
    project      TEXT NOT NULL,
    session_id   TEXT,
    bead_id      TEXT,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_prov_memory  ON provenance(memory_id);
CREATE INDEX IF NOT EXISTS idx_prov_project ON provenance(project);
CREATE INDEX IF NOT EXISTS idx_prov_bead    ON provenance(bead_id);
CREATE INDEX IF NOT EXISTS idx_prov_session ON provenance(session_id);

CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    memory_id    TEXT NOT NULL,
    kind         TEXT NOT NULL,
    delta        REAL NOT NULL DEFAULT 0,
    reason       TEXT,
    session_id   TEXT,
    bead_id      TEXT,
    commit_hash  TEXT,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_events_memory  ON events(memory_id);
CREATE INDEX IF NOT EXISTS idx_events_kind    ON events(kind);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);

CREATE TABLE IF NOT EXISTS edges (
    a_id                TEXT NOT NULL,
    b_id                TEXT NOT NULL,
    strength            REAL NOT NULL DEFAULT 0,
    co_activation_count INTEGER NOT NULL DEFAULT 0,
    updated_at          INTEGER NOT NULL,
    PRIMARY KEY (a_id, b_id),
    FOREIGN KEY (a_id) REFERENCES memories(id) ON DELETE CASCADE,
    FOREIGN KEY (b_id) REFERENCES memories(id) ON DELETE CASCADE,
    CHECK (a_id < b_id)
);
CREATE INDEX IF NOT EXISTS idx_edges_b        ON edges(b_id);
CREATE INDEX IF NOT EXISTS idx_edges_strength ON edges(strength);

CREATE TABLE IF NOT EXISTS episodes (
    session_id   TEXT PRIMARY KEY,
    payload      TEXT NOT NULL,
    created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS meta (
    key          TEXT PRIMARY KEY,
    value        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    project      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   INTEGER NOT NULL,
    closed_at    INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sessions_status    ON sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_created   ON sessions(created_at);

CREATE TABLE IF NOT EXISTS session_contracts (
    session_id   TEXT NOT NULL,
    step         TEXT NOT NULL,
    contract     TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (session_id, step),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS transcript_log (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id         TEXT NOT NULL,
    role               TEXT NOT NULL DEFAULT 'assistant',
    claude_session_id  TEXT,
    response           TEXT NOT NULL,
    result_text        TEXT,
    cost_usd           REAL,
    num_turns          INTEGER,
    created_at         INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_transcript_log_session ON transcript_log(session_id);
CREATE INDEX IF NOT EXISTS idx_transcript_log_claude  ON transcript_log(claude_session_id);

CREATE TABLE IF NOT EXISTS projects (
    path         TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS gui_chat_log (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL,
    phase        TEXT,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_gui_chat_session ON gui_chat_log(session_id);

`
