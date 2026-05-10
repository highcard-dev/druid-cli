package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	_ "modernc.org/sqlite"
)

var ErrScrollNotFound = errors.New("runtime scroll not found")

type RuntimeStateStore struct {
	stateDir string
	dbPath   string
}

type RuntimeScrollStore interface {
	StateDir() string
	ScrollRoot(id string) string
	DataRoot(id string) string
	CreateScroll(scroll *domain.RuntimeScroll) error
	ListScrolls() ([]*domain.RuntimeScroll, error)
	GetScroll(id string) (*domain.RuntimeScroll, error)
	UpdateScroll(scroll *domain.RuntimeScroll) error
	DeleteScroll(id string) error
}

func NewRuntimeStateStore(stateDir string) *RuntimeStateStore {
	return &RuntimeStateStore{
		stateDir: stateDir,
		dbPath:   filepath.Join(stateDir, "state.db"),
	}
}

func (s *RuntimeStateStore) StateDir() string {
	return s.stateDir
}

func (s *RuntimeStateStore) ScrollRoot(id string) string {
	return filepath.Join(s.stateDir, "scrolls", id)
}

func (s *RuntimeStateStore) DataRoot(id string) string {
	return s.ScrollRoot(id)
}

func (s *RuntimeStateStore) CreateScroll(scroll *domain.RuntimeScroll) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now().UTC()
	scroll.CreatedAt = now
	scroll.UpdatedAt = now
	if scroll.Status == "" {
		scroll.Status = domain.RuntimeScrollStatusCreated
	}
	if scroll.Commands == nil {
		scroll.Commands = map[string]domain.LockStatus{}
	}
	commands, err := json.Marshal(scroll.Commands)
	if err != nil {
		return err
	}
	routing, err := json.Marshal(scroll.Routing)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO scrolls (id, owner_id, artifact, scroll_root, data_root, scroll_name, scroll_yaml, status, last_error, created_at, updated_at, commands_json, routing_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, scroll.ID, scroll.OwnerID, scroll.Artifact, scroll.ScrollRoot, scroll.DataRoot, scroll.ScrollName, scroll.ScrollYAML, scroll.Status, scroll.LastError, formatTime(scroll.CreatedAt), formatTime(scroll.UpdatedAt), string(commands), string(routing))
	if err != nil {
		return fmt.Errorf("create runtime scroll %s: %w", scroll.ID, err)
	}
	return nil
}

func (s *RuntimeStateStore) ListScrolls() ([]*domain.RuntimeScroll, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, owner_id, artifact, scroll_root, data_root, scroll_name, scroll_yaml, status, last_error, created_at, updated_at, commands_json, routing_json
		FROM scrolls
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	scrolls := []*domain.RuntimeScroll{}
	for rows.Next() {
		scroll, err := scanRuntimeScroll(rows)
		if err != nil {
			return nil, err
		}
		scrolls = append(scrolls, scroll)
	}
	return scrolls, rows.Err()
}

func (s *RuntimeStateStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT id, owner_id, artifact, scroll_root, data_root, scroll_name, scroll_yaml, status, last_error, created_at, updated_at, commands_json, routing_json
		FROM scrolls
		WHERE id = ?
	`, id)
	scroll, err := scanRuntimeScroll(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScrollNotFound
	}
	return scroll, err
}

func (s *RuntimeStateStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	scroll.UpdatedAt = time.Now().UTC()
	commands, err := json.Marshal(scroll.Commands)
	if err != nil {
		return err
	}
	routing, err := json.Marshal(scroll.Routing)
	if err != nil {
		return err
	}
	res, err := db.Exec(`
		UPDATE scrolls
		SET owner_id = ?, artifact = ?, scroll_root = ?, data_root = ?, scroll_name = ?, scroll_yaml = ?, status = ?, last_error = ?, updated_at = ?, commands_json = ?, routing_json = ?
		WHERE id = ?
	`, scroll.OwnerID, scroll.Artifact, scroll.ScrollRoot, scroll.DataRoot, scroll.ScrollName, scroll.ScrollYAML, scroll.Status, scroll.LastError, formatTime(scroll.UpdatedAt), string(commands), string(routing), scroll.ID)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrScrollNotFound
	}
	return nil
}

func (s *RuntimeStateStore) DeleteScroll(id string) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	res, err := db.Exec(`DELETE FROM scrolls WHERE id = ?`, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrScrollNotFound
	}
	return nil
}

func (s *RuntimeStateStore) open() (*sql.DB, error) {
	if err := os.MkdirAll(s.stateDir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS scrolls (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL DEFAULT '',
			artifact TEXT NOT NULL,
			scroll_root TEXT NOT NULL,
			data_root TEXT NOT NULL DEFAULT '',
			scroll_name TEXT NOT NULL,
			scroll_yaml TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			commands_json TEXT NOT NULL DEFAULT '{}',
			routing_json TEXT NOT NULL DEFAULT '[]'
		)
	`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "data_root", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "scroll_yaml", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "last_error", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "routing_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		db.Close()
		return nil, err
	}
	if err := removeRuntimeColumn(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func removeRuntimeColumn(db *sql.DB) error {
	hasRuntime, err := tableHasColumn(db, "scrolls", "runtime")
	if err != nil || !hasRuntime {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE scrolls_new (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL DEFAULT '',
			artifact TEXT NOT NULL,
			scroll_root TEXT NOT NULL,
			data_root TEXT NOT NULL DEFAULT '',
			scroll_name TEXT NOT NULL,
			scroll_yaml TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			commands_json TEXT NOT NULL DEFAULT '{}',
			routing_json TEXT NOT NULL DEFAULT '[]'
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT INTO scrolls_new (id, owner_id, artifact, scroll_root, data_root, scroll_name, scroll_yaml, status, created_at, updated_at, commands_json)
		SELECT id, owner_id, artifact, scroll_root, data_root, scroll_name, scroll_yaml, status, created_at, updated_at, commands_json
		FROM scrolls
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`DROP TABLE scrolls`); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE scrolls_new RENAME TO scrolls`); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table string, column string, definition string) error {
	exists, err := tableHasColumn(db, table, column)
	if err != nil || exists {
		return err
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func tableHasColumn(db *sql.DB, table string, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

type runtimeScrollScanner interface {
	Scan(dest ...interface{}) error
}

func scanRuntimeScroll(scanner runtimeScrollScanner) (*domain.RuntimeScroll, error) {
	var scroll domain.RuntimeScroll
	var status string
	var lastError string
	var createdAt string
	var updatedAt string
	var commandsJSON string
	var routingJSON string
	if err := scanner.Scan(&scroll.ID, &scroll.OwnerID, &scroll.Artifact, &scroll.ScrollRoot, &scroll.DataRoot, &scroll.ScrollName, &scroll.ScrollYAML, &status, &lastError, &createdAt, &updatedAt, &commandsJSON, &routingJSON); err != nil {
		return nil, err
	}
	scroll.Status = domain.RuntimeScrollStatus(status)
	scroll.LastError = lastError
	scroll.CreatedAt = parseTime(createdAt)
	scroll.UpdatedAt = parseTime(updatedAt)
	if commandsJSON == "" {
		commandsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(commandsJSON), &scroll.Commands); err != nil {
		return nil, err
	}
	if scroll.Commands == nil {
		scroll.Commands = map[string]domain.LockStatus{}
	}
	if routingJSON == "" {
		routingJSON = "[]"
	}
	if err := json.Unmarshal([]byte(routingJSON), &scroll.Routing); err != nil {
		return nil, err
	}
	return &scroll, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
