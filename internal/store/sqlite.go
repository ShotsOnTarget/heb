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

const SchemaVersion = 4

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

// HebDir returns the .heb directory path for the given repo root.
func HebDir(repoRoot string) string { return filepath.Join(repoRoot, ".heb") }

// DBPath returns the SQLite database path for the given repo root.
func DBPath(repoRoot string) string { return filepath.Join(HebDir(repoRoot), "memory.db") }

// RepoRoot resolves the repository root by asking git. Falls back to
// the current working directory if not inside a git repo.
func RepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	return os.Getwd()
}

// Init creates .heb/ and the SQLite database if they do not exist,
// applies the schema, and writes meta rows. Idempotent: re-running on
// an already-initialised repo is a no-op that returns the existing
// store opened read/write.
func Init(repoRoot string) (*SQLiteStore, bool, error) {
	dir := HebDir(repoRoot)
	dbPath := DBPath(repoRoot)

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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
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

// Open opens an existing store. Errors if the db does not exist.
func Open(repoRoot string) (*SQLiteStore, error) {
	dbPath := DBPath(repoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("heb not initialised at %s: run 'heb init'", repoRoot)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
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

const schemaSQL = `
CREATE TABLE IF NOT EXISTS memories (
    id           TEXT PRIMARY KEY,
    subject      TEXT NOT NULL,
    predicate    TEXT NOT NULL,
    object       TEXT NOT NULL,
    weight       REAL NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_subject   ON memories(subject);
CREATE INDEX IF NOT EXISTS idx_memories_predicate ON memories(predicate);
CREATE INDEX IF NOT EXISTS idx_memories_object    ON memories(object);
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

`
