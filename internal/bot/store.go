package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS bot_definitions (
			id          TEXT PRIMARY KEY,
			data        TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS bot_states (
			definition_id TEXT PRIMARY KEY REFERENCES bot_definitions(id) ON DELETE CASCADE,
			data          TEXT NOT NULL
		);
	`)
	return err
}

func (s *Store) Create(ctx context.Context, def BotDefinition) error {
	data, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		def.ID, string(data), def.CreatedAt.Format(time.RFC3339), def.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	state := BotState{DefinitionID: def.ID, Status: BotStatusStopped}
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO bot_states (definition_id, data) VALUES (?, ?)`,
		def.ID, string(stateData),
	)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (*BotDefinition, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM bot_definitions WHERE id = ?`, id).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	var def BotDefinition
	if err := json.Unmarshal([]byte(data), &def); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &def, nil
}

func (s *Store) List(ctx context.Context) ([]BotDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM bot_definitions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var defs []BotDefinition
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var def BotDefinition
		if err := json.Unmarshal([]byte(data), &def); err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

func (s *Store) Update(ctx context.Context, def BotDefinition) error {
	data, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE bot_definitions SET data = ?, updated_at = ? WHERE id = ?`,
		string(data), time.Now().UTC().Format(time.RFC3339), def.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("bot %q not found", def.ID)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bot_definitions WHERE id = ?`, id)
	return err
}

func (s *Store) GetState(ctx context.Context, id string) (*BotState, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM bot_states WHERE definition_id = ?`, id).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("query state: %w", err)
	}
	var state BotState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &state, nil
}

func (s *Store) UpdateState(ctx context.Context, state BotState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO bot_states (definition_id, data) VALUES (?, ?)`,
		state.DefinitionID, string(data),
	)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}
