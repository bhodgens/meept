package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// ModelReassignmentParser parses natural language model reassignment instructions.
type ModelReassignmentParser struct {
	patterns      []*regexp.Regexp
	scopeKeywords map[string]IntentType
	modelAliases  map[string]string
	providerNames map[string]string
}

// ParseResult is the result of parsing a model reassignment instruction.
type ParseResult struct {
	// Found - true if a model reassignment was detected
	Found bool

	// Directive - the parsed directive (nil if not found)
	Directive *ModelReassignmentDirective

	// Ambiguities - list of ambiguities detected
	Ambiguities []string
}

// NewModelReassignmentParser creates a new model reassignment parser.
func NewModelReassignmentParser() *ModelReassignmentParser {
	patterns := compilePatterns(modelReassignmentPatterns)

	scopeKeywords := map[string]IntentType{
		// Planning/Synthesis
		"synthesis":    IntentPlan,
		"synthesize":   IntentPlan,
		"planning":     IntentPlan,
		"plan":         IntentPlan,
		"design":       IntentPlan,
		"architecture": IntentPlan,

		// Coding
		"coding":         IntentCode,
		"code":           IntentCode,
		"programming":    IntentCode,
		"implementation": IntentCode,
		"refactor":       IntentCode,
		"writing code":   IntentCode,

		// Research/Analysis
		"research":    IntentResearch,
		"analysis":    IntentAnalyze,
		"analyze":     IntentAnalyze,
		"investigate": IntentAnalyze,
		"study":       IntentResearch,

		// Debugging
		"debugging":       IntentDebug,
		"debug":           IntentDebug,
		"troubleshooting": IntentDebug,
		"fix":             IntentDebug,
		"bug":             IntentDebug,
	}

	modelAliases := map[string]string{
		// Anthropic
		"opus":          "anthropic/claude-3-opus",
		"claude-opus":   "anthropic/claude-3-opus",
		"sonnet":        "anthropic/claude-3-sonnet",
		"claude-sonnet": "anthropic/claude-3-sonnet",
		"haiku":         "anthropic/claude-3-haiku",
		"claude-haiku":  "anthropic/claude-3-haiku",

		// Z.AI (GLM)
		"glm":     "zai/glm-4.7",
		"glm-4.7": "zai/glm-4.7",
		"glm-4.5": "zai/glm-4.5-air",
		"glm-air": "zai/glm-4.5-air",

		// Ollama
		"qwen":       "ollama/qwen2.5-coder",
		"qwen-coder": "ollama/qwen2.5-coder",
		"llama":      "ollama/llama3.2",
		"llama3.2":   "ollama/llama3.2",
		"llama3":     "ollama/llama3.2",

		// OpenAI
		"gpt-4":       "openai/gpt-4",
		"gpt-4o":      "openai/gpt-4o",
		"gpt-4-turbo": "openai/gpt-4-turbo",
		"gpt-3.5":     "openai/gpt-3.5-turbo",

		// Local models
		"lfm-code":        "local/lfm-code",
		"lfm-thinking":    "local/lfm-thinking-claude",
		"lfm-24b":         "local/lfm-24b",
		"thinking-claude": "local/lfm-thinking-claude",
	}

	providerNames := map[string]string{
		"glm":    "zai",
		"qwen":   "ollama",
		"llama":  "ollama",
		"claude": "anthropic",
		"gpt":    "openai",
		"lfm":    "local",
		"local":  "local",
	}

	return &ModelReassignmentParser{
		patterns:      patterns,
		scopeKeywords: scopeKeywords,
		modelAliases:  modelAliases,
		providerNames: providerNames,
	}
}

// modelReassignmentPatterns are regex patterns for common model reassignment phrasings.
// Order matters - more specific patterns should come first
var modelReassignmentPatterns = []string{
	// "X models only for Y" - e.g., "GLM models only for synthesis" (MUST come before "use X for Y")
	`(?i)(?P<models>(?:[\w/\-\.]+\s*)+)\s+models\s+only\s+for\s+(?P<scope>(?:[\w\-]+\s*)+)`,

	// "X models for Y" - e.g., "GLM models for planning" (MUST come before "use X for Y")
	`(?i)(?P<models>(?:[\w/\-\.]+\s*)+)\s+models\s+for\s+(?P<scope>(?:[\w\-]+\s*)+)`,

	// "use X for Y" - e.g., "use GLM for coding"
	`(?i)use\s+(?P<models>(?:[\w/\-\.]+\s*)+)\s+for\s+(?P<scope>(?:[\w\-]+\s*)+)`,

	// "use X models" - e.g., "use GLM models" (no scope - will need clarification)
	`(?i)use\s+(?P<models>(?:[\w/\-\.]+\s*)+)\s+models`,

	// "synthesize using X" / "analyzing with X" / "code via X" - action words
	`(?i)(?P<action>synthesiz[e|ing]?|analys[e|ing]?|research|code|plan|debug|implement)\s+(?:using|with|via)\s+(?P<models>(?:[\w/\-\.]+\s*)+)`,

	// "do this with X" - e.g., "do this with GLM-4.7"
	`(?i)do\s+(?:this|that)\s+with\s+(?P<models>(?:[\w/\-\.]+\s*)+)`,

	// "I want X to handle Y" - e.g., "I want GLM to handle coding"
	`(?i)(?:want|i'd\s+like|i\s+want)\s+(?P<models>(?:[\w/\-\.]+\s*)+)\s+to\s+handle\s+(?P<scope>(?:[\w\-]+\s*)+)`,

	// "X for Y task" - e.g., "GLM for the synthesis task"
	`(?i)(?P<models>(?:[\w/\-\.]+\s*)+)\s+for\s+(?:the\s+)?(?P<scope>(?:[\w\-]+\s*?))(?:\s+task)?`,

	// "only use X" - e.g., "only use local models" (no scope, capture for clarification)
	`(?i)only\s+use\s+(?P<models>(?:[\w/\-\.]+\s*)+)`,
}

// compilePatterns compiles regex patterns, skipping invalid ones.
func compilePatterns(patternStrs []string) []*regexp.Regexp {
	var patterns []*regexp.Regexp
	for _, patternStr := range patternStrs {
		pattern, err := regexp.Compile(patternStr)
		if err != nil {
			continue // Skip invalid patterns
		}
		patterns = append(patterns, pattern)
	}
	return patterns
}

// Parse parses input for model reassignment instructions.
func (p *ModelReassignmentParser) Parse(input string) *ParseResult {
	result := &ParseResult{
		Found:       false,
		Ambiguities: []string{},
	}

	// Check for model reassignment patterns
	directive := &ModelReassignmentDirective{
		Instruction: input,
	}

	// Try to match patterns
	matched := false
	for _, pattern := range p.patterns {
		matches := pattern.FindStringSubmatch(input)
		if matches == nil {
			continue
		}

		matched = true
		result.Found = true

		// Extract named groups
		modelsMatch := p.extractGroup(pattern, "models", matches)
		scopeMatch := p.extractGroup(pattern, "scope", matches)
		actionMatch := p.extractGroup(pattern, "action", matches)

		// Parse models
		if modelsMatch != "" {
			directive.ModelReferences = p.parseModelReferences(modelsMatch)
		}

		// Parse scope
		if scopeMatch != "" {
			directive.TargetScope = strings.TrimSpace(scopeMatch)
		} else if actionMatch != "" {
			// Use action as scope
			directive.TargetScope = actionMatch
		}

		break // Use first matching pattern
	}

	if !matched {
		return result
	}

	// Resolve scope to intent type
	if directive.TargetScope != "" {
		if intentType, ok := p.ResolveScope(directive.TargetScope); ok {
			directive.TargetIntent = &intentType
		}
	}

	// Resolve model references to actual model configs
	// (This is done by the caller with access to resolver)

	// Check for ambiguities
	if len(directive.ModelReferences) == 0 {
		result.Ambiguities = append(result.Ambiguities, "no_models_parsed")
		directive.ClarificationNeeded = true
		directive.ClarificationQuestions = append(directive.ClarificationQuestions,
			"I couldn't identify specific model names. Which model would you like to use?")
	}

	if directive.TargetScope == "" {
		result.Ambiguities = append(result.Ambiguities, "no_scope_parsed")
		directive.ClarificationNeeded = true
		directive.ClarificationQuestions = append(directive.ClarificationQuestions,
			"What should the specified model(s) handle - research, coding, synthesis, or the entire task?")
	}

	// Check if model references are ambiguous (e.g., "local models" could mean multiple)
	for _, ref := range directive.ModelReferences {
		if p.isAmbiguousReference(ref) {
			result.Ambiguities = append(result.Ambiguities, fmt.Sprintf("ambiguous_model:%s", ref))
			directive.ClarificationNeeded = true
			available := p.getAvailableModelsForReference(ref)
			if len(available) > 0 {
				directive.ClarificationQuestions = append(directive.ClarificationQuestions,
					fmt.Sprintf("'%s' could refer to: %s. Which one?", ref, strings.Join(available, ", ")))
			}
		}
	}

	result.Directive = directive
	return result
}

// extractGroup extracts a named group from regex match results.
func (p *ModelReassignmentParser) extractGroup(pattern *regexp.Regexp, name string, matches []string) string {
	index := pattern.SubexpIndex(name)
	if index < 0 || index >= len(matches) {
		return ""
	}
	// Trim all whitespace including trailing
	result := strings.TrimSpace(matches[index])
	// Also remove any trailing whitespace that might have been captured
	result = strings.TrimRight(result, " \t\n\r")
	return result
}

// parseModelReferences parses a model reference string into individual references.
func (p *ModelReassignmentParser) parseModelReferences(input string) []string {
	input = strings.TrimSpace(input)

	// Remove common modifiers
	input = strings.ReplaceAll(input, "only", "")
	input = strings.ReplaceAll(input, "models", "")
	input = strings.TrimSpace(input)

	// Split on common conjunctions
	var refs []string

	// Handle "X or Y" or "X and Y"
	parts := regexp.MustCompile(`\s+(?:and|or)\s+`).Split(input, -1)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Handle provider references like "GLM" → extract provider
		partLower := strings.ToLower(part)
		if provider, ok := p.providerNames[partLower]; ok {
			refs = append(refs, "provider:"+provider)
			continue
		}

		// Handle multi-word parts like "GLM model" → extract first word
		words := strings.Fields(part)
		if len(words) > 0 {
			firstWord := strings.ToLower(words[0])
			// Check if first word is a provider
			if provider, ok := p.providerNames[firstWord]; ok {
				refs = append(refs, "provider:"+provider)
				continue
			}
			// Check if it's a specific model alias (full part matching, not just first word)
			if resolved, ok := p.modelAliases[partLower]; ok {
				refs = append(refs, resolved)
				continue
			}
		}

		// Keep as-is if nothing else matched - it might be a direct model ref like "zai/glm-4.7"
		refs = append(refs, part)
	}

	return refs
}

// ResolveScope resolves a scope keyword to an IntentType.
func (p *ModelReassignmentParser) ResolveScope(scope string) (IntentType, bool) {
	scope = strings.ToLower(strings.TrimSpace(scope))

	// Direct match
	if intentType, ok := p.scopeKeywords[scope]; ok {
		return intentType, true
	}

	// Partial match
	for keyword, intentType := range p.scopeKeywords {
		if strings.Contains(scope, keyword) || strings.Contains(keyword, scope) {
			return intentType, true
		}
	}

	return IntentType(""), false
}

// isAmbiguousReference checks if a model reference is ambiguous.
func (p *ModelReassignmentParser) isAmbiguousReference(ref string) bool {
	ref = strings.ToLower(ref)

	// Provider-only references are ambiguous (e.g., "local", "glm")
	if strings.HasPrefix(ref, "provider:") {
		return true
	}

	// Check if it's EXACTLY a broad category term (not containing it)
	broadTerms := []string{"local", "glm", "qwen", "llama", "claude", "gpt"}
	for _, term := range broadTerms {
		if ref == term {
			return true
		}
	}

	// If it looks like a specific model reference (contains / or - or .), it's not ambiguous
	if strings.Contains(ref, "/") || strings.Contains(ref, "-") || strings.Contains(ref, ".") {
		return false
	}

	// Check if it's in modelAliases - if so, it's not ambiguous
	if _, ok := p.modelAliases[ref]; ok {
		return false
	}

	// Unknown single word - might be ambiguous
	return true
}

// getAvailableModelsForReference returns available models for a reference.
func (p *ModelReassignmentParser) getAvailableModelsForReference(ref string) []string {
	ref = strings.ToLower(ref)
	var available []string

	// Handle provider references
	if provider, ok := p.providerNames[ref]; ok {
		for alias, modelRef := range p.modelAliases {
			if strings.HasPrefix(modelRef, provider+"/") {
				available = append(available, alias)
			}
		}
	}

	// Handle partial matches
	if len(available) == 0 {
		for alias, modelRef := range p.modelAliases {
			if strings.Contains(alias, ref) || strings.Contains(modelRef, ref) {
				available = append(available, alias)
			}
		}
	}

	return available
}

// ResolveModelReferences resolves model references to ModelConfig using the resolver.
// This is called by the dispatcher after parsing, as it needs access to the resolver.
func (p *ModelReassignmentParser) ResolveModelReferences(refs []string, resolver *llm.Resolver) []*llm.ModelConfig {
	var configs []*llm.ModelConfig

	for _, ref := range refs {
		// Handle provider: prefix (resolve to first available)
		if strings.HasPrefix(ref, "provider:") {
			provider := strings.TrimPrefix(ref, "provider:")
			models := resolver.FindByProvider(provider)
			if len(models) > 0 {
				configs = append(configs, models[0]) // Use first available
			}
			continue
		}

		// Try direct resolution
		mc := resolver.ResolveRef(ref)
		if mc != nil {
			configs = append(configs, mc)
		}
	}

	return configs
}
