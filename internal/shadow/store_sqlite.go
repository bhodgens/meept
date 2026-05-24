package shadow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite3 driver registration
)

// Ensure implementations satisfy interfaces
var (
	_ TrainingStore = (*SQLiteTrainingStore)(nil)
	_ ExamplesStore = (*SQLiteExamplesStore)(nil)
	_ AdaptersStore = (*SQLiteAdaptersStore)(nil)

	// ErrNotFound is returned when a shadow store record is not found.
	ErrNotFound = errors.New("record not found")
)

// Schema version constants
const (
	TrainingStoreSchemaVersion = 2
	ExamplesStoreSchemaVersion = 2
	AdaptersStoreSchemaVersion = 1
)

// SQLiteTrainingStore implements TrainingStore using SQLite.
type SQLiteTrainingStore struct {
	db *sql.DB
}

// NewSQLiteTrainingStore creates a new training store.
func NewSQLiteTrainingStore(dbPath string) (*SQLiteTrainingStore, error) {
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteTrainingStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return store, nil
}

func (s *SQLiteTrainingStore) migrate() error {
	// Create schema version table
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL,
			migrated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Get current version
	currentVersion := 0
	row := s.db.QueryRow("SELECT version FROM schema_version WHERE id = 1")
	_ = row.Scan(&currentVersion) // Ignore error - table might be empty

	// Run migrations based on version
	if currentVersion < 1 {
		if err := s.migrateToV1(); err != nil {
			return fmt.Errorf("migration to v1 failed: %w", err)
		}
	}

	if currentVersion < 2 {
		s.migrateToV2()
	}

	// Update version
	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO schema_version (id, version, migrated_at)
		VALUES (1, ?, ?)
	`, TrainingStoreSchemaVersion, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

func (s *SQLiteTrainingStore) migrateToV1() error {
	schema := `
	CREATE TABLE IF NOT EXISTS shadow_records (
		id TEXT PRIMARY KEY,
		created_at TEXT NOT NULL,
		conversation_id TEXT NOT NULL,
		messages_json TEXT NOT NULL,
		student_model TEXT NOT NULL,
		student_content TEXT NOT NULL,
		student_tokens_in INTEGER DEFAULT 0,
		student_tokens_out INTEGER DEFAULT 0,
		teacher_model TEXT,
		teacher_content TEXT,
		quality_score REAL DEFAULT 0.0,
		preference TEXT DEFAULT 'tie',
		domain TEXT DEFAULT 'general',
		task_type TEXT DEFAULT 'chat',
		is_high_quality INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_shadow_records_conversation ON shadow_records(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_shadow_records_domain ON shadow_records(domain);
	CREATE INDEX IF NOT EXISTS idx_shadow_records_quality ON shadow_records(quality_score);
	CREATE INDEX IF NOT EXISTS idx_shadow_records_created ON shadow_records(created_at);

	CREATE TABLE IF NOT EXISTS preference_pairs (
		id TEXT PRIMARY KEY,
		source_record_id TEXT NOT NULL,
		prompt_json TEXT NOT NULL,
		chosen_response TEXT NOT NULL,
		chosen_model TEXT NOT NULL,
		rejected_response TEXT NOT NULL,
		rejected_model TEXT NOT NULL,
		margin REAL NOT NULL,
		exported_at TEXT,
		FOREIGN KEY (source_record_id) REFERENCES shadow_records(id)
	);

	CREATE INDEX IF NOT EXISTS idx_preference_pairs_source ON preference_pairs(source_record_id);
	CREATE INDEX IF NOT EXISTS idx_preference_pairs_exported ON preference_pairs(exported_at);
	CREATE INDEX IF NOT EXISTS idx_preference_pairs_margin ON preference_pairs(margin);

	CREATE TABLE IF NOT EXISTS daily_usage (
		date TEXT PRIMARY KEY,
		teacher_queries INTEGER DEFAULT 0,
		teacher_cost REAL DEFAULT 0.0
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *SQLiteTrainingStore) migrateToV2() {
	// Add any V2 schema changes here
	// Example: Add metadata column to shadow_records if it doesn't exist
	_, err := s.db.Exec(`
		ALTER TABLE shadow_records ADD COLUMN metadata_json TEXT DEFAULT '';
	`)
	// Ignore error if column already exists (duplicate column). Otherwise log
	// at WARN so unexpected migration failures are visible.
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		slog.Warn("shadow training store migration: ALTER failed", "error", err)
	}

	// Add export tracking
	_, err = s.db.Exec(`
		ALTER TABLE shadow_records ADD COLUMN exported_at TEXT;
	`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		slog.Warn("shadow training store migration: add exported_at failed", "error", err)
	}

	// Create index for export tracking
	if _, ierr := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_shadow_records_exported ON shadow_records(exported_at);
	`); ierr != nil {
		slog.Warn("shadow training store migration: create index failed", "error", ierr)
	}
}

// SaveRecord saves a shadow record.
func (s *SQLiteTrainingStore) SaveRecord(ctx context.Context, record *ShadowRecord) error {
	query := `
		INSERT INTO shadow_records (
			id, created_at, conversation_id, messages_json,
			student_model, student_content, student_tokens_in, student_tokens_out,
			teacher_model, teacher_content, quality_score, preference,
			domain, task_type, is_high_quality
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	highQuality := 0
	if record.IsHighQuality {
		highQuality = 1
	}

	_, err := s.db.ExecContext(ctx, query,
		record.ID, record.CreatedAt.Format(time.RFC3339), record.ConversationID, record.MessagesJSON(),
		record.StudentModel, record.StudentContent, record.StudentTokensIn, record.StudentTokensOut,
		nullString(record.TeacherModel), nullString(record.TeacherContent),
		record.QualityScore, string(record.Preference),
		string(record.Domain), string(record.TaskType), highQuality,
	)
	return err
}

// GetRecord retrieves a shadow record by ID.
func (s *SQLiteTrainingStore) GetRecord(ctx context.Context, id string) (*ShadowRecord, error) {
	query := `
		SELECT id, created_at, conversation_id, messages_json,
			student_model, student_content, student_tokens_in, student_tokens_out,
			teacher_model, teacher_content, quality_score, preference,
			domain, task_type, is_high_quality
		FROM shadow_records WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanRecord(row)
}

// ListRecords lists shadow records with filtering.
func (s *SQLiteTrainingStore) ListRecords(ctx context.Context, opts ListRecordsOptions) ([]*ShadowRecord, error) {
	var conditions []string
	var args []any

	if opts.Domain != "" {
		conditions = append(conditions, "domain = ?")
		args = append(args, string(opts.Domain))
	}
	if opts.TaskType != "" {
		conditions = append(conditions, "task_type = ?")
		args = append(args, string(opts.TaskType))
	}
	if opts.MinQuality > 0 {
		conditions = append(conditions, "quality_score >= ?")
		args = append(args, opts.MinQuality)
	}
	if opts.HighQualityOnly {
		conditions = append(conditions, "is_high_quality = 1")
	}
	if opts.Since != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, opts.Since.Format(time.RFC3339))
	}
	if opts.Until != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, opts.Until.Format(time.RFC3339))
	}

	query := `
		SELECT id, created_at, conversation_id, messages_json,
			student_model, student_content, student_tokens_in, student_tokens_out,
			teacher_model, teacher_content, quality_score, preference,
			domain, task_type, is_high_quality
		FROM shadow_records
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ") //nolint:gosec // conditions use parameterized ? placeholders; dynamic WHERE is safe
	}
	query += " ORDER BY created_at DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*ShadowRecord
	for rows.Next() {
		record, err := s.scanRecordRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

// UpdateRecord updates a shadow record.
func (s *SQLiteTrainingStore) UpdateRecord(ctx context.Context, record *ShadowRecord) error {
	query := `
		UPDATE shadow_records SET
			teacher_model = ?, teacher_content = ?,
			quality_score = ?, preference = ?, is_high_quality = ?
		WHERE id = ?
	`
	highQuality := 0
	if record.IsHighQuality {
		highQuality = 1
	}

	_, err := s.db.ExecContext(ctx, query,
		nullString(record.TeacherModel), nullString(record.TeacherContent),
		record.QualityScore, string(record.Preference), highQuality,
		record.ID,
	)
	return err
}

// DeleteRecord deletes a shadow record.
func (s *SQLiteTrainingStore) DeleteRecord(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM shadow_records WHERE id = ?", id)
	return err
}

// SavePreferencePair saves a preference pair.
func (s *SQLiteTrainingStore) SavePreferencePair(ctx context.Context, pair *PreferencePair) error {
	query := `
		INSERT INTO preference_pairs (
			id, source_record_id, prompt_json,
			chosen_response, chosen_model, rejected_response, rejected_model,
			margin, exported_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	var exportedAt *string
	if pair.ExportedAt != nil {
		s := pair.ExportedAt.Format(time.RFC3339)
		exportedAt = &s
	}

	_, err := s.db.ExecContext(ctx, query,
		pair.ID, pair.SourceRecordID, pair.PromptJSON(),
		pair.ChosenResponse, pair.ChosenModel,
		pair.RejectedResponse, pair.RejectedModel,
		pair.Margin, exportedAt,
	)
	return err
}

// GetPreferencePair retrieves a preference pair by ID.
func (s *SQLiteTrainingStore) GetPreferencePair(ctx context.Context, id string) (*PreferencePair, error) {
	query := `
		SELECT id, source_record_id, prompt_json,
			chosen_response, chosen_model, rejected_response, rejected_model,
			margin, exported_at
		FROM preference_pairs WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanPair(row)
}

// ListPreferencePairs lists preference pairs with filtering.
func (s *SQLiteTrainingStore) ListPreferencePairs(ctx context.Context, opts ListPairsOptions) ([]*PreferencePair, error) {
	var conditions []string
	var args []any

	if opts.MinMargin > 0 {
		conditions = append(conditions, "margin >= ?")
		args = append(args, opts.MinMargin)
	}
	if opts.UnexportedOnly {
		conditions = append(conditions, "exported_at IS NULL")
	}
	if opts.Since != nil {
		conditions = append(conditions, "source_record_id IN (SELECT id FROM shadow_records WHERE created_at >= ?)")
		args = append(args, opts.Since.Format(time.RFC3339))
	}

	query := `
		SELECT id, source_record_id, prompt_json,
			chosen_response, chosen_model, rejected_response, rejected_model,
			margin, exported_at
		FROM preference_pairs
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ") //nolint:gosec // conditions use parameterized ? placeholders; dynamic WHERE is safe
	}
	query += " ORDER BY margin DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []*PreferencePair
	for rows.Next() {
		pair, err := s.scanPairRow(rows)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair)
	}

	return pairs, rows.Err()
}

// MarkExported marks preference pairs as exported.
func (s *SQLiteTrainingStore) MarkExported(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids)+1)
	args[0] = time.Now().UTC().Format(time.RFC3339)
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	//nolint:gosec // parameterized query
	query := fmt.Sprintf(
		"UPDATE preference_pairs SET exported_at = ? WHERE id IN (%s)",
		strings.Join(placeholders, ", "),
	)
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// GetStats returns shadow training statistics.
func (s *SQLiteTrainingStore) GetStats(ctx context.Context) (*ShadowStats, error) {
	stats := NewShadowStats()

	// Total records
	row := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM shadow_records")
	if err := row.Scan(&stats.TotalRecords); err != nil {
		return nil, err
	}

	// High quality count
	row = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM shadow_records WHERE is_high_quality = 1")
	if err := row.Scan(&stats.HighQualityCount); err != nil {
		return nil, err
	}

	// Preference pairs
	row = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM preference_pairs")
	if err := row.Scan(&stats.PreferencePairs); err != nil {
		return nil, err
	}

	// Average quality
	row = s.db.QueryRowContext(ctx, "SELECT COALESCE(AVG(quality_score), 0) FROM shadow_records")
	if err := row.Scan(&stats.AvgQualityScore); err != nil {
		return nil, err
	}

	// Records by domain
	rows, err := s.db.QueryContext(ctx, "SELECT domain, COUNT(*) FROM shadow_records GROUP BY domain")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		var count int
		if err := rows.Scan(&domain, &count); err != nil {
			return nil, err
		}
		stats.RecordsByDomain[domain] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err() scanning records by domain: %w", err)
	}

	// Records by task type
	rows, err = s.db.QueryContext(ctx, "SELECT task_type, COUNT(*) FROM shadow_records GROUP BY task_type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskType string
		var count int
		if err := rows.Scan(&taskType, &count); err != nil {
			return nil, err
		}
		stats.RecordsByTaskType[taskType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err() scanning records by task type: %w", err)
	}

	// Daily usage
	today := time.Now().UTC().Format("2006-01-02")
	row = s.db.QueryRowContext(ctx,
		"SELECT COALESCE(teacher_queries, 0), COALESCE(teacher_cost, 0) FROM daily_usage WHERE date = ?",
		today,
	)
	_ = row.Scan(&stats.TeacherQueries, &stats.TeacherCostToday) // Ignore error if no row

	return stats, nil
}

// Close closes the database connection.
func (s *SQLiteTrainingStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteTrainingStore) scanRecord(row *sql.Row) (*ShadowRecord, error) {
	var record ShadowRecord
	var createdAt, messagesJSON string
	var teacherModel, teacherContent sql.NullString
	var preference, domain, taskType string
	var highQuality int

	err := row.Scan(
		&record.ID, &createdAt, &record.ConversationID, &messagesJSON,
		&record.StudentModel, &record.StudentContent, &record.StudentTokensIn, &record.StudentTokensOut,
		&teacherModel, &teacherContent, &record.QualityScore, &preference,
		&domain, &taskType, &highQuality,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	record.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	_ = record.SetMessagesFromJSON(messagesJSON)
	record.TeacherModel = teacherModel.String
	record.TeacherContent = teacherContent.String
	record.Preference = Preference(preference)
	record.Domain = Domain(domain)
	record.TaskType = TaskType(taskType)
	record.IsHighQuality = highQuality == 1

	return &record, nil
}

func (s *SQLiteTrainingStore) scanRecordRow(rows *sql.Rows) (*ShadowRecord, error) {
	var record ShadowRecord
	var createdAt, messagesJSON string
	var teacherModel, teacherContent sql.NullString
	var preference, domain, taskType string
	var highQuality int

	err := rows.Scan(
		&record.ID, &createdAt, &record.ConversationID, &messagesJSON,
		&record.StudentModel, &record.StudentContent, &record.StudentTokensIn, &record.StudentTokensOut,
		&teacherModel, &teacherContent, &record.QualityScore, &preference,
		&domain, &taskType, &highQuality,
	)
	if err != nil {
		return nil, err
	}

	record.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	_ = record.SetMessagesFromJSON(messagesJSON)
	record.TeacherModel = teacherModel.String
	record.TeacherContent = teacherContent.String
	record.Preference = Preference(preference)
	record.Domain = Domain(domain)
	record.TaskType = TaskType(taskType)
	record.IsHighQuality = highQuality == 1

	return &record, nil
}

func (s *SQLiteTrainingStore) scanPair(row *sql.Row) (*PreferencePair, error) {
	var pair PreferencePair
	var promptJSON string
	var exportedAt sql.NullString

	err := row.Scan(
		&pair.ID, &pair.SourceRecordID, &promptJSON,
		&pair.ChosenResponse, &pair.ChosenModel,
		&pair.RejectedResponse, &pair.RejectedModel,
		&pair.Margin, &exportedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPreferencePairNotFound
		}
		return nil, err
	}

	_ = pair.SetPromptFromJSON(promptJSON)
	if exportedAt.Valid {
		t, _ := time.Parse(time.RFC3339, exportedAt.String)
		pair.ExportedAt = &t
	}

	return &pair, nil
}

func (s *SQLiteTrainingStore) scanPairRow(rows *sql.Rows) (*PreferencePair, error) {
	var pair PreferencePair
	var promptJSON string
	var exportedAt sql.NullString

	err := rows.Scan(
		&pair.ID, &pair.SourceRecordID, &promptJSON,
		&pair.ChosenResponse, &pair.ChosenModel,
		&pair.RejectedResponse, &pair.RejectedModel,
		&pair.Margin, &exportedAt,
	)
	if err != nil {
		return nil, err
	}

	_ = pair.SetPromptFromJSON(promptJSON)
	if exportedAt.Valid {
		t, _ := time.Parse(time.RFC3339, exportedAt.String)
		pair.ExportedAt = &t
	}

	return &pair, nil
}

// RecordTeacherUsage records teacher model usage for rate limiting.
func (s *SQLiteTrainingStore) RecordTeacherUsage(ctx context.Context, queries int, cost float64) error {
	today := time.Now().UTC().Format("2006-01-02")
	query := `
		INSERT INTO daily_usage (date, teacher_queries, teacher_cost)
		VALUES (?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			teacher_queries = teacher_queries + ?,
			teacher_cost = teacher_cost + ?
	`
	_, err := s.db.ExecContext(ctx, query, today, queries, cost, queries, cost)
	return err
}

// GetTeacherUsageToday returns today's teacher usage.
func (s *SQLiteTrainingStore) GetTeacherUsageToday(ctx context.Context) (n int, f float64, err error) {
	today := time.Now().UTC().Format("2006-01-02")
	var queries int
	var cost float64
	row := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(teacher_queries, 0), COALESCE(teacher_cost, 0) FROM daily_usage WHERE date = ?",
		today,
	)
	err = row.Scan(&queries, &cost)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return queries, cost, err
}

// SQLiteExamplesStore implements ExamplesStore using SQLite.
type SQLiteExamplesStore struct {
	db *sql.DB
}

// NewSQLiteExamplesStore creates a new examples store.
func NewSQLiteExamplesStore(dbPath string) (*SQLiteExamplesStore, error) {
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteExamplesStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return store, nil
}

func (s *SQLiteExamplesStore) migrate() error {
	// Create schema version table
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL,
			migrated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Get current version
	currentVersion := 0
	row := s.db.QueryRow("SELECT version FROM schema_version WHERE id = 1")
	_ = row.Scan(&currentVersion) // Ignore error - table might be empty

	// Run migrations based on version
	if currentVersion < 1 {
		if err := s.migrateToV1(); err != nil {
			return fmt.Errorf("migration to v1 failed: %w", err)
		}
	}

	if currentVersion < 2 {
		s.migrateToV2()
	}

	// Update version
	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO schema_version (id, version, migrated_at)
		VALUES (1, ?, ?)
	`, ExamplesStoreSchemaVersion, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

func (s *SQLiteExamplesStore) migrateToV1() error {
	schema := `
	CREATE TABLE IF NOT EXISTS fewshot_examples (
		id TEXT PRIMARY KEY,
		source_record_id TEXT NOT NULL,
		domain TEXT NOT NULL,
		task_type TEXT NOT NULL,
		user_message TEXT NOT NULL,
		assistant_response TEXT NOT NULL,
		quality_score REAL NOT NULL,
		usage_count INTEGER DEFAULT 0,
		created_at TEXT NOT NULL,
		embedding_json TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_examples_domain_task ON fewshot_examples(domain, task_type);
	CREATE INDEX IF NOT EXISTS idx_examples_quality ON fewshot_examples(quality_score);
	CREATE INDEX IF NOT EXISTS idx_examples_created ON fewshot_examples(created_at);

	CREATE VIRTUAL TABLE IF NOT EXISTS fewshot_fts USING fts5(
		user_message,
		content='fewshot_examples',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS fewshot_ai AFTER INSERT ON fewshot_examples BEGIN
		INSERT INTO fewshot_fts(rowid, user_message) VALUES (new.rowid, new.user_message);
	END;

	CREATE TRIGGER IF NOT EXISTS fewshot_ad AFTER DELETE ON fewshot_examples BEGIN
		INSERT INTO fewshot_fts(fewshot_fts, rowid, user_message) VALUES ('delete', old.rowid, old.user_message);
	END;

	CREATE TRIGGER IF NOT EXISTS fewshot_au AFTER UPDATE ON fewshot_examples BEGIN
		INSERT INTO fewshot_fts(fewshot_fts, rowid, user_message) VALUES ('delete', old.rowid, old.user_message);
		INSERT INTO fewshot_fts(rowid, user_message) VALUES (new.rowid, new.user_message);
	END;
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *SQLiteExamplesStore) migrateToV2() {
	// Add tags column for categorization. Ignore duplicate-column errors;
	// log other errors at WARN.
	_, err := s.db.Exec(`
		ALTER TABLE fewshot_examples ADD COLUMN tags TEXT DEFAULT '';
	`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		slog.Warn("shadow examples store migration: add tags failed", "error", err)
	}

	// Add last_used_at for recency tracking
	_, err = s.db.Exec(`
		ALTER TABLE fewshot_examples ADD COLUMN last_used_at TEXT;
	`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		slog.Warn("shadow examples store migration: add last_used_at failed", "error", err)
	}
}

// SaveExample saves a few-shot example.
func (s *SQLiteExamplesStore) SaveExample(ctx context.Context, example *FewShotExample) error {
	query := `
		INSERT INTO fewshot_examples (
			id, source_record_id, domain, task_type,
			user_message, assistant_response, quality_score,
			usage_count, created_at, embedding_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		example.ID, example.SourceRecordID, string(example.Domain), string(example.TaskType),
		example.UserMessage, example.AssistantResponse, example.QualityScore,
		example.UsageCount, example.CreatedAt.Format(time.RFC3339), nullString(example.EmbeddingJSON),
	)
	return err
}

// GetExample retrieves a few-shot example by ID.
func (s *SQLiteExamplesStore) GetExample(ctx context.Context, id string) (*FewShotExample, error) {
	query := `
		SELECT id, source_record_id, domain, task_type,
			user_message, assistant_response, quality_score,
			usage_count, created_at, embedding_json
		FROM fewshot_examples WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanExample(row)
}

// ListExamples lists examples filtered by domain and task type.
func (s *SQLiteExamplesStore) ListExamples(ctx context.Context, domain Domain, taskType TaskType) ([]*FewShotExample, error) {
	var conditions []string
	var args []any

	if domain != "" {
		conditions = append(conditions, "domain = ?")
		args = append(args, string(domain))
	}
	if taskType != "" {
		conditions = append(conditions, "task_type = ?")
		args = append(args, string(taskType))
	}

	query := `
		SELECT id, source_record_id, domain, task_type,
			user_message, assistant_response, quality_score,
			usage_count, created_at, embedding_json
		FROM fewshot_examples
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ") //nolint:gosec // conditions use parameterized ? placeholders; dynamic WHERE is safe
	}
	query += " ORDER BY quality_score DESC, created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var examples []*FewShotExample
	for rows.Next() {
		example, err := s.scanExampleRow(rows)
		if err != nil {
			return nil, err
		}
		examples = append(examples, example)
	}

	return examples, rows.Err()
}

// IncrementUsage increments the usage count for an example.
func (s *SQLiteExamplesStore) IncrementUsage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE fewshot_examples SET usage_count = usage_count + 1 WHERE id = ?",
		id,
	)
	return err
}

// DeleteExample deletes a few-shot example.
func (s *SQLiteExamplesStore) DeleteExample(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM fewshot_examples WHERE id = ?", id)
	return err
}

// PruneExamples removes examples older than maxAge.
func (s *SQLiteExamplesStore) PruneExamples(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM fewshot_examples WHERE created_at < ?",
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

// SearchSimilar searches for similar examples using FTS.
func (s *SQLiteExamplesStore) SearchSimilar(ctx context.Context, query string, domain Domain, taskType TaskType, limit int) ([]*FewShotExample, error) {
	var conditions []string
	var args []any

	// FTS search - sanitize query for FTS5 syntax
	sanitizedQuery := sanitizeFTSQuery(query)
	ftsQuery := "SELECT rowid FROM fewshot_fts WHERE fewshot_fts MATCH ?"
	args = append(args, sanitizedQuery)

	conditions = append(conditions, fmt.Sprintf("rowid IN (%s)", ftsQuery))

	if domain != "" {
		conditions = append(conditions, "domain = ?")
		args = append(args, string(domain))
	}
	if taskType != "" {
		conditions = append(conditions, "task_type = ?")
		args = append(args, string(taskType))
	}

	//nolint:gosec // conditions use parameterized ? placeholders; dynamic WHERE is safe
	sqlQuery := `
		SELECT id, source_record_id, domain, task_type,
			user_message, assistant_response, quality_score,
			usage_count, created_at, embedding_json
		FROM fewshot_examples
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY quality_score DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		// Fall back to LIKE search if FTS fails
		return s.fallbackSearch(ctx, query, domain, taskType, limit)
	}
	defer rows.Close()

	var examples []*FewShotExample
	for rows.Next() {
		example, err := s.scanExampleRow(rows)
		if err != nil {
			return nil, err
		}
		examples = append(examples, example)
	}

	return examples, rows.Err()
}

func (s *SQLiteExamplesStore) fallbackSearch(ctx context.Context, query string, domain Domain, taskType TaskType, limit int) ([]*FewShotExample, error) {
	var conditions []string
	var args []any

	conditions = append(conditions, "user_message LIKE ?")
	args = append(args, "%"+query+"%")

	if domain != "" {
		conditions = append(conditions, "domain = ?")
		args = append(args, string(domain))
	}
	if taskType != "" {
		conditions = append(conditions, "task_type = ?")
		args = append(args, string(taskType))
	}

	//nolint:gosec // conditions use parameterized ? placeholders; dynamic WHERE is safe
	sqlQuery := `
		SELECT id, source_record_id, domain, task_type,
			user_message, assistant_response, quality_score,
			usage_count, created_at, embedding_json
		FROM fewshot_examples
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY quality_score DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var examples []*FewShotExample
	for rows.Next() {
		example, err := s.scanExampleRow(rows)
		if err != nil {
			return nil, err
		}
		examples = append(examples, example)
	}

	return examples, rows.Err()
}

// RebuildFromRecords rebuilds examples from shadow records.
func (s *SQLiteExamplesStore) RebuildFromRecords(ctx context.Context, records []*ShadowRecord, minQuality float64) error {
	// Clear existing examples
	if _, err := s.db.ExecContext(ctx, "DELETE FROM fewshot_examples"); err != nil {
		return err
	}

	// Insert new examples from high-quality records
	for _, record := range records {
		if record.QualityScore >= minQuality {
			example := NewFewShotExample(record)
			if err := s.SaveExample(ctx, example); err != nil {
				return err
			}
		}
	}

	return nil
}

// Count returns the total number of examples.
func (s *SQLiteExamplesStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fewshot_examples").Scan(&count)
	return count, err
}

// Close closes the database connection.
func (s *SQLiteExamplesStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteExamplesStore) scanExample(row *sql.Row) (*FewShotExample, error) {
	var example FewShotExample
	var domain, taskType, createdAt string
	var embeddingJSON sql.NullString

	err := row.Scan(
		&example.ID, &example.SourceRecordID, &domain, &taskType,
		&example.UserMessage, &example.AssistantResponse, &example.QualityScore,
		&example.UsageCount, &createdAt, &embeddingJSON,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrExampleNotFound
		}
		return nil, err
	}

	example.Domain = Domain(domain)
	example.TaskType = TaskType(taskType)
	example.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	example.EmbeddingJSON = embeddingJSON.String

	return &example, nil
}

func (s *SQLiteExamplesStore) scanExampleRow(rows *sql.Rows) (*FewShotExample, error) {
	var example FewShotExample
	var domain, taskType, createdAt string
	var embeddingJSON sql.NullString

	err := rows.Scan(
		&example.ID, &example.SourceRecordID, &domain, &taskType,
		&example.UserMessage, &example.AssistantResponse, &example.QualityScore,
		&example.UsageCount, &createdAt, &embeddingJSON,
	)
	if err != nil {
		return nil, err
	}

	example.Domain = Domain(domain)
	example.TaskType = TaskType(taskType)
	example.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	example.EmbeddingJSON = embeddingJSON.String

	return &example, nil
}

// SQLiteAdaptersStore implements AdaptersStore using SQLite.
type SQLiteAdaptersStore struct {
	db *sql.DB
}

// NewSQLiteAdaptersStore creates a new adapters store.
func NewSQLiteAdaptersStore(dbPath string) (*SQLiteAdaptersStore, error) {
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteAdaptersStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return store, nil
}

func (s *SQLiteAdaptersStore) migrate() error {
	// Create schema version table
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL,
			migrated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Get current version
	currentVersion := 0
	row := s.db.QueryRow("SELECT version FROM schema_version WHERE id = 1")
	_ = row.Scan(&currentVersion) // Ignore error - table might be empty

	// Run migrations based on version
	if currentVersion < 1 {
		if err := s.migrateToV1(); err != nil {
			return fmt.Errorf("migration to v1 failed: %w", err)
		}
	}

	// Update version
	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO schema_version (id, version, migrated_at)
		VALUES (1, ?, ?)
	`, AdaptersStoreSchemaVersion, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

func (s *SQLiteAdaptersStore) migrateToV1() error {
	schema := `
	CREATE TABLE IF NOT EXISTS adapters (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		model_base TEXT NOT NULL,
		adapter_type TEXT NOT NULL,
		adapter_path TEXT NOT NULL,
		source_training_db TEXT,
		training_records INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 0,
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_adapters_model ON adapters(model_base);
	CREATE INDEX IF NOT EXISTS idx_adapters_active ON adapters(is_active);

	CREATE TABLE IF NOT EXISTS training_runs (
		id TEXT PRIMARY KEY,
		adapter_id TEXT NOT NULL,
		started_at TEXT NOT NULL,
		completed_at TEXT,
		records_used INTEGER DEFAULT 0,
		config_json TEXT,
		final_loss REAL DEFAULT 0,
		eval_score REAL DEFAULT 0,
		FOREIGN KEY (adapter_id) REFERENCES adapters(id)
	);

	CREATE INDEX IF NOT EXISTS idx_training_runs_adapter ON training_runs(adapter_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveAdapter saves an adapter record.
func (s *SQLiteAdaptersStore) SaveAdapter(ctx context.Context, adapter *Adapter) error {
	query := `
		INSERT INTO adapters (
			id, name, model_base, adapter_type, adapter_path,
			source_training_db, training_records, is_active, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	isActive := 0
	if adapter.IsActive {
		isActive = 1
	}

	_, err := s.db.ExecContext(ctx, query,
		adapter.ID, adapter.Name, adapter.ModelBase, adapter.AdapterType, adapter.AdapterPath,
		nullString(adapter.SourceTrainingDB), adapter.TrainingRecords, isActive,
		adapter.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// GetAdapter retrieves an adapter by ID.
func (s *SQLiteAdaptersStore) GetAdapter(ctx context.Context, id string) (*Adapter, error) {
	query := `
		SELECT id, name, model_base, adapter_type, adapter_path,
			source_training_db, training_records, is_active, created_at
		FROM adapters WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanAdapter(row)
}

// GetAdapterByName retrieves an adapter by name.
func (s *SQLiteAdaptersStore) GetAdapterByName(ctx context.Context, name string) (*Adapter, error) {
	query := `
		SELECT id, name, model_base, adapter_type, adapter_path,
			source_training_db, training_records, is_active, created_at
		FROM adapters WHERE name = ?
	`
	row := s.db.QueryRowContext(ctx, query, name)
	return s.scanAdapter(row)
}

// ListAdapters lists all adapters.
func (s *SQLiteAdaptersStore) ListAdapters(ctx context.Context) ([]*Adapter, error) {
	query := `
		SELECT id, name, model_base, adapter_type, adapter_path,
			source_training_db, training_records, is_active, created_at
		FROM adapters ORDER BY created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var adapters []*Adapter
	for rows.Next() {
		adapter, err := s.scanAdapterRow(rows)
		if err != nil {
			return nil, err
		}
		adapters = append(adapters, adapter)
	}

	return adapters, rows.Err()
}

// SetActiveAdapter activates an adapter and deactivates others for the same base model.
func (s *SQLiteAdaptersStore) SetActiveAdapter(ctx context.Context, id string) error {
	// Get the adapter to find its base model
	adapter, err := s.GetAdapter(ctx, id)
	if err != nil || adapter == nil {
		return fmt.Errorf("adapter not found: %s", id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Deactivate all adapters for this base model
	_, err = tx.ExecContext(ctx,
		"UPDATE adapters SET is_active = 0 WHERE model_base = ?",
		adapter.ModelBase,
	)
	if err != nil {
		return err
	}

	// Activate the specified adapter
	_, err = tx.ExecContext(ctx,
		"UPDATE adapters SET is_active = 1 WHERE id = ?",
		id,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetActiveAdapter returns the active adapter for a base model.
func (s *SQLiteAdaptersStore) GetActiveAdapter(ctx context.Context, modelBase string) (*Adapter, error) {
	query := `
		SELECT id, name, model_base, adapter_type, adapter_path,
			source_training_db, training_records, is_active, created_at
		FROM adapters WHERE model_base = ? AND is_active = 1
	`
	row := s.db.QueryRowContext(ctx, query, modelBase)
	return s.scanAdapter(row)
}

// DeleteAdapter deletes an adapter.
func (s *SQLiteAdaptersStore) DeleteAdapter(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM adapters WHERE id = ?", id)
	return err
}

// SaveTrainingRun saves a training run record.
func (s *SQLiteAdaptersStore) SaveTrainingRun(ctx context.Context, run *TrainingRun) error {
	query := `
		INSERT INTO training_runs (
			id, adapter_id, started_at, completed_at,
			records_used, config_json, final_loss, eval_score
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	var completedAt *string
	if run.CompletedAt != nil {
		s := run.CompletedAt.Format(time.RFC3339)
		completedAt = &s
	}

	_, err := s.db.ExecContext(ctx, query,
		run.ID, run.AdapterID, run.StartedAt.Format(time.RFC3339), completedAt,
		run.RecordsUsed, run.ConfigJSON, run.FinalLoss, run.EvalScore,
	)
	return err
}

// GetTrainingRun retrieves a training run by ID.
func (s *SQLiteAdaptersStore) GetTrainingRun(ctx context.Context, id string) (*TrainingRun, error) {
	query := `
		SELECT id, adapter_id, started_at, completed_at,
			records_used, config_json, final_loss, eval_score
		FROM training_runs WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanTrainingRun(row)
}

// ListTrainingRuns lists training runs for an adapter.
func (s *SQLiteAdaptersStore) ListTrainingRuns(ctx context.Context, adapterID string) ([]*TrainingRun, error) {
	query := `
		SELECT id, adapter_id, started_at, completed_at,
			records_used, config_json, final_loss, eval_score
		FROM training_runs WHERE adapter_id = ?
		ORDER BY started_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, adapterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*TrainingRun
	for rows.Next() {
		run, err := s.scanTrainingRunRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	return runs, rows.Err()
}

// CompleteTrainingRun marks a training run as completed.
func (s *SQLiteAdaptersStore) CompleteTrainingRun(ctx context.Context, id string, finalLoss, evalScore float64) error {
	query := `
		UPDATE training_runs SET
			completed_at = ?, final_loss = ?, eval_score = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query,
		time.Now().UTC().Format(time.RFC3339), finalLoss, evalScore, id,
	)
	return err
}

// Close closes the database connection.
func (s *SQLiteAdaptersStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteAdaptersStore) scanAdapter(row *sql.Row) (*Adapter, error) {
	var adapter Adapter
	var sourceDB sql.NullString
	var createdAt string
	var isActive int

	err := row.Scan(
		&adapter.ID, &adapter.Name, &adapter.ModelBase, &adapter.AdapterType, &adapter.AdapterPath,
		&sourceDB, &adapter.TrainingRecords, &isActive, &createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAdapterNotFound
		}
		return nil, err
	}

	adapter.SourceTrainingDB = sourceDB.String
	adapter.IsActive = isActive == 1
	adapter.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &adapter, nil
}

func (s *SQLiteAdaptersStore) scanAdapterRow(rows *sql.Rows) (*Adapter, error) {
	var adapter Adapter
	var sourceDB sql.NullString
	var createdAt string
	var isActive int

	err := rows.Scan(
		&adapter.ID, &adapter.Name, &adapter.ModelBase, &adapter.AdapterType, &adapter.AdapterPath,
		&sourceDB, &adapter.TrainingRecords, &isActive, &createdAt,
	)
	if err != nil {
		return nil, err
	}

	adapter.SourceTrainingDB = sourceDB.String
	adapter.IsActive = isActive == 1
	adapter.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &adapter, nil
}

func (s *SQLiteAdaptersStore) scanTrainingRun(row *sql.Row) (*TrainingRun, error) {
	var run TrainingRun
	var startedAt string
	var completedAt sql.NullString

	err := row.Scan(
		&run.ID, &run.AdapterID, &startedAt, &completedAt,
		&run.RecordsUsed, &run.ConfigJSON, &run.FinalLoss, &run.EvalScore,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	run.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		run.CompletedAt = &t
	}

	return &run, nil
}

func (s *SQLiteAdaptersStore) scanTrainingRunRow(rows *sql.Rows) (*TrainingRun, error) {
	var run TrainingRun
	var startedAt string
	var completedAt sql.NullString

	err := rows.Scan(
		&run.ID, &run.AdapterID, &startedAt, &completedAt,
		&run.RecordsUsed, &run.ConfigJSON, &run.FinalLoss, &run.EvalScore,
	)
	if err != nil {
		return nil, err
	}

	run.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		run.CompletedAt = &t
	}

	return &run, nil
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// sanitizeFTSQuery escapes FTS5 special characters to prevent query syntax errors.
func sanitizeFTSQuery(query string) string {
	// FTS5 special characters that need escaping: " * ( ) :
	// Wrap the entire query in double quotes for literal matching
	escaped := strings.ReplaceAll(query, `"`, `""`)
	return `"` + escaped + `"`
}
