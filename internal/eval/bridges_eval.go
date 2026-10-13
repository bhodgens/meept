package eval

// B05 — bridges-on/off recall eval (branch 09, P10 gate). The harness is
// hermetic by default: a committed synthetic fixture feeds the REAL bridge
// pipeline (memory.GenerateBridges + memory.VerifyBridges with a scripted
// generator/verifier — zero LLM calls) and the REAL recall-side selection
// seam (vector.SelectBalanced, arm B) versus plain top-k (arm A). Grading in
// synthetic mode is a mock grader: the "answer" is the injected context
// itself, and an expected fact counts as answered when its token overlap with
// the context clears a threshold. Live-model runs (a local llama-server
// answering each question from the injected context) are a follow-up — pass
// an AnswerFunc from the CLI wrapper (--live) to enable them.
//
// The runner is idempotent and resumable: per-(question, arm) rows already
// present in the output artifact (same fixture hash + config hash) are reused
// on re-run; only missing rows execute.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/vector"
)

// SchemaVersion is the artifact schema this harness writes. Resume logic
// discards rows from artifacts with a different version.
const SchemaVersion = 1

// BridgeEvalDefaults mirrors the arm-B contract (memory.bridges.* defaults):
// kb=2, verify on, bridge_df_max=10.
const (
	DefaultKB            = 2
	DefaultTopK          = 8
	DefaultMatchThresh   = 0.6
	DefaultBridgeDFMax   = 10
	BridgeEvalDim        = 256
	tokenizerCharsPerTok = 3.0 // llm.HeuristicTokenizer's ratio
)

// BridgeEvalMemory is one synthetic source memory in the fixture. Entities is
// the comma-separated BridgeMetaEntities form the generation pass reads.
type BridgeEvalMemory struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Entities  string `json:"entities"`
}

// BridgeEvalQuestion is one cross-session recall question. ExpectedFacts are
// the short gold facts the grader checks; SourceMemoryIDs record provenance
// (which memories the answer spans) without embedding any private content.
type BridgeEvalQuestion struct {
	ID              string   `json:"id"`
	Question        string   `json:"question"`
	ExpectedFacts   []string `json:"expected_facts"`
	SourceMemoryIDs []string `json:"source_memory_ids"`
}

// BridgesFixture is the committed question set + synthetic memory corpus.
type BridgesFixture struct {
	Memories  []BridgeEvalMemory   `json:"memories"`
	Questions []BridgeEvalQuestion `json:"questions"`
}

// BridgesEvalConfig carries the arm-independent run knobs. The config hash
// keys resume validity: changing any knob invalidates previous rows.
type BridgesEvalConfig struct {
	KB             int     `json:"kb"`
	TopK           int     `json:"top_k"`
	MatchThreshold float64 `json:"match_threshold"`
	BridgeDFMax    int     `json:"bridge_df_max"`
	Verify         bool    `json:"verify"`
	Live           bool    `json:"live"`
}

// AnswerFunc produces the answer for one question given the injected context.
// Synthetic mode supplies the mock (the context itself); live mode wraps an
// OpenAI-compatible endpoint. Errors abort the question, not the run — the
// row records the failure as zero correctness.
type AnswerFunc func(question, context string) (string, error)

// ---- scripted generator / verifier (hermetic stand-ins for the LLM seams) ----

// ScriptedBridgeGenerator is the deterministic BridgeGenerator used by the
// synthetic run: for each bridge entity it emits one fact joining the
// shortest sentence containing the entity from each candidate memory. The
// output matches the grammar-constrained shape the real generator produces
// ([{"text","evidence_ids"}]), so the real persist path (provenance rules
// included) runs unmodified. Entities records every entity the pass asked
// about (the side table does not persist the entity key, so the pool info
// captures it here instead).
type ScriptedBridgeGenerator struct {
	Entities []string
}

var _ memory.BridgeGenerator = (*ScriptedBridgeGenerator)(nil)

// GenerateBridgeFacts implements memory.BridgeGenerator (no LLM call).
func (g *ScriptedBridgeGenerator) GenerateBridgeFacts(_ context.Context, entity string, sources []memory.Memory) (string, error) {
	g.Entities = append(g.Entities, entity)
	type fact struct {
		Text        string   `json:"text"`
		EvidenceIDs []string `json:"evidence_ids"`
	}
	clauses := make([]string, 0, len(sources))
	ids := make([]string, 0, len(sources))
	for _, s := range sources {
		if c := shortestSentenceContaining(s.Content, entity); c != "" {
			clauses = append(clauses, c)
		}
		ids = append(ids, s.ID)
	}
	if len(clauses) < 2 {
		// A bridge needs at least two linking clauses to be meaningful.
		return "[]", nil
	}
	out := []fact{{
		Text:        fmt.Sprintf("%s bridge: %s", entity, strings.Join(clauses, "; ")),
		EvidenceIDs: ids,
	}}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// shortestSentenceContaining returns the shortest sentence of text that
// mentions entity (case-insensitive), trimmed of terminal punctuation.
func shortestSentenceContaining(text, entity string) string {
	best := ""
	for _, sent := range strings.Split(text, ". ") {
		sent = strings.TrimSpace(sent)
		if sent == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(sent), strings.ToLower(entity)) {
			continue
		}
		if best == "" || len(sent) < len(best) {
			best = sent
		}
	}
	return strings.Trim(best, ".")
}

// ScriptedBridgeVerifier is the deterministic BridgeVerifier: fixture bridges
// are constructed directly from their evidence sentences, so every record is
// entailed by construction and the B03 verify-on pass (arm B contract) runs
// with accept-all semantics.
type ScriptedBridgeVerifier struct{}

var _ memory.BridgeVerifier = ScriptedBridgeVerifier{}

// VerifyBridge implements memory.BridgeVerifier (no LLM call).
func (ScriptedBridgeVerifier) VerifyBridge(_ context.Context, _ memory.BridgeRecord) (bool, string, error) {
	return true, "scripted: fact joins sentences from its evidence memories", nil
}

// BuildBridges runs the REAL generation + verification pipeline over the
// fixture corpus with the scripted seams and returns the persisted, verified
// bridge records plus the list of bridge entities the pass explored. storePath
// is a temp-dir path owned by the caller.
func BuildBridges(ctx context.Context, fx *BridgesFixture, cfg BridgesEvalConfig, storePath string) ([]memory.BridgeRecord, []string, error) {
	memories := make([]memory.Memory, 0, len(fx.Memories))
	for _, m := range fx.Memories {
		memories = append(memories, memory.Memory{
			ID:      m.ID,
			Content: m.Content,
			Type:    memory.MemoryTypeEpisodic,
			Metadata: map[string]any{
				memory.BridgeMetaEntities: m.Entities,
			},
		})
	}
	store, err := memory.NewBridgeStore(storePath)
	if err != nil {
		return nil, nil, fmt.Errorf("eval: bridge store: %w", err)
	}
	defer func() { _ = store.Close() }()

	genCfg := memory.BridgesConfig{
		Enabled:     true,
		BridgeDFMax: cfg.BridgeDFMax,
		Verify:      cfg.Verify,
	}
	gen := &ScriptedBridgeGenerator{}
	if _, _, err := memory.GenerateBridges(ctx, store, gen, genCfg, memories); err != nil {
		return nil, nil, fmt.Errorf("eval: bridge generation: %w", err)
	}
	if cfg.Verify {
		if _, _, err := memory.VerifyBridges(ctx, store, ScriptedBridgeVerifier{}, genCfg); err != nil {
			return nil, nil, fmt.Errorf("eval: bridge verification: %w", err)
		}
	}
	records, err := store.ListBridges(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("eval: bridge list: %w", err)
	}
	return records, gen.Entities, nil
}

// ---- recall simulation (real vector.SelectBalanced seam) ----

// bridgeUnit is one ranked candidate for the recall pass.
type bridgeUnit struct {
	ID      string
	Content string
	Bridge  bool
	Score   float64
}

// HashWordEmbedding derives a deterministic dim-vector from the lowercase
// word tokens of text (FNV-1a bucketing, L2-normalized). Hermetic stand-in
// for the semantic embedder: lexical overlap drives the ranking, which is
// the property the mock-grader recall signal needs.
func HashWordEmbedding(text string, dim int) []float32 {
	v := make([]float32, dim)
	for _, w := range TokenizeLower(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[int(h.Sum32())%dim]++
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	out := make([]float32, dim)
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

// CosineSim returns the cosine similarity of two equal-length vectors.
func CosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// TokenizeLower lowercases, strips punctuation, and splits into word tokens.
func TokenizeLower(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	return fields
}

// tokenSet builds the multiset-free token map used by the mock grader.
func tokenSet(s string) map[string]bool {
	toks := map[string]bool{}
	for _, t := range TokenizeLower(s) {
		toks[t] = true
	}
	return toks
}

// TokenOverlap returns |A∩B| / |A| — the fraction of a's tokens present in b.
func TokenOverlap(a, b string) float64 {
	ta, tb := tokenSet(a), tokenSet(b)
	if len(ta) == 0 {
		return 0
	}
	hit := 0
	for t := range ta {
		if tb[t] {
			hit++
		}
	}
	return float64(hit) / float64(len(ta))
}

// runRecallQuery ranks sources (+ bridges when arm B), applies the REAL
// vector.SelectBalanced balancing pass, truncates to topK, and returns the
// selected units plus the selection stats the artifact reports.
func runRecallQuery(query string, sources, bridges []bridgeUnit, cfg BridgesEvalConfig, includeBridges bool) (selected []bridgeUnit, stats vector.SelectionStats) {
	q := HashWordEmbedding(query, BridgeEvalDim)
	ranked := make([]bridgeUnit, 0, len(sources)+len(bridges))
	for _, u := range sources {
		u.Score = CosineSim(q, HashWordEmbedding(u.Content, BridgeEvalDim))
		ranked = append(ranked, u)
	}
	if includeBridges {
		for _, u := range bridges {
			u.Score = CosineSim(q, HashWordEmbedding(u.Content, BridgeEvalDim))
			ranked = append(ranked, u)
		}
	}
	// Deterministic order: score desc, then ID asc on ties (hash embeddings
	// tie often; vector search ties would be resolved by row order).
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].ID < ranked[j].ID
	})

	// The production seam (B04): greedy pass capping bridge share at kb.
	results := make([]vector.SearchResult, 0, len(ranked))
	for _, u := range ranked {
		md := map[string]any{}
		if u.Bridge {
			md[vector.BridgeMetadataKey] = vector.BridgeUnitTypeValue
		}
		results = append(results, vector.SearchResult{
			MemoryID:       u.ID,
			Content:        u.Content,
			Metadata:       md,
			RelevanceScore: u.Score,
		})
	}
	filtered, stats := vector.SelectBalanced(results, cfg.KB)
	if len(filtered) > cfg.TopK {
		filtered = filtered[:cfg.TopK]
	}
	selected = make([]bridgeUnit, 0, len(filtered))
	for _, r := range filtered {
		selected = append(selected, bridgeUnit{
			ID:      r.MemoryID,
			Content: r.Content,
			Bridge:  r.Metadata[vector.BridgeMetadataKey] == vector.BridgeUnitTypeValue,
			Score:   r.RelevanceScore,
		})
	}
	return selected, stats
}

// InjectedContext assembles the context string from the selected units —
// token counts are measured on THIS string, matching what a real prompt
// builder would inject.
func InjectedContext(units []bridgeUnit) string {
	parts := make([]string, 0, len(units))
	for _, u := range units {
		parts = append(parts, u.Content)
	}
	return strings.Join(parts, "\n")
}

// CountContextTokens measures the injected context size with the production
// estimator (llm.HeuristicTokenizer, 3 chars/token) — measured, not
// estimated post-hoc from source sizes.
func CountContextTokens(context string) int {
	tk := llm.HeuristicTokenizer{}
	return tk.CountTokens(context)
}

// ---- grading ----

// GraderResult is the mock grader's verdict for one question.
type GraderResult struct {
	Correct      bool      `json:"correct"`
	CoveredFacts int       `json:"covered_facts"`
	TotalFacts   int       `json:"total_facts"`
	Overlap      []float64 `json:"overlap"`
}

// GradeRecallAnswer grades the answer against the question's expected facts:
// a fact is covered when its token overlap with the answer clears threshold.
// In synthetic mode the answer IS the injected context (context-coverage
// proxy); in live mode it is the model's reply. Deterministic, canned-output
// friendly — unit tests drive it directly.
func GradeRecallAnswer(answer string, expectedFacts []string, threshold float64) GraderResult {
	res := GraderResult{TotalFacts: len(expectedFacts), Overlap: make([]float64, 0, len(expectedFacts))}
	for _, f := range expectedFacts {
		ov := TokenOverlap(f, answer)
		res.Overlap = append(res.Overlap, math.Round(ov*1000)/1000)
		if ov >= threshold {
			res.CoveredFacts++
		}
	}
	res.Correct = res.TotalFacts > 0 && res.CoveredFacts == res.TotalFacts
	return res
}

// ---- run + artifact ----

// QuestionRow is the per-(question, arm) result row persisted in the
// artifact — the resume unit.
type QuestionRow struct {
	QuestionID      string            `json:"question_id"`
	Arm             string            `json:"arm"`
	ExpectedFacts   int               `json:"expected_facts"`
	CoveredFacts    int               `json:"covered_facts"`
	Correct         bool              `json:"correct"`
	CorrectnessPct  float64           `json:"correctness_pct"`
	ContextTokens   int               `json:"context_tokens"`
	LatencyMs       float64           `json:"latency_ms"`
	SourcesSelected []string          `json:"sources_selected"`
	BridgesSelected []string          `json:"bridges_selected"`
	Stats           SelectionStatsRow `json:"selection_stats"`
	Answer          string            `json:"answer,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// SelectionStatsRow carries the vector.SelectionStats outcome per query.
type SelectionStatsRow struct {
	Total           int `json:"total"`
	Sources         int `json:"sources"`
	BridgesIncluded int `json:"bridges_included"`
	BridgesExcluded int `json:"bridges_excluded"`
}

// ArmAggregate is the per-arm summary.
type ArmAggregate struct {
	Arm                string  `json:"arm"`
	Questions          int     `json:"questions"`
	Errors             int     `json:"errors"`
	CorrectnessPct     float64 `json:"correctness_pct"`
	MeanContextTokens  float64 `json:"mean_context_tokens"`
	MeanLatencyMs      float64 `json:"mean_latency_ms"`
	BridgeQueryCount   int     `json:"bridge_query_count"`
	BridgesIncluded    int     `json:"bridges_included_total"`
	BridgesIncludedAvg float64 `json:"bridges_included_avg_per_query"`
	BridgesExcluded    int     `json:"bridges_excluded_total"`
	BridgesAvailable   int     `json:"bridges_available"`
}

// PassCriteria is the owner-tunable adoption gate from the spec.
type PassCriteria struct {
	CorrectnessNonRegression bool   `json:"correctness_non_regression"`
	TokenReduction           bool   `json:"token_reduction"`
	CorrectnessGain          bool   `json:"correctness_gain"`
	Pass                     bool   `json:"pass"`
	Rule                     string `json:"rule"`
}

// BridgesEvalReport is the artifact document.
type BridgesEvalReport struct {
	SchemaVersion int                     `json:"schema_version"`
	Eval          string                  `json:"eval"`
	Mode          string                  `json:"mode"`
	RunAt         string                  `json:"run_at"`
	Note          string                  `json:"note"`
	Config        BridgesEvalConfig       `json:"config"`
	FixturePath   string                  `json:"fixture_path"`
	FixtureHash   string                  `json:"fixture_hash"`
	ConfigHash    string                  `json:"config_hash"`
	Bridges       BridgePoolInfo          `json:"bridge_pool"`
	Arms          map[string]ArmAggregate `json:"arms"`
	PassCriteria  *PassCriteria           `json:"pass_criteria"`
	Questions     []QuestionRow           `json:"questions"`
}

// BridgePoolInfo describes the generated bridge pool (arm B only).
type BridgePoolInfo struct {
	Generated int      `json:"generated"`
	Verified  int      `json:"verified"`
	Entities  []string `json:"entities,omitempty"`
}

// LoadFixture reads and validates the fixture JSON.
func LoadFixture(path string) (*BridgesFixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: fixture: %w", err)
	}
	var fx BridgesFixture
	if err := json.Unmarshal(b, &fx); err != nil {
		return nil, fmt.Errorf("eval: fixture parse: %w", err)
	}
	if len(fx.Memories) == 0 || len(fx.Questions) == 0 {
		return nil, fmt.Errorf("eval: fixture %s has no memories or no questions", path)
	}
	if len(fx.Questions) < 20 || len(fx.Questions) > 40 {
		return nil, fmt.Errorf("eval: fixture %s has %d questions, want 20-40", path, len(fx.Questions))
	}
	ids := make(map[string]bool, len(fx.Memories))
	for _, m := range fx.Memories {
		if m.ID == "" || m.Content == "" {
			return nil, fmt.Errorf("eval: fixture memory with empty id/content")
		}
		ids[m.ID] = true
	}
	for _, q := range fx.Questions {
		if q.ID == "" || q.Question == "" || len(q.ExpectedFacts) == 0 {
			return nil, fmt.Errorf("eval: fixture question %q malformed", q.ID)
		}
		for _, sid := range q.SourceMemoryIDs {
			if !ids[sid] {
				return nil, fmt.Errorf("eval: fixture question %q references unknown memory %q", q.ID, sid)
			}
		}
	}
	return &fx, nil
}

// HashBytes returns the sha256 hex prefix used for fixture/config hashing.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// ConfigHash keys resume validity on the arm-independent knobs.
func (c BridgesEvalConfig) ConfigHash() string {
	b, _ := json.Marshal(c)
	return HashBytes(b)
}

// fixtureHash hashes the raw fixture bytes (content-addressed resume).
func fixtureHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return HashBytes(b), nil
}

// LoadReport reads an existing artifact for resume; a missing file, a schema
// mismatch, or a fixture/config hash mismatch returns a fresh report.
func LoadReport(path string, fixtureHash, configHash string) *BridgesEvalReport {
	fresh := &BridgesEvalReport{
		SchemaVersion: SchemaVersion,
		Eval:          "bridges-on-off-recall",
		FixtureHash:   fixtureHash,
		ConfigHash:    configHash,
		Arms:          map[string]ArmAggregate{},
		Questions:     []QuestionRow{},
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fresh
	}
	var prev BridgesEvalReport
	if err := json.Unmarshal(b, &prev); err != nil {
		return fresh
	}
	if prev.SchemaVersion != SchemaVersion || prev.FixtureHash != fixtureHash || prev.ConfigHash != configHash {
		return fresh
	}
	prev.SchemaVersion = SchemaVersion
	prev.Eval = "bridges-on-off-recall"
	if prev.Questions == nil {
		prev.Questions = []QuestionRow{}
	}
	if prev.Arms == nil {
		prev.Arms = map[string]ArmAggregate{}
	}
	return &prev
}

// RunArm executes every fixture question for one arm and returns the rows.
// Existing rows (keyed by question ID + arm) are skipped — resume.
func RunArm(ctx context.Context, fx *BridgesFixture, cfg BridgesEvalConfig, arm string, bridges []memory.BridgeRecord, answer AnswerFunc, existing map[string]QuestionRow) ([]QuestionRow, error) {
	includeBridges := arm == "b"

	sources := make([]bridgeUnit, 0, len(fx.Memories))
	for _, m := range fx.Memories {
		sources = append(sources, bridgeUnit{ID: m.ID, Content: m.Content})
	}
	bridgeUnits := make([]bridgeUnit, 0, len(bridges))
	for _, r := range bridges {
		bridgeUnits = append(bridgeUnits, bridgeUnit{ID: r.ID, Content: r.Content, Bridge: true})
	}

	rows := make([]QuestionRow, 0, len(fx.Questions))
	for _, q := range fx.Questions {
		if prev, ok := existing[q.ID+"|"+arm]; ok {
			rows = append(rows, prev)
			continue
		}
		row := QuestionRow{QuestionID: q.ID, Arm: arm, ExpectedFacts: len(q.ExpectedFacts)}
		start := time.Now()

		selected, stats := runRecallQuery(q.Question, sources, bridgeUnits, cfg, includeBridges)
		row.Stats = SelectionStatsRow{
			Total:           stats.Total,
			Sources:         stats.Sources,
			BridgesIncluded: stats.BridgesIncluded,
			BridgesExcluded: stats.BridgesExcluded,
		}
		contextStr := InjectedContext(selected)
		row.ContextTokens = CountContextTokens(contextStr)
		for _, u := range selected {
			if u.Bridge {
				row.BridgesSelected = append(row.BridgesSelected, u.ID)
			} else {
				row.SourcesSelected = append(row.SourcesSelected, u.ID)
			}
		}

		ans, err := answer(q.Question, contextStr)
		row.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0
		if err != nil {
			row.Error = err.Error()
			rows = append(rows, row)
			continue
		}
		if cfg.Live {
			row.Answer = ans
		}
		g := GradeRecallAnswer(ans, q.ExpectedFacts, cfg.MatchThreshold)
		row.CoveredFacts = g.CoveredFacts
		row.Correct = g.Correct
		row.CorrectnessPct = math.Round(float64(g.CoveredFacts)/float64(g.TotalFacts)*10000) / 100
		rows = append(rows, row)
	}
	return rows, nil
}

// AggregateRows folds per-question rows into the arm summary.
func AggregateRows(arm string, rows []QuestionRow, bridgesAvailable int) ArmAggregate {
	agg := ArmAggregate{Arm: arm, Questions: len(rows), BridgesAvailable: bridgesAvailable}
	var tokSum, latSum float64
	for _, r := range rows {
		if r.Error != "" {
			agg.Errors++
			continue
		}
		agg.CorrectnessPct += r.CorrectnessPct
		tokSum += float64(r.ContextTokens)
		latSum += r.LatencyMs
		agg.BridgesIncluded += r.Stats.BridgesIncluded
		agg.BridgesExcluded += r.Stats.BridgesExcluded
		if r.Stats.BridgesIncluded > 0 {
			agg.BridgeQueryCount++
		}
	}
	n := float64(agg.Questions - agg.Errors)
	if n > 0 {
		agg.CorrectnessPct = math.Round(agg.CorrectnessPct/n*100) / 100
		agg.MeanContextTokens = math.Round(tokSum/n*1000) / 1000
		agg.MeanLatencyMs = math.Round(latSum/n*1000) / 1000
		agg.BridgesIncludedAvg = math.Round(float64(agg.BridgesIncluded)/n*1000) / 1000
	}
	return agg
}

// EvaluatePassCriteria applies the spec's suggested gate (owner-tunable):
// correctness(B) >= correctness(A) - 2pp AND tokens(B) <= tokens(A) * 0.9,
// OR correctness(B) >= correctness(A) + 3pp at (approximately) equal tokens.
func EvaluatePassCriteria(armA, armB ArmAggregate) *PassCriteria {
	const tol = 0.02 // 2% relative tolerance for "equal tokens"
	pc := &PassCriteria{
		Rule: "pass = (corrB >= corrA - 2pp AND tokensB <= tokensA*0.9) OR (corrB >= corrA + 3pp AND tokensB ~= tokensA)",
	}
	equalTokens := armA.MeanContextTokens > 0 &&
		math.Abs(armB.MeanContextTokens-armA.MeanContextTokens) <= tol*armA.MeanContextTokens
	pc.CorrectnessNonRegression = armB.CorrectnessPct >= armA.CorrectnessPct-2.0
	pc.TokenReduction = armB.MeanContextTokens <= armA.MeanContextTokens*0.9
	pc.CorrectnessGain = armB.CorrectnessPct >= armA.CorrectnessPct+3.0
	pc.Pass = (pc.CorrectnessNonRegression && pc.TokenReduction) || (pc.CorrectnessGain && equalTokens)
	return pc
}

// WriteReport merges rows into the report and writes the artifact atomically.
func WriteReport(path string, rep *BridgesEvalReport, rows []QuestionRow, armA, armB *ArmAggregate, pool BridgePoolInfo) error {
	byKey := make(map[string]QuestionRow, len(rep.Questions)+len(rows))
	for _, r := range rep.Questions {
		byKey[r.QuestionID+"|"+r.Arm] = r
	}
	for _, r := range rows {
		byKey[r.QuestionID+"|"+r.Arm] = r
	}
	merged := make([]QuestionRow, 0, len(byKey))
	for _, r := range byKey {
		merged = append(merged, r)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Arm != merged[j].Arm {
			return merged[i].Arm < merged[j].Arm
		}
		return merged[i].QuestionID < merged[j].QuestionID
	})
	rep.Questions = merged
	rep.Bridges = pool
	if armA != nil {
		rep.Arms["a"] = *armA
	}
	if armB != nil {
		rep.Arms["b"] = *armB
	}
	if armA != nil && armB != nil {
		rep.PassCriteria = EvaluatePassCriteria(*armA, *armB)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("eval: artifact dir: %w", err)
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: artifact encode: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: artifact write: %w", err)
	}
	return os.Rename(tmp, path)
}

// SynthesizeAnswer is the mock AnswerFunc: the answer is the injected
// context itself, so the mock grader measures context coverage of the
// expected facts — a correct answer requires every fact to be present.
func SynthesizeAnswer(_, context string) (string, error) {
	return context, nil
}

// ---- CLI-facing helpers (tools/bridges-eval) ----

// HashBytesFile hashes a file's bytes (content-addressed fixture identity).
func HashBytesFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return HashBytes(b), nil
}

// BridgeEvalBridgeRecord is the CLI-facing alias so the wrapper never
// imports internal/memory directly.
type BridgeEvalBridgeRecord = memory.BridgeRecord

// BuildBridgesForRun builds the verified bridge pool in a fresh temp store
// and returns the records plus pool info for the artifact.
func BuildBridgesForRun(ctx context.Context, fx *BridgesFixture, cfg BridgesEvalConfig) ([]BridgeEvalBridgeRecord, BridgePoolInfo, error) {
	dir, err := os.MkdirTemp("", "bridges-eval-*")
	if err != nil {
		return nil, BridgePoolInfo{}, fmt.Errorf("eval: tempdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	records, entities, err := BuildBridges(ctx, fx, cfg, filepath.Join(dir, "bridge.vec.db"))
	if err != nil {
		return nil, BridgePoolInfo{}, err
	}
	info := BridgePoolInfo{Generated: len(records), Entities: entities}
	for _, r := range records {
		if r.Verified {
			info.Verified++
		}
	}
	sort.Strings(info.Entities)
	return records, info, nil
}

// RowMap indexes the report's existing rows for one arm (resume lookup).
func RowMap(rows []QuestionRow, arm string) map[string]QuestionRow {
	out := make(map[string]QuestionRow)
	for _, r := range rows {
		if r.Arm == arm {
			out[r.QuestionID+"|"+arm] = r
		}
	}
	return out
}

// ReadReport loads the artifact for summary printing.
func ReadReport(path string) (*BridgesEvalReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rep BridgesEvalReport
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

// PrintSummary prints the aggregate table + pass gate.
func PrintSummary(rep *BridgesEvalReport) {
	fmt.Println("=== bridges on/off recall eval ===")
	a, hasA := rep.Arms["a"]
	b, hasB := rep.Arms["b"]
	if hasA {
		printArmRow(a)
	}
	if hasB {
		printArmRow(b)
	}
	if hasA && hasB && rep.PassCriteria != nil {
		pc := rep.PassCriteria
		fmt.Printf("gate: corr(B)=%.2f%% corr(A)=%.2f%% tokens(B)=%.1f tokens(A)=%.1f\n",
			b.CorrectnessPct, a.CorrectnessPct, b.MeanContextTokens, a.MeanContextTokens)
		fmt.Printf("  correctness_non_regression: %v\n", pc.CorrectnessNonRegression)
		fmt.Printf("  token_reduction:            %v\n", pc.TokenReduction)
		fmt.Printf("  correctness_gain:           %v\n", pc.CorrectnessGain)
		fmt.Printf("  PASS: %v\n", pc.Pass)
	}
}

func printArmRow(agg ArmAggregate) {
	fmt.Printf("arm %s: n=%d errors=%d correctness=%.2f%% tokens=%.1f latency=%.1fms bridges_included=%d (avg %.2f/query, excluded %d, pool %d)\n",
		agg.Arm, agg.Questions, agg.Errors, agg.CorrectnessPct, agg.MeanContextTokens,
		agg.MeanLatencyMs, agg.BridgesIncluded, agg.BridgesIncludedAvg, agg.BridgesExcluded, agg.BridgesAvailable)
}

// ---- live answer lane (optional follow-up; never required) ----

// liveAnswerClient is a minimal OpenAI-compatible chat client for --live.
// It mirrors tools/memory-eval's client shape (temperature 0.2, 512-token
// cap) so both harnesses grade the local stack under the same conditions.
type liveAnswerClient struct {
	endpoint string
	model    string
	timeout  time.Duration
	http     *http.Client
}

// NewLiveAnswerFunc builds an AnswerFunc over a local OpenAI-compatible
// endpoint (llama-server). The model answers the question using ONLY the
// injected context; empty model IDs are sent as "local" (llama-server
// accepts any id).
func NewLiveAnswerFunc(endpoint, model string, timeout time.Duration) (AnswerFunc, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("eval: --live requires --endpoint")
	}
	if model == "" {
		model = "local"
	}
	return (&liveAnswerClient{
		endpoint: strings.TrimSuffix(endpoint, "/") + "/chat/completions",
		model:    model,
		timeout:  timeout,
		http:     &http.Client{Timeout: timeout},
	}).answer, nil
}

// answer sends one completion for the question over the injected context.
func (c *liveAnswerClient) answer(question, injected string) (string, error) {
	system := "You answer cross-session recall questions using ONLY the context provided. Answer in one short sentence. If the context does not contain the answer, say exactly: not in context."
	body, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": "Context:\n" + injected + "\n\nQuestion: " + question},
		},
		"temperature": 0.2,
		"max_tokens":  512,
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("eval: live endpoint %s: %s: %s", c.endpoint, resp.Status, truncateLive(raw, 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("eval: live endpoint decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("eval: live endpoint returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func truncateLive(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
