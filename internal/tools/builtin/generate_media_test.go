package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

func testImageResolver(t *testing.T, baseURL, modelAPI string) *llm.Resolver {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		ImageModel: "test/gen",
		VideoModel: "test/clip",
		Providers: map[string]llm.ProviderConfig{
			"test": {
				API:     modelAPI,
				Options: llm.ProviderOptionsConfig{BaseURL: baseURL},
				Models: map[string]llm.ModelDef{
					"gen": {
						Name:         "gen",
						Capabilities: []string{llm.CapImageGen},
						API:          modelAPI,
					},
					"clip": {
						Name:         "clip",
						Capabilities: []string{llm.CapVideoGen},
						API:          "openai_videos",
					},
				},
			},
		},
	}
	if modelAPI == "http" {
		gen := cfg.Providers["test"].Models["gen"]
		gen.GenerationURL = baseURL + "/gen"
		gen.BodyTemplate = map[string]any{"text": "{{prompt}}"}
		gen.ResponseB64Path = "data.0.b64_json"
		cfg.Providers["test"].Models["gen"] = gen
	}
	return llm.NewResolver(cfg, nil)
}

func TestGenerateImageTool_NameAndSchema(t *testing.T) {
	tool := NewGenerateImageTool(nil, t.TempDir(), time.Second)
	if tool.Name() != "generate_image" {
		t.Fatalf("Name = %q", tool.Name())
	}
	params := tool.Parameters()
	if _, ok := params.Properties[schemaPropModel]; !ok {
		t.Fatal("missing model property")
	}
}

func TestGenerateVideoTool_NameAndSchema(t *testing.T) {
	tool := NewGenerateVideoTool(nil, t.TempDir(), time.Second)
	if tool.Name() != "generate_video" {
		t.Fatalf("Name = %q", tool.Name())
	}
}

func TestGenerateImage_RequiresPrompt(t *testing.T) {
	tool := NewGenerateImageTool(testImageResolver(t, "http://127.0.0.1", "openai_images"), t.TempDir(), time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("expected prompt error, got %v", err)
	}
}

func TestGenerateImage_UnknownModel(t *testing.T) {
	tool := NewGenerateImageTool(testImageResolver(t, "http://127.0.0.1", "openai_images"), t.TempDir(), time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"prompt": "a fox",
		"model":  "nope/missing",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown generation model") {
		t.Fatalf("expected unknown model, got %v", err)
	}
}

func TestGenerateImage_OpenAICompat(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body["prompt"] != "a red fox" {
			t.Errorf("prompt = %v", body["prompt"])
		}
		png := []byte{0x89, 'P', 'N', 'G'}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(png)}},
		})
	}))
	t.Cleanup(srv.Close)

	tool := NewGenerateImageTool(testImageResolver(t, srv.URL, "openai_images"), dir, 5*time.Second)
	raw, err := tool.Execute(context.Background(), map[string]any{"prompt": "a red fox"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, ok := raw.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("result = %#v", raw)
	}
	result, _ := tr.Result.(map[string]any)
	path, _ := result["path"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) < 4 || string(data[:4]) != "\x89PNG" {
		t.Fatalf("unexpected bytes %q", data)
	}
}

func TestGenerateImage_HTTPTemplate(t *testing.T) {
	dir := t.TempDir()
	mux := http.NewServeMux()
	mux.HandleFunc("/gen", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body["text"] != "hello" {
			t.Errorf("text = %v", body["text"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString([]byte("fakepng"))}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tool := NewGenerateImageTool(testImageResolver(t, srv.URL, "http"), dir, 5*time.Second)
	raw, err := tool.Execute(context.Background(), map[string]any{"prompt": "hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := raw.(*tools.ToolResult)
	result := tr.Result.(map[string]any)
	got, err := os.ReadFile(result["path"].(string))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "fakepng" {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateVideo_RejectsImageOnlyModel(t *testing.T) {
	cfg := &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"test": {
				API: "openai_images",
				Models: map[string]llm.ModelDef{
					"gen": {Name: "gen", Capabilities: []string{llm.CapImageGen}},
				},
			},
		},
	}
	tool := NewGenerateVideoTool(llm.NewResolver(cfg, nil), t.TempDir(), time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"prompt": "train",
		"model":  "test/gen",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown generation model") {
		t.Fatalf("expected capability miss, got %v", err)
	}
}

func TestResolveMediaOutputPath_UsesWorkingDir(t *testing.T) {
	wd := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), wd)
	path, err := resolveMediaOutputPath(ctx, "", mediaRequest{
		Kind:       mediaKindImage,
		OutputPath: "out/fox",
	}, &mediaArtifact{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, wd) {
		t.Fatalf("path %q not under %q", path, wd)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("ext = %q", filepath.Ext(path))
	}
}

func TestConstrainMediaPath_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	_, err := constrainMediaPath(filepath.Join(root, "..", "outside.png"), root, "")
	if err == nil {
		t.Fatal("expected escape to be rejected")
	}
}

func TestInjectComfyPrompt_SkipsNegative(t *testing.T) {
	wf := map[string]any{
		"1": map[string]any{
			"class_type": "CLIPTextEncode",
			"title":      "negative",
			"inputs":     map[string]any{"text": "bad"},
		},
		"2": map[string]any{
			"class_type": "CLIPTextEncode",
			"title":      "positive",
			"inputs":     map[string]any{"text": "old"},
		},
	}
	injectComfyPrompt(wf, "fox")
	pos := wf["2"].(map[string]any)["inputs"].(map[string]any)["text"]
	neg := wf["1"].(map[string]any)["inputs"].(map[string]any)["text"]
	if pos != "fox" {
		t.Fatalf("positive = %v", pos)
	}
	if neg != "bad" {
		t.Fatalf("negative overwritten: %v", neg)
	}
}

func TestMergeProvidersConfig_KeepsBundledImageModel(t *testing.T) {
	base := &llm.ProvidersConfig{
		ImageModel: "xai/grok-imagine-image-2.0",
		Providers: map[string]llm.ProviderConfig{
			"xai": {API: "openai", Models: map[string]llm.ModelDef{
				"grok-imagine-image-2.0": {Name: "grok-imagine-image-2.0", Capabilities: []string{llm.CapImageGen}},
			}},
			"local": {API: "openai", Models: map[string]llm.ModelDef{
				"old": {Name: "old", Capabilities: []string{"completion"}},
			}},
		},
	}
	user := &llm.ProvidersConfig{
		Model: "zai/glm-5.2",
		Providers: map[string]llm.ProviderConfig{
			"local": {Models: map[string]llm.ModelDef{
				"new": {Name: "new", Capabilities: []string{"completion"}},
			}},
		},
	}
	got := llm.MergeProvidersConfig(base, user)
	if got.ImageModel != "xai/grok-imagine-image-2.0" {
		t.Fatalf("image_model lost: %q", got.ImageModel)
	}
	if got.Model != "zai/glm-5.2" {
		t.Fatalf("user model slot not applied: %q", got.Model)
	}
	if _, ok := got.Providers["xai"].Models["grok-imagine-image-2.0"]; !ok {
		t.Fatal("bundled xai model missing")
	}
	if _, ok := got.Providers["local"].Models["old"]; !ok {
		t.Fatal("bundled local/old missing")
	}
	if _, ok := got.Providers["local"].Models["new"]; !ok {
		t.Fatal("user local/new missing")
	}
}

func TestResolveGeneration_WalksAlias(t *testing.T) {
	cfg := &llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			"image": {Models: []string{"local/chat", "local/flux"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API: "comfyui",
				Models: map[string]llm.ModelDef{
					"chat": {Name: "chat", Capabilities: []string{"completion"}},
					"flux": {Name: "flux", Capabilities: []string{llm.CapImageGen}},
				},
			},
		},
	}
	r := llm.NewResolver(cfg, nil)
	mc, err := r.ResolveGeneration("image", "image")
	if err != nil {
		t.Fatal(err)
	}
	if mc.ModelID != "flux" {
		t.Fatalf("got %q, want flux", mc.ModelID)
	}
}

func TestExtractGeminiInline(t *testing.T) {
	out := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"parts": []any{
						map[string]any{"text": "ok"},
						map[string]any{"inlineData": map[string]any{"data": "QUJD", "mimeType": "image/png"}},
					},
				},
			},
		},
	}
	art := extractGeminiInline(out)
	if art == nil || art.Base64 != "QUJD" {
		t.Fatalf("art = %#v", art)
	}
}

func TestResolveGeneration_UsesSlotAndAlias(t *testing.T) {
	cfg := &llm.ProvidersConfig{
		ImageModel: "image",
		ModelAliases: map[string]llm.ModelAliasEntry{
			"image": {Models: []string{"local/flux"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "comfyui",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8188"},
				Models: map[string]llm.ModelDef{
					"flux": {Name: "flux-dev", Capabilities: []string{llm.CapImageGen}, Workflow: "/tmp/w.json"},
				},
			},
		},
	}
	r := llm.NewResolver(cfg, nil)
	mc, err := r.ResolveGeneration("", "image")
	if err != nil {
		t.Fatal(err)
	}
	if mc.ModelID != "flux-dev" || mc.GenerationTransport() != "comfyui" {
		t.Fatalf("got %#v transport=%s", mc, mc.GenerationTransport())
	}
	if mc.Workflow != "/tmp/w.json" {
		t.Fatalf("workflow = %q", mc.Workflow)
	}
}
