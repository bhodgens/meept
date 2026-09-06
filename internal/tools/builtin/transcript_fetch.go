package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// Default configuration values for TranscriptFetchTool.
const (
	// DefaultTranscriptPythonPath is the Python interpreter used when
	// TranscriptConfig.PythonPath is empty.
	DefaultTranscriptPythonPath = "python3"
	// DefaultTranscriptModuleName is the Python module fetched when
	// TranscriptConfig.ModuleName is empty.
	DefaultTranscriptModuleName = "youtube-transcript-api"
	// DefaultTranscriptTimeoutSeconds is the subprocess timeout when
	// TranscriptConfig.TimeoutSeconds is <= 0.
	DefaultTranscriptTimeoutSeconds = 60
	// TranscriptMaxOutputLength caps the returned transcript text (100k chars),
	// matching web_fetch's output cap discipline.
	TranscriptMaxOutputLength = 100000
	// transcriptTruncationSuffix is appended when the transcript is truncated.
	transcriptTruncationSuffix = "...[truncated]"
)

// transcriptRunnerFunc is the injectable subprocess command runner. It
// exists so tests never execute real Python.
type transcriptRunnerFunc func(ctx context.Context, name string, args []string) (stdout []byte, stderr []byte, err error)

// TranscriptConfig configures the TranscriptFetchTool.
type TranscriptConfig struct {
	// PythonPath is the Python interpreter to invoke. Defaults to
	// "python3" when empty.
	PythonPath string
	// ModuleName is the Python module to import. Defaults to
	// "youtube-transcript-api" when empty.
	ModuleName string
	// TimeoutSeconds bounds the subprocess run. Defaults to 60 when <= 0.
	TimeoutSeconds int
}

// TranscriptFetchTool fetches YouTube video transcripts via the
// youtube-transcript-api Python package, executed as a subprocess. It
// mirrors the WebFetchTool pattern: injectable runner, actionable error
// strings, read-only behavior. The only network side effect is
// YouTube's transcript endpoint, reached via the Python library; the
// tool never fetches user-supplied URLs directly, so SSRF does not
// apply.
type TranscriptFetchTool struct {
	tools.ToolDefaults
	pythonPath     string
	moduleName     string
	timeoutSeconds int
	logger         *slog.Logger
	// runner executes the Python subprocess; defaults to
	// execCommandContextRunner (exec.CommandContext). Injectable for tests.
	runner transcriptRunnerFunc
}

// NewTranscriptFetchTool creates a new transcript fetch tool, applying
// config defaults. A nil logger is accepted and disables logging.
func NewTranscriptFetchTool(cfg TranscriptConfig, logger *slog.Logger) *TranscriptFetchTool {
	if cfg.PythonPath == "" {
		cfg.PythonPath = DefaultTranscriptPythonPath
	}
	if cfg.ModuleName == "" {
		cfg.ModuleName = DefaultTranscriptModuleName
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = DefaultTranscriptTimeoutSeconds
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TranscriptFetchTool{
		pythonPath:     cfg.PythonPath,
		moduleName:     cfg.ModuleName,
		timeoutSeconds: cfg.TimeoutSeconds,
		logger:         logger,
		runner:         execCommandContextRunner,
	}
}

// SetTranscriptRunner sets the subprocess command runner. Nil is
// ignored, preserving the default runner (nil-guard convention).
// Must be called before the tool serves requests; intended for tests.
func (t *TranscriptFetchTool) SetTranscriptRunner(fn transcriptRunnerFunc) {
	if fn != nil {
		t.runner = fn
	}
}

func (t *TranscriptFetchTool) Name() string { return "transcript_fetch" }

func (t *TranscriptFetchTool) Category() string { return "web" }

func (t *TranscriptFetchTool) Description() string {
	return "Fetch the transcript of a YouTube video and return it as plain text. Accepts any YouTube URL form (watch, youtu.be, shorts, embed, live) or a bare 11-character video ID. Auto-generated and human transcripts are both supported. Useful for reading video content without watching it."
}

func (t *TranscriptFetchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"url": {
				Type:        schemaTypeString,
				Description: "YouTube video URL (watch, youtu.be, shorts, embed, or live form) or a bare 11-character video ID.",
			},
			"timestamps": {
				Type:        schemaTypeBoolean,
				Description: "If true, prefix each segment with its start time as [MM:SS] (default false).",
			},
			"language": {
				Type:        schemaTypeString,
				Description: "Preferred transcript language code (e.g. \"en\", \"es\"). Falls back to the default transcript when unavailable.",
			},
		},
		Required: []string{"url"},
	}
}

// IsReadOnly reports that fetching transcripts never mutates state.
func (t *TranscriptFetchTool) IsReadOnly(map[string]any) bool { return true }

// IsConcurrencySafe reports that transcript fetches are safe for
// concurrent execution.
func (t *TranscriptFetchTool) IsConcurrencySafe(map[string]any) bool { return true }

// transcriptParams is the typed arg struct for Execute.
type transcriptParams struct {
	URL        string `json:"url"`
	Timestamps bool   `json:"timestamps"`
	Language   string `json:"language"`
}

// Execute fetches the transcript for the requested video and returns
// plain text (optionally timestamped) as a tool result.
func (t *TranscriptFetchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Typed struct parse: the two-value map walk used elsewhere is not
	// needed; re-marshal a normalized map and decode.
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("transcript_fetch: invalid arguments: %w", err)
	}
	var p transcriptParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("transcript_fetch: invalid arguments: %w", err)
	}
	if p.URL == "" {
		return nil, errors.New("transcript_fetch: no URL specified")
	}

	videoID, err := t.parseVideoID(p.URL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.timeoutSeconds)*time.Second)
	defer cancel()

	text, err := t.fetchTranscriptText(ctx, videoID, p.Language, p.Timestamps)
	if err != nil {
		return nil, err
	}
	if len(text) > TranscriptMaxOutputLength {
		text = text[:TranscriptMaxOutputLength] + transcriptTruncationSuffix
	}

	t.logger.Debug("fetched youtube transcript",
		"video_id", videoID,
		"language", p.Language,
		"timestamps", p.Timestamps,
		"chars", len(text))

	return map[string]any{
		"video_id":   videoID,
		"content":    text,
		"truncated":  len(text) > TranscriptMaxOutputLength,
		"timestamps": p.Timestamps,
	}, nil
}

// errNotYouTubeURL is returned when the input is neither a recognized
// YouTube URL form nor a bare 11-character video ID.
var errNotYouTubeURL = errors.New("transcript_fetch: not a youtube video url or video id")

// parseVideoID extracts the 11-character video ID from any supported
// YouTube URL form (watch?v=, youtu.be/<id>, /shorts/, /embed/,
// /live/; hosts: www.youtube.com, youtube.com, m.youtube.com,
// music.youtube.com, youtu.be) or from a bare ID. The returned ID is
// validated to exactly 11 chars of [A-Za-z0-9_-], which is what keeps
// it safe to place directly in subprocess argv (no shell involved).
func (t *TranscriptFetchTool) parseVideoID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errNotYouTubeURL
	}
	// Bare video ID form.
	if isYouTubeVideoID(raw) {
		return raw, nil
	}
	// URL form: normalize a missing scheme so url.Parse populates Host.
	s := raw
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", errNotYouTubeURL
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.youtube.com", "youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			if id := u.Query().Get("v"); isYouTubeVideoID(id) {
				return id, nil
			}
			return "", errNotYouTubeURL
		}
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segs) == 2 {
			switch segs[0] {
			case "shorts", "embed", "live":
				if isYouTubeVideoID(segs[1]) {
					return segs[1], nil
				}
			}
		}
		return "", errNotYouTubeURL
	case "youtu.be":
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segs) >= 1 && isYouTubeVideoID(segs[0]) {
			return segs[0], nil
		}
		return "", errNotYouTubeURL
	default:
		return "", errNotYouTubeURL
	}
}

// isYouTubeVideoID reports whether s is exactly 11 characters of
// [A-Za-z0-9_-], the YouTube video ID charset.
func isYouTubeVideoID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// transcriptSegment is one JSON line of the Python script's stdout.
type transcriptSegment struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
}

// transcriptFetchScript is the Python one-liner run via
// `python3 -c <script> <videoID> [language...]`. It uses the documented
// Python API of youtube-transcript-api (the module's CLI surface has
// churned across versions, so the stable `python3 -c` approach is
// preferred per the leaf Notes). It prints one JSON object per segment
// ({"text": ..., "start": ...}) as JSON lines on stdout and lets
// library exceptions surface on stderr with a nonzero exit code. The
// video ID reaching this script is validated to [A-Za-z0-9_-] by
// parseVideoID, so argv placement is injection-safe (no shell).
const transcriptFetchScript = `import json, sys
try:
    from youtube_transcript_api import YouTubeTranscriptApi
except ImportError:
    sys.stderr.write("No module named youtube_transcript_api\n")
    sys.exit(2)
vid = sys.argv[1]
langs = sys.argv[2:]
try:
    if langs:
        fetched = YouTubeTranscriptApi.list(vid)
        try:
            t = fetched.find_transcript(langs)
        except Exception:
            raise SystemExit(3)
    else:
        t = YouTubeTranscriptApi.get_transcript(vid)
    data = t.fetch()
except SystemExit:
    raise
except Exception as e:
    sys.stderr.write(type(e).__name__ + ": " + str(e) + "\n")
    sys.exit(1)
for seg in data:
    sys.stdout.write(json.dumps({"text": seg.get("text", ""), "start": seg.get("start", 0.0)}) + "\n")
`

// Sentinel stderr markers emitted by transcriptFetchScript (and their
// upstream library equivalents), matched prefix-wise when mapping the
// subprocess failure to an actionable error.
const (
	// transcriptLangMissSentinel is exit code 3: requested language had
	// no matching transcript.
	transcriptLangMissExit = 3
	// stderr prefix emitted when the package is not importable.
	transcriptModuleMissingMarker = "No module named youtube_transcript_api"
)

// fetchTranscriptText runs the Python subprocess (once, or twice when a
// requested language misses and the default transcript is retried),
// parses JSON-line segments, and formats them.
func (t *TranscriptFetchTool) fetchTranscriptText(ctx context.Context, videoID, language string, timestamps bool) (string, error) {
	run := func(lang string) (string, []byte, error) {
		args := []string{"-c", transcriptFetchScript, videoID}
		if lang != "" {
			args = append(args, lang)
		}
		stdout, stderr, err := t.runner(ctx, t.pythonPath, args)
		return string(stdout), stderr, err
	}

	stdout, stderr, err := run(language)
	if err != nil {
		// Language fallback: retry once without the language restriction.
		if language != "" && (strings.Contains(string(stderr), "NoTranscriptFound") || isExitCode(err, transcriptLangMissExit)) {
			stdout, stderr, err = run("")
		}
	}
	if err != nil {
		return "", t.mapSubprocessError(ctx, stderr, err)
	}

	segs, perr := parseTranscriptSegments(stdout)
	if perr != nil {
		// Non-JSON stdout with a zero exit: surface stderr for diagnosis.
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return "", fmt.Errorf("transcript_fetch: unexpected python output: %s", msg)
		}
		return "", fmt.Errorf("transcript_fetch: could not parse transcript output: %w", perr)
	}

	var b strings.Builder
	if timestamps {
		for _, s := range segs {
			b.WriteString(formatTranscriptTimestamp(s.Start))
			b.WriteString(" ")
			b.WriteString(s.Text)
			b.WriteString("\n")
		}
	} else {
		for _, s := range segs {
			b.WriteString(s.Text)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// mapSubprocessError converts a failed subprocess run into an
// actionable tool error per the leaf contract.
func (t *TranscriptFetchTool) mapSubprocessError(ctx context.Context, stderr []byte, err error) error {
	msg := string(stderr)
	switch {
	case ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded):
		d := t.timeoutSeconds
		return fmt.Errorf("transcript_fetch: timed out after %ds", d)
	case strings.Contains(msg, transcriptModuleMissingMarker) || strings.Contains(msg, "No module named"):
		// Covers both the missing module and a missing python3 binary:
		// a failed exec surfaces the same install guidance.
		return fmt.Errorf("transcript_fetch: youtube-transcript-api not installed (install: %s -m pip install youtube-transcript-api): %w", t.pythonPath, err)
	case strings.Contains(msg, "TranscriptsDisabled"):
		return errors.New("transcript_fetch: transcripts are disabled for this video")
	case strings.Contains(msg, "VideoUnavailable"):
		return fmt.Errorf("transcript_fetch: video unavailable: %w", err)
	default:
		detail := strings.TrimSpace(msg)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("transcript_fetch: transcript fetch failed: %s", detail)
	}
}

// isExitCode reports whether err carries the given subprocess exit code
// (exec.ExitError) or wraps one.
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == code
	}
	return false
}

// parseTranscriptSegments parses JSON-line transcript segments.
func parseTranscriptSegments(stdout string) ([]transcriptSegment, error) {
	var segs []transcriptSegment
	dec := json.NewDecoder(strings.NewReader(stdout))
	for dec.More() {
		var s transcriptSegment
		if err := dec.Decode(&s); err != nil {
			return nil, err
		}
		segs = append(segs, s)
	}
	return segs, nil
}

// formatTranscriptTimestamp renders start seconds as [MM:SS], or
// [HH:MM:SS] once the duration reaches an hour.
func formatTranscriptTimestamp(start float64) string {
	total := int(start)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("[%02d:%02d:%02d]", h, m, s)
	}
	return fmt.Sprintf("[%02d:%02d]", m, s)
}

// Ensure TranscriptFetchTool implements the Tool interface.
var _ tools.Tool = (*TranscriptFetchTool)(nil)

// execCommandContextRunner is the production runner: exec.CommandContext
// with CombinedOutput semantics split into stdout/stderr.
func execCommandContextRunner(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return []byte(stdout.String()), []byte(stderr.String()), err
}
