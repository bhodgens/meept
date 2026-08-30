package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

func main() {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load models: %v\n", err)
		os.Exit(1)
	}
	resolver := llm.NewResolver(cfg, nil)
	outDir := filepath.Join(os.Getenv("HOME"), ".meept", "media")
	tool := builtin.NewGenerateImageTool(resolver, outDir, 180*time.Second)

	// Wire OAuth token store (same path the daemon uses).
	enc, err := auth.NewEncryptionKey(os.Getenv("MEEPT_OAUTH_ENCRYPTION_KEY"))
	if err == nil {
		store := auth.NewTokenStore(enc)
		if err := store.Init(); err == nil {
			tool.SetTokenResolver(store)
		}
	}

	prompt := "A red fox standing in fresh snow, russet fur, amber eyes, winter forest edge, photorealistic wildlife photo, late afternoon light."
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}
	raw, err := tool.Execute(context.Background(), map[string]any{
		"prompt": prompt,
		"model":  "xai-oauth/grok-imagine-image-2.0",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate_image: %v\n", err)
		os.Exit(1)
	}
	if tr, ok := raw.(*tools.ToolResult); ok {
		b, _ := json.MarshalIndent(tr.Result, "", "  ")
		fmt.Println(string(b))
		if !tr.Success {
			os.Exit(1)
		}
		return
	}
	fmt.Printf("%v\n", raw)
}
