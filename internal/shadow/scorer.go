package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// Scorer evaluates response quality.
type Scorer struct {
	config        *QualityConfig
	teacherClient *TeacherClient
	logger        *slog.Logger
}

// ScorerOption is a functional option for Scorer.
type ScorerOption func(*Scorer)

// WithScorerLogger sets the logger.
func WithScorerLogger(logger *slog.Logger) ScorerOption {
	return func(s *Scorer) {
		s.logger = logger
	}
}

// WithTeacherClient sets the teacher client for LLM-based evaluation.
func WithTeacherClient(client *TeacherClient) ScorerOption {
	return func(s *Scorer) {
		s.teacherClient = client
	}
}

// NewScorer creates a new scorer.
func NewScorer(config *QualityConfig, opts ...ScorerOption) *Scorer {
	s := &Scorer{
		config: config,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ScoreResult holds the scoring output.
type ScoreResult struct {
	Score         float64            `json:"score"`
	Dimensions    map[string]float64 `json:"dimensions"`
	IsHighQuality bool               `json:"is_high_quality"`
	Method        string             `json:"method"`
}

// Score evaluates a response based on the configured method.
func (s *Scorer) Score(ctx context.Context, record *ShadowRecord) (*ScoreResult, error) {
	switch s.config.Method {
	case MethodHeuristic:
		return s.scoreHeuristic(record), nil

	case MethodTeacherEval:
		return s.scoreWithTeacher(ctx, record)

	case MethodHybrid:
		return s.scoreHybrid(ctx, record)

	default:
		return s.scoreHeuristic(record), nil
	}
}

// ScoreComparison scores and compares student vs teacher responses.
func (s *Scorer) ScoreComparison(ctx context.Context, record *ShadowRecord) (studentScore, teacherScore float64, err error) {
	// Score student response
	studentResult := s.scoreHeuristic(record)
	studentScore = studentResult.Score

	// If we have a teacher response, score it too
	if record.HasTeacherResponse() {
		// Create a temporary record with teacher as "student" for scoring
		teacherRecord := &ShadowRecord{
			Messages:       record.Messages,
			StudentContent: record.TeacherContent,
			Domain:         record.Domain,
			TaskType:       record.TaskType,
		}
		teacherResult := s.scoreHeuristic(teacherRecord)
		teacherScore = teacherResult.Score
	}

	return studentScore, teacherScore, nil
}

func (s *Scorer) scoreHeuristic(record *ShadowRecord) *ScoreResult {
	weights := s.config.HeuristicWeights
	dimensions := make(map[string]float64)

	// Get the user query (last user message)
	var userQuery string
	for _, v := range slices.Backward(record.Messages) {
		if v.Role == RoleUser {
			userQuery = v.Content
			break
		}
	}

	response := record.StudentContent

	// Score relevance
	dimensions["relevance"] = s.scoreRelevance(userQuery, response)

	// Score completeness
	dimensions["completeness"] = s.scoreCompleteness(userQuery, response, record.Domain)

	// Score correctness
	dimensions["correctness"] = s.scoreCorrectness(response, record.Domain)

	// Score style
	dimensions["style"] = s.scoreStyle(response)

	// Calculate weighted score
	score := dimensions["relevance"]*weights.Relevance +
		dimensions["completeness"]*weights.Completeness +
		dimensions["correctness"]*weights.Correctness +
		dimensions["style"]*weights.Style

	return &ScoreResult{
		Score:         score,
		Dimensions:    dimensions,
		IsHighQuality: score >= s.config.HighQualityThreshold,
		Method:        "heuristic",
	}
}

func (s *Scorer) scoreRelevance(query, response string) float64 {
	if query == "" || response == "" {
		return 0.5
	}

	// Extract key terms from query
	queryTerms := extractKeyTerms(query)
	if len(queryTerms) == 0 {
		return 0.7 // Default when can't extract terms
	}

	// Check how many query terms appear in response
	responseLower := strings.ToLower(response)
	matches := 0
	for _, term := range queryTerms {
		if strings.Contains(responseLower, strings.ToLower(term)) {
			matches++
		}
	}

	relevance := float64(matches) / float64(len(queryTerms))

	// Boost if response is substantial
	if len(response) > 100 {
		relevance = relevance*0.8 + 0.2
	}

	return clamp(relevance, 0, 1)
}

func (s *Scorer) scoreCompleteness(query, response string, domain Domain) float64 {
	// Check response length relative to query complexity
	queryLen := len(query)
	responseLen := len(response)

	// Very short responses are often incomplete
	if responseLen < 20 {
		return 0.2
	}

	// Base completeness on length ratio with domain-specific expectations
	var expectedRatio float64
	switch domain {
	case DomainCode:
		expectedRatio = 2.0 // Code explanations should be longer
	case DomainPlanning:
		expectedRatio = 3.0 // Plans should be detailed
	case DomainAnalysis:
		expectedRatio = 2.5
	default:
		expectedRatio = 1.5
	}

	actualRatio := float64(responseLen) / float64(queryLen+1)
	completeness := actualRatio / expectedRatio

	// Check for structure indicators
	hasStructure := strings.Contains(response, "\n") ||
		strings.Contains(response, "1.") ||
		strings.Contains(response, "- ") ||
		strings.Contains(response, "```")

	if hasStructure {
		completeness += 0.2
	}

	return clamp(completeness, 0, 1)
}

func (s *Scorer) scoreCorrectness(response string, domain Domain) float64 {
	score := 0.7 // Base score

	switch domain {
	case DomainCode:
		// Check for code quality indicators
		if strings.Contains(response, "```") {
			score += 0.1 // Has code blocks
		}

		// Check for common error patterns
		errorPatterns := []string{
			"undefined",
			"TypeError",
			"SyntaxError",
			"I don't know",
			"I'm not sure",
			"I cannot",
		}
		for _, pattern := range errorPatterns {
			if strings.Contains(response, pattern) {
				score -= 0.1
			}
		}

		// Check for balanced braces/brackets in code
		if hasBalancedBrackets(response) {
			score += 0.1
		}

		// Extract and validate code blocks
		codeBlocks := extractCodeBlocks(response)
		for _, block := range codeBlocks {
			validity := validateCodeBlock(block)
			score += validity * 0.1 // Bonus for valid code blocks
		}

	case DomainGeneral:
		// Check for hedging language (indicates uncertainty)
		hedgePatterns := []string{
			"I think",
			"probably",
			"maybe",
			"I'm not certain",
			"I believe",
		}
		hedgeCount := 0
		for _, pattern := range hedgePatterns {
			if strings.Contains(strings.ToLower(response), strings.ToLower(pattern)) {
				hedgeCount++
			}
		}
		score -= float64(hedgeCount) * 0.05

		// Check for factual consistency markers
		factualScore := checkFactualConsistency(response)
		score += factualScore * 0.1

	case DomainDebugging:
		// Check for debugging-specific quality indicators
		debugPatterns := []string{
			"root cause",
			"solution",
			KwFix,
			"error occurs",
			"stack trace shows",
		}
		debugMatches := 0
		for _, pattern := range debugPatterns {
			if strings.Contains(strings.ToLower(response), pattern) {
				debugMatches++
			}
		}
		score += float64(debugMatches) * 0.03

	case DomainPlanning:
		// Check for planning-specific quality indicators
		if hasNumberedSteps(response) {
			score += 0.1
		}
		if hasEstimates(response) {
			score += 0.05
		}
	}

	return clamp(score, 0, 1)
}

// extractCodeBlocks extracts code blocks from markdown-formatted text.
func extractCodeBlocks(text string) []codeBlock {
	var blocks []codeBlock
	lines := strings.Split(text, "\n")
	var currentBlock []string
	var language string
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "```"); ok {
			if inBlock {
				// End of block
				blocks = append(blocks, codeBlock{
					language: language,
					code:     strings.Join(currentBlock, "\n"),
				})
				currentBlock = nil
				inBlock = false
			} else {
				// Start of block
				inBlock = true
				language = after
			}
		} else if inBlock {
			currentBlock = append(currentBlock, line)
		}
	}

	return blocks
}

type codeBlock struct {
	language string
	code     string
}

// validateCodeBlock performs static validation on a code block.
// Returns a score from 0.0 to 1.0 indicating validity.
func validateCodeBlock(block codeBlock) float64 {
	code := strings.TrimSpace(block.code)
	if code == "" {
		return 0
	}

	score := 0.5 // Base score for having code

	// Check balanced delimiters
	if hasBalancedBrackets(code) {
		score += 0.2
	}

	// Language-specific checks
	lang := strings.ToLower(block.language)
	switch lang {
	case "go", "golang":
		score += validateGoCode(code)
	case "python", "py":
		score += validatePythonCode(code)
	case "javascript", "js", "typescript", "ts":
		score += validateJSCode(code)
	default:
		// Generic checks for unknown languages
		score += validateGenericCode(code)
	}

	return clamp(score, 0, 1)
}

// validateGoCode performs Go-specific syntax checks.
func validateGoCode(code string) float64 {
	score := 0.0

	// Check for common Go patterns
	if strings.Contains(code, "func ") {
		score += 0.1
	}
	if strings.Contains(code, "package ") || !strings.Contains(code, "\n") {
		// Single-line snippets don't need package declaration
		score += 0.05
	}
	if strings.Contains(code, "if err != nil") {
		score += 0.05 // Proper error handling pattern
	}

	// Check for syntax errors
	syntaxErrors := []string{
		"func()", // Missing function name (usually)
		":= =",   // Double assignment
	}
	for _, err := range syntaxErrors {
		if strings.Contains(code, err) {
			score -= 0.1
		}
	}

	return score
}

// validatePythonCode performs Python-specific syntax checks.
func validatePythonCode(code string) float64 {
	score := 0.0

	// Check for common Python patterns
	if strings.Contains(code, "def ") || strings.Contains(code, "class ") {
		score += 0.1
	}

	// Check indentation consistency
	lines := strings.Split(code, "\n")
	hasConsistentIndent := true
	for _, line := range lines {
		if line != "" && line[0] == ' ' {
			// Check if indentation is multiple of 4
			spaces := 0
			for _, c := range line {
				if c == ' ' {
					spaces++
				} else {
					break
				}
			}
			if spaces%4 != 0 && spaces%2 != 0 {
				hasConsistentIndent = false
				break
			}
		}
	}
	if hasConsistentIndent {
		score += 0.1
	}

	return score
}

// validateJSCode performs JavaScript/TypeScript-specific syntax checks.
func validateJSCode(code string) float64 {
	score := 0.0

	// Check for common JS patterns
	patterns := []string{KwFunction, "const ", "let ", "var ", "=>", "async "}
	for _, p := range patterns {
		if strings.Contains(code, p) {
			score += 0.05
			break
		}
	}

	// Check for semicolons (optional but common)
	if strings.Contains(code, ";") {
		score += 0.02
	}

	return score
}

// validateGenericCode performs generic code validation.
func validateGenericCode(code string) float64 {
	score := 0.0

	// Check for common programming patterns
	patterns := []string{"=", "(", ")", "{", "}"}
	for _, p := range patterns {
		if strings.Contains(code, p) {
			score += 0.02
		}
	}

	// Penalize obvious non-code
	if strings.Count(code, " ") > len(code)/2 {
		// Mostly spaces - probably not code
		score -= 0.1
	}

	return score
}

// checkFactualConsistency checks for internal consistency in factual statements.
// Returns a score from 0.0 to 0.5.
func checkFactualConsistency(response string) float64 {
	score := 0.0

	// Check for self-contradiction patterns
	contradictions := []struct {
		a, b string
	}{
		{"is true", "is false"},
		{"always", "never"},
		{"can", "cannot"},
		{"will", "won't"},
	}

	sentences := strings.Split(response, ".")
	for _, contra := range contradictions {
		hasA, hasB := false, false
		for _, sent := range sentences {
			lower := strings.ToLower(sent)
			if strings.Contains(lower, contra.a) {
				hasA = true
			}
			if strings.Contains(lower, contra.b) {
				hasB = true
			}
		}
		if hasA && hasB {
			score -= 0.1 // Potential contradiction
		}
	}

	// Check for citation/reference patterns (positive indicator)
	citationPatterns := []string{
		"according to",
		"based on",
		"documentation states",
		"as shown in",
	}
	for _, pattern := range citationPatterns {
		if strings.Contains(strings.ToLower(response), pattern) {
			score += 0.1
			break
		}
	}

	return clamp(score, -0.2, 0.5)
}

// hasNumberedSteps checks if response contains numbered steps.
func hasNumberedSteps(response string) bool {
	// Check for patterns like "1.", "2.", "Step 1", etc.
	patterns := []string{"1.", "2.", "Step 1", "First,", "Second,", "Third,"}
	for _, p := range patterns {
		if strings.Contains(response, p) {
			return true
		}
	}
	return false
}

// hasEstimates checks if response contains time/effort estimates.
func hasEstimates(response string) bool {
	patterns := []string{
		"hour", "minute", "day", "week",
		"estimate", "approximately", "about",
		"~", "roughly",
	}
	lower := strings.ToLower(response)
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (s *Scorer) scoreStyle(response string) float64 {
	score := 0.7

	// Check formatting quality
	lines := strings.Split(response, "\n")

	// Good: Has multiple paragraphs for longer responses
	if len(response) > 200 && len(lines) > 1 {
		score += 0.1
	}

	// Good: Uses markdown formatting appropriately
	if strings.Contains(response, "**") || strings.Contains(response, "##") {
		score += 0.05
	}

	// Bad: Very long paragraphs without breaks
	for _, line := range lines {
		if len(line) > 500 {
			score -= 0.1
			break
		}
	}

	// Good: Concise (not overly verbose)
	if len(response) < 2000 && len(response) > 50 {
		score += 0.05
	}

	// Bad: Excessive repetition
	words := strings.Fields(response)
	if len(words) > 20 {
		wordCounts := make(map[string]int)
		for _, w := range words {
			wordCounts[strings.ToLower(w)]++
		}
		maxRepeat := 0
		for _, count := range wordCounts {
			if count > maxRepeat {
				maxRepeat = count
			}
		}
		if float64(maxRepeat)/float64(len(words)) > 0.2 {
			score -= 0.1
		}
	}

	return clamp(score, 0, 1)
}

func (s *Scorer) scoreWithTeacher(ctx context.Context, record *ShadowRecord) (*ScoreResult, error) {
	if s.teacherClient == nil {
		return s.scoreHeuristic(record), nil
	}

	// Build evaluation prompt
	evalPrompt := s.buildEvalPrompt(record)

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: evalSystemPrompt},
		{Role: llm.RoleUser, Content: evalPrompt},
	}

	response, _, err := s.teacherClient.GetResponse(ctx, messages)
	if err != nil {
		s.logger.Warn("Teacher evaluation failed, falling back to heuristic", "error", err)
		return s.scoreHeuristic(record), nil
	}

	// Parse teacher's evaluation
	return s.parseTeacherEval(response, record)
}

func (s *Scorer) scoreHybrid(ctx context.Context, record *ShadowRecord) (*ScoreResult, error) {
	// First, do heuristic scoring
	heuristicResult := s.scoreHeuristic(record)

	// Only use teacher for borderline cases
	borderlineLow := s.config.TrainableThreshold
	borderlineHigh := s.config.HighQualityThreshold

	if heuristicResult.Score >= borderlineLow && heuristicResult.Score <= borderlineHigh {
		// Borderline case - use teacher for more accurate scoring
		if s.teacherClient != nil && s.teacherClient.IsAvailable(ctx) {
			teacherResult, err := s.scoreWithTeacher(ctx, record)
			if err == nil {
				// Combine scores with preference for teacher
				combined := heuristicResult.Score*0.3 + teacherResult.Score*0.7
				teacherResult.Score = combined
				teacherResult.Method = "hybrid"
				teacherResult.IsHighQuality = combined >= s.config.HighQualityThreshold
				return teacherResult, nil
			}
		}
	}

	return heuristicResult, nil
}

func (s *Scorer) buildEvalPrompt(record *ShadowRecord) string {
	// Get user query
	var userQuery string
	for _, v := range slices.Backward(record.Messages) {
		if v.Role == RoleUser {
			userQuery = v.Content
			break
		}
	}

	if s.config.EvalPromptTemplate != "" {
		// Use custom template
		prompt := s.config.EvalPromptTemplate
		prompt = strings.ReplaceAll(prompt, "{{query}}", userQuery)
		prompt = strings.ReplaceAll(prompt, "{{response}}", record.StudentContent)
		prompt = strings.ReplaceAll(prompt, "{{domain}}", string(record.Domain))
		return prompt
	}

	return fmt.Sprintf(`Evaluate this AI assistant response:

**User Query:**
%s

**Response:**
%s

**Domain:** %s

Rate the response on these dimensions (0.0 to 1.0):
1. Relevance: Does it address the query?
2. Completeness: Is it thorough enough?
3. Correctness: Is the information accurate?
4. Style: Is it well-formatted and clear?

Respond with JSON:
{"relevance": X.X, "completeness": X.X, "correctness": X.X, "style": X.X, "overall": X.X}`,
		userQuery, record.StudentContent, record.Domain)
}

func (s *Scorer) parseTeacherEval(response string, record *ShadowRecord) (*ScoreResult, error) {
	// Try to extract JSON from response
	jsonPattern := regexp.MustCompile(`\{[^}]+\}`)
	matches := jsonPattern.FindAllString(response, -1)

	for _, match := range matches {
		var eval struct {
			Relevance    float64 `json:"relevance"`
			Completeness float64 `json:"completeness"`
			Correctness  float64 `json:"correctness"`
			Style        float64 `json:"style"`
			Overall      float64 `json:"overall"`
		}

		if err := json.Unmarshal([]byte(match), &eval); err == nil {
			dimensions := map[string]float64{
				"relevance":    eval.Relevance,
				"completeness": eval.Completeness,
				"correctness":  eval.Correctness,
				"style":        eval.Style,
			}

			score := eval.Overall
			if score == 0 {
				// Calculate if overall not provided
				weights := s.config.HeuristicWeights
				score = eval.Relevance*weights.Relevance +
					eval.Completeness*weights.Completeness +
					eval.Correctness*weights.Correctness +
					eval.Style*weights.Style
			}

			return &ScoreResult{
				Score:         score,
				Dimensions:    dimensions,
				IsHighQuality: score >= s.config.HighQualityThreshold,
				Method:        "teacher_eval",
			}, nil
		}
	}

	// Failed to parse - fall back to heuristic
	s.logger.Warn("Failed to parse teacher evaluation, using heuristic")
	return s.scoreHeuristic(record), nil
}

const evalSystemPrompt = `You are an AI response quality evaluator. Your job is to objectively rate AI assistant responses on multiple dimensions. Be fair and consistent in your ratings. Consider the query context and expected response quality for the domain.

Always respond with valid JSON containing numeric scores from 0.0 to 1.0 for each dimension.`

// Helper functions

func extractKeyTerms(text string) []string {
	// Simple keyword extraction - remove common words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true, "to": true,
		"of": true, "in": true, "for": true, "on": true, "with": true,
		"at": true, "by": true, "from": true, "or": true, "and": true,
		"but": true, "if": true, KwThen: true, "that": true, "this": true,
		"it": true, "i": true, "you": true, "we": true, "they": true,
		"he": true, "she": true, "what": true, "how": true, "why": true,
		"when": true, "where": true, "which": true, "who": true, "me": true,
		"my": true, "your": true, "their": true, "our": true, "please": true,
	}

	words := strings.Fields(strings.ToLower(text))
	var terms []string
	seen := make(map[string]bool)

	for _, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,!?;:\"'()[]{}*")
		if len(word) < 3 || stopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
		if len(terms) >= 10 {
			break
		}
	}

	return terms
}

func hasBalancedBrackets(s string) bool {
	stack := []rune{}
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}

	for _, c := range s {
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[c] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}

	return len(stack) == 0
}

func clamp(v, lo, hi float64) float64 {
	return min(max(v, lo), hi)
}
