package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Embedder produces text embeddings for skill-relevance ranking
// (token-efficiency branch 04, leaf 02). One call embeds a batch of texts
// and returns one vector per text, same order. Implementations may hit a
// network endpoint; SkillIndex never calls an Embedder while holding its
// mutex.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// embeddingTieEpsilon bounds the cosine-score window treated as a tie, so
// float noise cannot reorder skills between runs (determinism contract:
// ties resolve by name ascending).
const embeddingTieEpsilon = 1e-9

// SkillIndexEntry holds skill metadata only (no body) for fast lookup.
type SkillIndexEntry struct {
	// Name is the unique identifier for the skill.
	Name string `json:"name"`
	// Description is a human-readable description.
	Description string `json:"description"`
	// Requires lists capability tags (e.g., ["code", "reasoning"]).
	Requires []string `json:"requires,omitempty"`
	// RequiresTools lists tools the skill needs at execution time
	// (bare built-in names or server-qualified MCP names, "server.tool").
	RequiresTools []string `json:"requires_tools,omitempty"`
	// Tags are categorization labels.
	Tags []string `json:"tags,omitempty"`
	// Path is the filesystem path for lazy loading.
	Path string `json:"path"`
	// Priority indicates the discovery tier (0=project, 1=user, 2=system).
	Priority int `json:"priority"`
	// RiskLevel is the risk classification: "low", "medium", "high".
	RiskLevel string `json:"risk_level"`
	// AllowedTools is a subset of tool names this skill may use.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// Examples are sample prompts for trigger matching.
	Examples []string `json:"examples,omitempty"`
	// DisableModelInvocation omits the skill from the model-facing listing
	// (frontmatter `disable-model-invocation: true`); the skill stays
	// executable via explicit invocation paths.
	DisableModelInvocation bool `json:"disable_model_invocation,omitempty"`
	// SourceOrigin tracks which skill system the skill originated from ("meept", "claude", "hermes").
	SourceOrigin string `json:"source_origin,omitempty"`
}

// HasCapability checks if the entry requires a specific capability.
func (e *SkillIndexEntry) HasCapability(cap string) bool {
	capLower := strings.ToLower(cap)
	for _, c := range e.Requires {
		if strings.EqualFold(c, capLower) {
			return true
		}
	}
	return false
}

// HasTag checks if the entry has a specific tag.
func (e *SkillIndexEntry) HasTag(tag string) bool {
	tagLower := strings.ToLower(tag)
	for _, t := range e.Tags {
		if strings.EqualFold(t, tagLower) {
			return true
		}
	}
	return false
}

// SkillIndex provides fast lookup of skill metadata without bodies.
type SkillIndex struct {
	mu      sync.RWMutex
	entries map[string]*SkillIndexEntry // normalized name -> entry
	byTag   map[string][]*SkillIndexEntry
	byCap   map[string][]*SkillIndexEntry
	// dirty flags that the on-disk skill set changed since the last
	// Refresh. Set by MarkDirty (e.g. a write-path change notifier) and
	// cleared by the next Refresh.
	dirty bool
	// rescan, when installed via SetRescanSource, returns the fresh
	// on-disk skill tiers so Refresh can rebuild the index in place. A
	// nil rescan (the default) makes Refresh a no-op beyond clearing the
	// dirty flag, so a bare index stays inert until a discovery source is
	// wired. Refresh runs it OUTSIDE the index mutex (it does I/O).
	rescan func() ([]*SkillIndexEntry, error)
	// embedder ranks the model-facing skill listing by cosine similarity
	// to the latest task text (token-efficiency branch 04 leaf 02). nil
	// (the default; opt-in via config) means RankedNames returns the
	// fallback order untouched — ranking is an optimization, never a gate.
	embedder Embedder
	// embedCache maps normalized skill name -> cached embedding keyed by
	// the sha256 of the name+description text that produced it; a re-index
	// whose frontmatter hash is unchanged reuses the vector (zero embed
	// calls), a changed hash re-embeds lazily. Guarded by idx.mu like the
	// rest of the index.
	embedCache map[string]*skillEmbedding
}

// skillEmbedding is one cached skill embedding plus the fingerprint of the
// frontmatter text it was computed from.
type skillEmbedding struct {
	textHash string
	vector   []float32
}

// NewSkillIndex creates a new empty skill index.
func NewSkillIndex() *SkillIndex {
	return &SkillIndex{
		entries:    make(map[string]*SkillIndexEntry),
		byTag:      make(map[string][]*SkillIndexEntry),
		byCap:      make(map[string][]*SkillIndexEntry),
		embedCache: make(map[string]*skillEmbedding),
	}
}

// Index adds or updates an entry in the index.
func (idx *SkillIndex) Index(entry *SkillIndexEntry) {
	if entry == nil || entry.Name == "" {
		return
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.indexLocked(entry)
}

// indexLocked adds or updates an entry; the caller must hold idx.mu (write).
// It is the single maintenance path shared by Index and Refresh, so the
// in-place refresh never forks a second rebuild.
func (idx *SkillIndex) indexLocked(entry *SkillIndexEntry) {
	key := normalizeName(entry.Name)

	// Remove existing entry from secondary indices if present
	if existing, ok := idx.entries[key]; ok {
		idx.removeFromSecondaryIndices(existing)
	}

	// Add to main index
	idx.entries[key] = entry

	// Add to tag index
	for _, tag := range entry.Tags {
		tagKey := strings.ToLower(tag)
		idx.byTag[tagKey] = append(idx.byTag[tagKey], entry)
	}

	// Add to capability index
	for _, cap := range entry.Requires {
		capKey := strings.ToLower(cap)
		idx.byCap[capKey] = append(idx.byCap[capKey], entry)
	}
}

// removeFromSecondaryIndices removes an entry from tag and capability indices.
func (idx *SkillIndex) removeFromSecondaryIndices(entry *SkillIndexEntry) {
	// Remove from tag index
	for _, tag := range entry.Tags {
		tagKey := strings.ToLower(tag)
		idx.byTag[tagKey] = removeEntry(idx.byTag[tagKey], entry)
	}

	// Remove from capability index
	for _, cap := range entry.Requires {
		capKey := strings.ToLower(cap)
		idx.byCap[capKey] = removeEntry(idx.byCap[capKey], entry)
	}
}

// removeEntry removes a specific entry from a slice.
func removeEntry(entries []*SkillIndexEntry, target *SkillIndexEntry) []*SkillIndexEntry {
	result := make([]*SkillIndexEntry, 0, len(entries))
	for _, e := range entries {
		if e != target {
			result = append(result, e)
		}
	}
	return result
}

// IndexAll adds multiple entries to the index.
func (idx *SkillIndex) IndexAll(entries []*SkillIndexEntry) {
	for _, entry := range entries {
		idx.Index(entry)
	}
}

// Get retrieves an entry by name (case-insensitive).
func (idx *SkillIndex) Get(name string) *SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.entries[normalizeName(name)]
}

// List returns all indexed entries sorted by name.
func (idx *SkillIndex) List() []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]*SkillIndexEntry, 0, len(idx.entries))
	for _, entry := range idx.entries {
		result = append(result, entry)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Names returns sorted list of all indexed skill names.
func (idx *SkillIndex) Names() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	names := make([]string, 0, len(idx.entries))
	for _, entry := range idx.entries {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// FindByTag returns entries that have a specific tag.
func (idx *SkillIndex) FindByTag(tag string) []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tagKey := strings.ToLower(tag)
	entries := idx.byTag[tagKey]
	result := make([]*SkillIndexEntry, len(entries))
	copy(result, entries)
	return result
}

// FindByCapability returns entries that require a specific capability.
func (idx *SkillIndex) FindByCapability(cap string) []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	capKey := strings.ToLower(cap)
	entries := idx.byCap[capKey]
	result := make([]*SkillIndexEntry, len(entries))
	copy(result, entries)
	return result
}

// FindByCapabilities returns entries whose requirements are all satisfied by caps.
func (idx *SkillIndex) FindByCapabilities(caps []string) []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Build capability set
	capSet := make(map[string]bool)
	for _, c := range caps {
		capSet[strings.ToLower(c)] = true
	}

	var results []*SkillIndexEntry
	for _, entry := range idx.entries {
		if idx.entrySatisfiedBy(entry, capSet) {
			results = append(results, entry)
		}
	}

	return results
}

// entrySatisfiedBy checks if an entry's requirements are satisfied by the capability set.
func (idx *SkillIndex) entrySatisfiedBy(entry *SkillIndexEntry, capSet map[string]bool) bool {
	// Entries with no requirements are always satisfied
	if len(entry.Requires) == 0 {
		return true
	}

	for _, req := range entry.Requires {
		if !capSet[strings.ToLower(req)] {
			return false
		}
	}
	return true
}

// Count returns the number of indexed entries.
func (idx *SkillIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// Clear removes all entries from the index.
func (idx *SkillIndex) Clear() {
	if idx == nil {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.entries = make(map[string]*SkillIndexEntry)
	idx.byTag = make(map[string][]*SkillIndexEntry)
	idx.byCap = make(map[string][]*SkillIndexEntry)
	idx.embedCache = make(map[string]*skillEmbedding)
}

// MarkDirty flags that the on-disk skill set changed since the last
// Refresh. It is O(1): it only sets the dirty flag under the index mutex,
// it does not rescan. Branch 2's write-path change notifier
// (lifecycle.Writer.SetChangeNotifier) is the in-tree caller that arrives
// after this leaf; marking is a cross-branch contract, so it lands now.
func (idx *SkillIndex) MarkDirty() {
	if idx == nil {
		return
	}
	idx.mu.Lock()
	idx.dirty = true
	idx.mu.Unlock()
}

// IsDirty reports whether MarkDirty has been observed since the last
// Refresh. A nil receiver reads clean so miswired indexes are inert.
func (idx *SkillIndex) IsDirty() bool {
	if idx == nil {
		return false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.dirty
}

// SetRescanSource installs the discovery source that Refresh uses to read
// the current on-disk skill tiers. fn returns freshly discovered metadata
// entries and may do I/O; Refresh runs it OUTSIDE the index mutex, then
// rebuilds the index in place. A nil fn (the default) leaves refresh inert:
// Refresh still settles the dirty flag but rescans nothing. The setter is
// nil-guarded; a nil receiver or nil fn is a no-op.
func (idx *SkillIndex) SetRescanSource(fn func() ([]*SkillIndexEntry, error)) {
	if idx == nil {
		return
	}
	idx.mu.Lock()
	idx.rescan = fn
	idx.mu.Unlock()
}

// Refresh rescans the configured skill tiers and rebuilds the index in place.
// It reuses the shared indexLocked maintenance path (the same primitive Index
// uses), so a refresh that observes an unchanged on-disk set reinserts
// byte-identical entries and downstream consumers (CapabilityIndex, the
// loop-economics stable prefix) see no change. It clears the dirty flag and
// returns a wrapped error only when the tier rescan itself fails. The rescan
// (the only I/O) runs before the write lock is taken, so the mutex is never
// held across I/O.
func (idx *SkillIndex) Refresh() error {
	if idx == nil {
		return nil
	}

	// Snapshot the rescan hook without holding the lock across its I/O.
	idx.mu.RLock()
	rescan := idx.rescan
	idx.mu.RUnlock()

	if rescan == nil {
		// No tiers wired: settle the flag and stop. Refresh is a no-op
		// beyond clearing dirty, preserving the byte-stable invariant.
		idx.mu.Lock()
		idx.dirty = false
		idx.mu.Unlock()
		return nil
	}

	entries, err := rescan()
	if err != nil {
		// Leave the flag set so a later turn retries the refresh.
		return fmt.Errorf("skill index rescan: %w", err)
	}

	// Rebuild in place under the write lock through the shared path.
	idx.mu.Lock()
	idx.entries = make(map[string]*SkillIndexEntry)
	idx.byTag = make(map[string][]*SkillIndexEntry)
	idx.byCap = make(map[string][]*SkillIndexEntry)
	for _, e := range entries {
		if e == nil || e.Name == "" {
			continue
		}
		idx.indexLocked(e)
	}
	idx.dirty = false
	idx.mu.Unlock()

	return nil
}

// AllTags returns all unique tags across all indexed entries.
func (idx *SkillIndex) AllTags() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tags := make([]string, 0, len(idx.byTag))
	for tag := range idx.byTag {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// AllCapabilities returns all unique capability requirements across all entries.
func (idx *SkillIndex) AllCapabilities() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	caps := make([]string, 0, len(idx.byCap))
	for cap := range idx.byCap {
		caps = append(caps, cap)
	}
	sort.Strings(caps)
	return caps
}

// Match performs fuzzy matching on skill name and description.
func (idx *SkillIndex) Match(query string) *SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)
	var bestMatch *SkillIndexEntry
	bestScore := 0

	// Iterate in sorted name order so ties at the same score resolve
	// deterministically (later-sorted name wins via the >= comparison).
	// Without this, map iteration order makes Match() non-deterministic
	// across runs for equally-scored entries.
	names := make([]string, 0, len(idx.entries))
	for name := range idx.entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := idx.entries[name]
		score := matchEntryScore(entry, queryLower)
		if score >= bestScore && score > 0 {
			bestScore = score
			bestMatch = entry
		}
	}

	// Require minimum score
	if bestScore < 1 {
		return nil
	}

	return bestMatch
}

// matchEntryScore calculates a fuzzy match score for an entry.
func matchEntryScore(entry *SkillIndexEntry, queryLower string) int {
	score := 0
	nameLower := strings.ToLower(entry.Name)
	descLower := strings.ToLower(entry.Description)

	// Exact name match
	switch {
	case nameLower == queryLower:
		score += 100
	case strings.HasPrefix(nameLower, queryLower):
		score += 50
	case strings.Contains(nameLower, queryLower):
		score += 30
	}

	// Description match
	if strings.Contains(descLower, queryLower) {
		score += 10
	}

	// Word match in name
	queryWords := strings.Fields(queryLower)
	nameWords := strings.Fields(nameLower)
	for _, qw := range queryWords {
		for _, nw := range nameWords {
			if nw == qw {
				score += 5
			}
		}
	}

	// Tag match
	for _, tag := range entry.Tags {
		if strings.EqualFold(tag, queryLower) {
			score += 20
		} else if strings.Contains(strings.ToLower(tag), queryLower) {
			score += 5
		}
	}

	// Example match
	for _, example := range entry.Examples {
		if strings.Contains(strings.ToLower(example), queryLower) {
			score += 15
		}
	}

	return score
}

// SkillIndexMatch holds an entry with its match score.
type SkillIndexMatch struct {
	Entry *SkillIndexEntry
	Score int
}

// MatchAll performs fuzzy matching and returns all entries with scores.
func (idx *SkillIndex) MatchAll(query string) []*SkillIndexMatch {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)
	var matches []*SkillIndexMatch

	for _, entry := range idx.entries {
		score := matchEntryScore(entry, queryLower)
		if score > 0 {
			matches = append(matches, &SkillIndexMatch{
				Entry: entry,
				Score: score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	return matches
}

// SetEmbedder installs the Embedder used by RankedNames to order the
// model-facing skill listing (token-efficiency branch 04 leaf 02, Q4:
// always rank). Nil-safe per the setter convention: a nil receiver or nil
// embedder is a no-op — passing nil explicitly disables ranking and reverts
// RankedNames to pure fallback order. The embed cache is dropped when the
// embedder changes: vectors from a different model are incomparable, and a
// fresh embedder must not rank against stale ones.
func (idx *SkillIndex) SetEmbedder(e Embedder) {
	if idx == nil {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if e == nil {
		idx.embedder = nil
		return
	}
	if idx.embedder != nil {
		// Embedder swap: prior vectors are meaningless under the new model.
		idx.embedCache = make(map[string]*skillEmbedding)
	}
	idx.embedder = e
}

// skillEmbeddingText is the text embedded per skill: name + description
// (leaf doc). It is the fingerprint basis for cache reuse, so a frontmatter
// edit that changes either field forces a re-embed.
func skillEmbeddingText(entry *SkillIndexEntry) string {
	return entry.Name + "\n" + entry.Description
}

func skillEmbeddingHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// RankedNames orders fallback names by cosine similarity between the latest
// task text and each skill's name+description embedding. It is ORDER-ONLY:
// the returned slice is a permutation of fallback — no name is ever added,
// hidden, or capped (Q4 ruling: always rank, never gate). Any failure mode
// — nil embedder, empty task text, embedder error, a name missing from the
// index or without a usable vector, or dimension mismatch — degrades to the
// fallback order untouched, never an error.
//
// There is no ctx parameter (leaf contract): embedder calls use
// context.Background() and rely on the embedder's own client timeout —
// ranking must never stall an episode, and degradation is silent-safe.
// Embedding work (network I/O) happens strictly OUTSIDE the index mutex.
// Determinism: ties within embeddingTieEpsilon resolve by name ascending, so
// repeated calls with the same inputs produce byte-identical order.
func (idx *SkillIndex) RankedNames(taskText string, fallback []string) []string {
	if idx == nil || len(fallback) == 0 {
		return fallback
	}

	idx.mu.RLock()
	embedder := idx.embedder
	idx.mu.RUnlock()

	if embedder == nil || strings.TrimSpace(taskText) == "" {
		return fallback
	}

	// Collect the names needing embedding (cache miss / frontmatter change)
	// under a read lock; snapshot texts deterministically (sorted names) so
	// batch order never depends on map iteration.
	type pendingEmbed struct {
		name string
		text string
	}
	var pending []pendingEmbed
	vectors := make(map[string][]float32, len(fallback))

	idx.mu.RLock()
	sortedNames := make([]string, 0, len(idx.entries))
	for _, defName := range fallback {
		sortedNames = append(sortedNames, normalizeName(defName))
	}
	sort.Strings(sortedNames)
	for _, n := range sortedNames {
		entry, ok := idx.entries[n]
		if !ok || entry.DisableModelInvocation {
			continue
		}
		text := skillEmbeddingText(entry)
		hash := skillEmbeddingHash(text)
		if cached, exists := idx.embedCache[n]; exists && cached.textHash == hash {
			vectors[n] = cached.vector
			continue
		}
		pending = append(pending, pendingEmbed{name: n, text: text})
	}
	idx.mu.RUnlock()

	// Embed missing/stale vectors outside the lock. On error the already
	// cached subset simply ranks what it can; the rest fall through below,
	// and an all-missing set degrades to fallback order.
	if len(pending) > 0 {
		texts := make([]string, len(pending))
		for i, p := range pending {
			texts[i] = p.text
		}
		emb, err := embedder.Embed(context.Background(), texts)
		if err == nil && len(emb) == len(pending) {
			idx.mu.Lock()
			if idx.embedCache == nil {
				idx.embedCache = make(map[string]*skillEmbedding, len(pending))
			}
			for i, p := range pending {
				idx.embedCache[p.name] = &skillEmbedding{
					textHash: skillEmbeddingHash(p.text),
					vector:   emb[i],
				}
				vectors[p.name] = emb[i]
			}
			idx.mu.Unlock()
		} else if err != nil {
			// Soft failure: ranking is never worth an episode error.
			return fallback
		}
	}

	// Embed the task text. Failure = fallback order.
	taskVecs, err := embedder.Embed(context.Background(), []string{taskText})
	if err != nil || len(taskVecs) == 0 {
		return fallback
	}
	taskVec := taskVecs[0]

	seen := make(map[string]bool, len(fallback))
	type nameScore struct {
		name  string
		score float64
		known bool
	}
	ranked := make([]nameScore, 0, len(fallback))
	for _, defName := range fallback {
		key := normalizeName(defName)
		if seen[key] {
			continue
		}
		seen[key] = true
		vec, ok := vectors[key]
		if !ok {
			// No vector (unknown skill, embed gap): keep it, sink to the
			// unranked tail in name order. Never drop.
			ranked = append(ranked, nameScore{name: defName})
			continue
		}
		ranked = append(ranked, nameScore{name: defName, score: cosineSimilarityF32(taskVec, vec), known: true})
	}

	// Stable sort: known vectors first by score desc (ties by name asc),
	// then unknowns by name asc. Deterministic across runs (-count=10).
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.known != b.known {
			return a.known
		}
		if math.Abs(a.score-b.score) > embeddingTieEpsilon {
			return a.score > b.score
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})

	out := make([]string, len(ranked))
	for i, r := range ranked {
		out[i] = r.name
	}
	return out
}

// cosineSimilarityF32 computes cosine similarity between two float32
// vectors, returning 0 for length mismatch or zero-norm inputs.
func cosineSimilarityF32(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
