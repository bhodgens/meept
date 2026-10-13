package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory/vector"

	_ "github.com/mattn/go-sqlite3" //nolint:revive // blank import for side effects (cgo driver required by vec0 shards)
)

// Bridge metadata keys — the canonical on-disk contract (branch 09, root
// contract 4). Written exactly as these string constants; recall-path tag
// checks (B04) match on BridgeMetaUnitType == BridgeUnitTypeValue.
const (
	// BridgeMetaUnitType is always BridgeUnitTypeValue ("bridge").
	BridgeMetaUnitType = "unit_type"
	// BridgeMetaEvidenceIDs is a JSON array of source memory ID strings.
	BridgeMetaEvidenceIDs = "evidence_ids"
	// BridgeMetaCreatedAt is an RFC3339 timestamp.
	BridgeMetaCreatedAt = "created_at"
	// BridgeMetaVerified is the string "true" or "false" (B03 flips it).
	BridgeMetaVerified = "verified"

	// BridgeUnitTypeValue is the metadata value stored under
	// BridgeMetaUnitType for every persisted bridge.
	BridgeUnitTypeValue = "bridge"
)

// ErrBridgeMissingEvidence is returned when a bridge write carries no
// evidence IDs (branch rule: no provenance, no ship).
var ErrBridgeMissingEvidence = errors.New("bridge write rejected: evidence_ids is empty (no provenance, no ship)")

// BridgeRecord is the typed representation of one bridging fact in the
// BridgeShard.
type BridgeRecord struct {
	// ID is the bridge's unique memory ID (primary key in the shard).
	ID string
	// Content is the human-readable bridging fact text.
	Content string
	// Embedding is the pre-computed vector; PutBridge does NOT call an
	// embedding model (generation, B02, owns that).
	Embedding []float32
	// Metadata carries any extra key/value pairs beyond the four canonical
	// bridge keys; canonical keys set here are overwritten by the typed
	// fields below.
	Metadata map[string]any
	// EvidenceIDs are the source memory IDs this bridge was derived from.
	// Mandatory: empty at write time is rejected (ErrBridgeMissingEvidence).
	EvidenceIDs []string
	// CreatedAt is the bridge creation time (RFC3339 in metadata).
	CreatedAt time.Time
	// Verified reports whether entailment verification (B03) passed.
	Verified bool
}

// BridgeStore provides typed CRUD over the vector package's BridgeShard.
// It owns the bridge.vec.db file directly (not via ShardManager) so the
// store can manage its own sqlite handle and metadata side table; the
// ShardManager's BridgeShard remains available for read-side integration
// (recall path, B04) through the standard GetShard flow.
type BridgeStore struct {
	shard *vector.VectorShard
	db    *sql.DB // same sqlite file as shard, for the evidence side table
	log   *slog.Logger
}

// NewBridgeStore opens (creating if needed) the bridge shard at path.
func NewBridgeStore(path string) (*BridgeStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("bridge store: path must not be empty")
	}
	shard, err := vector.NewVectorShard(path, vector.BridgeShard.Dimension(), vector.DefaultM, vector.DefaultEFConstruction)
	if err != nil {
		return nil, fmt.Errorf("bridge store: open shard: %w", err)
	}
	db, err := openBridgeSidecar(path)
	if err != nil {
		shard.Close()
		return nil, err
	}
	if err := initBridgeSidecar(db); err != nil {
		db.Close()
		shard.Close()
		return nil, err
	}
	return &BridgeStore{
		shard: shard,
		db:    db,
		log:   slog.New(slog.DiscardHandler),
	}, nil
}

// WithLogger sets the store's logger (used for best-effort warnings).
func (bs *BridgeStore) WithLogger(l *slog.Logger) *BridgeStore {
	if l != nil {
		bs.log = l
	}
	return bs
}

// bridgeMetadata builds the exact four-key canonical metadata map (plus any
// caller extras) for a bridge record.
func (r BridgeRecord) bridgeMetadata() (map[string]any, error) {
	evJSON, err := json.Marshal(r.EvidenceIDs)
	if err != nil {
		return nil, fmt.Errorf("bridge store: marshal evidence_ids: %w", err)
	}
	md := make(map[string]any, 4+len(r.Metadata))
	for k, v := range r.Metadata {
		md[k] = v
	}
	md[BridgeMetaUnitType] = BridgeUnitTypeValue
	md[BridgeMetaEvidenceIDs] = string(evJSON)
	md[BridgeMetaCreatedAt] = r.CreatedAt.UTC().Format(time.RFC3339)
	md[BridgeMetaVerified] = fmt.Sprintf("%t", r.Verified)
	return md, nil
}

// PutBridge validates and persists one bridge record. Write-time validation:
// empty EvidenceIDs (after trimming) is rejected with ErrBridgeMissingEvidence
// and nothing is persisted.
func (bs *BridgeStore) PutBridge(ctx context.Context, rec BridgeRecord) error {
	trimmed := make([]string, 0, len(rec.EvidenceIDs))
	for _, id := range rec.EvidenceIDs {
		if id = strings.TrimSpace(id); id != "" {
			trimmed = append(trimmed, id)
		}
	}
	if len(trimmed) == 0 {
		return ErrBridgeMissingEvidence
	}
	rec.EvidenceIDs = trimmed
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	md, err := rec.bridgeMetadata()
	if err != nil {
		return err
	}

	if err := bs.shard.Insert(ctx, rec.ID, rec.Embedding, rec.Content); err != nil {
		return fmt.Errorf("bridge store: insert vector: %w", err)
	}
	if err := bs.putBridgeMetadata(ctx, rec.ID, md); err != nil {
		// Roll back the vector row so a rejected/failed write never leaves a
		// half-persisted bridge behind.
		if delErr := bs.shard.Delete(ctx, rec.ID); delErr != nil {
			bs.log.Warn("bridge store: failed to roll back vector after metadata error", "id", rec.ID, "error", delErr)
		}
		return fmt.Errorf("bridge store: insert metadata: %w", err)
	}
	return nil
}

// putBridgeMetadata writes the canonical metadata side table rows.
func (bs *BridgeStore) putBridgeMetadata(ctx context.Context, id string, md map[string]any) error {
	tx, err := bs.db.BeginTx(ctx, nil) //nolint:mutexio // single sqlite connection, one tx at a time (matches shard internals)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:mutexio // rollback bound to the tx above

	for _, key := range []string{BridgeMetaUnitType, BridgeMetaEvidenceIDs, BridgeMetaCreatedAt, BridgeMetaVerified} {
		v, err := json.Marshal(md[key])
		if err != nil {
			return fmt.Errorf("marshal %s: %w", key, err)
		}
		vs := string(v)
		if key == BridgeMetaEvidenceIDs {
			// md carries the already-JSON-encoded array string; store it raw
			// so the side table keeps the canonical JSON-array form.
			if s, isStr := md[key].(string); isStr {
				vs = s
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO bridge_meta (memory_id, key, value) VALUES (?, ?, ?)
		`, id, key, vs); err != nil { //nolint:mutexio // tx bound to protected conn
			return fmt.Errorf("write %s: %w", key, err)
		}
	}
	return tx.Commit() //nolint:mutexio // commit bound to the tx above
}

// DeleteBridge removes a bridge (vector + metadata) by ID. Deleting a
// non-existent bridge is a no-op, matching VectorShard.Delete semantics.
func (bs *BridgeStore) DeleteBridge(ctx context.Context, id string) error {
	if err := bs.shard.Delete(ctx, id); err != nil {
		return fmt.Errorf("bridge store: delete vector: %w", err)
	}
	if _, err := bs.db.ExecContext(ctx, `DELETE FROM bridge_meta WHERE memory_id = ?`, id); err != nil { //nolint:mutexio // single connection, serialized by driver
		return fmt.Errorf("bridge store: delete metadata: %w", err)
	}
	return nil
}

// SearchByEmbedding runs a KNN search over the bridge shard and enriches hits
// with their canonical metadata. This is a store-level primitive — the recall
// path (B04) applies the k_b cap on top of it.
func (bs *BridgeStore) SearchByEmbedding(ctx context.Context, embedding []float32, k int) ([]BridgeRecord, error) {
	results, err := bs.shard.Search(ctx, embedding, k, max(2*k, vector.DefaultEFSearch))
	if err != nil {
		return nil, fmt.Errorf("bridge store: search: %w", err)
	}
	out := make([]BridgeRecord, 0, len(results))
	for _, r := range results {
		rec := BridgeRecord{
			ID:       r.MemoryID,
			Content:  r.Content,
			Metadata: map[string]any{},
		}
		if err := bs.loadBridgeMetadata(ctx, r.MemoryID, &rec); err != nil {
			bs.log.Warn("bridge store: skipping hit with unreadable metadata", "id", r.MemoryID, "error", err)
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// ListBridgesByEvidence returns every bridge whose evidence_ids contain the
// given source memory ID. B03 invalidation calls this on source-memory
// update/delete/expiry.
func (bs *BridgeStore) ListBridgesByEvidence(ctx context.Context, evidenceID string) ([]BridgeRecord, error) {
	if strings.TrimSpace(evidenceID) == "" {
		return nil, nil
	}
	rows, err := bs.db.QueryContext(ctx, `
		SELECT memory_id, value FROM bridge_meta
		WHERE key = ? AND value LIKE ?
	`, BridgeMetaEvidenceIDs, "%"+evidenceID+"%") //nolint:mutexio // single connection, serialized by driver
	if err != nil {
		return nil, fmt.Errorf("bridge store: evidence scan: %w", err)
	}
	defer rows.Close()

	var out []BridgeRecord
	for rows.Next() {
		var id, evJSON string
		if err := rows.Scan(&id, &evJSON); err != nil {
			continue
		}
		var ids []string
		if err := json.Unmarshal([]byte(evJSON), &ids); err != nil {
			continue
		}
		match := false
		for _, e := range ids {
			if e == evidenceID {
				match = true
				break
			}
		}
		if !match {
			continue // substring false positive (e.g. "mem-1" vs "mem-12")
		}
		rec := BridgeRecord{ID: id, EvidenceIDs: ids, Metadata: map[string]any{}}
		if err := bs.loadBridgeMetadata(ctx, id, &rec); err != nil {
			bs.log.Warn("bridge store: skipping evidence match with unreadable metadata", "id", id, "error", err)
			continue
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bridge store: iterate evidence scan: %w", err)
	}
	return out, nil
}

// Count returns the number of persisted bridges (by vector row).
func (bs *BridgeStore) Count(ctx context.Context) (int, error) {
	var n int
	if err := bs.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridge_meta`).Scan(&n); err != nil { //nolint:mutexio // single connection, serialized by driver
		return 0, fmt.Errorf("bridge store: count: %w", err)
	}
	return n, nil
}

// loadBridgeMetadata fills rec's typed fields + Metadata map from the side
// table (falling back to the shard's own metadata table when a row is
// missing, e.g. bridges written before the side table existed).
func (bs *BridgeStore) loadBridgeMetadata(ctx context.Context, id string, rec *BridgeRecord) error {
	rows, err := bs.db.QueryContext(ctx, `
		SELECT key, value FROM bridge_meta WHERE memory_id = ?
	`, id) //nolint:mutexio // single connection, serialized by driver
	if err != nil {
		return fmt.Errorf("query metadata: %w", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		found = true
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		applyBridgeMetaKey(rec, key, value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate metadata: %w", err)
	}
	if !found {
		return fmt.Errorf("no bridge metadata row for %s", id)
	}
	return nil
}

// applyBridgeMetaKey decodes one side-table row into the record's typed
// fields and its Metadata map.
func applyBridgeMetaKey(rec *BridgeRecord, key, value string) {
	var v any
	_ = json.Unmarshal([]byte(value), &v)
	switch key {
	case BridgeMetaUnitType:
		if s, ok := v.(string); ok {
			rec.Metadata[BridgeMetaUnitType] = s
		}
	case BridgeMetaEvidenceIDs:
		var ids []string
		if err := json.Unmarshal([]byte(value), &ids); err == nil {
			rec.EvidenceIDs = ids
			rec.Metadata[BridgeMetaEvidenceIDs] = ids
		}
	case BridgeMetaCreatedAt:
		if s, ok := v.(string); ok {
			rec.Metadata[BridgeMetaCreatedAt] = s
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				rec.CreatedAt = t
			}
		}
	case BridgeMetaVerified:
		if s, ok := v.(string); ok {
			rec.Metadata[BridgeMetaVerified] = s
			rec.Verified = s == "true"
		}
	default:
		rec.Metadata[key] = v
	}
}

// Close closes the shard and the side-table handle.
func (bs *BridgeStore) Close() error {
	var errs []error
	if bs.db != nil {
		if err := bs.db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if bs.shard != nil {
		if err := bs.shard.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// openBridgeSidecar opens a second connection to the shard's sqlite file for
// the key/value side table. The vec0 virtual table lives in the same file;
// plain tables coexist fine (the shard already stores its own metadata table
// there).
func openBridgeSidecar(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("bridge store: stat shard file: %w", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("bridge store: open sidecar db: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("bridge store: set busy timeout: %w", err)
	}
	return db, nil
}

// initBridgeSidecar creates the bridge_meta key/value table.
func initBridgeSidecar(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS bridge_meta (
			memory_id TEXT NOT NULL,
			key       TEXT NOT NULL,
			value     TEXT,
			UNIQUE(memory_id, key)
		);
		CREATE INDEX IF NOT EXISTS idx_bridge_meta_memory_id ON bridge_meta(memory_id);
		CREATE INDEX IF NOT EXISTS idx_bridge_meta_key ON bridge_meta(key);
	`)
	if err != nil {
		return fmt.Errorf("bridge store: init sidecar schema: %w", err)
	}
	return nil
}
