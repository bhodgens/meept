package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

const toolGenerateImage = "generate_image"

// GenerateImageTool creates still images through a models.json5 image model.
type GenerateImageTool struct {
	tools.ToolDefaults
	client *mediaClient
}

// NewGenerateImageTool creates a generate_image tool.
func NewGenerateImageTool(resolver *llm.Resolver, outputDir string, timeout time.Duration) *GenerateImageTool {
	return &GenerateImageTool{client: newMediaClient(resolver, outputDir, timeout)}
}

// SetTokenResolver wires OAuth access tokens (nil-safe per repo rule).
func (t *GenerateImageTool) SetTokenResolver(tr llm.TokenResolver) {
	if t != nil && t.client != nil {
		t.client.SetTokenResolver(tr)
	}
}

func (t *GenerateImageTool) Name() string { return toolGenerateImage }

func (t *GenerateImageTool) Category() string { return "media" }

func (t *GenerateImageTool) Description() string {
	return "Generate an image from a text prompt using a models.json5 model (provider/id or alias) with capability image. Empty model uses image_model. Returns the saved file path. Always enhance the prompt first."
}

func (t *GenerateImageTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPrompt: {
				Type:        schemaTypeString,
				Description: "Enhanced image prompt. Do not send a raw one-line brief.",
			},
			schemaPropModel: {
				Type:        schemaTypeString,
				Description: "Model ref (xai/grok-imagine-image-2.0, comfyui/flux-dev, google/nano-banana) or alias. Empty = image_model slot.",
			},
			schemaPropAspectRatio: {
				Type:        schemaTypeString,
				Description: "Aspect ratio such as 1:1, 16:9, 9:16.",
			},
			schemaPropOutputPath: {
				Type:        schemaTypeString,
				Description: "Optional destination path. Relative paths resolve against the session working dir.",
			},
		},
		Required: []string{schemaPropPrompt},
	}
}

func (t *GenerateImageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	prompt, _ := args[schemaPropPrompt].(string)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	req := mediaRequest{
		Kind:        mediaKindImage,
		Prompt:      prompt,
		Model:       stringArg(args, schemaPropModel),
		AspectRatio: stringArg(args, schemaPropAspectRatio),
		OutputPath:  stringArg(args, schemaPropOutputPath),
		N:           1,
	}
	return t.client.Generate(ctx, req)
}

func (t *GenerateImageTool) IsConcurrencySafe(map[string]any) bool { return true }

func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}
