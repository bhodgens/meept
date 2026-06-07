package bot

import (
	"testing"
	"time"
)

func TestBotTrigger_Validate(t *testing.T) {
	tests := []struct {
		name    string
		trigger BotTrigger
		wantErr bool
	}{
		{
			name:    "valid cron trigger",
			trigger: BotTrigger{Type: TriggerTypeCron, Schedule: "*/5 * * * *"},
			wantErr: false,
		},
		{
			name:    "cron trigger missing schedule",
			trigger: BotTrigger{Type: TriggerTypeCron},
			wantErr: true,
		},
		{
			name:    "valid bus event trigger",
			trigger: BotTrigger{Type: TriggerTypeBusEvent, Topic: "calendar.reminder"},
			wantErr: false,
		},
		{
			name:    "bus event trigger missing topic",
			trigger: BotTrigger{Type: TriggerTypeBusEvent},
			wantErr: true,
		},
		{
			name:    "valid webhook trigger",
			trigger: BotTrigger{Type: TriggerTypeWebhook},
			wantErr: false,
		},
		{
			name:    "invalid trigger type",
			trigger: BotTrigger{Type: "invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.trigger.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBotDefinition_Validate(t *testing.T) {
	validDef := BotDefinition{
		ID:          "ci-monitor",
		Name:        "CI Monitor",
		Description: "Monitors CI pipeline status",
		Prompt:      "Check the CI status and report any failures",
		Triggers: []BotTrigger{
			{Type: TriggerTypeCron, Schedule: "*/15 * * * *", Enabled: true},
		},
		MemoryScope: MemoryScopePrivate,
		Tools:       []string{"web_fetch", "memory_store", "memory_search"},
		Constraints: BotConstraints{
			MaxIterations:    5,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
			DailyBudgetCents: 50,
		},
	}

	if err := validDef.Validate(); err != nil {
		t.Fatalf("valid definition failed: %v", err)
	}

	noID := validDef
	noID.ID = ""
	if err := noID.Validate(); err == nil {
		t.Fatal("expected error for missing ID")
	}

	noPrompt := validDef
	noPrompt.Prompt = ""
	if err := noPrompt.Validate(); err == nil {
		t.Fatal("expected error for missing prompt")
	}

	noTriggers := validDef
	noTriggers.Triggers = nil
	if err := noTriggers.Validate(); err == nil {
		t.Fatal("expected error for no triggers")
	}

	badTrigger := validDef
	badTrigger.Triggers = []BotTrigger{{Type: "bogus"}}
	if err := badTrigger.Validate(); err == nil {
		t.Fatal("expected error for invalid trigger")
	}
}
