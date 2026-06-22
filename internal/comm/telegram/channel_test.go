package telegram

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/session"
)

func TestBaseBot_IDAndName(t *testing.T) {
	b := newBaseBot("test-bot", "Test Bot")
	if b.ID() != "test-bot" {
		t.Errorf("expected id=test-bot, got %s", b.ID())
	}
	if b.Name() != "Test Bot" {
		t.Errorf("expected name=Test Bot, got %s", b.Name())
	}
}

func TestTelegramChannelConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TelegramChannelConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: TelegramChannelConfig{
				Token:   "123:ABC",
				DataDir: "/tmp/test",
				BotID:   "tg-1",
			},
			wantErr: false,
		},
		{
			name: "missing token",
			cfg: TelegramChannelConfig{
				DataDir: "/tmp/test",
				BotID:   "tg-1",
			},
			wantErr: true,
		},
		{
			name: "missing data dir",
			cfg: TelegramChannelConfig{
				Token: "123:ABC",
				BotID: "tg-1",
			},
			wantErr: true,
		},
		{
			name: "missing bot id",
			cfg: TelegramChannelConfig{
				Token:   "123:ABC",
				DataDir: "/tmp/test",
			},
			wantErr: true,
		},
		{
			name: "default poll timeout",
			cfg: TelegramChannelConfig{
				Token:       "123:ABC",
				DataDir:     "/tmp/test",
				BotID:       "tg-1",
				PollTimeout: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestChatIDToTarget(t *testing.T) {
	if ChatIDToTarget(12345) != "12345" {
		t.Errorf("expected 12345, got %s", ChatIDToTarget(12345))
	}
	if ChatIDToTarget(-999) != "-999" {
		t.Errorf("expected -999, got %s", ChatIDToTarget(-999))
	}
}

func TestNewTelegramChannel_MissingToken(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	_, err := NewTelegramChannel(TelegramChannelConfig{
		Token:   "",
		DataDir: t.TempDir(),
		BotID:   "tg-1",
	}, store, loop, nil)
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestNewTelegramChannel_MissingDataDir(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	_, err := NewTelegramChannel(TelegramChannelConfig{
		Token:   "123:ABC",
		DataDir: "",
		BotID:   "tg-1",
	}, store, loop, nil)
	if err == nil {
		t.Error("expected error for missing data dir")
	}
}

func TestNewTelegramChannel_MissingBotID(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	_, err := NewTelegramChannel(TelegramChannelConfig{
		Token:   "123:ABC",
		DataDir: "/tmp/test",
		BotID:   "",
	}, store, loop, nil)
	if err == nil {
		t.Error("expected error for missing bot ID")
	}
}

func TestTelegramChannel_CreatesSuccessfully(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()

	ch, err := NewTelegramChannel(TelegramChannelConfig{
		Token:       "123:ABC",
		DataDir:     t.TempDir(),
		BotID:       "tg-test",
		BotName:     "Test Bot",
		PollTimeout: 10,
	}, store, loop, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.ID() != "tg-test" {
		t.Errorf("expected id=tg-test, got %s", ch.ID())
	}
	if ch.Name() != "Test Bot" {
		t.Errorf("expected name=Test Bot, got %s", ch.Name())
	}
}

func TestTelegramChannel_BotClientNotNil(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()

	ch, err := NewTelegramChannel(TelegramChannelConfig{
		Token:   "123:ABC",
		DataDir: t.TempDir(),
		BotID:   "tg-test",
		BotName: "Test Bot",
	}, store, loop, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.BotClient() == nil {
		t.Error("expected non-nil bot client")
	}
}

func TestTelegramChannel_CanInitiate(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()

	ch, err := NewTelegramChannel(TelegramChannelConfig{
		Token:   "123:ABC",
		DataDir: t.TempDir(),
		BotID:   "tg-test",
		BotName: "Test Bot",
	}, store, loop, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ch.CanInitiate() {
		t.Error("expected CanInitiate to return true")
	}
}

func TestTelegramChannel_SendMessage_InvalidTarget(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()

	ch, err := NewTelegramChannel(TelegramChannelConfig{
		Token:   "123:ABC",
		DataDir: t.TempDir(),
		BotID:   "tg-test",
		BotName: "Test Bot",
	}, store, loop, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ch.SendMessage(context.Background(), "not-a-number", "hello")
	if err == nil {
		t.Error("expected error for invalid target")
	}
}
