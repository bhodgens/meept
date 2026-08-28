package rpc

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/preferences"
)

// TestInstructionResponseShapeMatchesCLI proves the JSON envelope produced by
// the instruction RPC handlers decodes into the CLI's structs
// (cmd/meept/instructions.go InstructionListResponse/InstructionResponse/
// ParsedInstructionCLI). Regression guard: preferences types previously had
// no json tags, so encoding/json emitted Go field names ("ID", "ActionArgs")
// and the CLI decoded empty rows even on a successful call.
func TestInstructionResponseShapeMatchesCLI(t *testing.T) {
	t.Parallel()

	parsed := &preferences.ParsedInstruction{
		Trigger: preferences.TriggerConfig{
			Type:    "intent",
			Pattern: "run tests",
		},
		Action: preferences.ActionConfig{
			Tool: "shell",
			Args: map[string]any{"command": "go test ./..."},
		},
		Scope:      "project",
		Priority:   "normal",
		Confidence: 0.9,
	}
	instr := &preferences.UserInstruction{
		ID:       "instr_abc",
		Name:     "run tests",
		Trigger:  "intent:run tests",
		Action:   "shell",
		Enabled:  true,
		Scope:    "project",
		Priority: "normal",
	}

	// List envelope.
	listJSON, err := json.Marshal(InstructionResponse{
		Success:      true,
		Instructions: []*preferences.UserInstruction{instr},
	})
	if err != nil {
		t.Fatalf("marshal list response: %v", err)
	}

	// CLI-side list shape (mirrors cmd/meept/instructions.go; kept in sync
	// by this test rather than importing cmd).
	var cliList struct {
		Success      bool `json:"success"`
		Instructions []struct {
			ID       string `json:"id"`
			Trigger  string `json:"trigger"`
			Action   string `json:"action"`
			Enabled  bool   `json:"enabled"`
			Scope    string `json:"scope"`
			Priority string `json:"priority"`
		} `json:"instructions"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(listJSON, &cliList); err != nil {
		t.Fatalf("CLI list decode failed: %v", err)
	}
	if !cliList.Success || len(cliList.Instructions) != 1 {
		t.Fatalf("unexpected list envelope: success=%v n=%d", cliList.Success, len(cliList.Instructions))
	}
	got := cliList.Instructions[0]
	if got.ID != "instr_abc" || got.Trigger != "intent:run tests" || got.Action != "shell" ||
		got.Scope != "project" || got.Priority != "normal" || !got.Enabled {
		t.Fatalf("instruction fields lost in JSON round-trip: %+v", got)
	}

	// Preview/add envelope with a parsed instruction.
	pvJSON, err := json.Marshal(InstructionResponse{
		Success:              true,
		ParsedInstruction:    parsed,
		ConfirmationRequired: true,
	})
	if err != nil {
		t.Fatalf("marshal preview response: %v", err)
	}
	var cliParsed struct {
		Success bool `json:"success"`
		Parsed  struct {
			Trigger struct {
				Type    string `json:"type"`
				Pattern string `json:"pattern"`
			} `json:"trigger"`
			Action struct {
				Tool string         `json:"tool"`
				Args map[string]any `json:"args"`
			} `json:"action"`
			Scope      string  `json:"scope"`
			Priority   string  `json:"priority"`
			Confidence float64 `json:"confidence"`
		} `json:"parsed"`
		ConfirmationRequired bool `json:"confirmation_required"`
	}
	if err := json.Unmarshal(pvJSON, &cliParsed); err != nil {
		t.Fatalf("CLI preview decode failed: %v", err)
	}
	if !cliParsed.Success || !cliParsed.ConfirmationRequired {
		t.Fatalf("unexpected preview envelope: %+v", cliParsed)
	}
	if cliParsed.Parsed.Trigger.Type != "intent" || cliParsed.Parsed.Trigger.Pattern != "run tests" ||
		cliParsed.Parsed.Action.Tool != "shell" || cliParsed.Parsed.Action.Args["command"] != "go test ./..." {
		t.Fatalf("parsed fields lost in JSON round-trip: %+v", cliParsed.Parsed)
	}
	if cliParsed.Parsed.Confidence != 0.9 || cliParsed.Parsed.Scope != "project" {
		t.Fatalf("parsed scalars lost: %+v", cliParsed.Parsed)
	}
}

// TestAgentInstructionResponseShapeMatchesBus pins the same contract for the
// agent.InstructionHandler response envelope (bus path).
func TestAgentInstructionResponseShapeMatchesBus(t *testing.T) {
	t.Parallel()

	instr := &preferences.UserInstruction{ID: "i1", Trigger: "intent:x", Action: "shell", Enabled: true}
	data, err := json.Marshal(map[string]any{"resp": agent.InstructionResponse{
		Success:      true,
		Instructions: []*preferences.UserInstruction{instr},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe struct {
		Resp struct {
			Instructions []struct {
				ID      string `json:"id"`
				Trigger string `json:"trigger"`
			} `json:"instructions"`
		} `json:"resp"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(probe.Resp.Instructions) != 1 || probe.Resp.Instructions[0].ID != "i1" ||
		probe.Resp.Instructions[0].Trigger != "intent:x" {
		t.Fatalf("bus envelope lost fields: %+v", probe.Resp)
	}
}
