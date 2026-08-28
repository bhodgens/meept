package daemon

import (
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// applyGBNFConstrainedFromConfig pushes [agent.tools].gbnf_constrained
// onto the llm package kill-switch. Called once from NewComponents.
func applyGBNFConstrainedFromConfig(cfg config.AgentToolsConfig) {
	llm.SetGBNFConstrained(cfg.GBNFConstrained)
}
