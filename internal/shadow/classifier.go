package shadow

import (
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// Classifier provides unified classification for domain, task type, and complexity.
// This eliminates duplicate classification logic across middleware, manager, and loop.
type Classifier struct{}

// NewClassifier creates a new classifier.
func NewClassifier() *Classifier {
	return &Classifier{}
}

// ClassificationResult holds the result of classifying a request.
type ClassificationResult struct {
	Domain     Domain
	TaskType   TaskType
	Complexity Complexity
}

// Classify performs full classification of messages and optional response.
func (c *Classifier) Classify(messages []llm.ChatMessage, response *llm.Response) ClassificationResult {
	return ClassificationResult{
		Domain:     c.ClassifyDomain(messages),
		TaskType:   c.ClassifyTaskType(messages, response),
		Complexity: c.EstimateComplexity(messages),
	}
}

// ClassifyDomain determines the domain of the conversation.
func (c *Classifier) ClassifyDomain(messages []llm.ChatMessage) Domain {
	var text string
	for _, msg := range messages {
		text += " " + msg.Content
	}
	lower := strings.ToLower(text)

	// Keywords for each domain, ordered by specificity
	domainKeywords := map[Domain][]string{
		DomainDebugging: {
			"debug", "fix", "issue", "problem", "crash", "stack trace",
			"exception", "traceback", "error", "bug", "failing", "broken",
		},
		DomainCode: {
			"code", "function", "class", "variable", "compile", "syntax",
			"import", "package", "implement", "refactor", "method", "struct",
			"type", "interface", "module", "library", "api",
		},
		DomainPlanning: {
			"plan", "step", "strategy", "approach", "design", "architecture",
			"roadmap", "milestone", "timeline", "schedule", "prioritize",
		},
		DomainAnalysis: {
			"analyze", "explain", "how does", "what is", "understand",
			"review", "investigate", "examine", "study", "evaluate",
		},
	}

	// Check domains in order of specificity
	domainOrder := []Domain{DomainDebugging, DomainCode, DomainPlanning, DomainAnalysis}

	for _, domain := range domainOrder {
		keywords := domainKeywords[domain]
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				return domain
			}
		}
	}

	return DomainGeneral
}

// ClassifyTaskType determines the type of task.
func (c *Classifier) ClassifyTaskType(messages []llm.ChatMessage, response *llm.Response) TaskType {
	// Check if response has tool calls
	if response != nil && response.HasToolCalls() {
		return TaskTypeToolUse
	}

	// Analyze message content
	var text string
	for _, msg := range messages {
		text += " " + msg.Content
	}
	lower := strings.ToLower(text)

	// Multi-step patterns
	multiStepKeywords := []string{
		"step by step", "first", "second", "third", "then", "finally",
		"multiple steps", "next", "after that", "following",
	}
	for _, kw := range multiStepKeywords {
		if strings.Contains(lower, kw) {
			return TaskTypeMultiStep
		}
	}

	// Reasoning patterns
	reasoningKeywords := []string{
		"think", "reason", "consider", "analyze", "evaluate", "compare",
		"decide", "choose between", "trade-off", "pros and cons",
	}
	for _, kw := range reasoningKeywords {
		if strings.Contains(lower, kw) {
			return TaskTypeReasoning
		}
	}

	return TaskTypeChat
}

// EstimateComplexity estimates the complexity of the interaction.
func (c *Classifier) EstimateComplexity(messages []llm.ChatMessage) Complexity {
	var totalLength int
	var hasCode bool
	var hasMultipleMessages bool

	for _, msg := range messages {
		totalLength += len(msg.Content)
		lower := strings.ToLower(msg.Content)

		// Check for code indicators
		codeIndicators := []string{
			"```", "func ", "def ", "class ", "import ", "package ",
			"function ", "const ", "let ", "var ", "public ", "private ",
		}
		for _, indicator := range codeIndicators {
			if strings.Contains(lower, indicator) {
				hasCode = true
				break
			}
		}
	}

	hasMultipleMessages = len(messages) > 2

	// Complexity thresholds
	if totalLength > 2000 || (hasCode && hasMultipleMessages) {
		return ComplexityComplex
	}
	if totalLength > 500 || hasCode || hasMultipleMessages {
		return ComplexityModerate
	}

	return ComplexitySimple
}

// ClassifyFromShadowMessages classifies using shadow.Message instead of llm.ChatMessage.
func (c *Classifier) ClassifyFromShadowMessages(messages []Message) ClassificationResult {
	// Convert to llm.ChatMessage
	llmMessages := make([]llm.ChatMessage, len(messages))
	for i, msg := range messages {
		llmMessages[i] = llm.ChatMessage{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		}
	}
	return c.Classify(llmMessages, nil)
}

// DefaultClassifier is a package-level classifier for convenience.
var DefaultClassifier = NewClassifier()

// ClassifyDomain classifies domain using the default classifier.
func ClassifyDomain(messages []llm.ChatMessage) Domain {
	return DefaultClassifier.ClassifyDomain(messages)
}

// ClassifyTaskType classifies task type using the default classifier.
func ClassifyTaskType(messages []llm.ChatMessage, response *llm.Response) TaskType {
	return DefaultClassifier.ClassifyTaskType(messages, response)
}

// EstimateComplexity estimates complexity using the default classifier.
func EstimateComplexity(messages []llm.ChatMessage) Complexity {
	return DefaultClassifier.EstimateComplexity(messages)
}
