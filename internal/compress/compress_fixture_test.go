package compress

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Fixture test inputs — realistic samples for parity testing.

// fixtureJSONBlob is a large JSON array of objects with repetitive structure,
// typical of tool outputs like file listings or API responses.
var fixtureJSONBlob = `[` + jsonRepetitiveItems(80) + `]`

// fixtureGoCode is a Go source code snippet with multiple functions,
// typical of what CodeCompressor would encounter.
const fixtureGoCode = `package main

import (
	"fmt"
	"strings"
	"os"
)

// Config holds application configuration.
type Config struct {
	Name    string
	Version string
	Timeout int
}

// Server represents an HTTP server.
type Server struct {
	Config Config
	Addr   string
}

func main() {
	cfg := Config{Name: "app", Version: "1.0", Timeout: 30}
	srv := Server{Config: cfg, Addr: ":8080"}
	fmt.Println(srv.Config.Name)
}

func processItem(item string) string {
	result := strings.ToUpper(item)
	result = strings.TrimSpace(result)
	return result
}

func validateConfig(cfg Config) bool {
	if cfg.Name == "" {
		return false
	}
	if cfg.Version == "" {
		return false
	}
	if cfg.Timeout <= 0 {
		return false
	}
	return true
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func saveConfig(cfg Config, path string) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
`

// fixtureLogExcerpt is a log file with 100+ lines, including errors and repetitive INFO lines.
var fixtureLogExcerpt = generateLogFixture()

// --- SmartCrusher fixture tests ---

func TestFixture_SmartCrusher_JSONBlob(t *testing.T) {
	sc := NewSmartCrusher(DefaultSmartCrusherConfig())

	compressed, result := sc.Crush(fixtureJSONBlob)

	// Property: output should be smaller than input for large repetitive JSON.
	if len(compressed) >= len(fixtureJSONBlob) {
		t.Errorf("SmartCrusher: compressed (%d) should be smaller than input (%d) for repetitive JSON",
			len(compressed), len(fixtureJSONBlob))
	}

	// Property: tokens saved should be positive.
	if result.TokensSaved <= 0 {
		t.Errorf("SmartCrusher: expected tokens saved > 0, got %d", result.TokensSaved)
	}

	// Property: compression ratio should be < 1.0.
	if result.CompressionRatio >= 1.0 {
		t.Errorf("SmartCrusher: expected compression ratio < 1.0, got %f", result.CompressionRatio)
	}

	// Property: the output should be valid JSON.
	var decoded interface{}
	if err := json.Unmarshal([]byte(compressed), &decoded); err != nil {
		t.Errorf("SmartCrusher: compressed output should be valid JSON, got error: %v", err)
	}

	// Property: key tokens from the original should be preserved.
	if !strings.Contains(compressed, `"id"`) {
		t.Error("SmartCrusher: key token 'id' should be preserved in compressed output")
	}
}

func TestFixture_SmartCrusher_NonJSONPassthrough(t *testing.T) {
	sc := NewSmartCrusher(DefaultSmartCrusherConfig())

	// Non-JSON input should passthrough without errors.
	input := "This is plain text, not JSON at all."
	compressed, result := sc.Crush(input)

	if compressed != input {
		t.Error("SmartCrusher: non-JSON input should passthrough unchanged")
	}
	if result.Strategy != StrategySmartCrusher {
		t.Errorf("expected strategy %s, got %s", StrategySmartCrusher, result.Strategy)
	}
}

// --- CodeCompressor fixture tests ---

func TestFixture_CodeCompressor_GoCode(t *testing.T) {
	cc := NewCodeCompressor(DefaultCodeCompressorConfig())

	compressed, result := cc.Crush(fixtureGoCode, "go")

	// Property: strategy should be code.
	if result.Strategy != StrategyCode {
		t.Errorf("CodeCompressor: expected strategy %s, got %s", StrategyCode, result.Strategy)
	}

	// Property: key structural tokens should be preserved (function names, types).
	if !strings.Contains(compressed, "main") {
		t.Error("CodeCompressor: key token 'main' should be preserved")
	}
	if !strings.Contains(compressed, "Config") {
		t.Error("CodeCompressor: key token 'Config' (type name) should be preserved")
	}
}

// --- LogCompressor fixture tests ---

func TestFixture_LogCompressor_LogExcerpt(t *testing.T) {
	lc := NewLogCompressor(DefaultLogCompressorConfig())

	compressed, result := lc.Crush(fixtureLogExcerpt)

	// Property: output should be smaller or equal (injection guard may revert
	// if the log is not long enough, but our fixture is 120+ lines).
	if result.OriginalTokens > 0 && result.TokensSaved <= 0 {
		// Log compression may passthrough if below threshold.
		// Just verify it doesn't inflate.
		if len(compressed) > len(fixtureLogExcerpt) {
			t.Errorf("LogCompressor: compressed (%d) should not exceed input (%d)",
				len(compressed), len(fixtureLogExcerpt))
		}
	}

	// Property: ERROR lines should be preserved when KeepErrorLevels is true.
	if lc.KeepErrorLevels {
		if !strings.Contains(compressed, "ERROR") {
			t.Error("LogCompressor: ERROR lines should be preserved when KeepErrorLevels is true")
		}
	}
}

func TestFixture_LogCompressor_ShortPassthrough(t *testing.T) {
	lc := NewLogCompressor(DefaultLogCompressorConfig())

	// Short logs should passthrough.
	short := "2024-01-01 INFO: starting app\n2024-01-01 INFO: app ready\n"
	compressed, result := lc.Crush(short)

	if compressed != short {
		t.Error("LogCompressor: short logs should passthrough unchanged")
	}
	if result.TokensSaved != 0 {
		t.Errorf("LogCompressor: short log passthrough should have 0 tokens saved, got %d", result.TokensSaved)
	}
}

// --- SearchCompressor fixture tests ---

func TestFixture_SearchCompressor_SearchResults(t *testing.T) {
	sc := NewSearchCompressor(DefaultSearchCompressorConfig())

	// Simulate grep output with file:line:match format.
	searchOutput := generateSearchFixture()

	compressed, result := sc.Crush(searchOutput, "test")

	// Property: strategy should be search.
	if result.Strategy != StrategySearch {
		t.Errorf("SearchCompressor: expected strategy %s, got %s", StrategySearch, result.Strategy)
	}

	// Property: compressed output should not be larger than input.
	if len(compressed) > len(searchOutput) {
		t.Errorf("SearchCompressor: compressed (%d) should not exceed input (%d)",
			len(compressed), len(searchOutput))
	}

	// Property: the search query term should be preserved in output if matches exist.
	if strings.Contains(searchOutput, "test") && !strings.Contains(compressed, "test") {
		t.Error("SearchCompressor: query term should be preserved in compressed output")
	}
}

// --- ContentRouter fixture tests ---

func TestFixture_ContentRouter_Detection(t *testing.T) {
	router := NewContentRouter(DefaultContentRouterConfig())

	tests := []struct {
		name     string
		content  string
		expected ContentType
	}{
		{"json blob", fixtureJSONBlob, ContentJSON},
		{"log excerpt", fixtureLogExcerpt, ContentLogs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.DetectType(tt.content)
			if got != tt.expected {
				t.Errorf("DetectType(%s) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

// --- Pipeline fixture test ---

func TestFixture_Pipeline_MixedMessages(t *testing.T) {
	store := newMemStore()
	pipeline := NewPipeline(store)
	defer pipeline.Close()

	messages := []Message{
		{Role: "user", Content: "What files are in the project?"},
		{Role: "assistant", Content: "Here are the files:"},
		{Role: "tool", Content: fixtureJSONBlob},
		{Role: "tool", Content: fixtureLogExcerpt},
	}

	cfg := CompressConfig{
		MinTokensToCompress: 100,
	}

	result, err := pipeline.Compress(context.Background(), messages, cfg)
	if err != nil {
		t.Fatalf("Pipeline.Compress failed: %v", err)
	}

	// Property: should process all messages.
	if len(result.Messages) != len(messages) {
		t.Errorf("Pipeline: expected %d messages, got %d", len(messages), len(result.Messages))
	}

	// Property: tokens before should be positive.
	if result.TokensBefore == 0 {
		t.Error("Pipeline: expected TokensBefore > 0")
	}
}

// --- Helpers ---

func jsonRepetitiveItems(n int) string {
	items := make([]string, n)
	for i := 0; i < n; i++ {
		items[i] = `{"id":` + itoa(i) + `,"name":"item","status":"active","path":"/some/long/path/` + itoa(i) + `"}`
	}
	return strings.Join(items, ",")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func generateLogFixture() string {
	var lines []string
	for i := 1; i <= 80; i++ {
		lines = append(lines, "2024-06-01 10:"+pad2(i%60)+" INFO: processing request "+itoa(i))
	}
	// Add some errors in the middle
	for i := 0; i < 5; i++ {
		lines = append(lines, "2024-06-01 10:3"+itoa(i)+" ERROR: connection timeout for host-"+itoa(i))
	}
	// More repetitive INFO lines
	for i := 81; i <= 120; i++ {
		lines = append(lines, "2024-06-01 11:"+pad2(i%60)+" INFO: processing request "+itoa(i))
	}
	return strings.Join(lines, "\n")
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func generateSearchFixture() string {
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, "file_"+itoa(i)+".go:"+itoa(10*i)+": test func_"+itoa(i)+"() {}")
	}
	return strings.Join(lines, "\n")
}
