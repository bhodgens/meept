package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
)

func TestTranscriptFetch_Execute_FakeRunner(t *testing.T) {
	var gotName string
	var gotArgs []string
	calls := 0
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		calls++
		gotName = name
		gotArgs = append([]string(nil), args...)
		stdout := "{\"text\": \"hello\", \"start\": 0.0}\n{\"text\": \"world\", \"start\": 65.0}\n"
		return []byte(stdout), nil, nil
	}

	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1", calls)
	}
	if gotName != "python3" {
		t.Errorf("runner name = %q, want %q", gotName, "python3")
	}
	// Documented argv shape: <pythonPath> -c <script> <videoID> [lang...]
	if len(gotArgs) < 3 || gotArgs[0] != "-c" {
		t.Errorf("runner args = %v, want args[0] == \"-c\" and video ID present", gotArgs)
	} else {
		if gotArgs[2] != "DWoJZs6TuVs" {
			t.Errorf("runner video id arg = %q, want %q", gotArgs[2], "DWoJZs6TuVs")
		}
		if len(gotArgs) != 3 {
			t.Errorf("language unset: runner args = %v, want no language restriction args", gotArgs)
		}
	}

	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", res)
	}
	if got := m["content"]; got != "hello\nworld" {
		t.Errorf("content = %q, want %q", got, "hello\nworld")
	}
	if got := m["video_id"]; got != "DWoJZs6TuVs" {
		t.Errorf("video_id = %v, want DWoJZs6TuVs", got)
	}
}

func TestTranscriptFetch_Execute_Timestamps(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		stdout := "{\"text\": \"hello\", \"start\": 0.0}\n{\"text\": \"world\", \"start\": 65.0}\n{\"text\": \"late\", \"start\": 3671.5}\n"
		return []byte(stdout), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":        "DWoJZs6TuVs",
		"timestamps": true,
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m := res.(map[string]any)
	want := "[00:00] hello\n[01:05] world\n[01:01:11] late" // 3671.5s -> HH:MM:SS per spec
	if got := m["content"]; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestTranscriptFetch_Execute_LanguagePassedToRunner(t *testing.T) {
	var gotArgs []string
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		gotArgs = append([]string(nil), args...)
		return []byte("{\"text\": \"hola\", \"start\": 0.0}\n"), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	if _, err := tool.Execute(context.Background(), map[string]any{
		"url":      "https://www.youtube.com/watch?v=DWoJZs6TuVs",
		"language": "es",
	}); err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	if len(gotArgs) < 4 || gotArgs[3] != "es" {
		t.Errorf("runner args = %v, want %q appended after video id", gotArgs, "es")
	}
}

func TestTranscriptFetch_Execute_RunnerMissingDep(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		return nil, []byte("No module named youtube_transcript_api"), errors.New("exit status 1")
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	if err == nil {
		t.Fatal("Execute expected error for missing dependency, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "youtube-transcript-api not installed") {
		t.Errorf("error %q missing \"youtube-transcript-api not installed\"", msg)
	}
	if !strings.Contains(msg, "pip install") {
		t.Errorf("error %q missing pip install guidance", msg)
	}
}

func TestTranscriptFetch_Execute_Timeout(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return []byte("too late"), nil, nil
		}
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{TimeoutSeconds: 1}, nil)
	tool.SetTranscriptRunner(runner)

	start := time.Now()
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Execute expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error %q missing \"timed out\"", err.Error())
	}
	if elapsed > 3*time.Second {
		t.Errorf("timeout not enforced promptly: elapsed = %v", elapsed)
	}
}

func TestTranscriptFetch_Execute_LanguageFallbackRetry(t *testing.T) {
	calls := 0
	var callArgs [][]string
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		calls++
		callArgs = append(callArgs, append([]string(nil), args...))
		if calls == 1 {
			// Requested language unavailable.
			return nil, []byte("NoTranscriptFound: no transcript found for language"), errors.New("exit status 1")
		}
		return []byte("{\"text\": \"default\", \"start\": 0.0}\n"), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":      "https://youtu.be/DWoJZs6TuVs",
		"language": "fr",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("runner calls = %d, want 2 (requested language then default)", calls)
	}
	// First call carries the requested language; retry drops the restriction.
	if len(callArgs[0]) != 4 || callArgs[0][3] != "fr" {
		t.Errorf("first call args = %v, want language fr", callArgs[0])
	}
	if len(callArgs[1]) != 3 {
		t.Errorf("retry args = %v, want no language restriction", callArgs[1])
	}
	if got := res.(map[string]any)["content"]; got != "default" {
		t.Errorf("content = %q, want %q", got, "default")
	}
}

func TestTranscriptFetch_Execute_TranscriptsDisabled(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		return nil, []byte("TranscriptsDisabled: Subtitles are disabled for this video"), errors.New("exit status 1")
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	if err == nil || !strings.Contains(err.Error(), "transcripts are disabled for this video") {
		t.Fatalf("error = %v, want transcripts-disabled message", err)
	}
}

func TestTranscriptFetch_Execute_VideoUnavailable(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		return nil, []byte("VideoUnavailable: This video is unavailable"), errors.New("exit status 1")
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	if err == nil || !strings.Contains(err.Error(), "video unavailable") {
		t.Fatalf("error = %v, want video-unavailable context", err)
	}
}

func TestTranscriptFetch_Execute_BadURL(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		t.Error("runner must not be invoked for an invalid URL")
		return nil, nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.com/x",
	})
	if err == nil || !strings.Contains(err.Error(), "not a youtube video url or video id") {
		t.Fatalf("error = %v, want not-a-youtube-url message", err)
	}
}

func TestTranscriptFetch_Execute_Truncation(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		// Each "word \n" line is 6 formatted chars; 25k lines = 149999
		// formatted chars, comfortably over the 100k cap.
		var b strings.Builder
		for i := 0; i < 25000; i++ {
			b.WriteString("{\"text\": \"word \", \"start\": 0.0}\n")
		}
		return []byte(b.String()), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url": "DWoJZs6TuVs",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m := res.(map[string]any)
	content := m["content"].(string)
	if !strings.HasSuffix(content, "...[truncated]") {
		t.Errorf("content does not end with truncation suffix: %q", content[len(content)-40:])
	}
	if m["truncated"] != true {
		t.Error("truncated flag = false, want true")
	}
	// Pagination metadata is always present, even for unpaginated calls.
	if got, ok := m["total_chars"].(int); !ok || got != 149999 {
		t.Errorf("total_chars = %v (%T), want 149999", m["total_chars"], m["total_chars"])
	}
	if got, ok := m["offset"].(int); !ok || got != 0 {
		t.Errorf("offset = %v (%T), want 0", m["offset"], m["offset"])
	}
}

func TestTranscriptFetch_Execute_Pagination(t *testing.T) {
	// The runner fakes Python stdout (JSON lines); the tool formats each
	// segment as "text\n", so 20 segments of "abcdefghi" yield exactly
	// 199 formatted chars: chars 100..149 are five whole lines.
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		var b strings.Builder
		for i := 0; i < 20; i++ {
			b.WriteString(`{"text": "abcdefghi", "start": 0.0}` + "\n")
		}
		return []byte(b.String()), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":       "DWoJZs6TuVs",
		"offset":    100,
		"max_chars": 50,
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", res)
	}
	// Chars 100-150 of the formatted text; the page was clipped at its
	// end, so the truncation suffix is appended after the 50 chars.
	wantPrefix := strings.Repeat("abcdefghi\n", 5)
	got := m["content"].(string)
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("content = %q, want prefix %q", got, wantPrefix)
	}
	if !strings.HasSuffix(got, transcriptTruncationSuffix) {
		t.Errorf("clipped page must carry truncation suffix, got %q", got)
	}
	if len(got) != 50+len(transcriptTruncationSuffix) {
		t.Errorf("content length = %d, want %d (50 slice chars + suffix)", len(got), 50+len(transcriptTruncationSuffix))
	}
	if got := m["total_chars"]; got != 199 {
		t.Errorf("total_chars = %v (%T), want 199", m["total_chars"], m["total_chars"])
	}
	if got := m["offset"]; got != 100 {
		t.Errorf("offset = %v (%T), want 100", m["offset"], m["offset"])
	}
	if m["truncated"] != true {
		t.Error("truncated flag = false, want true (slice was clipped by max_chars)")
	}
}

func TestTranscriptFetch_Execute_OffsetBeyondTotal(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		var b strings.Builder
		for i := 0; i < 20; i++ {
			b.WriteString(`{"text": "abcdefghi", "start": 0.0}` + "\n")
		}
		return []byte(b.String()), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":    "DWoJZs6TuVs",
		"offset": 500,
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m := res.(map[string]any)
	if got := m["content"]; got != "" {
		t.Errorf("content = %q, want empty", got)
	}
	if got := m["total_chars"]; got != 199 {
		t.Errorf("total_chars = %v (%T), want 199", m["total_chars"], m["total_chars"])
	}
	if got := m["offset"]; got != 199 {
		t.Errorf("offset = %v (%T), want 199 (echo clamped to total)", m["offset"], m["offset"])
	}
	if m["truncated"] != false {
		t.Error("truncated flag = true, want false (nothing was clipped)")
	}
}

// TestTranscriptFetch_Execute_NoFalseTruncationUnderCap pins the BUG B
// fix: a transcript BELOW the 100k cap must never carry the truncated
// flag or suffix. 10000 lines of "abcdefgh\n" format to exactly 89999
// chars.
func TestTranscriptFetch_Execute_NoFalseTruncationUnderCap(t *testing.T) {
	runner := func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		var b strings.Builder
		for i := 0; i < 10000; i++ {
			b.WriteString(`{"text": "abcdefgh", "start": 0.0}` + "\n")
		}
		return []byte(b.String()), nil, nil
	}
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(runner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url": "DWoJZs6TuVs",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m := res.(map[string]any)
	if m["truncated"] != false {
		t.Error("truncated flag = true, want false for a sub-cap transcript")
	}
	content := m["content"].(string)
	if strings.HasSuffix(content, transcriptTruncationSuffix) {
		t.Errorf("content must not carry truncation suffix, got %q tail", content[len(content)-40:])
	}
	if got := m["total_chars"]; got != 89999 {
		t.Errorf("total_chars = %v, want 89999", got)
	}
	if len(content) != 89999 {
		t.Errorf("content length = %d, want 89999 (whole transcript, no page clipping)", len(content))
	}
}

func TestTranscriptFetch_SetTranscriptRunner_NilGuard(t *testing.T) {
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	// Nil must be ignored, preserving the default runner (no panic,
	// runner stays set).
	tool.SetTranscriptRunner(nil)
	if tool.runner == nil {
		t.Fatal("SetTranscriptRunner(nil) cleared the default runner")
	}
}

func TestTranscriptFetch_ParseVideoID(t *testing.T) {
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)

	valid := []struct {
		name string
		raw  string
		want string
	}{
		{"watch url", "https://www.youtube.com/watch?v=DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"watch url with extra params", "https://www.youtube.com/watch?si=abc&v=DWoJZs6TuVs&t=90", "DWoJZs6TuVs"},
		{"youtu.be", "https://youtu.be/DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"shorts", "https://www.youtube.com/shorts/DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"embed", "https://www.youtube.com/embed/DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"live", "https://www.youtube.com/live/DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"music host", "https://music.youtube.com/watch?v=DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"mobile host", "https://m.youtube.com/watch?v=DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"no scheme", "https://youtube.com/watch?v=DWoJZs6TuVs", "DWoJZs6TuVs"},
		{"bare id", "DWoJZs6TuVs", "DWoJZs6TuVs"},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			got, err := tool.parseVideoID(tc.raw)
			if err != nil {
				t.Fatalf("parseVideoID(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseVideoID(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	invalid := []struct {
		name string
		raw  string
	}{
		{"non-youtube host", "https://example.com/x"},
		{"empty string", ""},
		{"invalid id chars", "https://www.youtube.com/watch?v=DWoJZs6Tu!s"},
		{"invalid bare id chars", "DWoJZs6Tu!s"},
		{"too short bare id", "short"},
		{"watch without v param", "https://www.youtube.com/watch"},
		{"unknown path segment", "https://www.youtube.com/other/DWoJZs6TuVs"},
		{"shorts with invalid id", "https://youtu.be/short_id!!"},
		{"not a url at all", "hello world this is not an id"},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if got, err := tool.parseVideoID(tc.raw); err == nil {
				t.Fatalf("parseVideoID(%q) = %q, want error", tc.raw, got)
			}
		})
	}
}

func TestTranscriptFetch_NameAndSchema(t *testing.T) {
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	if tool.Name() != "transcript_fetch" {
		t.Fatalf("name = %q, want %q", tool.Name(), "transcript_fetch")
	}
	if tool.Description() == "" {
		t.Fatal("Description() must not be empty")
	}

	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Fatalf("params.Type = %q, want %q", params.Type, schemaTypeObject)
	}

	required := map[string]bool{}
	for _, r := range params.Required {
		required[r] = true
	}
	if !required["url"] {
		t.Error("schema must require \"url\"")
	}

	props := params.Properties
	if props == nil {
		t.Fatal("params.Properties must not be nil")
	}
	if p, ok := props["url"]; !ok {
		t.Error("schema missing \"url\" property")
	} else if p.Type != schemaTypeString {
		t.Errorf("url.Type = %q, want %q", p.Type, schemaTypeString)
	}
	if p, ok := props["timestamps"]; !ok {
		t.Error("schema missing \"timestamps\" property")
	} else if p.Type != schemaTypeBoolean {
		t.Errorf("timestamps.Type = %q, want %q", p.Type, schemaTypeBoolean)
	}
	if p, ok := props["language"]; !ok {
		t.Error("schema missing \"language\" property")
	} else if p.Type != schemaTypeString {
		t.Errorf("language.Type = %q, want %q", p.Type, schemaTypeString)
	}
	if p, ok := props["offset"]; !ok {
		t.Error("schema missing \"offset\" property")
	} else if p.Type != schemaTypeInteger {
		t.Errorf("offset.Type = %q, want %q", p.Type, schemaTypeInteger)
	}
	if p, ok := props["max_chars"]; !ok {
		t.Error("schema missing \"max_chars\" property")
	} else if p.Type != schemaTypeInteger {
		t.Errorf("max_chars.Type = %q, want %q", p.Type, schemaTypeInteger)
	}
	if p, ok := props["output_path"]; !ok {
		t.Error("schema missing \"output_path\" property")
	} else if p.Type != schemaTypeString {
		t.Errorf("output_path.Type = %q, want %q", p.Type, schemaTypeString)
	}
	if len(params.Required) != 1 || params.Required[0] != "url" {
		t.Errorf("Required = %v, want exactly [\"url\"] (offset/max_chars optional)", params.Required)
	}

	// Config defaults.
	def := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	if def.pythonPath != "python3" {
		t.Errorf("default pythonPath = %q, want %q", def.pythonPath, "python3")
	}
	if def.moduleName != "youtube-transcript-api" {
		t.Errorf("default moduleName = %q, want %q", def.moduleName, "youtube-transcript-api")
	}
	if def.timeoutSeconds != 60 {
		t.Errorf("default timeoutSeconds = %d, want 60", def.timeoutSeconds)
	}

	// Explicit config is respected.
	custom := NewTranscriptFetchTool(TranscriptConfig{
		PythonPath:     "/usr/local/bin/python3",
		ModuleName:     "custom-module",
		TimeoutSeconds: 120,
	}, nil)
	if custom.pythonPath != "/usr/local/bin/python3" {
		t.Errorf("pythonPath = %q, want /usr/local/bin/python3", custom.pythonPath)
	}
	if custom.moduleName != "custom-module" {
		t.Errorf("moduleName = %q, want custom-module", custom.moduleName)
	}
	if custom.timeoutSeconds != 120 {
		t.Errorf("timeoutSeconds = %d, want 120", custom.timeoutSeconds)
	}

	// Nil logger must not panic on construction or use.
	_ = NewTranscriptFetchTool(TranscriptConfig{}, nil)
}

// newTranscriptTestTool returns a tool whose runner serves the canned
// two-segment transcript "hello\nworld" (12 chars). Shared by the
// output_path tests so none of them touches a real subprocess.
func newTranscriptTestTool(t *testing.T) *TranscriptFetchTool {
	t.Helper()
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		stdout := "{\"text\": \"hello\", \"start\": 0.0}\n{\"text\": \"world\", \"start\": 65.0}\n"
		return []byte(stdout), nil, nil
	})
	return tool
}

func TestTranscriptFetch_ResolveOutputPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	abs := filepath.Join(t.TempDir(), "out.txt")
	wd := t.TempDir()
	fallbackRoot := filepath.Join(t.TempDir(), "fallback")
	fallbackWant := filepath.Join(fallbackRoot, "sub", "tr.txt")

	tests := []struct {
		name     string
		raw      string
		withWD   bool
		fallback string
		want     string
		wantErr  bool
	}{
		{
			name:    "absolute passes through",
			raw:     abs,
			want:    abs,
			wantErr: false,
		},
		{
			name:   "relative joins working dir",
			raw:    "sub/tr.txt",
			withWD: true,
			want:   filepath.Join(wd, "sub", "tr.txt"),
		},
		{
			name:     "relative without working dir joins fallback root",
			raw:      "sub/tr.txt",
			fallback: fallbackRoot,
			want:     fallbackWant,
		},
		{
			name: "tilde expands to home",
			raw:  "~/meept-tr-test/x.txt",
			want: filepath.Join(home, "meept-tr-test", "x.txt"),
		},
		{
			name: "empty raw resolves to nothing",
			raw:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewTranscriptFetchTool(TranscriptConfig{FallbackOutputDir: tt.fallback}, nil)
			ctx := context.Background()
			if tt.withWD {
				ctx = tools.ContextWithWorkingDir(ctx, wd)
			}
			got, err := tool.resolveOutputPath(ctx, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveOutputPath(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("resolveOutputPath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestTranscriptFetch_OutputPath_Fits(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "nested", "deep", "tr.txt")
	tool := newTranscriptTestTool(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":         "DWoJZs6TuVs",
		"output_path": out,
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", res)
	}

	full := "hello\nworld"
	// File on disk carries the FULL text (nested parents created).
	data, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("output file missing: %v", rerr)
	}
	if string(data) != full {
		t.Errorf("file content = %q, want %q", data, full)
	}
	info, rerr := os.Stat(out)
	if rerr != nil {
		t.Fatalf("stat: %v", rerr)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, want 0644", info.Mode().Perm())
	}

	// Result: bounded head preview + pointer line naming the path.
	wantContent := full + fmt.Sprintf("\n...[full transcript at %s]", out)
	if got := m["content"]; got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
	if got := m["path"]; got != out {
		t.Errorf("path = %v, want %v", got, out)
	}
	if got, ok := m["truncated"].(bool); !ok || got {
		t.Errorf("truncated = %v (%T), want false", m["truncated"], m["truncated"])
	}
	if got := m["total_chars"]; got != len(full) {
		t.Errorf("total_chars = %v, want %d", got, len(full))
	}
}

func TestTranscriptFetch_OutputPath_Paginated(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "tr.txt")
	tool := newTranscriptTestTool(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":         "DWoJZs6TuVs",
		"output_path": out,
		"offset":      0,
		"max_chars":   5,
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m := res.(map[string]any)

	// Pagination window keeps today's exact semantics (suffix included).
	if got := m["content"]; got != "hello"+transcriptTruncationSuffix {
		t.Errorf("content = %q, want %q", got, "hello"+transcriptTruncationSuffix)
	}
	if got := m["path"]; got != out {
		t.Errorf("path = %v, want %v", got, out)
	}
	// The file still carries the FULL text, not the window.
	data, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("output file missing: %v", rerr)
	}
	if string(data) != "hello\nworld" {
		t.Errorf("file content = %q, want %q", data, "hello\nworld")
	}
}

func TestTranscriptFetch_OutputPath_WriteError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	tool := newTranscriptTestTool(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":         "DWoJZs6TuVs",
		"output_path": filepath.Join(blocker, "tr.txt"),
	})
	if err == nil {
		t.Fatalf("Execute error = nil, want write failure mentioning the path")
	}
	if !strings.Contains(err.Error(), "tr.txt") {
		t.Errorf("error = %q, want it to mention the output path", err)
	}
	if res != nil {
		t.Errorf("result = %v, want nil on write failure", res)
	}
}

func TestTranscriptFetch_OutputPath_Empty_NoPathKey(t *testing.T) {
	tool := newTranscriptTestTool(t)
	res, err := tool.Execute(context.Background(), map[string]any{
		"url": "DWoJZs6TuVs",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	m := res.(map[string]any)
	if _, has := m["path"]; has {
		t.Error("result must not carry a \"path\" key when output_path is absent")
	}
}

func TestTranscriptFetch_OutputPath_RelativeUsesFallbackDir(t *testing.T) {
	fallback := t.TempDir()
	tool := newTranscriptTestTool(t)
	// Explicit config (no working dir in ctx): relative output_path
	// must land under the tool's fallback root.
	tool.fallbackOutputDir = fallback

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":         "DWoJZs6TuVs",
		"output_path": "gen/tr.txt",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	want := filepath.Join(fallback, "gen", "tr.txt")
	m := res.(map[string]any)
	if got := m["path"]; got != want {
		t.Errorf("path = %v, want %v", got, want)
	}
	if _, rerr := os.Stat(want); rerr != nil {
		t.Errorf("file not written to fallback root: %v", rerr)
	}
}
