package memory

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// MaxReasonBytes caps the stored reason length for a vote.
const MaxReasonBytes = 512

// HarmfulVoteThreshold is the net-vote level at or below which a memory is
// considered harmful and is evicted regardless of age when usefulness
// scoring is enabled.
const HarmfulVoteThreshold = -2

// createVotesTableSQL creates the votes table used for usefulness scoring.
const createVotesTableSQL = `
CREATE TABLE IF NOT EXISTS memory_votes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    memory_id  TEXT NOT NULL,
    delta      INTEGER NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_votes_memory_id ON memory_votes(memory_id);`

// Weights holds the usefulness scoring weights.
type Weights struct {
	Base float64 // baseline score for an unvoted, unused, brand-new memory
	Wv   float64 // weight per unit of net vote sum
	Wa   float64 // weight for log1p(accesses)
	Ws   float64 // penalty per day of age
}

// DefaultWeights returns the default usefulness weights (config-overridable).
func DefaultWeights() Weights {
	return Weights{Base: 0.5, Wv: 0.08, Wa: 0.05, Ws: 0.005}
}

// clamp01 constrains v to [0, 1].
func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// Usefulness computes the usefulness score for a memory:
//
//	clamp01(base + Wv*sumVotes + Wa*log1p(accesses) - Ws*ageDays)
//
// sumVotes is the signed net total of votes (+1/-1 each).
func Usefulness(sumVotes, accesses int, ageDays float64, w Weights) float64 {
	return clamp01(w.Base +
		w.Wv*float64(sumVotes) +
		w.Wa*math.Log1p(float64(accesses)) -
		w.Ws*ageDays)
}

// VoteRecord is a single usefulness vote on a memory.
type VoteRecord struct {
	MemoryID  string    `json:"memory_id"`
	Delta     int       `json:"delta"` // +1 or -1
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// voteStore provides lazy persistence for usefulness votes.
// When the episodic SQLite store is available its database hosts the
// memory_votes table; otherwise a standalone votes.db is opened under the
// manager data directory.
type voteStore struct {
	mu   sync.Mutex
	db   *sqlx.DB
	own  bool // true if we opened db ourselves and must close it
	path string
}

func newVoteStore(dataDir string, shared *SQLiteFTSStore) (*voteStore, error) {
	if shared != nil && shared.Initialized() {
		if db := shared.GetDB(); db != nil {
			vs := &voteStore{db: db}
			if err := vs.ensureTable(); err != nil {
				return nil, err
			}
			return vs, nil
		}
	}
	dbPath := filepath.Join(dataDir, "votes.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil { //nolint:gosec // user config dir
		return nil, fmt.Errorf("failed to create votes directory: %w", err)
	}
	rawDB, err := sql.Open("sqlite", "file:"+dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open votes database: %w", err)
	}
	db := sqlx.NewDb(rawDB, "sqlite")
	db.SetMaxOpenConns(1)
	vs := &voteStore{db: db, own: true, path: dbPath}
	if err := vs.ensureTable(); err != nil {
		db.Close()
		return nil, err
	}
	return vs, nil
}

func (v *voteStore) ensureTable() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := v.db.Exec(createVotesTableSQL); err != nil { //nolint:mutexio // db is mutex-guarded at this layer; driver serializes (MaxOpenConns=1)
		return fmt.Errorf("failed to create votes table: %w", err)
	}
	return nil
}

// Close closes the underlying database only if this store owns it.
func (v *voteStore) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.own && v.db != nil {
		err := v.db.Close() //nolint:mutexio // Close under store mutex is the ownership contract here
		v.db = nil
		return err
	}
	return nil
}

// Insert persists one vote record.
func (v *voteStore) Insert(rec VoteRecord) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, err := v.db.Exec( //nolint:mutexio // db is mutex-guarded at this layer; driver serializes (MaxOpenConns=1)
		`INSERT INTO memory_votes (memory_id, delta, reason, created_at) VALUES (?, ?, ?, ?)`,
		rec.MemoryID, rec.Delta, rec.Reason, rec.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to insert vote: %w", err)
	}
	return nil
}

// NetVotes returns the summed vote delta per memory ID in a single batched
// query (no N+1).
func (v *voteStore) NetVotes(ctx context.Context, ids []string) (map[string]int, error) {
	out := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	query, args, err := sqlx.In(
		`SELECT memory_id, COALESCE(SUM(delta), 0) AS net FROM memory_votes WHERE memory_id IN (?) GROUP BY memory_id`,
		ids,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build net-votes query: %w", err)
	}
	rows, err := v.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query net votes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var net int
		if err := rows.Scan(&id, &net); err != nil {
			return nil, fmt.Errorf("failed to scan net votes row: %w", err)
		}
		out[id] = net
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating net votes rows: %w", err)
	}
	return out, nil
}

// VotesFor returns all votes for a single memory (diagnostics/tests).
func (v *voteStore) VotesFor(ctx context.Context, id string) ([]VoteRecord, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	rows, err := v.db.QueryContext(ctx, //nolint:mutexio // db is mutex-guarded at this layer; driver serializes (MaxOpenConns=1)
		`SELECT memory_id, delta, reason, created_at FROM memory_votes WHERE memory_id = ? ORDER BY id ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query votes: %w", err)
	}
	defer rows.Close()
	var recs []VoteRecord
	for rows.Next() {
		var r VoteRecord
		var created string
		if err := rows.Scan(&r.MemoryID, &r.Delta, &r.Reason, &created); err != nil {
			return nil, fmt.Errorf("failed to scan vote row: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, created)
		if err == nil {
			r.CreatedAt = t
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

// initVoteStore lazily initializes the manager's vote store. Safe for
// concurrent use; errors are returned on first call only.
func (m *Manager) initVoteStore() error {
	m.voteOnce.Do(func() {
		dataDir := m.dataDir
		if dataDir == "" {
			dataDir = m.config.DataDir
		}
		if dataDir == "" || dataDir[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				m.voteErr = fmt.Errorf("failed to resolve home directory: %w", err)
				return
			}
			if dataDir == "" {
				dataDir = filepath.Join(home, ".meept", "memory")
			} else {
				dataDir = filepath.Join(home, dataDir[1:])
			}
		}
		m.mu.RLock()
		episodic := m.episodic
		m.mu.RUnlock()
		vs, err := newVoteStore(dataDir, episodic.sharedStore())
		if err != nil {
			m.voteErr = err
			return
		}
		m.votes = vs
	})
	return m.voteErr
}

// RecordVote records a usefulness vote (+1 or -1) on a memory with an
// optional reason (capped at 512 bytes). Returns an error if delta is not
// exactly +1 or -1, or if the memory does not exist.
func (m *Manager) RecordVote(id string, delta int, reason string) error {
	if id == "" {
		return fmt.Errorf("memory_id is required")
	}
	if delta != 1 && delta != -1 {
		return fmt.Errorf("delta must be +1 or -1, got %d", delta)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > MaxReasonBytes {
		reason = reason[:MaxReasonBytes]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mem, err := m.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to look up memory %s: %w", id, err)
	}
	if mem == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	if err := m.initVoteStore(); err != nil {
		return fmt.Errorf("failed to initialize vote store: %w", err)
	}
	if m.votes == nil {
		return fmt.Errorf("vote store unavailable")
	}
	return m.votes.Insert(VoteRecord{
		MemoryID:  id,
		Delta:     delta,
		Reason:    reason,
		CreatedAt: time.Now(),
	})
}

// NetVotes returns the summed vote delta for each given memory ID using one
// batched query. Missing entries have no votes (net 0).
func (m *Manager) NetVotes(ctx context.Context, ids []string) (map[string]int, error) {
	if err := m.initVoteStore(); err != nil {
		return nil, fmt.Errorf("failed to initialize vote store: %w", err)
	}
	if m.votes == nil {
		return map[string]int{}, nil
	}
	return m.votes.NetVotes(ctx, ids)
}

// UsefulEvictionConfig is the resolved (default-applied) usefulness config
// used by the consolidator.
type UsefulEvictionConfig struct {
	Enabled  bool
	FloorPct float64
	Weights  Weights
}

// ResolveUsefulEviction applies package defaults to the raw config values so
// a zero-value config behaves sensibly even when partially specified.
func ResolveUsefulEviction(cfg config.MemoryUsefulnessConfig) UsefulEvictionConfig {
	floor := cfg.FloorPct
	if floor <= 0 {
		floor = 0.05
	}
	w := Weights{Base: cfg.Base, Wv: cfg.Wv, Wa: cfg.Wa, Ws: cfg.Ws}
	def := DefaultWeights()
	if w.Base <= 0 {
		w.Base = def.Base
	}
	if w.Wv <= 0 {
		w.Wv = def.Wv
	}
	if w.Wa <= 0 {
		w.Wa = def.Wa
	}
	if w.Ws <= 0 {
		w.Ws = def.Ws
	}
	return UsefulEvictionConfig{Enabled: cfg.Enabled, FloorPct: floor, Weights: w}
}

// ScoreCandidate computes the usefulness score of one memory against a
// batched net-vote map.
func ScoreCandidate(mem Memory, netVotes int, accesses int, now time.Time, w Weights) float64 {
	ageDays := now.Sub(mem.CreatedAt).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	return Usefulness(netVotes, accesses, ageDays, w)
}

// RankForUsefulness sorts candidates by usefulness score descending
// (highest-value memories first). Ties fall back to newer-first so output
// stays deterministic. It mutates and returns the slice.
func RankForUsefulness(candidates []MemoryResult, netVotes map[string]int, now time.Time, w Weights) []MemoryResult {
	sort.SliceStable(candidates, func(i, j int) bool {
		si := ScoreCandidate(candidates[i].Memory, netVotes[candidates[i].Memory.ID], 0, now, w)
		sj := ScoreCandidate(candidates[j].Memory, netVotes[candidates[j].Memory.ID], 0, now, w)
		if si != sj {
			return si > sj
		}
		return candidates[i].Memory.CreatedAt.After(candidates[j].Memory.CreatedAt)
	})
	return candidates
}

// UsefulnessEvictionPlan separates harmful memories and the bottom
// floor-pct of candidates (by usefulness) from survivors.
type UsefulnessEvictionPlan struct {
	// Harmful are memories with net votes <= -2; evicted regardless of age.
	Harmful []string
	// Floor are the lowest-usefulness memories within the floor percentile;
	// evicted before any age-based rule.
	Floor []string
	// Survivors are the remaining candidates in usefulness-descending order.
	Survivors []MemoryResult
}

// PlanUsefulEviction ranks candidates and splits them into harmful / floor /
// survivor sets. floorCount is max(int(floorPct*len), 0) and never consumes
// all candidates when len > 1.
func PlanUsefulEviction(candidates []MemoryResult, netVotes map[string]int, now time.Time, cfg UsefulEvictionConfig) UsefulnessEvictionPlan {
	var plan UsefulnessEvictionPlan

	// Harmful eviction happens regardless of age/rank.
	var rest []MemoryResult
	for _, c := range candidates {
		if netVotes[c.Memory.ID] <= HarmfulVoteThreshold {
			plan.Harmful = append(plan.Harmful, c.Memory.ID)
			continue
		}
		rest = append(rest, c)
	}

	RankForUsefulness(rest, netVotes, now, cfg.Weights)

	floorCount := int(math.Round(float64(len(rest)) * cfg.FloorPct))
	if floorCount >= len(rest) && floorCount > 0 {
		floorCount = len(rest) - 1 // never evict everything via the floor
	}
	if floorCount < 0 {
		floorCount = 0
	}
	if floorCount > 0 {
		// rest is sorted usefulness-descending: the floor is its tail.
		for _, c := range rest[len(rest)-floorCount:] {
			plan.Floor = append(plan.Floor, c.Memory.ID)
		}
	}
	plan.Survivors = append(plan.Survivors, rest[:len(rest)-floorCount]...)
	return plan
}
