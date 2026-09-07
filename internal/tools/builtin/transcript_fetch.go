package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	// TranscriptResultTokens is the declared minimum token budget for this
	// tool's results (via tools.ResultSizer). The agent loop never compresses
	// a transcript_fetch result below this floor regardless of the decaying
	// dynamic budget, so the tool's path pointer metadata and head preview
	// survive even a late-turn, budget-exhausted tool call. The loop caps
	// this at the global ToolResultMaxTokens ceiling (3000).
	TranscriptResultTokens = 1400
	// DefaultTranscriptFallbackOutputDir is the fallback root for relative
	// output_path values when TranscriptConfig.FallbackOutputDir is empty.
	DefaultTranscriptFallbackOutputDir = "~/.meept/media"
	// TranscriptPreviewChars bounds the in-result head preview when the
	// full transcript went to a file (pointer line may push past it).
	TranscriptPreviewChars = 1000
	// transcriptTruncationSuffix is appended when the transcript is truncated.
	transcriptTruncationSuffix = "...[truncated]"
	// transcriptFilePointerSuffix names the file carrying the full text.
	transcriptFilePointerSuffix = "\n...[full transcript at %s]"

	// transcriptSummarizeWindowChars is the map-stage window size: text
	// at or below this length gets a single reduce call over the whole
	// text; longer text is sliced into windows of this size.
	transcriptSummarizeWindowChars = 12000
	// transcriptSummarizeOverlapChars is how much consecutive windows
	// overlap, so sentences straddling a boundary survive in at least
	// one window whole.
	transcriptSummarizeOverlapChars = 500
	// transcriptSummarizeBackoffChars bounds the whitespace backoff when
	// searching backwards from a window boundary for a word break.
	transcriptSummarizeBackoffChars = 200
	// transcriptSummarizeDigestChars caps the final digest returned in
	// the result content (~4k chars keeps the conversation small).
	transcriptSummarizeDigestChars = 4000

	// transcriptSummarizeMapSystem is the MAP-stage system prompt
	// (contract text; do not paraphrase).
	transcriptSummarizeMapSystem = "Summarize this transcript segment. Preserve: steps, decision rules, tool/API names, numbers, and the WHY behind choices. Drop filler and repetition. Max 300 words."
	// transcriptSummarizeReduceSystem is the REDUCE-stage system prompt
	// (contract text; do not paraphrase).
	transcriptSummarizeReduceSystem = "Merge these segment summaries into one coherent summary. Keep every step, rule, name, and number. Max 800 words."
	// transcriptSummarizeSep joins per-chunk summaries for the reduce call.
	transcriptSummarizeSep = "\n---\n"

	// transcriptSummarizeNotConfiguredError is returned when summarize
	// mode runs without a wired summarizer client. Verbatim contract.
	transcriptSummarizeNotConfiguredError = "transcript_fetch: summarization not configured (set [transcript] summarize_enabled = true and a summarizer_model or small_model in models.json5)"
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
	// FallbackOutputDir is the root relative output_path values join when
	// the session context carries no working directory. Defaults to
	// "~/.meept/media" when empty; "~" expands at resolve time.
	FallbackOutputDir string
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
	pythonPath        string
	moduleName        string
	timeoutSeconds    int
	fallbackOutputDir string
	logger            *slog.Logger
	// runner executes the Python subprocess; defaults to
	// execCommandContextRunner (exec.CommandContext). Injectable for tests.
	runner transcriptRunnerFunc
	// summarizer is the optional local summarizer client (llm.Chatter)
	// used by summarize mode. Nil until SetSummarizer wires it; nil +
	// summarize=true yields the config error.
	summarizer llm.Chatter
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
	if cfg.FallbackOutputDir == "" {
		cfg.FallbackOutputDir = DefaultTranscriptFallbackOutputDir
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TranscriptFetchTool{
		pythonPath:        cfg.PythonPath,
		moduleName:        cfg.ModuleName,
		timeoutSeconds:    cfg.TimeoutSeconds,
		fallbackOutputDir: cfg.FallbackOutputDir,
		logger:            logger,
		runner:            execCommandContextRunner,
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

// SetSummarizer injects the local summarizer client used by summarize
// mode. Nil is ignored, leaving the field unset (nil-guard convention).
// Must be called before the tool serves requests; the daemon wires it
// when [transcript] summarize_enabled is true (leaf 04 wiring).
func (t *TranscriptFetchTool) SetSummarizer(chatter llm.Chatter) {
	if chatter != nil {
		t.summarizer = chatter
	}
}

func (t *TranscriptFetchTool) Name() string { return "transcript_fetch" }

func (t *TranscriptFetchTool) Category() string { return "web" }

func (t *TranscriptFetchTool) Description() string {
	return "Fetch the transcript of a YouTube video and return it as plain text. Accepts any YouTube URL form (watch, youtu.be, shorts, embed, live) or a bare 11-character video ID. Auto-generated and human transcripts are both supported. Useful for reading video content without watching it. Supports an optional summarize mode that returns an LLM digest instead of the verbatim text."
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
			"offset": {
				Type:        schemaTypeInteger,
				Description: "Character offset into the returned transcript text (the same formatted form, timestamps included) at which to start this page. Defaults to 0.",
			},
			"max_chars": {
				Type:        schemaTypeInteger,
				Description: "Maximum characters of transcript text to return for this page (excluding any truncation suffix), capped at 100000. Defaults to 100000. Use with offset to paginate long transcripts; the response carries total_chars and offset for computing the next page.",
			},
			"output_path": {
				Type:        schemaTypeString,
				Description: "Optional file path to write the full formatted transcript to. Relative paths resolve against the session working directory (falling back to the configured output root outside a workspace); parent directories are created automatically. The response carries the resolved path plus a bounded preview, or the normal paginated window when offset/max_chars is used.",
			},
			"summarize": {
				Type:        schemaTypeBoolean,
				Description: "If true, return an LLM-generated digest of the transcript (map-reduce over ~12k-char windows via the configured local summarizer) instead of verbatim text. offset/max_chars pagination is ignored in this mode. Composes with output_path: the full text still goes to disk while the response carries the digest. Errors with configuration guidance when no summarizer is wired.",
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

// MaxResultTokens declares this tool's minimum result-token budget
// (tools.ResultSizer). See TranscriptResultTokens.
func (t *TranscriptFetchTool) MaxResultTokens() int { return TranscriptResultTokens }

// transcriptParams is the typed arg struct for Execute.
type transcriptParams struct {
	URL        string `json:"url"`
	Timestamps bool   `json:"timestamps"`
	Language   string `json:"language"`
	Offset     int    `json:"offset"`
	MaxChars   int    `json:"max_chars"`
	OutputPath string `json:"output_path"`
	Summarize  bool   `json:"summarize"`
}

// Execute fetches the transcript for the requested video and returns
// plain text (optionally timestamped) as a tool result. With
// summarize=true it instead returns a map-reduce digest.
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
	// File-backed output: write the FULL formatted text to disk before
	// any pagination slicing, so the file is byte-identical to the text
	// the caller paginates. Write failure is a hard error — no silent
	// fallback to in-result-only behavior.
	outPath := ""
	if p.OutputPath != "" {
		outPath, err = t.resolveOutputPath(ctx, p.OutputPath)
		if err != nil {
			return nil, err
		}
		if err := writeTranscriptFile(outPath, text); err != nil {
			return nil, err
		}
	}
	// Summarize mode: map-reduce the FULL text into a digest and return
	// it instead of paginating. Branch order matters — this runs after
	// the output_path write (the file still receives the full text, so
	// summarize composes with leaf-01 file-backed output) and BEFORE
	// pagination. Summarize and pagination are mutually exclusive paths
	// in one call: when summarize=true, offset/max_chars are IGNORED —
	// the digest is already conversation-sized (~4k chars).
	if p.Summarize {
		if t.summarizer == nil {
			return nil, errors.New(transcriptSummarizeNotConfiguredError)
		}
		digest, chunkCount, err := t.summarizeText(ctx, text)
		if err != nil {
			return nil, err
		}
		t.logger.Debug("summarized youtube transcript",
			"video_id", videoID,
			"chunks", chunkCount,
			"source_chars", len(text))
		result := map[string]any{
			"video_id":    videoID,
			"content":     digest,
			"summarized":  true,
			"chunk_count": chunkCount,
			"total_chars": len(text),
			"timestamps":  p.Timestamps,
		}
		// Contract: "path" appears only when output_path was given.
		if outPath != "" {
			result["path"] = outPath
		}
		return result, nil
	}
	// Pagination slices the FORMATTED text (timestamps included), so
	// offsets stay consistent with the text the caller received. Total
	// is captured before any mutation; the truncated flag is computed
	// BEFORE the suffix mutation (computing it after appending
	// "...[truncated]" made the flag an accident of suffix length).
	totalChars := len(text)

	// Normalize pagination: negative or oversized offsets clamp to the
	// valid range; max_chars <= 0 falls back to the 100k default and is
	// hard-capped at it.
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > totalChars {
		offset = totalChars
	}
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = TranscriptMaxOutputLength
	}
	if maxChars > TranscriptMaxOutputLength {
		maxChars = TranscriptMaxOutputLength
	}

	end := offset + maxChars
	if end > totalChars {
		end = totalChars
	}
	page := text[offset:end]
	// Clipped = the remaining text after the offset exceeded max_chars.
	// Computed from lengths captured BEFORE any mutation, never from the
	// post-suffix length (which made the flag an accident of suffix
	// length). With default pagination this degenerates to the legacy
	// "full text exceeded the 100k cap" semantics.
	clipped := totalChars-offset > maxChars
	if clipped {
		// Content was actually cut: mark it in-band for the caller.
		page += transcriptTruncationSuffix
	}
	truncated := clipped
	// Bounded preview: when the full text went to a file AND it fit
	// entirely inside one page, swap the in-result body for a head
	// preview plus a pointer to the file. The conversation never carries
	// the bulk; the agent pages the rest via file_read.
	if outPath != "" && !clipped && totalChars <= maxChars {
		preview := text
		if len(preview) > TranscriptPreviewChars {
			preview = preview[:TranscriptPreviewChars]
		}
		page = preview + fmt.Sprintf(transcriptFilePointerSuffix, outPath)
	}

	t.logger.Debug("fetched youtube transcript",
		"video_id", videoID,
		"language", p.Language,
		"timestamps", p.Timestamps,
		"chars", len(page))

	result := map[string]any{
		"video_id":    videoID,
		"content":     page,
		"truncated":   truncated,
		"timestamps":  p.Timestamps,
		"offset":      offset,
		"total_chars": totalChars,
	}
	// Contract: "path" appears only when output_path was given.
	if outPath != "" {
		result["path"] = outPath
	}
	return result, nil
}

// summarizeText runs the sequential map-reduce over text through the
// injected summarizer chatter and returns the trimmed/capped digest
// plus the chunk count (1 for the short-text single-reduce path, else
// the window count). No goroutines: each stage waits for the previous.
// The caller's ctx deadline (set in Execute from timeoutSeconds) bounds
// the whole operation.
func (t *TranscriptFetchTool) summarizeText(ctx context.Context, text string) (string, int, error) {
	chat := func(system, user string) (string, error) {
		resp, err := t.summarizer.Chat(ctx, []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		})
		if err != nil {
			return "", err
		}
		if resp == nil {
			return "", errors.New("empty summarizer response")
		}
		// Models add whitespace; trim before capping.
		return strings.TrimSpace(resp.Content), nil
	}
	// Stage failure mapping: a deadline abort surfaces as a timeout,
	// otherwise the failing stage is named (map/reduce) — no partial
	// digest is ever returned.
	stageErr := func(stage string, err error) error {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return errors.New("transcript_fetch: summarization timed out")
		}
		return fmt.Errorf("transcript_fetch: summarization %s stage failed: %w", stage, err)
	}

	if len(text) <= transcriptSummarizeWindowChars {
		// Short text: single reduce call over the whole text; the map
		// stage is skipped.
		digest, err := chat(transcriptSummarizeReduceSystem, text)
		if err != nil {
			return "", 0, stageErr("reduce", err)
		}
		return capSummarizeDigest(digest), 1, nil
	}

	windows := splitIntoWindows(text)
	summaries := make([]string, 0, len(windows))
	for _, w := range windows {
		s, err := chat(transcriptSummarizeMapSystem, w)
		if err != nil {
			return "", 0, stageErr("map", err)
		}
		summaries = append(summaries, s)
	}
	joined := strings.Join(summaries, transcriptSummarizeSep)
	digest, err := chat(transcriptSummarizeReduceSystem, joined)
	if err != nil {
		return "", 0, stageErr("reduce", err)
	}
	return capSummarizeDigest(digest), len(windows), nil
}

// capSummarizeDigest trims the digest and caps it at
// transcriptSummarizeDigestChars, appending the truncation suffix when
// content was cut.
func capSummarizeDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if len(digest) > transcriptSummarizeDigestChars {
		digest = digest[:transcriptSummarizeDigestChars] + transcriptTruncationSuffix
	}
	return digest
}

// splitIntoWindows slices text into ~transcriptSummarizeWindowChars
// windows with transcriptSummarizeOverlapChars overlap. Window
// boundaries back off to the nearest whitespace byte (never mid-word
// when a break is reachable within the backoff budget); the next window
// starts overlap chars before the previous window's end, so its head
// literally repeats the previous window's tail. Empty input returns
// nil; text at or below one window returns a single window unchanged.
func splitIntoWindows(text string) []string {
	if text == "" {
		return nil
	}
	if len(text) <= transcriptSummarizeWindowChars {
		return []string{text}
	}
	const ws = " \t\n\r"
	var windows []string
	start := 0
	for start < len(text) {
		endCut := start + transcriptSummarizeWindowChars
		if endCut >= len(text) {
			// Final window: the remainder, unmodified.
			windows = append(windows, text[start:])
			break
		}
		// Whitespace backoff: walk backwards from the boundary looking
		// for a separator to end the window just before.
		cut := endCut
		for back := 0; cut > start+1 && back < transcriptSummarizeBackoffChars; back++ {
			if strings.IndexByte(ws, text[cut-1]) >= 0 {
				cut--
				break
			}
			cut--
		}
		w := strings.TrimRight(text[start:cut], ws)
		if w == "" {
			// Degenerate whitespace-only span: skip it, keep scanning.
			start = cut
			continue
		}
		windows = append(windows, w)
		// Next window starts overlap chars before this window's end
		// (measured on the trimmed window, so the overlap is exact).
		next := start + len(w) - transcriptSummarizeOverlapChars
		if next <= start {
			// Degenerate window shorter than the overlap: advance
			// without repetition rather than stalling.
			next = start + len(w)
		}
		start = next
	}
	return windows
}

// resolveOutputPath resolves a raw output_path against the session
// working directory (tools.WorkingDirFromContext), falling back to the
// tool's configured fallback root when no working dir is present. "~"
// expands to the user's home directory. Empty input resolves to ""
// (no file). No os.Getwd anywhere: the daemon carries the working dir
// through the context (AGENTS.md).
func (t *TranscriptFetchTool) resolveOutputPath(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	p := raw
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("transcript_fetch: cannot expand %q: home directory unknown", raw)
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		if wd := tools.WorkingDirFromContext(ctx); wd != "" {
			p = filepath.Join(wd, p)
		} else if t.fallbackOutputDir != "" {
			root := t.fallbackOutputDir
			if strings.HasPrefix(root, "~") {
				home, err := os.UserHomeDir()
				if err != nil {
					return "", fmt.Errorf("transcript_fetch: cannot resolve fallback output dir: %w", err)
				}
				root = filepath.Join(home, root[1:])
			}
			p = filepath.Join(root, p)
		}
	}
	return p, nil
}

// writeTranscriptFile creates the parent directory (0o755) and writes
// text (0o644). Errors are wrapped with the path for actionable reports.
func writeTranscriptFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("transcript_fetch: creating output directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("transcript_fetch: writing transcript to %s: %w", path, err)
	}
	return nil
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
