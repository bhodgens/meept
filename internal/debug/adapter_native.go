package debug

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// CoreAdapterType identifies which native debugger is used for core dump analysis.
type CoreAdapterType string

const (
	// CoreAdapterGDB uses GNU gdb for core dump analysis (C/C++, Linux).
	CoreAdapterGDB CoreAdapterType = "gdb"
	// CoreAdapterLLDB uses Apple lldb for core dump analysis (C/C++, macOS).
	CoreAdapterLLDB CoreAdapterType = "lldb"
	// CoreAdapterDelve uses Delve for Go core dump analysis.
	CoreAdapterDelve CoreAdapterType = "delve"
)

// CoreDumpResult holds the parsed output of a core dump analysis.
type CoreDumpResult struct {
	Adapter     CoreAdapterType `json:"adapter"`
	Program     string          `json:"program"`
	CoreFile    string          `json:"core_file"`
	Signal      string          `json:"signal,omitempty"`       // e.g. "SIGSEGV", "SIGABRT"
	FaultAddr   string          `json:"fault_addr,omitempty"`   // Address that caused the fault
	CrashReason string          `json:"crash_reason,omitempty"` // Human-readable crash reason
	Threads     []CoreThread    `json:"threads,omitempty"`
	Variables   []CoreVariable  `json:"variables,omitempty"`
	RawOutput   string          `json:"raw_output,omitempty"`
}

// CoreThread represents a thread from a core dump.
type CoreThread struct {
	ID        int         `json:"id"`
	IsCrashed bool        `json:"is_crashed"`
	Stack     []CoreFrame `json:"stack,omitempty"`
	Reason    string      `json:"reason,omitempty"`
}

// CoreFrame represents a single stack frame.
type CoreFrame struct {
	Index    int    `json:"index"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Address  string `json:"address,omitempty"`
}

// CoreVariable represents a variable at the crash frame.
type CoreVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// DetectCoreAdapter picks the best native debugger for analyzing a core dump.
// It considers the platform and available binaries. If adapterName is non-empty,
// it validates that the requested adapter is available.
func DetectCoreAdapter(adapterName string) (CoreAdapterType, error) {
	if adapterName != "" {
		switch CoreAdapterType(adapterName) {
		case CoreAdapterGDB:
			if _, err := exec.LookPath("gdb"); err != nil {
				return "", fmt.Errorf("gdb not found in PATH")
			}
			return CoreAdapterGDB, nil
		case CoreAdapterLLDB:
			if _, err := exec.LookPath("lldb"); err != nil {
				return "", fmt.Errorf("lldb not found in PATH")
			}
			return CoreAdapterLLDB, nil
		case CoreAdapterDelve:
			if _, err := exec.LookPath("dlv"); err != nil {
				return "", fmt.Errorf("dlv not found in PATH")
			}
			return CoreAdapterDelve, nil
		default:
			return "", fmt.Errorf("unknown core adapter: %q (valid: gdb, lldb, delve)", adapterName)
		}
	}

	// Auto-detect based on platform.
	switch runtime.GOOS {
	case "darwin":
		// Prefer lldb on macOS, fall back to gdb.
		if _, err := exec.LookPath("lldb"); err == nil {
			return CoreAdapterLLDB, nil
		}
		if _, err := exec.LookPath("gdb"); err == nil {
			return CoreAdapterGDB, nil
		}
		return "", fmt.Errorf("no native debugger found; install lldb or gdb")
	case "linux":
		// Prefer gdb on Linux, fall back to lldb.
		if _, err := exec.LookPath("gdb"); err == nil {
			return CoreAdapterGDB, nil
		}
		if _, err := exec.LookPath("lldb"); err == nil {
			return CoreAdapterLLDB, nil
		}
		return "", fmt.Errorf("no native debugger found; install gdb or lldb")
	default:
		return "", fmt.Errorf("core dump analysis not supported on %s", runtime.GOOS)
	}
}

// DetectCoreAdapterForBinary picks a core dump adapter based on the program binary.
// If the binary is a Go binary, it prefers delve (if available); otherwise falls
// back to the platform default.
func DetectCoreAdapterForBinary(adapterName, program string) (CoreAdapterType, error) {
	if adapterName != "" {
		return DetectCoreAdapter(adapterName)
	}

	// Check if the program is a Go binary.
	isGo, dlvPath, err := DetectGoBinary(program)
	if err == nil && isGo && dlvPath != "" {
		return CoreAdapterDelve, nil
	}

	return DetectCoreAdapter("")
}

// AnalyzeCoreGDB runs gdb in batch mode to analyze a core dump.
// It executes bt, info registers, and info locals commands, then parses the output.
func AnalyzeCoreGDB(ctx context.Context, coreFile, program string, timeout time.Duration) (*CoreDumpResult, error) {
	_ = timeout // timeout is enforced by the caller via context; kept for API compatibility
	logger := slog.Default().With("component", "core-gdb")

	// Build gdb command arguments: -batch with multiple -ex commands.
	exArgs := []string{
		"-batch",
		"-core", coreFile,
		"-ex", "set pagination off",
		"-ex", "bt full",
		"-ex", "info registers",
		"-ex", "info locals",
		"-ex", "thread apply all bt",
		"-ex", "quit",
		program,
	}

	cmd := exec.CommandContext(ctx, "gdb", exArgs...)
	cmd.Stderr = cmd.Stdout // Capture all output to stdout.

	output, err := cmd.Output()
	if err != nil {
		// gdb may still produce useful output even on "error" (e.g., incomplete symbols).
		logger.Warn("gdb exited with error (output may still be useful)", "error", err)
		if len(output) == 0 {
			return nil, fmt.Errorf("gdb failed to analyze core dump: %w", err)
		}
	}

	raw := string(output)
	result := parseGDBOutput(raw, coreFile, program)
	return result, nil
}

// AnalyzeCoreLLDB runs lldb in batch mode to analyze a core dump.
// It executes bt, frame variable, and register read commands.
func AnalyzeCoreLLDB(ctx context.Context, coreFile, program string, timeout time.Duration) (*CoreDumpResult, error) {
	_ = timeout // timeout is enforced by the caller via context; kept for API compatibility
	logger := slog.Default().With("component", "core-lldb")

	cmd := exec.CommandContext(ctx, "lldb",
		"--core", coreFile,
		"--file", program,
		"-o", "script import lldb; lldb.debugger.HandleCommand('settings set auto-confirm true')",
		"-o", "bt",
		"-o", "frame variable",
		"-o", "register read",
		"-o", "thread list",
		"-o", "thread backtrace all",
		"-o", "quit",
		"--batch",
	)
	cmd.Stderr = cmd.Stdout

	output, err := cmd.Output()
	if err != nil {
		logger.Warn("lldb exited with error (output may still be useful)", "error", err)
		if len(output) == 0 {
			return nil, fmt.Errorf("lldb failed to analyze core dump: %w", err)
		}
	}

	raw := string(output)
	result := parseLLDBOutput(raw, coreFile, program)
	return result, nil
}

// AnalyzeCoreDelve runs delve in core mode to analyze a Go core dump.
func AnalyzeCoreDelve(ctx context.Context, coreFile, program string, timeout time.Duration) (*CoreDumpResult, error) {
	_ = timeout // timeout is enforced by the caller via context; kept for API compatibility
	logger := slog.Default().With("component", "core-delve")

	// delve core mode: dlv core <program> <core>
	// Use -c flag to run commands: goroutines, stack, then quit.
	// Note: delve's non-DAP CLI mode is interactive, so we pipe commands.
	script := fmt.Sprintf("goroutines\nstack\nlocals\nquit\n")

	cmd := exec.CommandContext(ctx, "dlv", "core", program, coreFile)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe for dlv: %w", err)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start dlv core: %w", err)
	}

	// Write commands and close stdin.
	go func() {
		defer stdin.Close()
		for _, line := range strings.Split(script, "\n") {
			if line != "" {
				fmt.Fprintln(stdin, line)
			}
		}
	}()

	// Wait with timeout.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		// Reap the process to avoid a zombie (S6-6). The cmd.Wait call
		// in the goroutine above has either returned already or will
		// return as a result of the Kill.
		<-done
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			logger.Warn("dlv exited with error (output may still be useful)", "error", err)
		}
	}

	raw := stdout.String()
	if raw == "" {
		raw = stderr.String()
	}

	if raw == "" {
		return nil, fmt.Errorf("dlv produced no output for core dump analysis")
	}

	result := parseDelveCoreOutput(raw, coreFile, program)
	return result, nil
}

// AnalyzeCoreDump is the main entry point for core dump analysis.
// It detects the appropriate adapter, runs the analysis, and returns a structured result.
func AnalyzeCoreDump(ctx context.Context, coreFile, program, adapterName string) (*CoreDumpResult, error) {
	logger := slog.Default().With("component", "core-analysis")

	if coreFile == "" {
		return nil, fmt.Errorf("core file path is required")
	}
	if program == "" {
		return nil, fmt.Errorf("program path is required")
	}

	// Detect the adapter.
	adapterType, err := DetectCoreAdapterForBinary(adapterName, program)
	if err != nil {
		return nil, fmt.Errorf("failed to detect core adapter: %w", err)
	}

	logger.Info("analyzing core dump",
		"core_file", coreFile,
		"program", program,
		"adapter", string(adapterType),
	)

	// Set a default timeout for the analysis.
	analysisCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var result *CoreDumpResult
	switch adapterType {
	case CoreAdapterGDB:
		result, err = AnalyzeCoreGDB(analysisCtx, coreFile, program, 60*time.Second)
	case CoreAdapterLLDB:
		result, err = AnalyzeCoreLLDB(analysisCtx, coreFile, program, 60*time.Second)
	case CoreAdapterDelve:
		result, err = AnalyzeCoreDelve(analysisCtx, coreFile, program, 60*time.Second)
	default:
		return nil, fmt.Errorf("unsupported core adapter: %s", adapterType)
	}

	if err != nil {
		return nil, fmt.Errorf("core dump analysis failed with %s: %w", adapterType, err)
	}

	return result, nil
}

// CrashReport generates a human-readable crash report from a CoreDumpResult.
func CrashReport(r *CoreDumpResult) string {
	var b strings.Builder

	b.WriteString("=== core dump crash report ===\n")
	b.WriteString(fmt.Sprintf("program:    %s\n", r.Program))
	b.WriteString(fmt.Sprintf("core file:  %s\n", r.CoreFile))
	b.WriteString(fmt.Sprintf("adapter:    %s\n", r.Adapter))

	if r.Signal != "" {
		b.WriteString(fmt.Sprintf("signal:     %s\n", r.Signal))
	}
	if r.FaultAddr != "" {
		b.WriteString(fmt.Sprintf("fault addr: %s\n", r.FaultAddr))
	}
	if r.CrashReason != "" {
		b.WriteString(fmt.Sprintf("reason:     %s\n", r.CrashReason))
	}

	if len(r.Threads) > 0 {
		b.WriteString(fmt.Sprintf("\nthreads (%d):\n", len(r.Threads)))
		for _, thread := range r.Threads {
			prefix := "  "
			if thread.IsCrashed {
				prefix = "* " // Mark the crashing thread.
			}
			b.WriteString(fmt.Sprintf("%sthread %d", prefix, thread.ID))
			if thread.Reason != "" {
				b.WriteString(fmt.Sprintf(" [%s]", thread.Reason))
			}
			b.WriteString("\n")

			for _, frame := range thread.Stack {
				b.WriteString(fmt.Sprintf("    #%d  %s", frame.Index, frame.Function))
				if frame.File != "" {
					b.WriteString(fmt.Sprintf("  at %s:%d", frame.File, frame.Line))
				}
				if frame.Address != "" {
					b.WriteString(fmt.Sprintf("  [%s]", frame.Address))
				}
				b.WriteString("\n")
			}
		}
	}

	if len(r.Variables) > 0 {
		b.WriteString("\nlocal variables at crash frame:\n")
		for _, v := range r.Variables {
			line := fmt.Sprintf("  %s", v.Name)
			if v.Type != "" {
				line += fmt.Sprintf(" (%s)", v.Type)
			}
			line += fmt.Sprintf(" = %s", v.Value)
			b.WriteString(line + "\n")
		}
	}

	if r.RawOutput != "" {
		b.WriteString("\nraw debugger output:\n")
		b.WriteString(r.RawOutput)
	}

	b.WriteString("=== end of crash report ===\n")
	return b.String()
}

// --- GDB output parsing ---

func parseGDBOutput(raw, coreFile, program string) *CoreDumpResult {
	result := &CoreDumpResult{
		Adapter:  CoreAdapterGDB,
		Program:  program,
		CoreFile: coreFile,
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	var (
		currentThread *CoreThread
		threads       []CoreThread
		crashThreadID int
	)

	for scanner.Scan() {
		line := scanner.Text()

		// Detect signal info: "Program terminated with signal SIGSEGV, ..."
		if strings.Contains(line, "Program terminated with signal") || strings.Contains(line, "received signal") {
			result.Signal = extractGDBSignal(line)
			result.CrashReason = extractGDBReason(line)
			continue
		}

		// Detect faulting address.
		if idx := strings.Index(line, "faulting address"); idx >= 0 {
			result.FaultAddr = extractAddress(strings.TrimSpace(line[idx+len("faulting address"):]))
			continue
		}

		// Thread header: "Thread 1 ..."
		if strings.HasPrefix(line, "Thread ") && (strings.Contains(line, "(LWP") || strings.Contains(line, "(Thread")) {
			// Start a new thread section.
			threadID := parseGDBThreadID(line)
			if currentThread != nil {
				threads = append(threads, *currentThread)
			}
			currentThread = &CoreThread{ID: threadID}
			continue
		}

		// "thread apply all bt" produces: "Thread 1 (LWP 12345):"
		// followed by #0 ... #1 ...
		if strings.HasPrefix(line, "Thread ") && strings.HasSuffix(line, ":") {
			threadID := parseGDBThreadID(line)
			if currentThread != nil && len(currentThread.Stack) > 0 {
				threads = append(threads, *currentThread)
			}
			currentThread = &CoreThread{ID: threadID}
			continue
		}

		// Check for stack frame: #N  0xADDR in FUNCTION at FILE:LINE
		if frame := parseGDBFrame(line); frame != nil {
			if currentThread == nil {
				currentThread = &CoreThread{ID: 1}
			}
			currentThread.Stack = append(currentThread.Stack, *frame)
			if frame.Index == 0 && crashThreadID == 0 {
				crashThreadID = currentThread.ID
			}
			continue
		}

		// Parse "info locals" output: variable = value
		if varInfo := parseGDBVariable(line); varInfo != nil {
			result.Variables = append(result.Variables, *varInfo)
			continue
		}
	}

	// Close the last thread.
	if currentThread != nil {
		threads = append(threads, *currentThread)
	}

	result.Threads = threads

	// Mark the crashing thread.
	for i := range result.Threads {
		if result.Threads[i].ID == crashThreadID {
			result.Threads[i].IsCrashed = true
			result.Threads[i].Reason = result.Signal
			break
		}
	}

	return result
}

func extractGDBSignal(line string) string {
	// "Program terminated with signal SIGSEGV, ..."
	upper := strings.ToUpper(line)
	for _, sig := range []string{"SIGSEGV", "SIGABRT", "SIGBUS", "SIGFPE", "SIGILL", "SIGTRAP", "SIGKILL", "SIGTERM", "SIGPIPE", "SIGSYS"} {
		if strings.Contains(upper, sig) {
			return sig
		}
	}
	// Generic extraction: find "terminated with signal ..." or "received signal ..." pattern.
	if idx := strings.Index(line, "terminated with signal "); idx >= 0 {
		rest := line[idx+len("terminated with signal "):]
		if comma := strings.Index(rest, ","); comma >= 0 {
			return strings.TrimSpace(rest[:comma])
		}
		return strings.TrimSpace(rest)
	}
	if idx := strings.Index(line, "received signal "); idx >= 0 {
		rest := line[idx+len("received signal "):]
		if comma := strings.Index(rest, ","); comma >= 0 {
			return strings.TrimSpace(rest[:comma])
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

func extractGDBReason(line string) string {
	// Extract text after the comma in "Program terminated with signal SIGSEGV, ..."
	if idx := strings.Index(line, ","); idx >= 0 {
		return strings.TrimSpace(line[idx+1:])
	}
	return line
}

func extractAddress(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") {
		// Take up to the next space.
		if space := strings.Index(s, " "); space >= 0 {
			return s[:space]
		}
		return s
	}
	if strings.HasPrefix(s, "0X") {
		if space := strings.Index(s, " "); space >= 0 {
			return s[:space]
		}
		return s
	}
	return s
}

func parseGDBThreadID(line string) int {
	// "Thread 1 (LWP 12345):" -> extract "1"
	// Or "(LWP 12345)" -> extract 12345
	// Try "Thread N" pattern first.
	after := strings.TrimPrefix(line, "Thread ")
	after = strings.TrimSpace(after)
	// Take the number before the first non-digit.
	var idStr string
	for _, ch := range after {
		if ch >= '0' && ch <= '9' {
			idStr += string(ch)
		} else {
			break
		}
	}
	if idStr != "" {
		id := 0
		for _, ch := range idStr {
			id = id*10 + int(ch-'0')
		}
		return id
	}
	return 0
}

func parseGDBFrame(line string) *CoreFrame {
	// GDB frame format: "#0  0x00007f8a1b2c3d4e in function_name (args) at file.c:42"
	if !strings.HasPrefix(line, "#") {
		return nil
	}

	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 {
		return nil
	}

	frame := &CoreFrame{}
	// Extract frame index.
	rest := trimmed[1:] // Skip '#'
	var idxStr string
	for _, ch := range rest {
		if ch >= '0' && ch <= '9' {
			idxStr += string(ch)
		} else {
			break
		}
	}
	if idxStr != "" {
		frame.Index = 0
		for _, ch := range idxStr {
			frame.Index = frame.Index*10 + int(ch-'0')
		}
	}

	// Extract address: 0xNNNN
	if addrIdx := strings.Index(rest, "0x"); addrIdx >= 0 {
		addrStart := rest[addrIdx:]
		if space := strings.Index(addrStart, " "); space >= 0 {
			frame.Address = addrStart[:space]
		} else {
			frame.Address = addrStart
		}
	}

	// Extract "in function_name" or directly the function name.
	if inIdx := strings.Index(rest, " in "); inIdx >= 0 {
		funcStart := rest[inIdx+4:]
		// Trim arguments if present.
		if paren := strings.Index(funcStart, "("); paren >= 0 {
			frame.Function = strings.TrimSpace(funcStart[:paren])
		} else if at := strings.Index(funcStart, " at "); at >= 0 {
			frame.Function = strings.TrimSpace(funcStart[:at])
		} else {
			frame.Function = strings.TrimSpace(funcStart)
		}
	}

	// Extract "at file:line".
	if atIdx := strings.Index(rest, " at "); atIdx >= 0 {
		location := strings.TrimSpace(rest[atIdx+4:])
		// file:line format.
		if colon := strings.LastIndex(location, ":"); colon >= 0 {
			frame.File = location[:colon]
			lineStr := location[colon+1:]
			lineNum := 0
			for _, ch := range lineStr {
				if ch >= '0' && ch <= '9' {
					lineNum = lineNum*10 + int(ch-'0')
				} else {
					break
				}
			}
			if lineNum > 0 {
				frame.Line = lineNum
			}
		}
	}

	return frame
}

func parseGDBVariable(line string) *CoreVariable {
	// GDB info locals output: "var_name = value"
	// Or: "type var_name = value"
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "No locals") {
		return nil
	}

	// Skip lines that are clearly not variable output.
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "Thread") ||
		strings.HasPrefix(trimmed, "No") {
		return nil
	}

	eqIdx := strings.Index(trimmed, "=")
	if eqIdx < 1 {
		return nil
	}

	name := strings.TrimSpace(trimmed[:eqIdx])
	value := strings.TrimSpace(trimmed[eqIdx+1:])

	// Filter out non-variable lines.
	if name == "" || strings.Contains(name, " ") && !strings.HasPrefix(name, "const ") {
		// Could be a type declaration: "int x = 42"
		parts := strings.SplitN(name, " ", 2)
		if len(parts) == 2 {
			return &CoreVariable{
				Name:  parts[1],
				Value: value,
				Type:  parts[0],
			}
		}
		// Not a clean variable line.
		return nil
	}

	return &CoreVariable{
		Name:  name,
		Value: value,
	}
}

// --- LLDB output parsing ---

func parseLLDBOutput(raw, coreFile, program string) *CoreDumpResult {
	result := &CoreDumpResult{
		Adapter:  CoreAdapterLLDB,
		Program:  program,
		CoreFile: coreFile,
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	var (
		currentThread *CoreThread
		threads       []CoreThread
		crashThreadID int
	)

	for scanner.Scan() {
		line := scanner.Text()

		// LLDB signal: "stop reason = EXC_BAD_ACCESS" or "Signal: SIGSEGV"
		if strings.Contains(line, "stop reason") {
			result.CrashReason = extractLLDBStopReason(line)
			// Try to extract signal name.
			upper := strings.ToUpper(line)
			for _, sig := range []string{"SIGSEGV", "SIGABRT", "SIGBUS", "SIGFPE", "SIGILL"} {
				if strings.Contains(upper, sig) {
					result.Signal = sig
					break
				}
			}
			// Extract fault address.
			if addrIdx := strings.Index(line, "address="); addrIdx >= 0 {
				result.FaultAddr = extractAddress(line[addrIdx+8:])
			}
			continue
		}

		// Thread list: "thread #1: ..."
		if strings.HasPrefix(line, "thread #") {
			threadID := parseLLDBThreadID(line)
			if currentThread != nil {
				threads = append(threads, *currentThread)
			}
			currentThread = &CoreThread{ID: threadID}
			// Check if this is the crashed thread.
			if strings.Contains(line, "stop reason") || strings.Contains(line, "crashed") {
				crashThreadID = threadID
				currentThread.IsCrashed = true
				currentThread.Reason = extractLLDBStopReason(line)
			}
			continue
		}

		// Stack frame: "  frame #0: ..."
		if strings.HasPrefix(line, "  frame #") || strings.HasPrefix(line, "frame #") {
			frame := parseLLDBFrame(line)
			if frame != nil {
				if currentThread == nil {
					currentThread = &CoreThread{ID: 1}
				}
				currentThread.Stack = append(currentThread.Stack, *frame)
				if frame.Index == 0 && crashThreadID == 0 {
					crashThreadID = currentThread.ID
				}
			}
			continue
		}

		// Variable from "frame variable": "name = value" or "(type) name = value"
		if varInfo := parseLLDBVariable(line); varInfo != nil {
			result.Variables = append(result.Variables, *varInfo)
			continue
		}
	}

	if currentThread != nil {
		threads = append(threads, *currentThread)
	}

	result.Threads = threads

	return result
}

func extractLLDBStopReason(line string) string {
	if idx := strings.Index(line, "stop reason = "); idx >= 0 {
		rest := line[idx+len("stop reason = "):]
		// Trim trailing context (usually parenthetical details like "(code=1, ...)").
		// But also handle bare reason strings.
		// If the reason starts with a parenthesized group, skip it.
		if open := strings.Index(rest, "("); open > 0 {
			// The actual reason is before the parenthesized details.
			return strings.TrimSpace(rest[:open])
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

func parseLLDBThreadID(line string) int {
	rest := strings.TrimPrefix(line, "thread #")
	var idStr string
	for _, ch := range rest {
		if ch >= '0' && ch <= '9' {
			idStr += string(ch)
		} else {
			break
		}
	}
	id := 0
	for _, ch := range idStr {
		id = id*10 + int(ch-'0')
	}
	return id
}

func parseLLDBFrame(line string) *CoreFrame {
	// "frame #0: 0x00007f...`function_name at file.c:42:3"
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "frame #")
	if !strings.HasPrefix(trimmed, "#") {
		trimmed = "frame #" + trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "frame #")

	frame := &CoreFrame{}
	var idxStr string
	for _, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			idxStr += string(ch)
		} else {
			break
		}
	}
	if idxStr != "" {
		frame.Index = 0
		for _, ch := range idxStr {
			frame.Index = frame.Index*10 + int(ch-'0')
		}
	}

	// Extract address.
	if addrIdx := strings.Index(trimmed, "0x"); addrIdx >= 0 {
		addrStart := trimmed[addrIdx:]
		if tick := strings.Index(addrStart, "`"); tick >= 0 {
			frame.Address = addrStart[:tick]
		} else if space := strings.Index(addrStart, " "); space >= 0 {
			frame.Address = addrStart[:space]
		}
	}

	// Extract function: after backtick "`".
	if tick := strings.Index(trimmed, "`"); tick >= 0 {
		funcStart := trimmed[tick+1:]
		// "function_name at file.c:42:col" (lldb uses file:line:col format).
		if at := strings.Index(funcStart, " at "); at >= 0 {
			frame.Function = funcStart[:at]
			location := funcStart[at+4:]
			// Split on colon. lldb produces "file:line:col".
			// Use the first colon to separate file from line:col.
			parts := strings.SplitN(location, ":", 3)
			if len(parts) >= 2 {
				frame.File = parts[0]
				lineNum := 0
				for _, ch := range parts[1] {
					if ch >= '0' && ch <= '9' {
						lineNum = lineNum*10 + int(ch-'0')
					} else {
						break
					}
				}
				if lineNum > 0 {
					frame.Line = lineNum
				}
			}
		} else {
			frame.Function = funcStart
		}
	}

	return frame
}

func parseLLDBVariable(line string) *CoreVariable {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	// "(type) name = value"
	if strings.HasPrefix(trimmed, "(") {
		closeParen := strings.Index(trimmed, ")")
		if closeParen < 0 {
			return nil
		}
		typeName := trimmed[1:closeParen]
		rest := strings.TrimSpace(trimmed[closeParen+1:])
		eqIdx := strings.Index(rest, "=")
		if eqIdx < 1 {
			return nil
		}
		return &CoreVariable{
			Type:  typeName,
			Name:  strings.TrimSpace(rest[:eqIdx]),
			Value: strings.TrimSpace(rest[eqIdx+1:]),
		}
	}

	// "name = value"
	eqIdx := strings.Index(trimmed, "=")
	if eqIdx < 1 {
		return nil
	}
	name := strings.TrimSpace(trimmed[:eqIdx])
	value := strings.TrimSpace(trimmed[eqIdx+1:])
	if name == "" {
		return nil
	}

	return &CoreVariable{
		Name:  name,
		Value: value,
	}
}

// --- Delve core output parsing ---

func parseDelveCoreOutput(raw, coreFile, program string) *CoreDumpResult {
	result := &CoreDumpResult{
		Adapter:  CoreAdapterDelve,
		Program:  program,
		CoreFile: coreFile,
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	var (
		currentThread *CoreThread
		threads       []CoreThread
	)

	frameIdx := 0
	for scanner.Scan() {
		line := scanner.Text()

		// Goroutine header: "Goroutine N - User: ..."
		if strings.HasPrefix(line, "Goroutine ") {
			if currentThread != nil && len(currentThread.Stack) > 0 {
				threads = append(threads, *currentThread)
			}
			gID := 0
			rest := strings.TrimPrefix(line, "Goroutine ")
			for _, ch := range rest {
				if ch >= '0' && ch <= '9' {
					gID = gID*10 + int(ch-'0')
				} else {
					break
				}
			}
			currentThread = &CoreThread{ID: gID}
			frameIdx = 0
			continue
		}

		// Stack frame: "    N  runtime.main ..."
		if strings.HasPrefix(line, "    ") {
			trimmed := strings.TrimSpace(line)
			// Check if it starts with a number (frame index).
			var numStr string
			for _, ch := range trimmed {
				if ch >= '0' && ch <= '9' {
					numStr += string(ch)
				} else {
					break
				}
			}
			if numStr != "" && currentThread != nil {
				funcName := strings.TrimSpace(trimmed[len(numStr):])
				frame := CoreFrame{
					Index:    frameIdx,
					Function: funcName,
				}
				// Parse "at file:line" suffix.
				if at := strings.Index(funcName, " at "); at >= 0 {
					frame.Function = funcName[:at]
					location := funcName[at+4:]
					if colon := strings.LastIndex(location, ":"); colon >= 0 {
						frame.File = location[:colon]
						lineStr := location[colon+1:]
						lineNum := 0
						for _, ch := range lineStr {
							if ch >= '0' && ch <= '9' {
								lineNum = lineNum*10 + int(ch-'0')
							} else {
								break
							}
						}
						if lineNum > 0 {
							frame.Line = lineNum
						}
					}
				}
				currentThread.Stack = append(currentThread.Stack, frame)
				frameIdx++
			}
			continue
		}

		// Panic or fatal signal indicators.
		if strings.Contains(line, "panic") || strings.Contains(line, "fatal error") {
			result.Signal = "panic"
			result.CrashReason = strings.TrimSpace(line)
		}
	}

	if currentThread != nil {
		threads = append(threads, *currentThread)
	}

	result.Threads = threads
	return result
}
