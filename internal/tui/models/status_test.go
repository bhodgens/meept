package models

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// MockStatusRPCClient implements StatusRPCClient for testing.
type MockStatusRPCClient struct {
	connected      bool
	StatusResponse *types.DaemonStatusResponse
	StatusError    error
	StatusCalls    int
}

func NewMockStatusRPCClient() *MockStatusRPCClient {
	return &MockStatusRPCClient{
		connected: true,
		StatusResponse: &types.DaemonStatusResponse{
			Status:            "running",
			UptimeSeconds:     3600,
			Model:             "gpt-4",
			DefaultModel:      "gpt-4",
			RegisteredMethods: []string{"chat", "status", "memory.query"},
			BusSubscribers:    3,
			TokensUsed:        5000,
			TokensRemaining:   95000,
			BudgetUsed:        1.50,
			BudgetRemaining:   8.50,
		},
	}
}

func (m *MockStatusRPCClient) Status() (*types.DaemonStatusResponse, error) {
	m.StatusCalls++
	if m.StatusError != nil {
		return nil, m.StatusError
	}
	return m.StatusResponse, nil
}

func (m *MockStatusRPCClient) IsConnected() bool {
	return m.connected
}

func TestStatusModel_NewStatusModel(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	if model == nil {
		t.Fatal("expected non-nil status model")
	}
	if model.pollInterval != 5*time.Second {
		t.Errorf("expected poll interval 5s, got %v", model.pollInterval)
	}
}

func TestStatusModel_SetSize(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	model.SetSize(100, 40)

	if model.width != 100 {
		t.Errorf("expected width 100, got %d", model.width)
	}
	if model.height != 40 {
		t.Errorf("expected height 40, got %d", model.height)
	}
}

func TestStatusModel_Init(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	cmd := model.Init()

	if cmd == nil {
		t.Error("expected Init to return a command")
	}
}

func TestStatusModel_FetchStatus(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	msg := model.fetchStatus()
	updateMsg, ok := msg.(StatusUpdateMsg)

	if !ok {
		t.Fatal("expected StatusUpdateMsg")
	}
	if updateMsg.Err != nil {
		t.Errorf("unexpected error: %v", updateMsg.Err)
	}
	if updateMsg.Status == nil {
		t.Error("expected status to be set")
	}
	if updateMsg.Status.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", updateMsg.Status.Status)
	}
}

func TestStatusModel_FetchStatusError(t *testing.T) {
	mock := NewMockStatusRPCClient()
	mock.StatusError = errors.New("connection failed")
	model := NewStatusModel(mock)

	msg := model.fetchStatus()
	updateMsg, ok := msg.(StatusUpdateMsg)

	if !ok {
		t.Fatal("expected StatusUpdateMsg")
	}
	if updateMsg.Err == nil {
		t.Error("expected error to be set")
	}
	if !strings.Contains(updateMsg.Err.Error(), "connection failed") {
		t.Errorf("expected 'connection failed' error, got '%v'", updateMsg.Err)
	}
}

func TestStatusModel_UpdateWithStatusUpdateMsg(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.loading = true

	updateMsg := StatusUpdateMsg{
		Status: mock.StatusResponse,
		Err:    nil,
	}
	model.Update(updateMsg)

	if model.loading {
		t.Error("expected loading to be false")
	}
	if model.err != nil {
		t.Errorf("unexpected error: %v", model.err)
	}
	if model.status == nil {
		t.Error("expected status to be set")
	}
	if model.lastUpdate.IsZero() {
		t.Error("expected lastUpdate to be set")
	}
}

func TestStatusModel_UpdateWithError(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.loading = true

	testErr := errors.New("test error")
	updateMsg := StatusUpdateMsg{
		Err: testErr,
	}
	model.Update(updateMsg)

	if model.loading {
		t.Error("expected loading to be false")
	}
	if !errors.Is(model.err, testErr) {
		t.Errorf("expected error to be set, got %v", model.err)
	}
}

func TestStatusModel_UpdateWithTickMsg(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	cmd := model.Update(StatusTickMsg{})

	if cmd == nil {
		t.Error("expected command to be returned on tick")
	}
}

func TestStatusModel_UpdateWithTickMsgWhileLoading(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.loading = true

	cmd := model.Update(StatusTickMsg{})

	// Should return tick command but not fetch
	if cmd == nil {
		t.Error("expected tick command to be returned")
	}
}

func TestStatusModel_ManualRefresh(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	msg := tea.KeyPressMsg{Code: 'r', Text: "r"}
	cmd := model.Update(msg)

	if !model.loading {
		t.Error("expected loading to be true after refresh")
	}
	if cmd == nil {
		t.Error("expected command to be returned for fetch")
	}
}

func TestStatusModel_ViewLoading(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.SetSize(80, 24)

	view := model.View()

	if !strings.Contains(view, "loading") {
		t.Error("expected 'loading' in view when no status")
	}
}

func TestStatusModel_ViewError(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.SetSize(80, 24)
	model.err = errors.New("test error")

	view := model.View()

	if !strings.Contains(view, "error") {
		t.Error("expected 'error' in view")
	}
	if !strings.Contains(view, "test error") {
		t.Error("expected error message in view")
	}
	if !strings.Contains(view, "refresh") {
		t.Error("expected refresh hint in view")
	}
}

func TestStatusModel_ViewDashboard(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.SetSize(120, 40)
	model.status = mock.StatusResponse
	model.lastUpdate = time.Now()

	view := model.View()

	// Check for panel content
	if !strings.Contains(view, "daemon status") {
		t.Error("expected 'daemon status' panel")
	}
	if !strings.Contains(view, "running") {
		t.Error("expected 'running' status")
	}
	if !strings.Contains(view, "token budget") {
		t.Error("expected 'token budget' panel")
	}
	if !strings.Contains(view, "quick actions") {
		t.Error("expected 'quick actions' panel")
	}
	if !strings.Contains(view, "last updated") {
		t.Error("expected last updated hint")
	}
}

func TestStatusModel_RenderStatusPanel(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.SetSize(80, 24)
	model.status = mock.StatusResponse

	panel := model.renderStatusPanel(30)

	if !strings.Contains(panel, "daemon status") {
		t.Error("expected 'daemon status' title")
	}
	if !strings.Contains(panel, "running") {
		t.Error("expected status value")
	}
	if !strings.Contains(panel, "gpt-4") {
		t.Error("expected model name")
	}
}

func TestStatusModel_RenderStatusPanelNotRunning(t *testing.T) {
	mock := NewMockStatusRPCClient()
	mock.StatusResponse.Status = "stopped"
	model := NewStatusModel(mock)
	model.SetSize(80, 24)
	model.status = mock.StatusResponse

	panel := model.renderStatusPanel(30)

	if !strings.Contains(panel, "stopped") {
		t.Error("expected 'stopped' status")
	}
}

func TestStatusModel_RenderStatusPanelNoModel(t *testing.T) {
	mock := NewMockStatusRPCClient()
	mock.StatusResponse.Model = ""
	mock.StatusResponse.DefaultModel = ""
	model := NewStatusModel(mock)
	model.SetSize(80, 24)
	model.status = mock.StatusResponse

	panel := model.renderStatusPanel(30)

	if !strings.Contains(panel, "n/a") {
		t.Error("expected 'n/a' for missing model")
	}
}

func TestStatusModel_RenderMetricsPanel(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.SetSize(80, 24)
	model.status = mock.StatusResponse

	panel := model.renderMetricsPanel(30)

	if !strings.Contains(panel, "token budget") {
		t.Error("expected 'token budget' title")
	}
	if !strings.Contains(panel, "tokens used") {
		t.Error("expected 'tokens used' label")
	}
	if !strings.Contains(panel, "budget used") {
		t.Error("expected 'budget used' label")
	}
}

func TestStatusModel_RenderInfoPanel(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)
	model.SetSize(80, 24)

	panel := model.renderInfoPanel(30)

	if !strings.Contains(panel, "quick actions") {
		t.Error("expected 'quick actions' title")
	}
	if !strings.Contains(panel, "Chat view") {
		t.Error("expected chat hint")
	}
	if !strings.Contains(panel, "Refresh") {
		t.Error("expected refresh hint")
	}
}

func TestStatusModel_RenderProgressBar(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	tests := []struct {
		name    string
		percent float64
	}{
		{"zero", 0.0},
		{"half", 0.5},
		{"full", 1.0},
		{"over", 1.5},
		{"negative", -0.5},
		{"high warning", 0.8},
		{"critical", 0.95},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := model.renderProgressBar(20, tt.percent)

			if !strings.Contains(bar, "[") || !strings.Contains(bar, "]") {
				t.Error("expected progress bar brackets")
			}
		})
	}
}

func TestStatusModel_RenderProgressBarMinWidth(t *testing.T) {
	mock := NewMockStatusRPCClient()
	model := NewStatusModel(mock)

	bar := model.renderProgressBar(5, 0.5)

	// Should still render with minimum width
	if !strings.Contains(bar, "[") || !strings.Contains(bar, "]") {
		t.Error("expected progress bar brackets")
	}
}

// Note: teatest integration tests for sub-models are skipped because they don't
// implement the full tea.Model interface (missing quit command handling).
// The App-level teatest tests provide full integration testing.
// Sub-models are thoroughly tested via unit tests above.
