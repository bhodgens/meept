package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

const (
	mediaKindImage      = "image"
	mediaKindVideo      = "video"
	maxMediaBytes       = 80 * 1024 * 1024
	defaultMediaTimeout = 180 * time.Second
)

type mediaRequest struct {
	Kind        string
	Prompt      string
	Model       string
	AspectRatio string
	DurationS   int
	N           int
	OutputPath  string
}

type mediaArtifact struct {
	URL      string
	Base64   string
	Bytes    []byte
	MIME     string
	Filename string
}

type mediaClient struct {
	resolver  *llm.Resolver
	outputDir string
	client    *http.Client
	// tokenResolver supplies OAuth access tokens for models with an
	// OAuthProvider set (e.g. xai-oauth / SuperGrok).
	tokenResolver llm.TokenResolver
}

func newMediaClient(resolver *llm.Resolver, outputDir string, timeout time.Duration) *mediaClient {
	if timeout <= 0 {
		timeout = defaultMediaTimeout
	}
	return &mediaClient{
		resolver:  resolver,
		outputDir: outputDir,
		client:    &http.Client{Timeout: timeout},
	}
}

// SetTokenResolver wires the OAuth token resolver (nil-safe per repo rule).
func (c *mediaClient) SetTokenResolver(tr llm.TokenResolver) {
	if tr != nil {
		c.tokenResolver = tr
	}
}

func (c *mediaClient) Generate(ctx context.Context, req mediaRequest) (any, error) {
	if c.resolver == nil {
		return nil, fmt.Errorf("model resolver is not configured")
	}
	mc, err := c.resolver.ResolveGeneration(req.Model, req.Kind)
	if err != nil {
		return nil, err
	}
	if req.N <= 0 {
		req.N = 1
	}
	if shouldEnhancePrompt(req.Prompt) {
		if enhanced, enhErr := enhanceMediaPrompt(ctx, c.resolver, req.Kind, req.Prompt); enhErr == nil && strings.TrimSpace(enhanced) != "" {
			req.Prompt = enhanced
		}
	}
	art, err := c.dispatch(ctx, req, mc)
	if err != nil {
		return nil, err
	}
	path, err := c.save(ctx, req, art)
	if err != nil {
		return nil, err
	}
	ref := mc.CatalogRef
	if ref == "" {
		ref = mc.ProviderID + "/" + mc.ModelID
	}
	result := map[string]any{
		"success":      true,
		"model":        ref,
		"kind":         req.Kind,
		"path":         path,
		"prompt":       req.Prompt,
		"aspect_ratio": req.AspectRatio,
		"transport":    mc.GenerationTransport(),
	}
	if art.URL != "" {
		result["url"] = art.URL
	}
	tr := tools.NewSuccessResult(result)
	tr.TaintLabel = taint.TaintExternal
	return tr, nil
}

func (c *mediaClient) dispatch(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	kind := mc.GenerationTransport()
	if kind == "openai_images" && req.Kind == mediaKindVideo {
		kind = "openai_videos"
	}
	if kind == "openai_videos" && req.Kind == mediaKindImage {
		kind = "openai_images"
	}
	switch kind {
	case "openai_images":
		return c.runOpenAIImages(ctx, req, mc)
	case "openai_videos":
		return c.runOpenAIVideos(ctx, req, mc)
	case "gemini":
		return c.runGeminiImage(ctx, req, mc)
	case "http":
		return c.runHTTP(ctx, req, mc)
	case "comfyui":
		return c.runComfy(ctx, req, mc)
	case "infsh":
		return c.runInfsh(ctx, req, mc)
	default:
		return nil, fmt.Errorf("model %s/%s has no generation transport (set api on the model or provider)", mc.ProviderID, mc.ModelID)
	}
}

func (c *mediaClient) save(ctx context.Context, req mediaRequest, art *mediaArtifact) (string, error) {
	data := art.Bytes
	if len(data) == 0 && art.Base64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(art.Base64)
		if err != nil {
			return "", fmt.Errorf("decode image: %w", err)
		}
		data = decoded
	}
	if len(data) == 0 && art.URL != "" {
		body, err := c.download(ctx, art.URL)
		if err != nil {
			return "", err
		}
		data = body
	}
	if len(data) == 0 {
		return "", fmt.Errorf("provider returned no image or video bytes")
	}

	path, err := resolveMediaOutputPath(ctx, c.outputDir, req, art)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create media dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write media file: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func (c *mediaClient) download(ctx context.Context, rawURL string) ([]byte, error) {
	if err := checkURL(rawURL); err != nil {
		return nil, fmt.Errorf("download blocked: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	resp, err := c.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	return readLimited(resp.Body, maxMediaBytes)
}

func (c *mediaClient) doJSON(ctx context.Context, method, rawURL string, headers map[string]string, body any) (map[string]any, int, error) {
	var rdr io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, 0, err
	}
	raw, err := readLimited(resp.Body, maxMediaBytes)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(raw) == 0 {
		return map[string]any{}, resp.StatusCode, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	return out, resp.StatusCode, nil
}

func resolveMediaOutputPath(ctx context.Context, outputDir string, req mediaRequest, art *mediaArtifact) (string, error) {
	ext := mediaExt(req.Kind, art)
	if req.OutputPath != "" {
		p := req.OutputPath
		if !filepath.IsAbs(p) {
			if wd := tools.WorkingDirFromContext(ctx); wd != "" {
				p = filepath.Join(wd, p)
			} else if outputDir != "" {
				p = filepath.Join(outputDir, p)
			}
		}
		if filepath.Ext(p) == "" {
			p += ext
		}
		return constrainMediaPath(p, outputDir, tools.WorkingDirFromContext(ctx))
	}
	dir := outputDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("media output_dir is empty and home is unknown")
		}
		dir = filepath.Join(home, ".meept", "media")
	}
	name := art.Filename
	if name == "" {
		name = id.Generate(req.Kind+"-") + ext
	} else if filepath.Ext(name) == "" {
		name += ext
	}
	return filepath.Join(dir, filepath.Base(name)), nil
}

func constrainMediaPath(p, outputDir, workDir string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	var roots []string
	for _, root := range []string{outputDir, workDir} {
		if root == "" {
			continue
		}
		r, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		roots = append(roots, r)
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("output_path %q rejected: no media.output_dir or session working dir", p)
	}
	for _, root := range roots {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("output_path %q is outside media.output_dir and the session working dir", p)
}

func mediaExt(kind string, art *mediaArtifact) string {
	mime := strings.ToLower(art.MIME)
	switch {
	case strings.Contains(mime, "jpeg"), strings.Contains(mime, "jpg"):
		return ".jpg"
	case strings.Contains(mime, "webp"):
		return ".webp"
	case strings.Contains(mime, "gif"):
		return ".gif"
	case strings.Contains(mime, "mp4"):
		return ".mp4"
	case strings.Contains(mime, "webm"):
		return ".webm"
	case kind == mediaKindVideo:
		return ".mp4"
	default:
		return ".png"
	}
}

func modelAuthHeaders(ctx context.Context, mc *llm.ModelConfig, tr llm.TokenResolver) (map[string]string, error) {
	headers := map[string]string{}
	for k, v := range mc.ExtraHeaders {
		headers[k] = v
	}
	if mc.OAuthProvider != "" && tr != nil {
		tok, err := tr.ResolveToken(ctx, mc.OAuthProvider)
		if err != nil {
			return nil, fmt.Errorf("oauth token for %s: %w", mc.OAuthProvider, err)
		}
		if tok != "" {
			headers["Authorization"] = "Bearer " + tok
			return headers, nil
		}
	}
	if mc.APIKey != "" {
		if _, ok := headers["Authorization"]; !ok {
			headers["Authorization"] = "Bearer " + mc.APIKey
		}
	}
	return headers, nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func shouldEnhancePrompt(prompt string) bool {
	n := 0
	for _, f := range strings.Fields(prompt) {
		if f != "" {
			n++
		}
	}
	return n > 0 && n < 15
}

func enhanceMediaPrompt(ctx context.Context, resolver *llm.Resolver, kind, prompt string) (string, error) {
	if resolver == nil {
		return prompt, nil
	}
	small := resolver.SmallModel()
	if small == nil {
		if mc, err := resolver.ResolveForAlias("small"); err == nil {
			small = mc
		}
	}
	if small == nil || small.BaseURL == "" {
		return prompt, nil
	}
	client := llm.NewClient(small)
	sys := "Expand this " + kind + " brief into a concrete generator prompt. One paragraph. Concrete nouns. No masterpiece/8k filler. Output only the prompt."
	resp, err := client.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: sys},
		{Role: llm.RoleUser, Content: prompt},
	})
	if err != nil {
		return prompt, err
	}
	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return prompt, nil
	}
	return out, nil
}
