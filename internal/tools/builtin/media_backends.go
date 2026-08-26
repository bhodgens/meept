package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func (c *mediaClient) runOpenAIImages(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	body := map[string]any{
		"prompt": req.Prompt,
		"n":      req.N,
	}
	if mc.ModelID != "" {
		body["model"] = mc.ModelID
	}
	if req.AspectRatio != "" {
		body["aspect_ratio"] = req.AspectRatio
	}
	base := strings.TrimRight(mc.BaseURL, "/")
	headers, err := modelAuthHeaders(ctx, mc, c.tokenResolver)
	if err != nil {
		return nil, err
	}
	out, status, err := c.doJSON(ctx, http.MethodPost, base+"/images/generations", headers, body)
	if err != nil {
		return nil, fmt.Errorf("openai_images: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("openai_images: HTTP %d: %s", status, compactJSON(out))
	}
	return artifactFromOpenAI(out)
}

func (c *mediaClient) runOpenAIVideos(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	body := map[string]any{
		"prompt": req.Prompt,
	}
	if mc.ModelID != "" {
		body["model"] = mc.ModelID
	}
	if req.AspectRatio != "" {
		body["aspect_ratio"] = req.AspectRatio
	}
	if req.DurationS > 0 {
		body["duration"] = req.DurationS
	}
	base := strings.TrimRight(mc.BaseURL, "/")
	headers, err := modelAuthHeaders(ctx, mc, c.tokenResolver)
	if err != nil {
		return nil, err
	}
	out, status, err := c.doJSON(ctx, http.MethodPost, base+"/videos/generations", headers, body)
	if err != nil {
		return nil, fmt.Errorf("openai_videos: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("openai_videos: HTTP %d: %s", status, compactJSON(out))
	}
	if art, err := artifactFromOpenAI(out); err == nil {
		return art, nil
	}
	reqID, ok := lookupString(out, "request_id")
	if !ok {
		reqID, ok = lookupString(out, "id")
	}
	if !ok {
		return nil, fmt.Errorf("openai_videos: no url or request_id: %s", compactJSON(out))
	}
	return c.pollOpenAIVideo(ctx, base, reqID, headers)
}

func (c *mediaClient) pollOpenAIVideo(ctx context.Context, base, reqID string, headers map[string]string) (*mediaArtifact, error) {
	deadline := time.Now().Add(c.client.Timeout)
	if c.client.Timeout <= 0 {
		deadline = time.Now().Add(3 * time.Minute)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("openai_videos: timed out waiting for %s", reqID)
		}
		out, status, err := c.doJSON(ctx, http.MethodGet, base+"/videos/"+reqID, headers, nil)
		if err != nil {
			return nil, fmt.Errorf("openai_videos poll: %w", err)
		}
		if status >= 200 && status < 300 {
			if art, aerr := artifactFromOpenAI(out); aerr == nil {
				return art, nil
			}
			if url, ok := lookupString(out, "video.url"); ok {
				return &mediaArtifact{URL: url, MIME: "video/mp4"}, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func artifactFromOpenAI(out map[string]any) (*mediaArtifact, error) {
	if url, ok := lookupString(out, "data.0.url"); ok {
		return &mediaArtifact{URL: url}, nil
	}
	if b64, ok := lookupString(out, "data.0.b64_json"); ok {
		return &mediaArtifact{Base64: b64}, nil
	}
	if url, ok := lookupString(out, "url"); ok {
		return &mediaArtifact{URL: url}, nil
	}
	return nil, fmt.Errorf("response missing data[0].url or data[0].b64_json: %s", compactJSON(out))
}

func (c *mediaClient) runGeminiImage(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	if req.Kind != mediaKindImage {
		return nil, fmt.Errorf("gemini transport only supports image generation")
	}
	if mc.ModelID == "" {
		return nil, fmt.Errorf("gemini: model name is required")
	}
	headers, err := modelAuthHeaders(ctx, mc, c.tokenResolver)
	if err != nil {
		return nil, err
	}
	delete(headers, "Authorization")
	if mc.APIKey != "" {
		headers["x-goog-api-key"] = mc.APIKey
	}
	base := strings.TrimRight(mc.BaseURL, "/")
	url := base + "/models/" + mc.ModelID + ":generateContent"
	body := map[string]any{
		"contents": []any{
			map[string]any{
				"parts": []any{map[string]any{"text": req.Prompt}},
			},
		},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}
	out, status, err := c.doJSON(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("gemini: HTTP %d: %s", status, compactJSON(out))
	}
	art := extractGeminiInline(out)
	if art == nil {
		return nil, fmt.Errorf("gemini: no inline image in response: %s", compactJSON(out))
	}
	return art, nil
}

func extractGeminiInline(out map[string]any) *mediaArtifact {
	cands, _ := out["candidates"].([]any)
	for _, cand := range cands {
		cm, _ := cand.(map[string]any)
		content, _ := cm["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, part := range parts {
			pm, _ := part.(map[string]any)
			inline, _ := pm["inlineData"].(map[string]any)
			if inline == nil {
				inline, _ /* snake_case alias */ = pm["inline_data"].(map[string]any)
			}
			if inline == nil {
				continue
			}
			data, _ := inline["data"].(string)
			mime, _ := inline["mimeType"].(string)
			if mime == "" {
				if alt, ok := inline["mime_type"].(string); ok {
					mime = alt
				}
			}
			if data != "" {
				return &mediaArtifact{Base64: data, MIME: mime}
			}
		}
	}
	return nil
}

func (c *mediaClient) runHTTP(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	method := http.MethodPost
	rawURL := mc.GenerationURL
	if rawURL == "" {
		rawURL = strings.TrimRight(mc.BaseURL, "/")
	}
	if rawURL == "" {
		return nil, fmt.Errorf("http model needs generation_url or provider baseURL")
	}
	vars := templateVars(req, mc.ModelID)
	rawURL = applyTemplate(rawURL, vars)
	var body any
	if mc.BodyTemplate != nil {
		body = applyTemplateValue(mc.BodyTemplate, vars)
	}
	headers, err := modelAuthHeaders(ctx, mc, c.tokenResolver)
	if err != nil {
		return nil, err
	}
	out, status, err := c.doJSON(ctx, method, rawURL, headers, body)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("http: HTTP %d: %s", status, compactJSON(out))
	}
	urlPath := mc.ResponseURLPath
	if urlPath == "" {
		urlPath = "data.0.url"
	}
	b64Path := mc.ResponseB64Path
	if b64Path == "" {
		b64Path = "data.0.b64_json"
	}
	if u, ok := lookupString(out, urlPath); ok {
		return &mediaArtifact{URL: u}, nil
	}
	if b, ok := lookupString(out, b64Path); ok {
		return &mediaArtifact{Base64: b}, nil
	}
	return nil, fmt.Errorf("http: no value at %s or %s: %s", urlPath, b64Path, compactJSON(out))
}

func (c *mediaClient) runComfy(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	if mc.Workflow == "" {
		return nil, fmt.Errorf("comfyui: set workflow on the model in models.json5")
	}
	raw, err := os.ReadFile(mc.Workflow)
	if err != nil {
		return nil, fmt.Errorf("comfyui: read workflow: %w", err)
	}
	var workflow map[string]any
	if err := json.Unmarshal(raw, &workflow); err != nil {
		return nil, fmt.Errorf("comfyui: parse workflow: %w", err)
	}
	injectComfyPrompt(workflow, req.Prompt)
	base := strings.TrimRight(mc.BaseURL, "/")
	body := map[string]any{"prompt": workflow}
	out, status, err := c.doJSON(ctx, http.MethodPost, base+"/prompt", map[string]string{}, body)
	if err != nil {
		return nil, fmt.Errorf("comfyui: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("comfyui: HTTP %d: %s", status, compactJSON(out))
	}
	promptID, _ := lookupString(out, "prompt_id")
	if promptID == "" {
		return nil, fmt.Errorf("comfyui: no prompt_id: %s", compactJSON(out))
	}
	deadline := time.Now().Add(c.client.Timeout)
	if deadline.Before(time.Now().Add(30 * time.Second)) {
		deadline = time.Now().Add(2 * time.Minute)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("comfyui: timed out waiting for %s", promptID)
		}
		hist, hStatus, err := c.doJSON(ctx, http.MethodGet, base+"/history/"+promptID, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("comfyui history: %w", err)
		}
		if hStatus == http.StatusOK {
			if art := extractComfyOutput(hist, promptID); art != nil {
				if art.URL != "" && !strings.HasPrefix(art.URL, "http") {
					art.URL = base + art.URL
				}
				return art, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func injectComfyPrompt(workflow map[string]any, prompt string) {
	type candidate struct {
		inputs map[string]any
		neg    bool
	}
	var found []candidate
	for _, node := range workflow {
		nm, ok := node.(map[string]any)
		if !ok {
			continue
		}
		inputs, _ := nm["inputs"].(map[string]any)
		if inputs == nil {
			continue
		}
		if _, has := inputs["text"]; !has {
			continue
		}
		blob := strings.ToLower(fmt.Sprint(nm["class_type"], nm["_meta"], nm["title"], inputs["text"]))
		neg := strings.Contains(blob, "negative")
		classType, _ := nm["class_type"].(string)
		if strings.Contains(strings.ToLower(classType), "cliptextencode") || strings.TrimSpace(fmt.Sprint(inputs["text"])) != "" {
			found = append(found, candidate{inputs: inputs, neg: neg})
		}
	}
	for _, c := range found {
		if !c.neg {
			c.inputs["text"] = prompt
			return
		}
	}
	if len(found) > 0 {
		found[0].inputs["text"] = prompt
	}
}

func extractComfyOutput(hist map[string]any, promptID string) *mediaArtifact {
	entry, _ := hist[promptID].(map[string]any)
	if entry == nil {
		entry = hist
	}
	outputs, _ := entry["outputs"].(map[string]any)
	for _, nodeOut := range outputs {
		om, _ := nodeOut.(map[string]any)
		for _, key := range []string{"images", "gifs", "video"} {
			items, _ := om[key].([]any)
			if len(items) == 0 {
				continue
			}
			first, _ := items[0].(map[string]any)
			filename, _ := first["filename"].(string)
			sub, _ := first["subfolder"].(string)
			typ, _ := first["type"].(string)
			if typ == "" {
				typ = "output"
			}
			if filename == "" {
				continue
			}
			view := "/view?filename=" + filename + "&type=" + typ
			if sub != "" {
				view += "&subfolder=" + sub
			}
			return &mediaArtifact{URL: view, Filename: filename}
		}
	}
	return nil
}

func (c *mediaClient) runInfsh(ctx context.Context, req mediaRequest, mc *llm.ModelConfig) (*mediaArtifact, error) {
	app := mc.ImageApp
	if req.Kind == mediaKindVideo {
		app = mc.VideoApp
	}
	if app == "" {
		app = mc.ModelID
	}
	if app == "" {
		return nil, fmt.Errorf("infsh: set image_app/video_app or name on the model")
	}
	input := map[string]any{"prompt": req.Prompt}
	if req.AspectRatio != "" {
		input["aspect_ratio"] = req.AspectRatio
	}
	if req.DurationS > 0 && req.Kind == mediaKindVideo {
		input["duration"] = req.DurationS
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("infsh encode: %w", err)
	}
	cmd := exec.CommandContext(ctx, "infsh", "app", "run", app, "--input", string(encoded), "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("infsh: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var parsed any
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("infsh: parse json: %w", err)
	}
	if u := firstURL(parsed); u != "" {
		return &mediaArtifact{URL: u}, nil
	}
	return nil, fmt.Errorf("infsh: no URL in output")
}

func templateVars(req mediaRequest, model string) map[string]string {
	return map[string]string{
		"prompt":       req.Prompt,
		"model":        model,
		"aspect_ratio": req.AspectRatio,
		"duration_s":   strconv.Itoa(req.DurationS),
		"n":            strconv.Itoa(req.N),
		"kind":         req.Kind,
	}
}

func applyTemplate(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

func applyTemplateValue(v any, vars map[string]string) any {
	switch t := v.(type) {
	case string:
		return applyTemplate(t, vars)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = applyTemplateValue(child, vars)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = applyTemplateValue(child, vars)
		}
		return out
	default:
		return v
	}
}

func lookupString(root any, path string) (string, bool) {
	cur := root
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return "", false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok && s != ""
}

func firstURL(v any) string {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			return t
		}
	case map[string]any:
		for _, key := range []string{"url", "output", "image", "video", "uri"} {
			if s, ok := t[key].(string); ok && strings.HasPrefix(s, "http") {
				return s
			}
		}
		for _, child := range t {
			if u := firstURL(child); u != "" {
				return u
			}
		}
	case []any:
		for _, child := range t {
			if u := firstURL(child); u != "" {
				return u
			}
		}
	}
	return ""
}

func compactJSON(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	if len(b) > 400 {
		return string(b[:400]) + "…"
	}
	return string(b)
}
