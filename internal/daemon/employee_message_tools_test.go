package daemon

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/tools"
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
