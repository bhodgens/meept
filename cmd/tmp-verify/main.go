package main

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/llm"
)

// tmp-verify is a one-off check that config/models.json5 parses with the
// opencode extra_headers sentinel intact after the ${session_id} placeholder
// preservation change. Kept because direct rm was declined; owner deletes it.
func main() {
	cfg, err := llm.LoadProvidersConfig("config/models.json5")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load providers config:", err)
		os.Exit(1)
	}
	mc := llm.ResolveModelRef("opencode/kimi-k2.6", cfg)
	if mc == nil {
		fmt.Fprintln(os.Stderr, "resolve opencode/kimi-k2.6: nil model config")
		os.Exit(1)
	}
	fmt.Printf("opencode/kimi-k2.6 extra_headers=%v baseURL=%s\n", mc.ExtraHeaders, mc.BaseURL)
}
