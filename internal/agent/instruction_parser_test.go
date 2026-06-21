package agent

import (
	"context"
	"testing"

	)

func TestInstructionParser_ExtractTrigger(t *testing.T) {
	p := NewInstructionParser()

	tests := []struct {
		name     string
		input    string
		wantType string
		wantConf float64
	}{
		{"cron daily", "every day at 9am", "cron", 0.85},
		{"cron weekly", "every monday at 10am", "cron", 0.85},
		{"post hook write", "after I write go files", "post_hook", 0.75},
		{"post hook commit", "after I commit", "post_hook", 0.75},
		{"git pre-commit", "before commit", "git", 0.80},
		{"git post-commit", "after push", "git", 0.80},
		{"intent research", "whenever I research", "post_hook", 0.70},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger, conf := p.extractTrigger(tt.input)
			if trigger.Type != tt.wantType {
				t.Errorf("extractTrigger(%q) type = %v, want %v", tt.input, trigger.Type, tt.wantType)
			}
			if conf < tt.wantConf-0.1 || conf > tt.wantConf+0.1 {
				t.Errorf("extractTrigger(%q) conf = %v, want ~%v", tt.input, conf, tt.wantConf)
			}
		})
	}
}

func TestInstructionParser_ExtractAction(t *testing.T) {
	p := NewInstructionParser()

	tests := []struct {
		name      string
		input     string
		wantTool  string
		wantCommand string
	}{
		{"tests", "run tests", "shell_execute", "go test ./..."},
		{"build", "build the project", "shell_execute", "go build ./..."},
		{"memory", "remember this", "memory_retain", ""},
		{"notify", "notify me", "notification", ""},
		{"agent coder", "ask coder", "agent_trigger", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := p.extractAction(tt.input)
			if action.Tool != tt.wantTool {
				t.Errorf("extractAction(%q) tool = %v, want %v", tt.input, action.Tool, tt.wantTool)
			}
			if tt.wantCommand != "" {
				if cmd, ok := action.Args["command"]; !ok || cmd != tt.wantCommand {
					t.Errorf("extractAction(%q) command = %v, want %v", tt.input, cmd, tt.wantCommand)
				}
			}
		})
	}
}

func TestInstructionParser_ExtractScope(t *testing.T) {
	p := NewInstructionParser()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"project explicit", "in this project", "project"},
		{"project here", "here always", "project"},
		{"global always", "always do this", "global"},
		{"default", "run tests", "global"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractScope(tt.input)
			if got != tt.want {
				t.Errorf("extractScope(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestInstructionParser_Parse(t *testing.T) {
	p := NewInstructionParser()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		wantTool string
	}{
		{"full cron", "Every day at 9am run tests", "shell_execute"},
		{"full post-hook", "After I write go files lint them", "shell_execute"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Parse(ctx, tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Action.Tool != tt.wantTool {
				t.Errorf("Parse() action.tool = %v, want %v", result.Action.Tool, tt.wantTool)
			}
		})
	}
}
