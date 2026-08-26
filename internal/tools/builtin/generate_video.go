package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

const toolGenerateVideo = "generate_video"

// GenerateVideoTool creates video clips through a models.json5 video model.
type GenerateVideoTool struct {
	tools.ToolDefaults
	client *mediaClient
}

// NewGenerateVideoTool creates a generate_video tool.
func NewGenerateVideoTool(resolver *llm.Resolver, outputDir string, timeout time.Duration) *GenerateVideoTool {
	return &GenerateVideoTool{client: newMediaClient(resolver, outputDir, timeout)}
}

// SetTokenResolver wires OAuth access tokens (nil-safe per repo rule).
func (t *GenerateVideoTool) SetTokenResolver(tr llm.TokenResolver) {
	if t != nil && t.client != nil {
		t.client.SetTokenResolver(tr)
	}
}

func (t *GenerateVideoTool) Name() string { return toolGenerateVideo }

func (t *GenerateVideoTool) Category() string { return "media" }

func (t *GenerateVideoTool) Description() string {
	return "Generate a video clip from a text prompt using a models.json5 model (provider/id or alias) with capability video. Empty model uses video_model. Returns the saved file path. Always enhance the prompt first."
}

func (t *GenerateVideoTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPrompt: {
				Type:        schemaTypeString,
				Description: "Enhanced clip prompt. One beat, under the model duration cap.",
			},
			schemaPropModel: {
				Type:        schemaTypeString,
				Description: "Model ref (xai/grok-imagine-video, comfyui/wan) or alias. Empty = video_model slot.",
			},
			schemaPropAspectRatio: {
				Type:        schemaTypeString,
				Description: "Aspect ratio such as 16:9 or 9:16.",
			},
			schemaPropDurationS: {
				Type:        schemaTypeInteger,
				Description: "Clip length in seconds. Default 8.",
			},
			schemaPropOutputPath: {
				Type:        schemaTypeString,
				Description: "Optional destination path. Relative paths resolve against the session working dir.",
			},
		},
		Required: []string{schemaPropPrompt},
	}
}

func (t *GenerateVideoTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	prompt, _ := args[schemaPropPrompt].(string)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	duration := 8
	if v, ok := args[schemaPropDurationS].(float64); ok && v > 0 {
		duration = int(v)
	}
	req := mediaRequest{
		Kind:        mediaKindVideo,
		Prompt:      prompt,
		Model:       stringArg(args, schemaPropModel),
		AspectRatio: stringArg(args, schemaPropAspectRatio),
		DurationS:   duration,
		OutputPath:  stringArg(args, schemaPropOutputPath),
		N:           1,
	}
	return t.client.Generate(ctx, req)
}

func (t *GenerateVideoTool) IsConcurrencySafe(map[string]any) bool { return true }
