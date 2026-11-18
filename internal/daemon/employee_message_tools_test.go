package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

func TestWireEmployeeMessagingRegistersToolsWhenStoreExists(t *testing.T) {
	ms, err := employee.NewMessageStore(filepath.Join(t.TempDir(), "messages.db"), slog.Default())
	if err != nil {
		t.Fatalf("message store: %v", err)
	}
	t.Cleanup(func() { _ = ms.Close() })

	reg := tools.NewRegistry(slog.Default())
	WireEmployeeMessagingForComponents(&Components{
		ToolRegistry:         reg,
		EmployeeMessageStore: ms,
	})

	if reg.Get("send_agent_message") == nil {
		t.Fatal("send_agent_message was not registered")
	}
	if reg.Get("inbox") == nil {
		t.Fatal("inbox was not registered")
	}
}

func TestWireEmployeeMessagingSkipsToolsWithoutStore(t *testing.T) {
	reg := tools.NewRegistry(slog.Default())
	WireEmployeeMessagingForComponents(&Components{ToolRegistry: reg})
	if reg.Get("inbox") != nil || reg.Get("send_agent_message") != nil {
		t.Fatal("messaging tools registered without a message store")
	}
}

// TestRoster_IncludesBotID pins C4's bot-roster half: a bot id must be an
// addressable message target through the REAL WireEmployeeMessagingForComponents
// wiring (not a hand-built closure), and must appear in the roster's valid
// targets. Uses a real bot.Store + bot.Manager; no predicate is mocked.
func TestRoster_IncludesBotID(t *testing.T) {
	ctx := context.Background()

	ms, err := employee.NewMessageStore(filepath.Join(t.TempDir(), "messages.db"), slog.Default())
	if err != nil {
		t.Fatalf("message store: %v", err)
	}
	t.Cleanup(func() { _ = ms.Close() })

	botStore, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	botMgr := bot.NewManager(botStore, nil)
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID:      "b1",
		Prompt:  "a roster-addressable worker",
		Enabled: true,
		Triggers: []bot.BotTrigger{{
			Type:     bot.TriggerTypeCron,
			Schedule: "0 9 * * *",
			Enabled:  true,
		}},
	}); err != nil {
		t.Fatalf("create bot b1: %v", err)
	}

	reg := tools.NewRegistry(slog.Default())
	WireEmployeeMessagingForComponents(&Components{
		ToolRegistry:         reg,
		EmployeeMessageStore: ms,
		BotManager:           botMgr,
	})
	sendTool := reg.Get("send_agent_message")
	if sendTool == nil {
		t.Fatal("send_agent_message was not registered")
	}

	t.Run("bot id is an accepted target", func(t *testing.T) {
		res, err := sendTool.Execute(ctx, map[string]any{"to": "b1", "message": "hello bot"})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		receipt, ok := res.(builtin.SendMessageResult)
		if !ok {
			t.Fatalf("bot id b1 was not accepted as a message target; got %T (%+v)", res, res)
		}
		if receipt.To != "b1" || receipt.ID == "" {
			t.Fatalf("receipt mismatch for bot target: %+v", receipt)
		}
	})

	t.Run("bot id is listed among valid targets", func(t *testing.T) {
		res, err := sendTool.Execute(ctx, map[string]any{"to": "ghost", "message": "hi"})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		tr, ok := res.(*tools.ToolResult)
		if !ok || tr.Success {
			t.Fatalf("expected error ToolResult for unknown recipient; got %T (%+v)", res, res)
		}
		if !strings.Contains(tr.Error, "b1") {
			t.Fatalf("valid-targets error must list bot id b1; got %q", tr.Error)
		}
	})
}
