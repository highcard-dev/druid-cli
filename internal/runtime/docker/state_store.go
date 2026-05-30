package docker

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils"
	_ "modernc.org/sqlite"
)

type StateStore struct {
	stateDir string
	dbPath   string
}

const scrollsTableSQL = `
	CREATE TABLE IF NOT EXISTS scrolls (
		id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL DEFAULT '',
		artifact TEXT NOT NULL,
		artifact_digest TEXT NOT NULL DEFAULT '',
		root TEXT NOT NULL,
		scroll_name TEXT NOT NULL,
			scroll_yaml TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			procedures_json TEXT NOT NULL DEFAULT '{}',
			routing_json TEXT NOT NULL DEFAULT '[]',
			ui_packages_json TEXT NOT NULL DEFAULT '{}'
		)
	`

func NewStateStore(stateDir string) (*StateStore, error) {
	if stateDir == "" {
		defaultStateDir, err := utils.DefaultRuntimeStateDir()
		if err != nil {
			return nil, err
		}
		stateDir = defaultStateDir
	}
	return &StateStore{
		stateDir: stateDir,
		dbPath:   filepath.Join(stateDir, "state.db"),
	}, nil
}

func (s *StateStore) StateDir() string {
	return s.stateDir
}

func (s *StateStore) Root(id string) string {
	return filepath.Join(s.stateDir, "scrolls", id)
}

func (s *StateStore) CreateScroll(scroll *domain.RuntimeScroll) error {
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
	if scroll.Procedures == nil {
		scroll.Procedures = domain.ProcedureStatusMap{}
	}
	procedures, err := json.Marshal(scroll.Procedures)
	if err != nil {
		return err
	}
	routing, err := json.Marshal(scroll.Routing)
	if err != nil {
		return err
	}

	uiPackages, err := json.Marshal(scroll.UIPackages)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
			INSERT INTO scrolls (id, owner_id, artifact, artifact_digest, root, scroll_name, scroll_yaml, status, last_error, created_at, updated_at, procedures_json, routing_json, ui_packages_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, scroll.ID, scroll.OwnerID, scroll.Artifact, scroll.ArtifactDigest, scroll.Root, scroll.ScrollName, scroll.ScrollYAML, scroll.Status, scroll.LastError, formatTime(scroll.CreatedAt), formatTime(scroll.UpdatedAt), string(procedures), string(routing), string(uiPackages))
	if err != nil {
		return fmt.Errorf("create runtime scroll %s: %w", scroll.ID, err)
	}
	return nil
}

func (s *StateStore) ListScrolls() ([]*domain.RuntimeScroll, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
			SELECT id, owner_id, artifact, artifact_digest, root, scroll_name, scroll_yaml, status, last_error, created_at, updated_at, procedures_json, routing_json, ui_packages_json
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

func (s *StateStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	row := db.QueryRow(`
			SELECT id, owner_id, artifact, artifact_digest, root, scroll_name, scroll_yaml, status, last_error, created_at, updated_at, procedures_json, routing_json, ui_packages_json
			FROM scrolls
			WHERE id = ?
		`, id)
	scroll, err := scanRuntimeScroll(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrRuntimeScrollNotFound
	}
	return scroll, err
}

func (s *StateStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	scroll.UpdatedAt = time.Now().UTC()
	procedures, err := json.Marshal(scroll.Procedures)
	if err != nil {
		return err
	}
	routing, err := json.Marshal(scroll.Routing)
	if err != nil {
		return err
	}
	uiPackages, err := json.Marshal(scroll.UIPackages)
	if err != nil {
		return err
	}
	res, err := db.Exec(`
		UPDATE scrolls
			SET owner_id = ?, artifact = ?, artifact_digest = ?, root = ?, scroll_name = ?, scroll_yaml = ?, status = ?, last_error = ?, updated_at = ?, procedures_json = ?, routing_json = ?, ui_packages_json = ?
			WHERE id = ?
		`, scroll.OwnerID, scroll.Artifact, scroll.ArtifactDigest, scroll.Root, scroll.ScrollName, scroll.ScrollYAML, scroll.Status, scroll.LastError, formatTime(scroll.UpdatedAt), string(procedures), string(routing), string(uiPackages), scroll.ID)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return domain.ErrRuntimeScrollNotFound
	}
	return nil
}

func (s *StateStore) DeleteScroll(id string) error {
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
		return domain.ErrRuntimeScrollNotFound
	}
	return nil
}

func (s *StateStore) open() (*sql.DB, error) {
	if err := os.MkdirAll(s.stateDir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(scrollsTableSQL); err != nil {
		db.Close()
		return nil, err
	}
	hasLegacyCommands, err := tableHasColumn(db, "scrolls", "commands_"+"json")
	if err != nil {
		db.Close()
		return nil, err
	}
	if hasLegacyCommands {
		db.Close()
		if err := s.resetDB(); err != nil {
			return nil, err
		}
		return s.open()
	}
	if err := ensureColumn(db, "scrolls", "artifact_digest", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "root", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
	if err := ensureColumn(db, "scrolls", "procedures_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "routing_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scrolls", "ui_packages_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *StateStore) resetDB() error {
	for _, path := range []string{s.dbPath, s.dbPath + "-wal", s.dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
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
	columns, err := tableColumns(db, table)
	if err != nil {
		return false, err
	}
	return columns[column], nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
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
	var proceduresJSON string
	var routingJSON string
	var uiPackagesJSON string
	if err := scanner.Scan(&scroll.ID, &scroll.OwnerID, &scroll.Artifact, &scroll.ArtifactDigest, &scroll.Root, &scroll.ScrollName, &scroll.ScrollYAML, &status, &lastError, &createdAt, &updatedAt, &proceduresJSON, &routingJSON, &uiPackagesJSON); err != nil {
		return nil, err
	}
	scroll.Status = domain.RuntimeScrollStatus(status)
	scroll.LastError = lastError
	scroll.CreatedAt = parseTime(createdAt)
	scroll.UpdatedAt = parseTime(updatedAt)
	if proceduresJSON == "" {
		proceduresJSON = "{}"
	}
	if err := json.Unmarshal([]byte(proceduresJSON), &scroll.Procedures); err != nil {
		return nil, err
	}
	if scroll.Procedures == nil {
		scroll.Procedures = domain.ProcedureStatusMap{}
	}
	if routingJSON == "" {
		routingJSON = "[]"
	}
	if err := json.Unmarshal([]byte(routingJSON), &scroll.Routing); err != nil {
		return nil, err
	}
	if uiPackagesJSON == "" {
		uiPackagesJSON = "{}"
	}
	if err := json.Unmarshal([]byte(uiPackagesJSON), &scroll.UIPackages); err != nil {
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
