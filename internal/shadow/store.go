package shadow

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for store operations.
var (
	ErrRecordNotFound      = errors.New("shadow record not found")
	ErrPreferencePairNotFound = errors.New("preference pair not found")
	ErrExampleNotFound     = errors.New("few-shot example not found")
	ErrAdapterNotFound     = errors.New("adapter not found")
	ErrActiveAdapterNotFound = errors.New("no active adapter found")
	ErrStoreNotInitialized = errors.New("store not initialized")
)

// TrainingStore provides access to shadow training data (training.db).
type TrainingStore interface {
	// Shadow records
	SaveRecord(ctx context.Context, record *ShadowRecord) error
	GetRecord(ctx context.Context, id string) (*ShadowRecord, error)
	ListRecords(ctx context.Context, opts ListRecordsOptions) ([]*ShadowRecord, error)
	UpdateRecord(ctx context.Context, record *ShadowRecord) error
	DeleteRecord(ctx context.Context, id string) error

	// Preference pairs
	SavePreferencePair(ctx context.Context, pair *PreferencePair) error
	GetPreferencePair(ctx context.Context, id string) (*PreferencePair, error)
	ListPreferencePairs(ctx context.Context, opts ListPairsOptions) ([]*PreferencePair, error)
	MarkExported(ctx context.Context, ids []string) error

	// Statistics
	GetStats(ctx context.Context) (*ShadowStats, error)

	// Lifecycle
	Close() error
}

// ExamplesStore provides access to few-shot examples (examples.db).
type ExamplesStore interface {
	// Examples
	SaveExample(ctx context.Context, example *FewShotExample) error
	GetExample(ctx context.Context, id string) (*FewShotExample, error)
	ListExamples(ctx context.Context, domain Domain, taskType TaskType) ([]*FewShotExample, error)
	IncrementUsage(ctx context.Context, id string) error
	DeleteExample(ctx context.Context, id string) error
	PruneExamples(ctx context.Context, maxAge time.Duration) (int, error)

	// Search
	SearchSimilar(ctx context.Context, query string, domain Domain, taskType TaskType, limit int) ([]*FewShotExample, error)

	// Rebuild from training data
	RebuildFromRecords(ctx context.Context, records []*ShadowRecord, minQuality float64) error

	// Count
	Count(ctx context.Context) (int, error)

	// Lifecycle
	Close() error
}

// AdaptersStore provides access to adapter registry (adapters.db).
type AdaptersStore interface {
	// Adapters
	SaveAdapter(ctx context.Context, adapter *Adapter) error
	GetAdapter(ctx context.Context, id string) (*Adapter, error)
	GetAdapterByName(ctx context.Context, name string) (*Adapter, error)
	ListAdapters(ctx context.Context) ([]*Adapter, error)
	SetActiveAdapter(ctx context.Context, id string) error
	GetActiveAdapter(ctx context.Context, modelBase string) (*Adapter, error)
	DeleteAdapter(ctx context.Context, id string) error

	// Training runs
	SaveTrainingRun(ctx context.Context, run *TrainingRun) error
	GetTrainingRun(ctx context.Context, id string) (*TrainingRun, error)
	ListTrainingRuns(ctx context.Context, adapterID string) ([]*TrainingRun, error)
	CompleteTrainingRun(ctx context.Context, id string, finalLoss, evalScore float64) error

	// Lifecycle
	Close() error
}

// ListRecordsOptions specifies filters for listing shadow records.
type ListRecordsOptions struct {
	Domain        Domain
	TaskType      TaskType
	MinQuality    float64
	HighQualityOnly bool
	Since         *time.Time
	Until         *time.Time
	Limit         int
	Offset        int
}

// ListPairsOptions specifies filters for listing preference pairs.
type ListPairsOptions struct {
	MinMargin     float64
	UnexportedOnly bool
	Since         *time.Time
	Limit         int
	Offset        int
}
