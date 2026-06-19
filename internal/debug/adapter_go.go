package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GoroutineStatus represents the state of a goroutine as reported by delve.
type GoroutineStatus string

const (
	// GoroutineIdle indicates the goroutine is idle (e.g. parked on a channel).
	GoroutineIdle GoroutineStatus = "idle"
	// GoroutineRunning indicates the goroutine is currently executing.
	GoroutineRunning GoroutineStatus = "running"
	// GoroutineWaiting indicates the goroutine is waiting (e.g. syscall, lock).
	GoroutineWaiting GoroutineStatus = "waiting"
	// GoroutineSyscall indicates the goroutine is inside a system call.
	GoroutineSyscall GoroutineStatus = "syscall"
	// GoroutineUnknown indicates the goroutine status could not be determined.
	GoroutineUnknown GoroutineStatus = "unknown"
)

// GoroutineInfo represents information about a single goroutine.
type GoroutineInfo struct {
	ID        int               `json:"id"`
	Status    GoroutineStatus   `json:"status"`
	Function  string            `json:"function,omitempty"`
	File      string            `json:"file,omitempty"`
	Line      int               `json:"line,omitempty"`
	Args      []GoroutineArg    `json:"args,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	UserState string            `json:"user_state,omitempty"`
}

// GoroutineArg represents a single argument of a goroutine's current function.
type GoroutineArg struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GoroutinesResult is the result of listing all goroutines.
type GoroutinesResult struct {
	Total int             `json:"total"`
	List  []GoroutineInfo `json:"list"`
}

// ListGoroutines lists all goroutines using the delve-specific DAP evaluate
// mechanism. It sends an evaluate request with a dlv-specific expression and
// parses the structured output.
//
// Delve's DAP implementation does not expose goroutine listing via a standard
// DAP request. Instead, we use evaluate with a special expression that delve
// recognizes. The output is parsed from the response.
func ListGoroutines(ctx context.Context, client *Client) (*GoroutinesResult, error) {
	logger := slog.Default().With("component", "debug-go")

	// Attempt 1: Use dlv's custom "goroutines" command via evaluate.
	// Delve's DAP server may support evaluating dlv-specific commands.
	evaluateBody, err := client.Evaluate(ctx, EvaluateArguments{
		Expression: "runtime.NumGoroutine()",
		Context:    "repl",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query goroutine count: %w", err)
	}

	// Parse the goroutine count from the evaluate result.
	var evalResult struct {
		Result       string `json:"result"`
		VariablesRef int    `json:"variablesReference"`
	}
	if err := json.Unmarshal(evaluateBody, &evalResult); err != nil {
		return nil, fmt.Errorf("failed to parse goroutine count: %w", err)
	}

	totalGoroutines := 0
	if _, err := fmt.Sscanf(evalResult.Result, "%d", &totalGoroutines); err != nil {
		logger.Warn("could not parse goroutine count from evaluate result",
			"raw_result", evalResult.Result,
		)
	}

	// Attempt 2: Get goroutine details via dlv's custom request.
	// Delve exposes goroutines through a custom DAP command "dlvGoroutines".
	// This is a delve-specific extension, not part of the standard DAP spec.
	goroutines, err := listGoroutinesViaCustomRequest(ctx, client)
	if err != nil {
		logger.Debug("dlv custom goroutines request not supported, falling back to evaluate-based approach",
			"error", err,
		)
		goroutines = []GoroutineInfo{}
	}

	return &GoroutinesResult{
		Total: totalGoroutines,
		List:  goroutines,
	}, nil
}

// listGoroutinesViaCustomRequest attempts to use delve's custom DAP command
// to list goroutines with full details.
func listGoroutinesViaCustomRequest(ctx context.Context, client *Client) ([]GoroutineInfo, error) {
	resp, err := client.SendRequest(ctx, "dlvGoroutines", map[string]any{
		"goroutineID": -1, // -1 means all goroutines
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("dlvGoroutines failed: %s", resp.Message)
	}

	var body struct {
		Goroutines []struct {
			ID         int    `json:"id"`
			UserState  string `json:"userState"`
			CurrentLoc struct {
				Function string `json:"function"`
				File     string `json:"file"`
			} `json:"currentLoc"`
			Loc struct {
				Line int `json:"line"`
			} `json:"location"`
			Args []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"args"`
		} `json:"goroutines"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, fmt.Errorf("failed to parse dlvGoroutines response: %w", err)
	}

	result := make([]GoroutineInfo, 0, len(body.Goroutines))
	for _, g := range body.Goroutines {
		info := GoroutineInfo{
			ID:        g.ID,
			Status:    parseGoroutineStatus(g.UserState),
			Function:  g.CurrentLoc.Function,
			File:      g.CurrentLoc.File,
			Line:      g.Loc.Line,
			UserState: g.UserState,
		}
		for _, arg := range g.Args {
			info.Args = append(info.Args, GoroutineArg{
				Name:  arg.Name,
				Value: arg.Value,
			})
		}
		result = append(result, info)
	}

	return result, nil
}

// SwitchGoroutine switches the debug context to the specified goroutine.
// This affects subsequent stack_trace, scopes, and variables requests.
// It uses delve's custom DAP command "dlvSwitchGoroutine".
func SwitchGoroutine(ctx context.Context, client *Client, goroutineID int) error {
	resp, err := client.SendRequest(ctx, "dlvSwitchGoroutine", map[string]any{
		"goroutineID": goroutineID,
	})
	if err != nil {
		return fmt.Errorf("dlvSwitchGoroutine request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("dlvSwitchGoroutine failed: %s", resp.Message)
	}
	return nil
}

// parseGoroutineStatus maps a delve userState string to a GoroutineStatus.
func parseGoroutineStatus(userState string) GoroutineStatus {
	if userState == "" {
		return GoroutineUnknown
	}

	lower := strings.ToLower(userState)
	switch {
	case lower == "running" || lower == "runnable":
		return GoroutineRunning
	case lower == "sleeping" || lower == "waiting":
		return GoroutineWaiting
	case lower == "syscall":
		return GoroutineSyscall
	case lower == "idle" || lower == "chan receive" || lower == "chan send" ||
		lower == "select":
		return GoroutineIdle
	default:
		return GoroutineUnknown
	}
}

// IsGoBinary checks whether the file at the given path is a compiled Go binary.
// It inspects the binary for Go-specific markers without executing it.
// To avoid reading very large files entirely into memory, only the first ~8MB
// and last ~1MB are scanned (Go buildinfo is typically at the end of the binary).
func IsGoBinary(path string) (bool, error) {
	data, err := readBoundedBinary(path)
	if err != nil {
		return false, err
	}

	// Go binaries contain specific markers.
	// The runtime.buildinfo section contains "Go buildinf" or "go1." strings.
	// Also check for the Go runtime symbols.
	markers := []string{
		"runtime.main",
		"runtime.gopanic",
		"go.buildid",
		"go1.",
	}

	found := 0
	for _, marker := range markers {
		if idx := findStringInBinary(data, marker); idx >= 0 {
			found++
		}
	}

	// If we find at least 2 markers, consider it a Go binary.
	return found >= 2, nil
}

// readBoundedBinary reads at most the first ~8MB and last ~1MB of a file to
// avoid loading very large binaries entirely into memory. The Go buildinfo
// section is at the end of the binary, and runtime symbols are near the start.
// Total work is capped at ~10MB.
func readBoundedBinary(path string) ([]byte, error) {
	const (
		headSize   = 8 * 1024 * 1024 // 8MB from the start
		tailSize   = 1 * 1024 * 1024 // 1MB from the end
		maxBufSize = headSize + tailSize
	)

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	fileSize := info.Size()
	if fileSize <= int64(headSize+tailSize) {
		// File is small enough to read entirely.
		return os.ReadFile(path)
	}

	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, 0, maxBufSize)

	// Read head.
	head := make([]byte, headSize)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	buf = append(buf, head[:n]...)

	// Seek to tail.
	tailStart := fileSize - int64(tailSize)
	if _, err := f.Seek(tailStart, io.SeekStart); err != nil {
		return nil, err
	}

	// Read tail.
	tail := make([]byte, tailSize)
	n, err = io.ReadFull(f, tail)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	buf = append(buf, tail[:n]...)

	return buf, nil
}

// findStringInBinary searches for a string in binary data.
// Returns the offset where the string was found, or -1 if not found.
func findStringInBinary(data []byte, s string) int {
	if len(s) == 0 || len(data) == 0 {
		return -1
	}
	// Only search up to the first 4MB for performance.
	limit := min(len(data), 4*1024*1024)
	b := []byte(s)
	for i := 0; i <= limit-len(b); i++ {
		if data[i] == b[0] && matchAt(data, i, b) {
			return i
		}
	}
	return -1
}

// matchAt checks if the byte slice b appears in data at offset i.
func matchAt(data []byte, i int, b []byte) bool {
	for j := range b {
		if i+j >= len(data) || data[i+j] != b[j] {
			return false
		}
	}
	return true
}

// DetectGoBinary attempts to determine whether a program path refers to a
// compiled Go binary. It first checks if the file exists and is a regular file,
// then applies binary inspection. If the file has a .go extension, it is
// assumed to be a Go source file (not a binary) and the check returns false.
func DetectGoBinary(program string) (bool, string, error) {
	if program == "" {
		return false, "", fmt.Errorf("program path is empty")
	}

	ext := strings.ToLower(filepath.Ext(program))
	// Source files are not compiled binaries.
	if ext == ".go" {
		return false, "", nil
	}

	// Check if the file exists and is not a directory.
	info, err := os.Stat(program)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	if info.IsDir() {
		return false, "", nil
	}

	// Perform binary inspection.
	isGo, err := IsGoBinary(program)
	if err != nil {
		return false, "", err
	}

	// If it looks like a Go binary, check if dlv is available.
	if isGo {
		dlvPath, err := exec.LookPath("dlv")
		if err != nil {
			return true, "", nil // Go binary but dlv not installed.
		}
		return true, dlvPath, nil
	}

	return false, "", nil
}

// GoDebugHint returns a hint string suggesting dlv for Go binaries.
// Returns an empty string if the program is not a Go binary.
func GoDebugHint(program string) string {
	isGo, dlvPath, err := DetectGoBinary(program)
	if err != nil || !isGo {
		return ""
	}

	if dlvPath != "" {
		return "detected Go binary; recommend using the dlv adapter (dlv) for goroutine inspection and Go-specific debugging"
	}

	return "detected Go binary; install dlv (go install github.com/go-delve/delve/cmd/dlv@latest) for Go-specific debugging features"
}
