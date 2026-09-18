package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// EvalResult is the top-level metrics document.
type EvalResult struct {
	Model      string            `json:"model"`
	Endpoint   string            `json:"endpoint"`
	Mode       string            `json:"mode"`
	Threshold  float64           `json:"match_threshold"`
	RunAt      string            `json:"run_at"`
	Conditions map[string]string `json:"conditions"`
	Ambient    *AmbientMetrics   `json:"ambient,omitempty"`
	Distill    *DistillMetrics   `json:"distill,omitempty"`
}

// AmbientMetrics grades the ambient epistemic extraction run. The lexical
// block (precision/recall/f1) is the reproducible regression gate; the Judge
// block is a parallel, optional semantic second opinion (self-judged — see
// README caveat).
type AmbientMetrics struct {
	Calls            int            `json:"calls"`
	TransportErrors  int            `json:"transport_errors"`
	JSONParseOK      int            `json:"json_parse_ok"`
	SchemaConformant int            `json:"schema_conformant"`
	Extracted        int            `json:"extracted_total"`
	ByType           map[string]int `json:"extracted_by_type"`
	TruePositives    int            `json:"true_positives"`
	FalsePositives   int            `json:"false_positives"`
	FalseNegatives   int            `json:"false_negatives"`
	DecoyFalsePos    int            `json:"decoy_false_positives"`
	Precision        float64        `json:"precision"`
	Recall           float64        `json:"recall"`
	F1               float64        `json:"f1"`
	MeanLatencyMs    float64        `json:"mean_latency_ms"`
	TokensUsed       int            `json:"tokens_used"`
	// Judge holds the LLM-judge lane (nil unless --judge is on).
	Judge *JudgeMetrics `json:"judge,omitempty"`
	// ConfidenceAnalysis records (confidence, match) per candidate for the
	// confidence-gating sweep. Candidates from judge-off runs have
	// MatchedJudge equal to MatchedLexical.
	ConfidenceAnalysis []ConfidencePoint `json:"confidence_analysis,omitempty"`
	// ConfidenceSweep is the threshold sweep over ConfidenceAnalysis.
	ConfidenceSweep []SweepRow `json:"confidence_sweep,omitempty"`
}

// ConfidencePoint is one candidate's confidence plus how it was matched.
type ConfidencePoint struct {
	Confidence     float64 `json:"confidence"`
	MatchedLexical bool    `json:"matched_lexical"`
	MatchedJudge   bool    `json:"matched_judge"`
}

// SweepRow is one threshold's operating point in the confidence sweep.
type SweepRow struct {
	Threshold     float64 `json:"threshold"`
	CandidatesGT  int     `json:"candidates_at_threshold"`
	TruePositives int     `json:"true_positives_at_threshold"`
	PrecisionAtT  float64 `json:"precision_at_threshold"`
	KeptFraction  float64 `json:"kept_fraction"`
	Precision     float64 `json:"precision"`
	Recall        float64 `json:"recall"`
}

// JudgeMetrics is the semantic-judge parallel grading block.
type JudgeMetrics struct {
	// JudgeMatched counts lexical TPs plus judge-only TPs (unmatched
	// candidates the judge confirmed against some gold).
	JudgeMatched   int     `json:"judge_matched"`
	TruePositives  int     `json:"true_positives"`
	FalsePositives int     `json:"false_positives"`
	FalseNegatives int     `json:"false_negatives"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	F1             float64 `json:"f1"`
	Calls          int     `json:"judge_calls"`
	CacheHits      int     `json:"cache_hits"`
}

// DistillMetrics grades the distillation run.
type DistillMetrics struct {
	Calls            int     `json:"calls"`
	TransportErrors  int     `json:"transport_errors"`
	JSONParseOK      int     `json:"json_parse_ok"`
	SchemaConformant int     `json:"schema_conformant"`
	LessonsExtracted int     `json:"lessons_extracted"`
	TruePositives    int     `json:"true_positives"`
	FalseNegatives   int     `json:"false_negatives"`
	Recall           float64 `json:"recall"`
	CapViolations    int     `json:"cap_violations"`
	MeanLatencyMs    float64 `json:"mean_latency_ms"`
	TokensUsed       int     `json:"tokens_used"`
}

// ---- ambient eval ----

type candidate struct {
	Type       string   `json:"type"`
	Text       string   `json:"text"`
	Confidence *float64 `json:"confidence,omitempty"`
	Premises   []string `json:"premises,omitempty"`
	Category   string   `json:"category,omitempty"`
}

var validTypes = map[string]bool{"claim": true, "decision": true, "prediction": true}
var validCategories = map[string]bool{
	"architecture": true, "business": true, "technical": true,
	"prediction": true, "opinion": true, "methodology": true,
}

// ambientPrompt matches production's ambientExtractionPromptTemplate wording
// (see internal/memory/epistemic_ambient.go) with the conversation substituted.
// SYNC REQUIREMENT: any wording change here MUST be applied identically to
// ambientExtractionPromptTemplate in internal/memory/epistemic_ambient.go —
// the eval only measures what production would say.
const ambientPrompt = `You are an epistemic extractor. Read the following conversation segment and
extract assertions of belief (claims), forward-looking commitments (decisions),
and forecasts (predictions). For each candidate:

- Extract only assertions the USER commits to. An assistant's restatement,
  summary, or confirmation of what the user said is NOT a new candidate —
  skip it.
- Never split one assertion into fragments; each candidate must be a complete,
  self-contained assertion.
- Only extract statements the speaker is committing to, not hypotheticals,
  questions, sarcasm, jokes, or quotations of others' views.
- Skip pleasantries, agreements without content, and meta-conversation.

Return JSON array. Each element:
{
  "type": "claim" | "decision" | "prediction",
  "text": "<the assertion>",
  "source": "conversation",
  "confidence": 0.0-1.0,
  "premises": [],
  "category": "<one of: architecture, business, technical, prediction, opinion, methodology>"
}

If no candidates, return [].

Conversation:
%s`

// judgeJudgePrompt asks the strict yes/no question per unmatched candidate.
const judgeJudgePrompt = `Candidate: %s

Gold assertion: %s

Does the candidate express the same assertion as the gold? Answer only yes or no.`

const judgeSystemPrompt = `You are a strict yes/no judge. You answer only "yes" or "no", nothing else.`

// judgePairer answers "does this candidate express the same assertion as some
// gold item?" with one LLM call per unmatched candidate (O(unmatched), not
// O(n*m)) plus a cache keyed on (candidate-hash, gold-hash).
type judgePairer struct {
	client    *chatClient
	cache     map[string]bool
	calls     int
	cacheHits int
	transport int
}

func newJudgePairer(client *chatClient) *judgePairer {
	return &judgePairer{client: client, cache: map[string]bool{}}
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.Join(strings.Fields(s), " "))))
	return hex.EncodeToString(sum[:8])
}

func (j *judgePairer) judgePair(candText, goldText string) bool {
	key := hashText(candText) + ":" + hashText(goldText)
	if v, ok := j.cache[key]; ok {
		j.cacheHits++
		return v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	j.calls++
	content, _, _, err := j.client.chatJudge(ctx, judgeSystemPrompt,
		fmt.Sprintf(judgeJudgePrompt, candText, goldText))
	if err != nil {
		j.transport++
		j.cache[key] = false // failed judgment: treat as no (conservative)
		return false
	}
	v := parseJudgeYesNo(content)
	j.cache[key] = v
	return v
}

// judgeCandidate: does any gold item match this candidate according to the
// judge? One call per (candidate, gold) pair on cache miss, but the pair cache
// dedups across candidates that repeat the same text (dedupes fragments).
func (j *judgePairer) judgeCandidate(candText string, gold []string) bool {
	for _, g := range gold {
		if j.judgePair(candText, g) {
			return true
		}
	}
	return false
}

// parseJudgeYesNo parses the judge's answer: yes = true. Tolerates case,
// whitespace, markdown fences (with or without a language tag), and trailing
// punctuation ("yes.", "Yes", " YES ", "```yes```", "```json\nyes\n```").
// Junk (anything else) is false.
func parseJudgeYesNo(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSpace(s)
	// Strip a fence language tag (e.g. "json") if present.
	if i := strings.IndexAny(s, " 	\n"); i > 0 && s[:i] == "json" {
		s = strings.TrimSpace(s[i:])
	}
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, " .!?	\n\"'")
	return s == "yes" || s == "y"
}

// computeConfidenceSweep sweeps thresholds 0.0..0.9 over confidence-gated
// candidates using judge matching. The t=0.0 row is the keep-everything
// baseline (total precision/recall); the 0.0-0.3 region is mandatory
// viewing — live 2026-09-17 runs found confidence bimodal (mass at 0.0 and
// 1.0), so the entire useful signal sits in that low band (README).
func computeConfidenceSweep(pts []ConfidencePoint, totalCandidates int) []SweepRow {
	var rows []SweepRow
	for t := 0.0; t <= 0.9001; t += 0.1 {
		kept, tp := 0, 0
		for _, p := range pts {
			if p.Confidence >= t {
				kept++
				if p.MatchedJudge {
					tp++
				}
			}
		}
		row := SweepRow{Threshold: math.Round(t*100) / 100, CandidatesGT: kept, TruePositives: tp}
		if kept > 0 {
			row.PrecisionAtT = float64(tp) / float64(kept)
			row.KeptFraction = float64(kept) / float64(totalCandidates)
		}
		if totalCandidates > 0 {
			row.Precision = row.PrecisionAtT
			row.Recall = float64(tp) / float64(totalCandidates)
		}
		rows = append(rows, row)
	}
	return rows
}

func runAmbientEval(client *chatClient, corpus *corpus, threshold float64) *AmbientMetrics {
	return runAmbientEvalJudge(client, corpus, threshold, nil)
}

func runAmbientEvalJudge(client *chatClient, corpus *corpus, threshold float64, judge *judgePairer) *AmbientMetrics {
	m := &AmbientMetrics{ByType: map[string]int{}}
	if judge != nil {
		m.Judge = &JudgeMetrics{}
	}
	var latencies []float64

	for _, seg := range corpus.Segments {
		if len(seg.Gold.Lessons) > 0 && segGoldOnlyDistill(seg) {
			continue // distill-only segment
		}
		conv := strings.Join(seg.Messages, "\n")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		content, latency, tokens, err := client.chat(ctx,
			"You are a strict JSON extraction engine.", fmt.Sprintf(ambientPrompt, conv))
		cancel()
		m.Calls++
		m.TokensUsed += tokens
		latencies = append(latencies, latency)
		if err != nil {
			m.TransportErrors++
			m.FalseNegatives += len(seg.Gold.Claims) + len(seg.Gold.Decisions) + len(seg.Gold.Predictions)
			if judge != nil {
				m.Judge.FalseNegatives += len(seg.Gold.Claims) + len(seg.Gold.Decisions) + len(seg.Gold.Predictions)
			}
			continue
		}

		cands, parseErr := parseCandidates(content)
		if parseErr != nil {
			m.FalseNegatives += len(seg.Gold.Claims) + len(seg.Gold.Decisions) + len(seg.Gold.Predictions)
			if judge != nil {
				m.Judge.FalseNegatives += len(seg.Gold.Claims) + len(seg.Gold.Decisions) + len(seg.Gold.Predictions)
			}
			continue // JSONParseOK stays false
		}
		m.JSONParseOK++
		conform := true
		for _, c := range cands {
			if !validTypes[c.Type] || c.Text == "" {
				conform = false
				continue
			}
			if c.Category != "" && !validCategories[c.Category] {
				conform = false
			}
			m.ByType[c.Type]++
			m.Extracted++
		}
		if conform {
			m.SchemaConformant++
		}

		// Grade against gold: claims + decisions + predictions share one pool.
		gold := append(append(append([]string{}, seg.Gold.Claims...), seg.Gold.Decisions...), seg.Gold.Predictions...)
		matched := make([]bool, len(cands))
		judgeMatched := make([]bool, len(cands))
		for _, g := range gold {
			hit := false
			for i, c := range cands {
				if matched[i] {
					continue
				}
				if tokenOverlap(g, c.Text) >= threshold {
					matched[i] = true
					judgeMatched[i] = true // lexical TP is also a judge TP
					hit = true
					break
				}
			}
			if hit {
				m.TruePositives++
			} else {
				m.FalseNegatives++
			}
		}

		// Judge lane: each candidate with no lexical match gets a second
		// opinion against the gold pool. When the judge rescues a candidate,
		// the judge lane's FN for this segment shrinks.
		segJudgeRescues := 0
		for i := range cands {
			if judgeMatched[i] || !validTypes[cands[i].Type] || cands[i].Text == "" {
				continue
			}
			if judge != nil && judge.judgeCandidate(cands[i].Text, gold) {
				judgeMatched[i] = true
				segJudgeRescues++
			}
		}

		// Confidence-gating record: one point per valid candidate.
		for i, c := range cands {
			if !validTypes[c.Type] || c.Text == "" {
				continue
			}
			conf := 0.0
			if c.Confidence != nil {
				conf = *c.Confidence
			}
			jm := judgeMatched[i]
			if judge == nil {
				jm = matched[i] // judge-off runs: judge-match equals lexical
			}
			m.ConfidenceAnalysis = append(m.ConfidenceAnalysis, ConfidencePoint{
				Confidence:     conf,
				MatchedLexical: matched[i],
				MatchedJudge:   jm,
			})
		}

		for i, c := range cands {
			if matched[i] {
				continue
			}
			m.FalsePositives++
			if seg.Gold.Decoys > 0 {
				m.DecoyFalsePos++
			}
			_ = c
		}

		if judge != nil {
			m.Judge.JudgeMatched += segJudgeRescues
			m.Judge.FalseNegatives -= segJudgeRescues
			// Copy judge MatchedJudge into JudgeMatched for the run-level count:
			// judge_matched = lexical TPs + judge-only TPs is computed in finalize.
		}
	}
	finalizeAmbient(m, latencies)
	if judge != nil {
		m.Judge.Calls = judge.calls
		m.Judge.CacheHits = judge.cacheHits
	}
	return m
}

func segGoldOnlyDistill(seg segment) bool {
	return len(seg.Gold.Claims) == 0 && len(seg.Gold.Decisions) == 0 && len(seg.Gold.Predictions) == 0
}

// stripThink removes LFM2.5's (possibly empty) <think>...</think> block so the
// JSON payload parses; production strips fences analogously before decoding.
func stripThink(s string) string {
	if i := strings.Index(s, "<think>"); i >= 0 {
		if j := strings.Index(s[i:], "</think>"); j >= 0 {
			s = s[:i] + s[i+j+len("</think>"):]
		} else {
			s = s[:i] // unterminated think: drop the rest
		}
	}
	return s
}

func parseCandidates(content string) ([]candidate, error) {
	content = stripThink(content)
	// Strip markdown fences if present, then parse a JSON array. Accept an
	// object wrapping an array under "candidates"/"results" too (models do this).
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		var wrap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &wrap); err != nil {
			return nil, err
		}
		for _, key := range []string{"candidates", "results", "items"} {
			if raw, ok := wrap[key]; ok {
				var cands []candidate
				if err := json.Unmarshal(raw, &cands); err != nil {
					return nil, err
				}
				return cands, nil
			}
		}
		return nil, fmt.Errorf("object without candidates array")
	}
	var cands []candidate
	if err := json.Unmarshal([]byte(s), &cands); err != nil {
		return nil, err
	}
	return cands, nil
}

// finalizeAmbient computes derived fields.
func finalizeAmbient(m *AmbientMetrics, latencies []float64) {
	if m.TruePositives+m.FalsePositives > 0 {
		m.Precision = float64(m.TruePositives) / float64(m.TruePositives+m.FalsePositives)
	}
	if m.TruePositives+m.FalseNegatives > 0 {
		m.Recall = float64(m.TruePositives) / float64(m.TruePositives+m.FalseNegatives)
	}
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}
	if len(latencies) > 0 {
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		m.MeanLatencyMs = sum / float64(len(latencies))
	}
	if m.Judge != nil {
		finalizeJudge(m.Judge, m.TruePositives, m.FalsePositives, m.FalseNegatives)
	}
	m.ConfidenceSweep = computeConfidenceSweep(m.ConfidenceAnalysis, m.Extracted)
}

// finalizeJudge derives the judge lane's parallel P/R/F1. Judge TPs are the
// lexical TPs plus judge-only rescues; FPs are lexical FPs minus judge-only
// rescues (candidates the judge confirms are not false positives in the
// judge lane); the judge FN denominator matches the lexical one net of
// rescues.
func finalizeJudge(j *JudgeMetrics, lexicalTP, lexicalFP, lexicalFN int) {
	j.TruePositives = lexicalTP + j.JudgeMatched
	j.FalsePositives = lexicalFP - j.JudgeMatched
	if j.FalsePositives < 0 {
		j.FalsePositives = 0
	}
	j.FalseNegatives = lexicalFN - j.JudgeMatched
	if j.FalseNegatives < 0 {
		j.FalseNegatives = 0
	}
	if j.TruePositives+j.FalsePositives > 0 {
		j.Precision = float64(j.TruePositives) / float64(j.TruePositives+j.FalsePositives)
	}
	if j.TruePositives+j.FalseNegatives > 0 {
		j.Recall = float64(j.TruePositives) / float64(j.TruePositives+j.FalseNegatives)
	}
	if j.Precision+j.Recall > 0 {
		j.F1 = 2 * j.Precision * j.Recall / (j.Precision + j.Recall)
	}
}

// ---- token overlap ----

func tokenize(s string) map[string]int {
	toks := map[string]int{}
	for _, f := range strings.Fields(strings.ToLower(s)) {
		f = strings.Trim(f, ".,!?;:'\"()[]{}")
		if f != "" {
			toks[f]++
		}
	}
	return toks
}

// tokenOverlap returns |A∩B| / |A| — the fraction of a's tokens present in b.
func tokenOverlap(a, b string) float64 {
	ta, tb := tokenize(a), tokenize(b)
	if len(ta) == 0 {
		return 0
	}
	hit := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(ta))
}

// ---- distill eval ----

// lessonFromJSON tolerantly decodes a lesson object. evidence_ids may be
// strings or integers (models invent ids); the shape mismatch is reported via
// evidenceTyped so the eval can count it against schema conformance.
func lessonFromJSON(content string) (principle, because string, evidenceTyped bool, err error) {
	var raw struct {
		Principle   string          `json:"principle"`
		Because     string          `json:"because"`
		EvidenceIDs json.RawMessage `json:"evidence_ids"`
	}
	if err := unmarshalLoose(content, &raw); err != nil {
		return "", "", false, err
	}
	if len(raw.EvidenceIDs) > 0 {
		var ss []string
		if json.Unmarshal(raw.EvidenceIDs, &ss) == nil {
			evidenceTyped = true
		} else {
			var anys []any
			if json.Unmarshal(raw.EvidenceIDs, &anys) == nil {
				evidenceTyped = false
			}
		}
	} else {
		evidenceTyped = true // absent is fine
	}
	return raw.Principle, raw.Because, evidenceTyped, nil
}

const distillPrompt = `Distill the principle below into ONE reusable lesson.
Return ONLY a JSON object: {"principle": "<= 280 chars", "because": "<one sentence>", "evidence_ids": []}
No markdown fences, no prose. Use the speaker's committed knowledge, not speculation.

Source observation:
%s`

func runDistillEval(client *chatClient, corpus *corpus, threshold float64) *DistillMetrics {
	m := &DistillMetrics{}
	var latencies []float64

	for _, seg := range corpus.Segments {
		for _, obs := range seg.Gold.Lessons {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			content, latency, tokens, err := client.chat(ctx,
				"You are a strict JSON extraction engine.",
				fmt.Sprintf(distillPrompt, obs))
			cancel()
			m.Calls++
			m.TokensUsed += tokens
			latencies = append(latencies, latency)
			if err != nil {
				m.TransportErrors++
				m.FalseNegatives++
				continue
			}
			principle, _, evidenceTyped, err := lessonFromJSON(content)
			if err != nil {
				m.FalseNegatives++
				continue
			}
			m.JSONParseOK++
			conform := principle != "" && len(principle) <= 280 && evidenceTyped
			if principle != "" && len(principle) <= 280 && evidenceTyped {
				m.SchemaConformant++
			} else if principle != "" && len(principle) > 280 {
				m.CapViolations++
			}
			_ = conform
			if principle == "" {
				// F7: a parsed-but-empty lesson is a miss, not an invisible
				// success — it must stay in the recall denominator.
				m.FalseNegatives++
				continue
			}
			m.LessonsExtracted++
			// gold for distill segment = the observation itself (a good lesson
			// retains the observation's substance)
			if tokenOverlap(obs, principle) >= threshold {
				m.TruePositives++
			} else {
				m.FalseNegatives++
			}
		}
	}
	if m.TruePositives+m.FalseNegatives > 0 {
		m.Recall = float64(m.TruePositives) / float64(m.TruePositives+m.FalseNegatives)
	}
	if len(latencies) > 0 {
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		m.MeanLatencyMs = sum / float64(len(latencies))
	}
	return m
}

// unmarshalLoose strips fences before parsing a JSON object.
func unmarshalLoose(content string, v any) error {
	s := stripThink(strings.TrimSpace(content))
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	return json.Unmarshal([]byte(s), v)
}

// ---- summary printing ----

func printSummary(r EvalResult) {
	if r.Ambient != nil {
		m := r.Ambient
		fmt.Println("=== AMBIENT EXTRACTION ===")
		printBar("precision ", m.Precision)
		printBar("recall    ", m.Recall)
		printBar("f1        ", m.F1)
		fmt.Printf("  json_parse_ok      %d/%d\n", m.JSONParseOK, m.Calls)
		fmt.Printf("  schema_conformant  %d/%d\n", m.SchemaConformant, m.Calls)
		fmt.Printf("  tp/fp/fn           %d/%d/%d\n", m.TruePositives, m.FalsePositives, m.FalseNegatives)
		fmt.Printf("  decoy_false_pos    %d\n", m.DecoyFalsePos)
		fmt.Printf("  extracted_by_type  %v\n", sortedCounts(m.ByType))
		fmt.Printf("  mean_latency_ms    %.0f\n", m.MeanLatencyMs)
		fmt.Printf("  tokens_used        %d\n", m.TokensUsed)
		fmt.Printf("  transport_errors   %d\n", m.TransportErrors)
		if m.Judge != nil {
			j := m.Judge
			fmt.Println("--- judge lane (semantic second opinion) ---")
			printBar("precision ", j.Precision)
			printBar("recall    ", j.Recall)
			printBar("f1        ", j.F1)
			fmt.Printf("  tp/fp/fn           %d/%d/%d\n", j.TruePositives, j.FalsePositives, j.FalseNegatives)
			fmt.Printf("  judge_only_rescues %d\n", j.JudgeMatched)
			fmt.Printf("  judge_calls        %d (cache hits %d)\n", j.Calls, j.CacheHits)
		}
		printConfidenceSweep(m.ConfidenceSweep)
	}
	if r.Distill != nil {
		m := r.Distill
		fmt.Println("=== DISTILLATION ===")
		printBar("recall    ", m.Recall)
		fmt.Printf("  json_parse_ok      %d/%d\n", m.JSONParseOK, m.Calls)
		fmt.Printf("  schema_conformant  %d/%d\n", m.SchemaConformant, m.Calls)
		fmt.Printf("  cap_violations     %d\n", m.CapViolations)
		fmt.Printf("  mean_latency_ms    %.0f\n", m.MeanLatencyMs)
		fmt.Printf("  tokens_used        %d\n", m.TokensUsed)
		fmt.Printf("  transport_errors   %d\n", m.TransportErrors)
	}
}

func printBar(label string, v float64) {
	width := 30
	filled := int(v * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Printf("  %s %s %.3f\n", label, bar, v)
}

// printConfidenceSweep prints the confidence-gating threshold sweep table.
func printConfidenceSweep(rows []SweepRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Println("--- confidence sweep (judge matching; 0.0-0.3 region is mandatory viewing) ---")
	fmt.Println("  t     kept  tp  prec@t  kept_frac  precision  recall")
	for _, r := range rows {
		fmt.Printf("  %.1f  %4d  %3d   %.3f     %.3f      %.3f     %.3f\n",
			r.Threshold, r.CandidatesGT, r.TruePositives,
			r.PrecisionAtT, r.KeptFraction, r.Precision, r.Recall)
	}
}

func sortedCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return "{" + strings.Join(parts, " ") + "}"
}
