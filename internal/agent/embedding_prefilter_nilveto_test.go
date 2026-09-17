package agent

// F3 (2026-09-17 bughunt): NewEmbeddingPrefilter dereferenced a nil veto
// when cfg.VetoPath pointed at a MISSING file — loadTfidfVeto returns
// (nil, nil) for "not built" (tfidf_veto.go), which is not an error, and
// the old code took the same branch as a successful load, panicking on
// veto.trainDocs. Construction with a missing veto file must succeed with
// the veto disabled (Door 1 routes on the kNN vote alone — legacy
// behavior), never panic.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

type nilVetoTestEmbedder struct{}

func (nilVetoTestEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	return make([]float64, 4), nil
}

// TestNewEmbeddingPrefilter_MissingVetoFileNoPanic pins the nil-veto
// constructor path: a configured VetoPath whose file does not exist must
// produce a usable prefilter with veto == nil, not a nil-pointer panic.
func TestNewEmbeddingPrefilter_MissingVetoFileNoPanic(t *testing.T) {
	cfg := config.ClassifierPrefilterConfig{
		CentroidsPath: filepath.Join(t.TempDir(), "absent_centroids.json"),
		VetoPath:      filepath.Join(t.TempDir(), "absent_veto_model.json"),
	}

	p := NewEmbeddingPrefilter(nilVetoTestEmbedder{}, cfg, nil)

	if p == nil {
		t.Fatal("NewEmbeddingPrefilter returned nil for a missing veto file")
	}
	if p.veto != nil {
		t.Error("veto must stay nil when the configured model file is missing")
	}
}
