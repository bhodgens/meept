package agent

import (
	"sync"

	"github.com/caimlas/meept/internal/plan"
)

// Artifact has moved to internal/plan/artifacts.go to avoid import cycles
// (plan is lower-level than agent). This alias preserves existing call sites
// that reference agent.Artifact; it resolves to plan.Artifact at compile time.
type Artifact = plan.Artifact

// artifactStore tracks produced artifacts per task. The orchestrator owns
// one instance per active task; cleared on task completion. All methods
// are goroutine-safe.
type artifactStore struct {
	mu        sync.RWMutex
	artifacts map[string]Artifact            // by name
	producers map[string]map[string]struct{} // name -> set of step IDs that produced it
}

func newArtifactStore() *artifactStore {
	return &artifactStore{
		artifacts: make(map[string]Artifact),
		producers: make(map[string]map[string]struct{}),
	}
}

func (s *artifactStore) Add(a Artifact, producerStepID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[a.Name] = a
	if s.producers[a.Name] == nil {
		s.producers[a.Name] = make(map[string]struct{})
	}
	s.producers[a.Name][producerStepID] = struct{}{}
}

func (s *artifactStore) Has(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.artifacts[name]
	return ok
}

func (s *artifactStore) Get(name string) (Artifact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.artifacts[name]
	return a, ok
}

func (s *artifactStore) IsProducedBy(name, stepID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set, ok := s.producers[name]
	if !ok {
		return false
	}
	_, ok = set[stepID]
	return ok
}
