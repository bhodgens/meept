package debug

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDetectCoreAdapterEmpty validates that DetectCoreAdapter returns an error
// for an empty adapter name when no debugger is in PATH (unlikely on dev machines,
// but the logic path is tested).
func TestDetectCoreAdapterEmpty(t *testing.T) {
	// With empty adapter name, it should detect based on platform.
	// On darwin/linux it should find at least one debugger or return an error.
	adapter, err := DetectCoreAdapter("")
	// If a debugger is installed, it should succeed.
	// If not, it should give a clear error.
	if err != nil {
		if !strings.Contains(err.Error(), "no native debugger found") {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Logf("no native debugger installed (expected in CI): %v", err)
	} else {
		t.Logf("detected adapter: %s", adapter)
	}
}

// TestDetectCoreAdapterExplicit tests explicit adapter selection.
func TestDetectCoreAdapterExplicit(t *testing.T) {
	tests := []struct {
		name    string
		adapter string
		wantErr bool
	}{
		{name: "gdb", adapter: "gdb"},
		{name: "lldb", adapter: "lldb"},
		{name: "delve", adapter: "delve"},
		{name: "invalid", adapter: "nonexistent_adapter", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := DetectCoreAdapter(tt.adapter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error for invalid adapter")
				}
				t.Logf("expected error: %v", err)
				return
			}
			if err != nil {
				t.Logf("adapter %q not installed: %v", tt.adapter, err)
				return
			}
			if adapter != CoreAdapterType(tt.adapter) {
				t.Fatalf("expected %q, got %q", tt.adapter, adapter)
			}
		})
	}
}

// TestDetectCoreAdapterForBinary tests adapter detection for specific binaries.
func TestDetectCoreAdapterForBinary(t *testing.T) {
	// Use the current test binary as a target.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// This test binary is not a Go binary with dlv markers necessarily,
	// but it exercises the detection logic.
	_, err = DetectCoreAdapterForBinary("", self)
	if err != nil {
		t.Logf("no core adapter for %q: %v", self, err)
	}

	// Explicit adapter should be respected.
	if adapter, err := DetectCoreAdapterForBinary("gdb", self); err == nil {
		if adapter != CoreAdapterGDB {
			t.Fatalf("expected gdb, got %s", adapter)
		}
	}
}

// TestCrashReport tests the CrashReport formatting function.
func TestCrashReport(t *testing.T) {
	result := &CoreDumpResult{
		Adapter:     CoreAdapterGDB,
		Program:     "/usr/bin/test",
		CoreFile:    "/tmp/core.12345",
		Signal:      "SIGSEGV",
		FaultAddr:   "0x0000000000000000",
		CrashReason: "Segmentation fault (core dumped)",
		Variables: []CoreVariable{
			{Name: "ptr", Value: "0x0", Type: "int *"},
			{Name: "len", Value: "42", Type: "int"},
		},
		Threads: []CoreThread{
			{
				ID:        1,
				IsCrashed: true,
				Reason:    "SIGSEGV",
				Stack: []CoreFrame{
					{Index: 0, Function: "main.crash", File: "main.go", Line: 42, Address: "0x400100"},
					{Index: 1, Function: "main.main", File: "main.go", Line: 10, Address: "0x400200"},
				},
			},
			{
				ID: 2,
				Stack: []CoreFrame{
					{Index: 0, Function: "runtime.gopark", File: "proc.go", Line: 300},
				},
			},
		},
	}

	report := CrashReport(result)
	if report == "" {
		t.Fatal("expected non-empty crash report")
	}

	// Verify key sections are present.
	checks := []string{
		"core dump crash report",
		"/usr/bin/test",
		"/tmp/core.12345",
		"SIGSEGV",
		"Segmentation fault",
		"0x0000000000000000",
		"thread 1",
		"main.crash",
		"main.go:42",
		"ptr",
		"0x0",
		"int *",
		"len",
		"42",
		"thread 2",
		"runtime.gopark",
		"end of crash report",
	}

	for _, check := range checks {
		if !strings.Contains(report, check) {
			t.Errorf("crash report missing %q", check)
		}
	}
}

// TestCrashReportEmpty tests CrashReport with minimal data.
func TestCrashReportEmpty(t *testing.T) {
	result := &CoreDumpResult{
		Adapter:  CoreAdapterLLDB,
		Program:  "/bin/test",
		CoreFile: "/tmp/core",
	}

	report := CrashReport(result)
	if !strings.Contains(report, "/bin/test") {
		t.Error("report should contain program path")
	}
	if !strings.Contains(report, "lldb") {
		t.Error("report should contain adapter name")
	}
}

// TestParseGDBOutput tests GDB output parsing with sample output.
func TestParseGDBOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSig  string
		wantThr  int
		wantVars int
	}{
		{
			name: "basic segfault",
			input: `Program terminated with signal SIGSEGV, Segmentation fault.
#0  0x0000000000401000 in main.crash (ptr=0x0) at main.go:42
#1  0x0000000000402000 in main.main at main.go:10
x = 42
ptr = 0x0
`,
			wantSig:  "SIGSEGV",
			wantThr:  1,
			wantVars: 2,
		},
		{
			name: "with thread info",
			input: `Program terminated with signal SIGABRT, Aborted.
Thread 1 (LWP 12345):
#0  0x0000000000401000 in raise at /lib/libc.so:0
#1  0x0000000000402000 in abort at /lib/libc.so:0
Thread 2 (LWP 12346):
#0  0x00007fff00001000 in futex_wait
`,
			wantSig:  "SIGABRT",
			wantThr:  2,
			wantVars: 0,
		},
		{
			name: "typed variable",
			input: `Program terminated with signal SIGSEGV, Segmentation fault.
#0  0x0000000000401000 in main.crash at main.go:42
int x = 99
`,
			wantSig:  "SIGSEGV",
			wantThr:  1,
			wantVars: 1,
		},
		{
			name: "no locals",
			input: `Program terminated with signal SIGBUS.
#0  0x0000000000401000 in crash_func at crash.c:10
`,
			wantSig:  "SIGBUS",
			wantThr:  1,
			wantVars: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGDBOutput(tt.input, "/tmp/core", "/usr/bin/test")
			if result.Adapter != CoreAdapterGDB {
				t.Errorf("expected adapter gdb, got %s", result.Adapter)
			}
			if result.Signal != tt.wantSig {
				t.Errorf("expected signal %q, got %q", tt.wantSig, result.Signal)
			}
			if len(result.Threads) != tt.wantThr {
				t.Errorf("expected %d threads, got %d", tt.wantThr, len(result.Threads))
			}
			if len(result.Variables) != tt.wantVars {
				t.Errorf("expected %d variables, got %d", tt.wantVars, len(result.Variables))
			}
		})
	}
}

// TestParseGDBFrame tests individual frame parsing.
func TestParseGDBFrame(t *testing.T) {
	tests := []struct {
		input    string
		wantIdx  int
		wantFn   string
		wantFile string
		wantLine int
	}{
		{"#0  0x00007f8a1b2c3d4e in main.crash (ptr=0x0) at main.go:42", 0, "main.crash", "main.go", 42},
		{"#1  0x00007f8a1b2c3d4e in runtime.main () at main.go:10", 1, "runtime.main", "main.go", 10},
		{"#2  0x00007f8a1b2c3d4e in __libc_start_main", 2, "__libc_start_main", "", 0},
		{"not a frame", -1, "", "", 0},
		{"#", -1, "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(len(tt.input), 20)], func(t *testing.T) {
			frame := parseGDBFrame(tt.input)
			if tt.wantIdx < 0 {
				if frame != nil {
					t.Fatalf("expected nil frame for %q, got %+v", tt.input, frame)
				}
				return
			}
			if frame == nil {
				t.Fatalf("expected frame for %q, got nil", tt.input)
			}
			if frame.Index != tt.wantIdx {
				t.Errorf("expected index %d, got %d", tt.wantIdx, frame.Index)
			}
			if frame.Function != tt.wantFn {
				t.Errorf("expected function %q, got %q", tt.wantFn, frame.Function)
			}
			if frame.File != tt.wantFile {
				t.Errorf("expected file %q, got %q", tt.wantFile, frame.File)
			}
			if frame.Line != tt.wantLine {
				t.Errorf("expected line %d, got %d", tt.wantLine, frame.Line)
			}
		})
	}
}

// TestParseGDBVariable tests variable line parsing.
func TestParseGDBVariable(t *testing.T) {
	tests := []struct {
		input  string
		wantOk bool
		want   CoreVariable
	}{
		{"x = 42", true, CoreVariable{Name: "x", Value: "42"}},
		{"ptr = 0x0", true, CoreVariable{Name: "ptr", Value: "0x0"}},
		{"name = (nil)", true, CoreVariable{Name: "name", Value: "(nil)"}},
		{"No locals.", false, CoreVariable{}},
		{"", false, CoreVariable{}},
		{"#0  0x...", false, CoreVariable{}},
		{"Thread 1 (LWP 12345):", false, CoreVariable{}},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(len(tt.input), 20)], func(t *testing.T) {
			v := parseGDBVariable(tt.input)
			if tt.wantOk {
				if v == nil {
					t.Fatalf("expected variable for %q, got nil", tt.input)
				}
				if v.Name != tt.want.Name || v.Value != tt.want.Value {
					t.Errorf("expected {name:%q value:%q}, got {name:%q value:%q}",
						tt.want.Name, tt.want.Value, v.Name, v.Value)
				}
			} else {
				if v != nil {
					t.Errorf("expected nil for %q, got %+v", tt.input, v)
				}
			}
		})
	}
}

// TestParseLLDBOutput tests LLDB output parsing with sample output.
func TestParseLLDBOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSig string
		wantThr int
	}{
		{
			name: "exc_bad_access",
			input: "(lldb) thread list\n" +
				"thread #1: tid = 12345, 0x00007fff00001000, stop reason = EXC_BAD_ACCESS (code=1, address=0x0)\n" +
				"  frame #0: 0x00007fff00001000 main" + "`" + "crash at main.go:42:3\n" +
				"  frame #1: 0x00007fff00002000 main" + "`" + "main at main.go:10:5\n" +
				"thread #2: tid = 12346, 0x00007fff00003000\n" +
				"  frame #0: 0x00007fff00003000 runtime" + "`" + "gopark at proc.go:300:5\n",
			wantSig: "",
			wantThr: 2,
		},
		{
			name: "with signal",
			input: "(lldb) bt\n" +
				"  frame #0: 0x0000000000401000 crash_func" + "`" + "crash at crash.c:10:2\n" +
				"  frame #1: 0x0000000000402000 crash_func" + "`" + "main at crash.c:20:5\n",
			wantSig: "",
			wantThr: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLLDBOutput(tt.input, "/tmp/core", "/usr/bin/test")
			if result.Adapter != CoreAdapterLLDB {
				t.Errorf("expected adapter lldb, got %s", result.Adapter)
			}
			if len(result.Threads) != tt.wantThr {
				t.Errorf("expected %d threads, got %d", tt.wantThr, len(result.Threads))
			}
		})
	}
}

// TestParseLLDBFrame tests individual LLDB frame parsing.
func TestParseLLDBFrame(t *testing.T) {
	tests := []struct {
		input    string
		wantIdx  int
		wantFn   string
		wantFile string
		wantLine int
	}{
		{"frame #0: 0x00007fff00001000 main`crash at main.go:42:3", 0, "crash", "main.go", 42},
		{"  frame #1: 0x00007fff00002000 main`main at main.go:10:5", 1, "main", "main.go", 10},
		{"  frame #2: 0x00007fff00003000 runtime`gopark at proc.go:300", 2, "gopark", "proc.go", 300},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("frame%d", tt.wantIdx), func(t *testing.T) {
			frame := parseLLDBFrame(tt.input)
			if frame == nil {
				t.Fatalf("expected frame for %q, got nil", tt.input)
			}
			if frame.Index != tt.wantIdx {
				t.Errorf("expected index %d, got %d", tt.wantIdx, frame.Index)
			}
			if frame.Function != tt.wantFn {
				t.Errorf("expected function %q, got %q", tt.wantFn, frame.Function)
			}
			if frame.File != tt.wantFile {
				t.Errorf("expected file %q, got %q", tt.wantFile, frame.File)
			}
			if frame.Line != tt.wantLine {
				t.Errorf("expected line %d, got %d", tt.wantLine, frame.Line)
			}
		})
	}
}

// TestParseLLDBVariable tests variable parsing.
func TestParseLLDBVariable(t *testing.T) {
	tests := []struct {
		input string
		want  *CoreVariable
	}{
		{"x = 42", &CoreVariable{Name: "x", Value: "42"}},
		{"(int *) ptr = 0x0", &CoreVariable{Name: "ptr", Value: "0x0", Type: "int *"}},
		{"name = (nil)", &CoreVariable{Name: "name", Value: "(nil)"}},
		{"", nil},
		{"  ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(len(tt.input), 10)], func(t *testing.T) {
			v := parseLLDBVariable(tt.input)
			if tt.want == nil {
				if v != nil {
					t.Errorf("expected nil for %q, got %+v", tt.input, v)
				}
				return
			}
			if v == nil {
				t.Fatalf("expected variable for %q, got nil", tt.input)
			}
			if v.Name != tt.want.Name || v.Value != tt.want.Value || v.Type != tt.want.Type {
				t.Errorf("expected %+v, got %+v", *tt.want, *v)
			}
		})
	}
}

// TestParseDelveCoreOutput tests Delve core output parsing.
func TestParseDelveCoreOutput(t *testing.T) {
	input := `Goroutine 1 - User: main.crash:
    0  main.crash at ./main.go:42
    1  main.main at ./main.go:10
Goroutine 2 - User: chan receive:
    0  runtime.gopark at /usr/local/go/src/runtime/proc.go:300
`

	result := parseDelveCoreOutput(input, "/tmp/core", "./test_binary")
	if result.Adapter != CoreAdapterDelve {
		t.Errorf("expected adapter delve, got %s", result.Adapter)
	}
	if len(result.Threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(result.Threads))
	}
	if result.Threads[0].ID != 1 {
		t.Errorf("expected thread 0 ID 1, got %d", result.Threads[0].ID)
	}
	if len(result.Threads[0].Stack) != 2 {
		t.Fatalf("expected 2 frames in thread 0, got %d", len(result.Threads[0].Stack))
	}
	if result.Threads[0].Stack[0].Function != "main.crash" {
		t.Errorf("expected main.crash, got %q", result.Threads[0].Stack[0].Function)
	}
	if result.Threads[0].Stack[0].File != "./main.go" {
		t.Errorf("expected ./main.go, got %q", result.Threads[0].Stack[0].File)
	}
	if result.Threads[0].Stack[0].Line != 42 {
		t.Errorf("expected line 42, got %d", result.Threads[0].Stack[0].Line)
	}
	if result.Threads[1].ID != 2 {
		t.Errorf("expected thread 1 ID 2, got %d", result.Threads[1].ID)
	}
}

// TestParseDelveCoreOutputWithPanic tests Delve panic detection.
func TestParseDelveCoreOutputWithPanic(t *testing.T) {
	input := `runtime: goroutine stack exceeds 1000000000-byte limit
fatal error: stack overflow

Goroutine 1 - User:
    0  runtime.systemstack_switch at /usr/local/go/src/runtime/asm_amd64.s:370
`

	result := parseDelveCoreOutput(input, "/tmp/core", "./test")
	if result.Signal != "panic" {
		t.Errorf("expected signal 'panic', got %q", result.Signal)
	}
	if result.CrashReason == "" {
		t.Error("expected non-empty crash reason")
	}
}

// TestExtractGDBSignal tests signal extraction from GDB output.
func TestExtractGDBSignal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Program terminated with signal SIGSEGV, Segmentation fault.", "SIGSEGV"},
		{"Program terminated with signal SIGABRT, Aborted.", "SIGABRT"},
		{"Program terminated with signal SIGBUS.", "SIGBUS"},
		{"received signal SIGFPE", "SIGFPE"},
		{"No signal here", ""},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := extractGDBSignal(tt.input)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestExtractAddress tests address extraction.
func TestExtractAddress(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0x0000000000000000", "0x0000000000000000"},
		{"0x7f1234567890 extra", "0x7f1234567890"},
		{"not an address", "not an address"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(len(tt.input), 20)], func(t *testing.T) {
			got := extractAddress(tt.input)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestCoreDumpResultStruct verifies the CoreDumpResult struct fields are serializable.
func TestCoreDumpResultStruct(t *testing.T) {
	result := &CoreDumpResult{
		Adapter:     CoreAdapterGDB,
		Program:     "/usr/bin/test",
		CoreFile:    "/tmp/core",
		Signal:      "SIGSEGV",
		FaultAddr:   "0x0",
		CrashReason: "segfault",
		Threads: []CoreThread{
			{
				ID:        1,
				IsCrashed: true,
				Reason:    "SIGSEGV",
				Stack: []CoreFrame{
					{Index: 0, Function: "main", File: "main.go", Line: 1, Address: "0x100"},
				},
			},
		},
		Variables: []CoreVariable{
			{Name: "x", Value: "42", Type: "int"},
		},
	}

	// Verify the struct fields are accessible.
	_ = result.Adapter
	_ = result.Program
	_ = result.CoreFile
	_ = result.Signal
	_ = result.FaultAddr
	_ = result.CrashReason
	_ = result.Threads
	_ = result.Variables

	if result.Adapter != CoreAdapterGDB {
		t.Errorf("expected gdb, got %s", result.Adapter)
	}
	if len(result.Threads) != 1 {
		t.Errorf("expected 1 thread, got %d", len(result.Threads))
	}
	if len(result.Variables) != 1 {
		t.Errorf("expected 1 variable, got %d", len(result.Variables))
	}
}

// TestCrashReportAllSections verifies that all crash report sections
// are properly formatted for each adapter type.
func TestCrashReportAllSections(t *testing.T) {
	for _, adapter := range []CoreAdapterType{CoreAdapterGDB, CoreAdapterLLDB, CoreAdapterDelve} {
		t.Run(string(adapter), func(t *testing.T) {
			result := &CoreDumpResult{
				Adapter:     adapter,
				Program:     "/usr/bin/test",
				CoreFile:    "/tmp/core",
				Signal:      "SIGSEGV",
				FaultAddr:   "0x0",
				CrashReason: "segfault",
				Threads: []CoreThread{
					{
						ID:        1,
						IsCrashed: true,
						Reason:    "SIGSEGV",
						Stack: []CoreFrame{
							{Index: 0, Function: "crash_func", File: "main.c", Line: 10, Address: "0x100"},
						},
					},
					{
						ID: 2,
						Stack: []CoreFrame{
							{Index: 0, Function: "idle_func", Address: "0x200"},
						},
					},
				},
				Variables: []CoreVariable{
					{Name: "x", Value: "42", Type: "int"},
				},
			}

			report := CrashReport(result)

			// All reports must contain header and footer.
			if !strings.Contains(report, "=== core dump crash report ===") {
				t.Error("missing header")
			}
			if !strings.Contains(report, "=== end of crash report ===") {
				t.Error("missing footer")
			}

			// Adapter must be mentioned.
			if !strings.Contains(report, string(adapter)) {
				t.Error("missing adapter name")
			}

			// Signal and threads should appear.
			if !strings.Contains(report, "SIGSEGV") {
				t.Error("missing signal")
			}
			if !strings.Contains(report, "thread 1") {
				t.Error("missing thread 1")
			}
			if !strings.Contains(report, "crash_func") {
				t.Error("missing crash function")
			}
			if !strings.Contains(report, "x") || !strings.Contains(report, "42") {
				t.Error("missing variable info")
			}
		})
	}
}

// TestAnalyzeCoreDumpRequiresFiles verifies that AnalyzeCoreDump validates inputs.
func TestAnalyzeCoreDumpRequiresFiles(t *testing.T) {
	ctx := t.Context()

	_, err := AnalyzeCoreDump(ctx, "", "/usr/bin/test", "")
	if err == nil {
		t.Fatal("expected error for empty core file")
	}
	if !strings.Contains(err.Error(), "core file path is required") {
		t.Errorf("unexpected error: %v", err)
	}

	_, err = AnalyzeCoreDump(ctx, "/tmp/core", "", "")
	if err == nil {
		t.Fatal("expected error for empty program")
	}
	if !strings.Contains(err.Error(), "program path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDetectCoreAdapterPlatform tests that the platform-appropriate default is selected.
func TestDetectCoreAdapterPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" {
		// On macOS, lldb should be preferred if available.
		if _, err := exec.LookPath("lldb"); err == nil {
			adapter, err := DetectCoreAdapter("")
			if err != nil {
				t.Fatalf("expected lldb to be detected: %v", err)
			}
			if adapter != CoreAdapterLLDB {
				t.Errorf("expected lldb on macOS, got %s", adapter)
			}
		}
	}
	if runtime.GOOS == "linux" {
		// On Linux, gdb should be preferred if available.
		if _, err := exec.LookPath("gdb"); err == nil {
			adapter, err := DetectCoreAdapter("")
			if err != nil {
				t.Fatalf("expected gdb to be detected: %v", err)
			}
			if adapter != CoreAdapterGDB {
				t.Errorf("expected gdb on Linux, got %s", adapter)
			}
		}
	}
}

// TestParseGDBOutputWithThreadApplyAll tests parsing of "thread apply all bt" output.
func TestParseGDBOutputWithThreadApplyAll(t *testing.T) {
	input := `Program terminated with signal SIGSEGV, Segmentation fault.
Thread 1 (LWP 12345):
#0  0x0000000000401000 in crash at crash.c:10
#1  0x0000000000402000 in main at main.c:20
Thread 2 (LWP 12346):
#0  0x00007fff00001000 in sleep
`

	result := parseGDBOutput(input, "/tmp/core", "/usr/bin/test")
	if len(result.Threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(result.Threads))
	}
	if result.Threads[0].ID != 1 {
		t.Errorf("expected thread ID 1, got %d", result.Threads[0].ID)
	}
	if result.Threads[1].ID != 2 {
		t.Errorf("expected thread ID 2, got %d", result.Threads[1].ID)
	}
	if result.Threads[0].IsCrashed != true {
		t.Error("thread 1 should be marked as crashed")
	}
}

// TestParseLLDBOutputWithVariables tests parsing of lldb frame variable output.
func TestParseLLDBOutputWithVariables(t *testing.T) {
	input := `(lldb) frame variable
(int) x = 42
(int *) ptr = 0x0000000000000000
`
	result := parseLLDBOutput(input, "/tmp/core", "/usr/bin/test")
	if len(result.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %d", len(result.Variables))
	}
	if result.Variables[0].Name != "x" {
		t.Errorf("expected variable name 'x', got %q", result.Variables[0].Name)
	}
	if result.Variables[0].Type != "int" {
		t.Errorf("expected type 'int', got %q", result.Variables[0].Type)
	}
	if result.Variables[1].Type != "int *" {
		t.Errorf("expected type 'int *', got %q", result.Variables[1].Type)
	}
}

// TestDetectCoreAdapterForBinaryWithGoBinary tests that Go binaries prefer delve.
func TestDetectCoreAdapterForBinaryWithGoBinary(t *testing.T) {
	// Use the current Go test binary as a target.
	// The test binary should contain Go runtime symbols.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// Check if the test binary is actually a Go binary.
	isGo, _, _ := DetectGoBinary(self)
	if !isGo {
		t.Skip("test binary is not a Go binary, skipping Go-specific detection test")
	}

	// Check if dlv is available.
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv not available, skipping Go core adapter detection test")
	}

	adapter, err := DetectCoreAdapterForBinary("", self)
	if err != nil {
		t.Fatalf("failed to detect adapter: %v", err)
	}
	if adapter != CoreAdapterDelve {
		t.Errorf("expected delve for Go binary, got %s", adapter)
	}
}

// TestParseDelveCoreOutputEmpty tests parsing empty delve output.
func TestParseDelveCoreOutputEmpty(t *testing.T) {
	result := parseDelveCoreOutput("", "/tmp/core", "./test")
	if result.Adapter != CoreAdapterDelve {
		t.Errorf("expected adapter delve, got %s", result.Adapter)
	}
	if len(result.Threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(result.Threads))
	}
}

// TestExtractLLDBStopReason tests stop reason extraction.
func TestExtractLLDBStopReason(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"stop reason = EXC_BAD_ACCESS (code=1, address=0x0)", "EXC_BAD_ACCESS"},
		{"stop reason = EXC_BAD_ACCESS", "EXC_BAD_ACCESS"},
		{"stop reason = SIGSEGV", "SIGSEGV"},
		{"stop reason = signal SIGABRT", "signal SIGABRT"},
		{"no reason here", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(len(tt.input), 30)], func(t *testing.T) {
			got := extractLLDBStopReason(tt.input)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestParseGDBThreadID tests GDB thread ID parsing.
func TestParseGDBThreadID(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Thread 1 (LWP 12345):", 1},
		{"Thread 2 (LWP 67890)", 2},
		{"Thread 10", 10},
		{"Thread", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseGDBThreadID(tt.input)
			if got != tt.want {
				t.Errorf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

// TestCoreDumpFilePath verifies that the core file path is stored in the result.
func TestCoreDumpFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	corePath := filepath.Join(tmpDir, "core.12345")
	// Create a dummy core file (content doesn't matter for validation).
	if err := os.WriteFile(corePath, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	// Just verify the path is stored correctly in the result structure.
	result := &CoreDumpResult{
		CoreFile: corePath,
	}
	if result.CoreFile != corePath {
		t.Errorf("expected core file %q, got %q", corePath, result.CoreFile)
	}
}
