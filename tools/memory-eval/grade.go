package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// AmbientMetrics grades the ambient epistemic extraction run.
type AmbientMetrics struct {
	Calls            int     `json:"calls"`
	TransportErrors  int     `json:"transport_errors"`
	JSONParseOK      int     `json:"json_parse_ok"`
	SchemaConformant int     `json:"schema_conformant"`
	Extracted        int     `json:"extracted_total"`
	ByType           map[string]int `json:"extracted_by_type"`
	TruePositives    int     `json:"true_positives"`
	FalsePositives   int     `json:"false_positives"`
	FalseNegatives   int     `json:"false_negatives"`
	DecoyFalsePos    int     `json:"decoy_false_positives"`
	Precision        float64 `json:"precision"`
	Recall           float64 `json:"recall"`
	F1               float64 `json:"f1"`
	MeanLatencyMs    float64 `json:"mean_latency_ms"`
	TokensUsed       int     `json:"tokens_used"`
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
const ambientPrompt = `You are an epistemic extractor. Read the following conversation segment and
extract assertions of belief (claims), forward-looking commitments (decisions),
and forecasts (predictions). For each candidate:

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

func runAmbientEval(client *chatClient, corpus *corpus, threshold float64) *AmbientMetrics {
	m := &AmbientMetrics{ByType: map[string]int{}}
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
			continue
		}

		cands, parseErr := parseCandidates(content)
		if parseErr != nil {
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
		for _, g := range gold {
			hit := false
			for i, c := range cands {
				if matched[i] {
					continue
				}
				if tokenOverlap(g, c.Text) >= threshold {
					matched[i] = true
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
	}
	finalizeAmbient(m, latencies)
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
				continue
			}
			principle, _, evidenceTyped, err := lessonFromJSON(content)
			if err != nil {
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
