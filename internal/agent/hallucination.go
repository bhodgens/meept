package agent

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// HallucinationSensitivity determines how aggressively to detect hallucinations.
type HallucinationSensitivity string

const (
	SensitivityLow      HallucinationSensitivity = "low"
	SensitivityMedium   HallucinationSensitivity = "medium"
	SensitivityHigh     HallucinationSensitivity = "high"
)

// HallucinationConfig configures the hallucination detector.
type HallucinationConfig struct {
	// Enabled turns on hallucination detection
	Enabled bool
	// Sensitivity determines detection aggressiveness (default: "low")
	Sensitivity HallucinationSensitivity
	// MaxIndicators is the number of indicators needed to trigger recovery (default: 2)
	MaxIndicators int
}

// DefaultHallucinationConfig returns sensible defaults.
func DefaultHallucinationConfig() HallucinationConfig {
	return HallucinationConfig{
		Enabled:       true,
		Sensitivity:   SensitivityLow,
		MaxIndicators: 2,
	}
}

// HallucinationIndicator represents a single detected hallucination indicator.
type HallucinationIndicator struct {
	Type        string  `json:"type"`         // "confident_claim", "fabricated_ref", "contradiction", "impossible_response"
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`   // 0.0-1.0 confidence this is actually a hallucination
	Content     string  `json:"content"`      // The specific text that triggered detection
}

// HallucinationResult holds the results of hallucination analysis.
type HallucinationResult struct {
	Indicators    []HallucinationIndicator `json:"indicators"`
	Score         float64                  `json:"score"`          // 0.0-1.0 overall hallucination probability
	ShouldRecover bool                     `json:"should_recover"` // true if MaxIndicators threshold exceeded
}

// HallucinationDetector detects hallucination patterns in LLM output.
type HallucinationDetector struct {
	mu       sync.RWMutex
	config   HallucinationConfig
	logger   *slog.Logger

	// Known symbols for fact-checking (file paths, function names, etc.)
	knownSymbols map[string]bool
	knownFiles   map[string]bool

	// History tracking for contradiction detection
	history []string
}

// NewHallucinationDetector creates a new hallucination detector.
func NewHallucinationDetector(cfg HallucinationConfig, logger *slog.Logger) *HallucinationDetector {
	if logger == nil {
		logger = slog.Default()
	}

	return &HallucinationDetector{
		config:       cfg,
		logger:       logger.With("component", "hallucination-detector"),
		knownSymbols: make(map[string]bool),
		knownFiles:   make(map[string]bool),
		history:      make([]string, 0, 20),
	}
}

// Analyze checks LLM output for hallucination indicators.
// The output is the LLM's response, and conversation provides context for contradiction checking.
func (hd *HallucinationDetector) Analyze(output string, conversation []string) *HallucinationResult {
	if !hd.config.Enabled {
		return &HallucinationResult{ShouldRecover: false}
	}

	var indicators []HallucinationIndicator

	// Run detection checks
	indicators = append(indicators, hd.detectConfidentClaims(output)...)
	indicators = append(indicators, hd.detectFabricatedReferences(output)...)
	indicators = append(indicators, hd.detectContradictions(output, conversation)...)
	indicators = append(indicators, hd.detectImpossibleResponses(output)...)

	// Filter by sensitivity
	indicators = hd.filterBySensitivity(indicators)

	// Calculate score
	score := hd.calculateScore(indicators)

	// Determine if recovery should trigger
	maxIndicators := hd.config.MaxIndicators
	if maxIndicators <= 0 {
		maxIndicators = 2
	}
	shouldRecover := len(indicators) >= maxIndicators

	result := &HallucinationResult{
		Indicators:    indicators,
		Score:         score,
		ShouldRecover: shouldRecover,
	}

	if shouldRecover {
		hd.logger.Warn("Hallucination detected, recovery recommended",
			"indicators", len(indicators),
			"score", score,
		)
	} else if len(indicators) > 0 {
		hd.logger.Debug("Hallucination indicators detected but below threshold",
			"indicators", len(indicators),
			"score", score,
			"threshold", maxIndicators,
		)
	}

	return result
}

// RecordHistory records output for future contradiction detection.
func (hd *HallucinationDetector) RecordHistory(content string) {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	hd.history = append(hd.history, content)
	// Keep bounded history
	if len(hd.history) > 20 {
		hd.history = hd.history[1:]
	}
}

// RegisterKnownSymbol registers a known symbol (function name, type name, etc.) for fact-checking.
func (hd *HallucinationDetector) RegisterKnownSymbol(name string, isFile bool) {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	if isFile {
		hd.knownFiles[name] = true
	} else {
		hd.knownSymbols[name] = true
	}
}

// RegisterKnownSymbols registers multiple known symbols at once.
func (hd *HallucinationDetector) RegisterKnownSymbols(symbols map[string]bool, files map[string]bool) {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	for k, v := range symbols {
		hd.knownSymbols[k] = v
	}
	for k, v := range files {
		hd.knownFiles[k] = v
	}
}

// detectConfidentClaims detects claims stated with unwarranted certainty.
func (hd *HallucinationDetector) detectConfidentClaims(output string) []HallucinationIndicator {
	var indicators []HallucinationIndicator

	// Patterns that indicate confident but potentially fabricated claims
	confidentPatterns := []struct {
		pattern   string
		severity  float64
		desc      string
	}{
		{`(?i)\bI (?:have |have already )?(?:created|modified|deleted|fixed|updated|implemented)\b.*\b(?:file|function|method|class|module)\b`, 0.6, "claims file/code creation without evidence"},
		{`(?i)\bthe (?:file|function|method|class) (?:now|has been|is now) (?:updated|modified|fixed|changed)\b`, 0.5, "asserts change without showing diff"},
		{`(?i)\bI (?:can confirm|verified|confirmed|checked) that\b`, 0.4, "claims verification without evidence"},
	}

	for _, cp := range confidentPatterns {
		re := regexp.MustCompile(cp.pattern)
		matches := re.FindAllString(output, -1)
		for _, match := range matches {
			indicators = append(indicators, HallucinationIndicator{
				Type:        "confident_claim",
				Description: cp.desc,
				Confidence:  cp.severity,
				Content:     truncateForIndicator(match),
			})
		}
	}

	return indicators
}

// detectFabricatedReferences detects references to files or symbols that don't exist.
func (hd *HallucinationDetector) detectFabricatedReferences(output string) []HallucinationIndicator {
	hd.mu.RLock()
	defer hd.mu.RUnlock()

	var indicators []HallucinationIndicator

	// Extract file paths from output
	filePathPattern := regexp.MustCompile(`(?i)(?:at |in |file |path |from )["']?([a-zA-Z0-9_./\-]+\.[a-zA-Z]{1,10})["']?`)
	matches := filePathPattern.FindAllStringSubmatch(output, -1)

	for _, match := range matches {
		if len(match) > 1 {
			filePath := match[1]
			// Check if this is a known file
			if len(hd.knownFiles) > 0 && !hd.knownFiles[filePath] {
				// Only flag if we have registered files (otherwise we can't tell)
				indicators = append(indicators, HallucinationIndicator{
					Type:        "fabricated_ref",
					Description: fmt.Sprintf("reference to potentially non-existent file: %s", filePath),
					Confidence:  0.5,
					Content:     truncateForIndicator(match[0]),
				})
			}
		}
	}

	// Extract symbol references
	symbolPattern := regexp.MustCompile(`(?i)(?:function|method|class|type|struct|interface)\s+([A-Z][a-zA-Z0-9_]*)`)
	symbolMatches := symbolPattern.FindAllStringSubmatch(output, -1)

	for _, match := range symbolMatches {
		if len(match) > 1 {
			symbol := match[1]
			if len(hd.knownSymbols) > 0 && !hd.knownSymbols[symbol] {
				indicators = append(indicators, HallucinationIndicator{
					Type:        "fabricated_ref",
					Description: fmt.Sprintf("reference to potentially non-existent symbol: %s", symbol),
					Confidence:  0.4,
					Content:     truncateForIndicator(match[0]),
				})
			}
		}
	}

	return indicators
}

// detectContradictions detects contradictions between current output and previous statements.
func (hd *HallucinationDetector) detectContradictions(output string, conversation []string) []HallucinationIndicator {
	hd.mu.RLock()
	defer hd.mu.RUnlock()

	var indicators []HallucinationIndicator

	// Check for explicit negations of previous statements
	negationPatterns := []string{
		"actually,",
		"on second thought,",
		"i was wrong",
		"that was incorrect",
		"let me correct",
		"i made a mistake",
	}

	lower := strings.ToLower(output)
	for _, pattern := range negationPatterns {
		if strings.Contains(lower, pattern) {
			// Only flag if there's prior history to contradict
			if len(hd.history) > 0 || len(conversation) > 0 {
				indicators = append(indicators, HallucinationIndicator{
					Type:        "contradiction",
					Description: "output contains self-correction or contradiction indicator",
					Confidence:  0.6,
					Content:     truncateForIndicator(pattern),
				})
			}
			break // Only report once per check
		}
	}

	return indicators
}

// detectImpossibleResponses detects outputs that are impossible or nonsensical.
func (hd *HallucinationDetector) detectImpossibleResponses(output string) []HallucinationIndicator {
	var indicators []HallucinationIndicator

	// Check for impossibly large numbers
	largeNumPattern := regexp.MustCompile(`\b(\d{10,})\b`)
	matches := largeNumPattern.FindAllString(output, -1)
	for _, match := range matches {
		indicators = append(indicators, HallucinationIndicator{
			Type:        "impossible_response",
			Description: "suspiciously large number in output",
			Confidence:  0.3,
			Content:     truncateForIndicator(match),
		})
	}

	// Check for repeated phrases (sign of generation loop)
	// Look for triple repetition of the same word (e.g., "the the the")
	words := strings.Fields(strings.ToLower(output))
	for i := 0; i < len(words)-2; i++ {
		w1 := strings.Trim(words[i], ".,;:!?'\"()[]{}")
		w2 := strings.Trim(words[i+1], ".,;:!?'\"()[]{}")
		w3 := strings.Trim(words[i+2], ".,;:!?'\"()[]{}")
		if len(w1) > 2 && w1 == w2 && w2 == w3 {
			indicators = append(indicators, HallucinationIndicator{
				Type:        "impossible_response",
				Description: "repeated phrase pattern detected",
				Confidence:  0.5,
				Content:     truncateForIndicator(w1 + " " + w2 + " " + w3),
			})
			break // Only report once
		}
	}

	return indicators
}

// filterBySensitivity filters indicators based on the configured sensitivity level.
func (hd *HallucinationDetector) filterBySensitivity(indicators []HallucinationIndicator) []HallucinationIndicator {
	var minConfidence float64
	switch hd.config.Sensitivity {
	case SensitivityLow:
		minConfidence = 0.6
	case SensitivityMedium:
		minConfidence = 0.4
	case SensitivityHigh:
		minConfidence = 0.2
	default:
		minConfidence = 0.6
	}

	filtered := make([]HallucinationIndicator, 0, len(indicators))
	for _, ind := range indicators {
		if ind.Confidence >= minConfidence {
			filtered = append(filtered, ind)
		}
	}
	return filtered
}

// calculateScore calculates an overall hallucination probability score.
func (hd *HallucinationDetector) calculateScore(indicators []HallucinationIndicator) float64 {
	if len(indicators) == 0 {
		return 0.0
	}

	var totalConfidence float64
	for _, ind := range indicators {
		totalConfidence += ind.Confidence
	}

	// Average confidence, capped at 1.0
	score := totalConfidence / float64(len(indicators))
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// truncateForIndicator truncates content for inclusion in indicators.
func truncateForIndicator(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
