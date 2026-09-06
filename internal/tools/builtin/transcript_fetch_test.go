package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
		// Each "word \n" line is 6 formatted chars; 25k lines = 150k
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
	if len(content) > TranscriptMaxOutputLength+len("...[truncated]") {
		t.Errorf("content length = %d, exceeds cap", len(content))
	}
	if m["truncated"] != true {
		t.Error("truncated flag = false, want true")
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
