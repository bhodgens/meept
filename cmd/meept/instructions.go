package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/transport"
	"github.com/spf13/cobra"
)

func instructionsCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: meept instructions <list|add|delete|show|preview> [args]")
	}

	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	cmd := args[0]
	switch cmd {
	case "list":
		return instructionsList(client)
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: meept instructions add \"<natural language>\"")
		}
		return instructionsAdd(client, strings.Join(args[1:], " "))
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: meept instructions delete <id>")
		}
		return instructionsDelete(client, args[1])
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: meept instructions show <id>")
		}
		return instructionsShow(client, args[1])
	case "preview":
		if len(args) < 2 {
			return fmt.Errorf("usage: meept instructions preview \"<natural language>\"")
		}
		return instructionsPreview(client, strings.Join(args[1:], " "))
	default:
		return fmt.Errorf("unknown instruction command: %s", cmd)
	}
}

func instructionsList(client transport.Client) error {
	result, err := client.Call("instruction.list", nil)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	var resp InstructionListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	if len(resp.Instructions) == 0 {
		fmt.Println("No active instructions.")
		return nil
	}

	fmt.Printf("%-32s %-25s %-15s %-10s %-10s\n", "ID", "Trigger", "Action", "Scope", "Priority")
	fmt.Println(strings.Repeat("-", 100))
	for _, instr := range resp.Instructions {
		fmt.Printf("%-32s %-25s %-15s %-10s %-10s\n",
			instr.ID, truncate(instr.Trigger, 24), instr.Action, instr.Scope, instr.Priority)
	}
	return nil
}

func instructionsAdd(client transport.Client, input string) error {
	req := map[string]string{"input": input}
	result, err := client.Call("instruction.add", req)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	var resp InstructionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	fmt.Println("Instruction created successfully!")
	printInstruction(resp.Instruction, resp.ConfirmationRequired)
	return nil
}

func instructionsDelete(client transport.Client, id string) error {
	req := map[string]string{"id": id}
	result, err := client.Call("instruction.delete", req)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	var resp InstructionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	fmt.Printf("Instruction '%s' deleted.\n", id)
	return nil
}

func instructionsShow(client transport.Client, id string) error {
	result, err := client.Call("instruction.list", nil)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	var resp InstructionListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	for _, instr := range resp.Instructions {
		if instr.ID == id {
			printInstruction(instr, false)
			return nil
		}
	}

	return fmt.Errorf("instruction not found: %s", id)
}

func instructionsPreview(client transport.Client, input string) error {
	req := map[string]string{"input": input}
	result, err := client.Call("instruction.preview", req)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	var resp InstructionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	fmt.Println("Parsed instruction preview:")
	printParsedInstruction(resp.ParsedInstruction, resp.ConfirmationRequired)
	return nil
}

type InstructionResponse struct {
	Success              bool                   `json:"success"`
	Instruction          *InstructionInfo       `json:"instruction,omitempty"`
	Instructions         []*InstructionInfo     `json:"instructions,omitempty"`
	ParsedInstruction    *ParsedInstructionCLI  `json:"parsed,omitempty"`
	ConfirmationRequired bool                   `json:"confirmation_required"`
	Error                string                 `json:"error,omitempty"`
}

type InstructionListResponse struct {
	Success      bool             `json:"success"`
	Instructions []*InstructionInfo `json:"instructions"`
	Error        string           `json:"error"`
}

type InstructionInfo struct {
	ID         string         `json:"id"`
	Trigger    string         `json:"trigger"`
	Action     string         `json:"action"`
	ActionArgs map[string]any `json:"action_args"`
	Enabled    bool           `json:"enabled"`
	Scope      string         `json:"scope"`
	Priority   string         `json:"priority"`
}

type ParsedInstructionCLI struct {
	Trigger struct {
		Type    string         `json:"type"`
		Pattern string         `json:"pattern"`
	} `json:"trigger"`
	Action struct {
		Tool    string         `json:"tool"`
		Args    map[string]any `json:"args"`
		AgentID string         `json:"agent_id"`
	} `json:"action"`
	Scope      string  `json:"scope"`
	Priority   string  `json:"priority"`
	Confidence float64 `json:"confidence"`
}

func printInstruction(instr *InstructionInfo, needsConfirm bool) {
	if instr == nil {
		return
	}
	fmt.Printf("  ID:       %s\n", instr.ID)
	fmt.Printf("  Trigger:  %s\n", instr.Trigger)
	fmt.Printf("  Action:   %s\n", instr.Action)
	fmt.Printf("  Scope:    %s\n", instr.Scope)
	fmt.Printf("  Priority: %s\n", instr.Priority)
	fmt.Printf("  Enabled:  %v\n", instr.Enabled)
	if needsConfirm {
		fmt.Println("\n  [!] This instruction requires confirmation before activation.")
	}
}

func printParsedInstruction(parsed *ParsedInstructionCLI, needsConfirm bool) {
	if parsed == nil {
		return
	}
	fmt.Printf("  Trigger Type:    %s\n", parsed.Trigger.Type)
	fmt.Printf("  Trigger Pattern: %s\n", parsed.Trigger.Pattern)
	fmt.Printf("  Action Tool:     %s\n", parsed.Action.Tool)
	if parsed.Action.Tool == "shell_execute" {
		if cmd, ok := parsed.Action.Args["command"]; ok {
			fmt.Printf("  Command:         %v\n", cmd)
		}
	}
	fmt.Printf("  Scope:           %s\n", parsed.Scope)
	fmt.Printf("  Priority:        %s\n", parsed.Priority)
	fmt.Printf("  Confidence:      %.2f\n", parsed.Confidence)
	if needsConfirm {
		fmt.Println("\n  [!] This instruction would require confirmation.")
	}
}

func newInstructionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "instructions",
		Short: "Manage user instructions",
		Long: `Manage user instructions (automation rules).

Examples:
  meept instructions list                          # List all instructions
  meept instructions add "always run tests"        # Add instruction
  meept instructions delete <id>                   # Remove instruction
  meept instructions preview "every day at 9am"    # Preview parsed instruction`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return instructionsCmd(args)
		},
	}
}
