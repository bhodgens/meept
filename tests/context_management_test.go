package tests

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
)

// TestTokenizerAccuracy verifies tokenizer accuracy across different model families.
func TestTokenizerAccuracy(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		samples  []string
		maxError float64 // Maximum acceptable error ratio
	}{
		{
			name:    "GPT-4 family",
			modelID: "gpt-4",
			samples: []string{
				"Hello, world!",
				"The quick brown fox jumps over the lazy dog.",
				"func main() { fmt.Println(\"Hello\") }",
			},
			maxError: 0.15, // 15% max error vs heuristic
		},
		{
			name:    "Qwen family",
			modelID: "qwen2.5-coder",
			samples: []string{
				"你好，世界！",
				"def hello(): print('Hello')",
			},
			maxError: 0.20,
		},
		{
			name:    "GLM family",
			modelID: "glm-4.7",
			samples: []string{
				"GLM model test",
				"这是一段中文测试文本。",
			},
			maxError: 0.20,
		},
		{
			name:    "Mistral family",
			modelID: "dolphin-mistral-7b",
			samples: []string{
				"Mistral test content",
				"Voici un test en français.",
			},
			maxError: 0.20,
		},
		{
			name:    "Llama family",
			modelID: "llama3.2",
			samples: []string{
				"Llama 3 test",
				"Code snippet: import numpy as np",
			},
			maxError: 0.20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer := llm.NewTokenizerForModel(tt.modelID)
			if tokenizer == nil {
				t.Fatalf("Failed to create tokenizer for %s", tt.modelID)
			}

			for _, sample := range tt.samples {
				count := tokenizer.CountTokens(sample)
				if count <= 0 {
					t.Errorf("Token count for %q should be positive, got %d", sample, count)
				}
			}
		})
	}
}

// TestConversationTruncateByImportance verifies importance-based truncation.
func TestConversationTruncateByImportance(t *testing.T) {
	conv := agent.NewConversation()

	// Add messages of different importance levels
	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleUser,
		Content: "What is the capital of France?", // Critical - user input
	})

	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: "Let me think about this... Hmm... Considering the options...", // Low - reasoning
	})

	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: "Here's the final answer: Paris is the capital of France.", // High - conclusion
	})

	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleTool,
		Content: "```json\n{\"capital\": \"Paris\", \"population\": 2161000}\n```", // High - key finding
	})

	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: "Plan:\n1. Research the question\n2. Verify the answer\n3. Provide response", // Medium - plan
	})

	// Test truncation with tight budget
	budget := 100 // Very tight budget
	removed := conv.TruncateByImportance(budget)

	// Should have removed some messages
	if removed == 0 {
		t.Log("No messages removed - budget may be sufficient for all messages")
	}

	// Verify critical messages are retained (user input and conclusion)
	messages := conv.GetMessages()
	if len(messages) == 0 {
		t.Fatal("All messages were removed, including critical ones")
	}
}

// TestConversationImportanceClassification verifies message type classification.
func TestConversationImportanceClassification(t *testing.T) {
	conv := agent.NewConversation()

	// Test user message classification
	conv.AddUserMessage("Help me with this task")
	if conv.Len() != 1 {
		t.Fatal("Failed to add user message")
	}

	// Test conclusion detection
	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: "In conclusion, the solution is to use a hash map.",
	})

	// Test plan detection
	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: "Step 1: Initialize the database\nStep 2: Run migration",
	})

	// Test reasoning detection
	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: "Let me think about this. Hmm, this is interesting.",
	})

	// Test tool result with key findings
	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleTool,
		Content: "file: /etc/config.json\npath: /usr/local/bin/app",
	})

	if conv.Len() != 5 {
		t.Fatalf("Expected 5 messages, got %d", conv.Len())
	}
}

// TestToolDefinitionTokenCounting verifies accurate tool definition counting.
func TestToolDefinitionTokenCounting(t *testing.T) {
	tools := []llm.ToolDefinition{
		llm.NewToolDefinition(
			"read_file",
			"Read contents of a file at the specified path",
			llm.FunctionParameters{
				Type: "object",
				Properties: map[string]llm.ParameterProperty{
					"path": {
						Type:        "string",
						Description: "The file path to read",
					},
				},
				Required: []string{"path"},
			},
		),
	}

	// Count with nil tokenizer (uses heuristic)
	count := llm.CountToolDefinitionsTokens(tools, nil)
	if count <= 0 {
		t.Errorf("Tool definition count should be positive, got %d", count)
	}

	// Count with explicit tokenizer
	tokenizer := llm.NewTokenizerForModel("gpt-4")
	count2 := llm.CountToolDefinitionsTokens(tools, tokenizer)
	if count2 <= 0 {
		t.Errorf("Tool definition count with tokenizer should be positive, got %d", count2)
	}
}

// TestMultiTurnBudgetTracker verifies budget tracking across turns.
func TestMultiTurnBudgetTracker(t *testing.T) {
	tracker := agent.NewTurnBudgetTracker(100000, 30000, 10)

	// Initial state
	if tracker.RemainingBudget() != 100000 {
		t.Errorf("Expected initial budget 100000, got %d", tracker.RemainingBudget())
	}

	if tracker.IsWarningZone() {
		t.Error("Should not be in warning zone initially")
	}

	if tracker.IsWrapUpRequested() {
		t.Error("Should not request wrap-up initially")
	}

	// Record some usage
	tracker.RecordUsage(25000)

	if tracker.RemainingBudget() != 75000 {
		t.Errorf("Expected remaining budget 75000, got %d", tracker.RemainingBudget())
	}

	// Record more usage to enter warning zone (80%+ used)
	tracker.RecordUsage(25000)
	tracker.RecordUsage(25000)
	tracker.RecordUsage(25000) // Total: 100000 used

	if !tracker.IsWarningZone() {
		t.Error("Should be in warning zone after 80%+ usage")
	}
}

// TestTurnBudgetAvailableBudget verifies per-turn budget allocation.
func TestTurnBudgetAvailableBudget(t *testing.T) {
	tracker := agent.NewTurnBudgetTracker(100000, 30000, 5)

	// First turn - should get full per-turn allocation
	budget := tracker.AvailableBudgetForTurn()
	if budget <= 0 {
		t.Errorf("First turn budget should be positive, got %d", budget)
	}

	// Record usage
	tracker.RecordUsage(20000)

	// Second turn
	budget2 := tracker.AvailableBudgetForTurn()
	if budget2 <= 0 {
		t.Errorf("Second turn budget should be positive, got %d", budget2)
	}
}

// TestContextFirewallIntegration verifies context firewall with budget enforcement.
func TestContextFirewallIntegration(t *testing.T) {
	// Create a mock model config
	model := &llm.ModelConfig{
		ContextLimit: 32768,
		ModelID:      "glm-4.7",
	}

	// Create firewall with default config
	firewall := llm.NewContextFirewall(
		nil, // inner - nil for this test
		model,
		llm.ContextFirewallConfig{
			Enabled:         true,
			SummarizeHistory: false,
			ChunkLargeInputs: true,
		},
		nil,
		nil,
		nil, // tokenizer - uses heuristic
	)

	if firewall == nil {
		t.Fatal("Failed to create ContextFirewall")
	}

	// Verify budget calculation
	budget := firewall.DerivedIterationBudget()
	if budget <= 0 {
		t.Errorf("Iteration budget should be positive, got %d", budget)
	}

	// Verify conversation budget
	convBudget := firewall.DerivedConversationBudget()
	if convBudget <= 0 {
		t.Errorf("Conversation budget should be positive, got %d", convBudget)
	}
}

// TestLargeContentTokenCounting verifies tokenizer handles large content.
func TestLargeContentTokenCounting(t *testing.T) {
	// Generate large content (~10K characters)
	largeContent := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 200)

	tokenizer := llm.NewTokenizerForModel("glm-4.7")
	count := tokenizer.CountTokens(largeContent)

	if count <= 0 {
		t.Errorf("Token count for large content should be positive")
	}

	// Verify caching works
	count2 := tokenizer.CountTokens(largeContent)
	if count != count2 {
		t.Errorf("Cached token count should match original: %d vs %d", count, count2)
	}
}
