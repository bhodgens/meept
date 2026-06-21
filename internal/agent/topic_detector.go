package agent

import (
	"log/slog"
	"strings"
	"sync"
)

// TopicDetector identifies conversation topics from user input using
// keyword-scoring heuristics. It is safe for concurrent use.
type TopicDetector struct {
	mu           sync.RWMutex
	keywords     map[string][]string // topic -> keywords
	defaultTopic string
	logger       *slog.Logger
}

// TopicDetectorOption configures a TopicDetector.
type TopicDetectorOption func(*TopicDetector)

// WithTopicKeywords adds keywords for the given topic.
func WithTopicKeywords(topic string, keywords []string) TopicDetectorOption {
	return func(td *TopicDetector) {
		td.keywords[topic] = append(td.keywords[topic], keywords...)
	}
}

// WithDefaultTopic sets the default topic returned when no keywords match.
func WithDefaultTopic(topic string) TopicDetectorOption {
	return func(td *TopicDetector) {
		td.defaultTopic = topic
	}
}

// WithLogger sets the logger used for debug-level topic scoring output.
func WithLogger(l *slog.Logger) TopicDetectorOption {
	return func(td *TopicDetector) {
		if l != nil {
			td.logger = l
		}
	}
}

// NewTopicDetector creates a TopicDetector with the default keyword
// categories used by the thread-based context partitioning system.
//
// The default categories are:
//
//	   "work"      – task, feature, bug, code, build, deploy, api
//	   "code"      – debug, error, panic, compile, test
//	   "food"      – lunch, dinner, food, eat, recipe, restaurant
//	   "personal"  – weekend, vacation, hobby, shopping
//	   "health"    – workout, gym, exercise, running, diet, sleep
//
// The default fallback topic is "general".
func NewTopicDetector(opts ...TopicDetectorOption) *TopicDetector {
	td := &TopicDetector{
		keywords: map[string][]string{
			"work":     {"task", "feature", "bug", "code", "build", "deploy", "api", "function", "method", "endpoint"},
			"code":     {"debug", "error", "panic", "stack trace", "compile", "test", "lint"},
			"food":     {"lunch", "dinner", "breakfast", "food", "eat", "recipe", "restaurant", "cook", "hungry"},
			"personal": {"weekend", "vacation", "hobby", "shopping", "family", "friend", "party", "travel"},
			"health":   {"workout", "gym", "exercise", "running", "diet", "sleep", "doctor", "medicine"},
		},
		defaultTopic: "general",
		logger:       slog.Default(),
	}

	for _, opt := range opts {
		opt(td)
	}

	return td
}

// Detect returns the topic label that best matches the given input.
// It scores every topic by the number of keyword matches (after
// lower-casing the input) and returns the topic with the highest score.
// When two topics tie, the first encountered wins; the default topic
// is returned when all scores are zero.
//
// Detect is safe for concurrent use.
func (td *TopicDetector) Detect(input string) string {
	td.mu.RLock()
	defer td.mu.RUnlock()

	lowerInput := strings.ToLower(input)

	bestTopic := td.defaultTopic
	bestScore := 0

	for topic, keywords := range td.keywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lowerInput, kw) {
				score++
			}
		}
		if td.logger != nil && score > 0 {
			td.logger.Debug("topic detector score", "topic", topic, "score", score)
		}
		if score > bestScore {
			bestScore = score
			bestTopic = topic
		}
	}

	return bestTopic
}

// GenerateThreadID creates a deterministic thread ID from a session ID
// and topic label, using the last 4 runes of sessionID (or the full
// string if shorter than 4 characters).
func (td *TopicDetector) GenerateThreadID(sessionID, topic string) string {
	suffix := sessionID
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	return "thread-" + topic + "-" + suffix
}
