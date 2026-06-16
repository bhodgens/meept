package models

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// StatusModel is the model for the status/dashboard view.
type StatusModel struct {
	rpc          StatusRPCClient
	status       *types.DaemonStatusResponse
	width        int
	height       int
	lastUpdate   time.Time
	loading      bool
	err          error
	pollInterval time.Duration
}

// StatusRPCClient interface for the status model.
type StatusRPCClient interface {
	Status() (*types.DaemonStatusResponse, error)
	IsConnected() bool
}

// NewStatusModel creates a new status model.
func NewStatusModel(rpc StatusRPCClient) *StatusModel {
	return &StatusModel{
		rpc:          rpc,
		pollInterval: 5 * time.Second,
	}
}

// SetSize updates the model dimensions.
func (m *StatusModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// StatusUpdateMsg carries the status update.
type StatusUpdateMsg struct {
	Status *types.DaemonStatusResponse
	Err    error
}

// StatusTickMsg triggers a status refresh.
type StatusTickMsg struct{}

// Init initializes the status model.
func (m *StatusModel) Init() tea.Cmd {
	return tea.Batch(
		m.fetchStatus,
		m.tickCmd(),
	)
}

func (m *StatusModel) tickCmd() tea.Cmd {
	return tea.Tick(m.pollInterval, func(t time.Time) tea.Msg {
		return StatusTickMsg{}
	})
}

func (m *StatusModel) fetchStatus() tea.Msg {
	status, err := m.rpc.Status()
	return StatusUpdateMsg{Status: status, Err: err}
}

// Update handles messages for the status view.
func (m *StatusModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case StatusTickMsg:
		if !m.loading {
			m.loading = true
			return tea.Batch(m.fetchStatus, m.tickCmd())
		}
		return m.tickCmd()

	case StatusUpdateMsg:
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.err = nil
			m.status = msg.Status
		}
		return nil

	case tea.KeyPressMsg:
		if msg.String() == "r" {
			// Manual refresh
			m.loading = true
			return m.fetchStatus
		}
	}

	return nil
}

// View renders the status view.
func (m *StatusModel) View() string {
	if m.status == nil && m.err == nil {
		return m.renderLoading()
	}

	if m.err != nil {
		return m.renderError()
	}

	return m.renderDashboard()
}

func (m *StatusModel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(m.width-4).
		Align(lipgloss.Center).
		Padding(4, 0)

	return style.Render("loading status...")
}

func (m *StatusModel) renderError() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Padding(1, 2).
		Width(m.width - 4)

	return style.Render(
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render("error") +
			"\n\n" +
			fmt.Sprintf("%v", m.err) +
			"\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("press 'r' to refresh"),
	)
}

func (m *StatusModel) renderDashboard() string {
	// Calculate panel widths
	panelWidth := max((m.width-8)/3, 20)

	// Create three panels
	leftPanel := m.renderStatusPanel(panelWidth)
	centerPanel := m.renderMetricsPanel(panelWidth)
	rightPanel := m.renderInfoPanel(panelWidth)

	// Join panels horizontally
	panels := lipgloss.JoinHorizontal(lipgloss.Top,
		leftPanel,
		centerPanel,
		rightPanel,
	)

	// Add refresh hint
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1).
		Render(fmt.Sprintf("last updated: %s | press 'r' to refresh",
			m.lastUpdate.Format("15:04:05")))

	return lipgloss.JoinVertical(lipgloss.Left, panels, hint)
}

func (m *StatusModel) renderStatusPanel(width int) string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Width(width).
		Height(m.height - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(12)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	// Status color
	statusColor := ColorGreen // Green
	if m.status.Status != StateRunning {
		statusColor = ColorRed // Red
	}
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(statusColor)).
		Bold(true)

	content := titleStyle.Render("daemon status") + "\n\n"
	content += labelStyle.Render("status:") + statusStyle.Render(m.status.Status) + "\n"
	content += labelStyle.Render("uptime:") + valueStyle.Render(types.FormatUptime(m.status.UptimeSeconds)) + "\n"

	// Model
	model := m.status.Model
	if model == "" {
		model = m.status.DefaultModel
	}
	if model == "" {
		model = StatusNA
	}
	content += labelStyle.Render("llm model:") + valueStyle.Render(model) + "\n"

	// RPC methods
	methodCount := len(m.status.RegisteredMethods)
	content += labelStyle.Render("rpc methods:") + valueStyle.Render(fmt.Sprintf("%d", methodCount)) + "\n"

	// Bus subscribers
	content += labelStyle.Render("bus subs:") + valueStyle.Render(fmt.Sprintf("%d", m.status.BusSubscribers)) + "\n"

	return panelStyle.Render(content)
}

func (m *StatusModel) renderMetricsPanel(width int) string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Width(width).
		Height(m.height - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	content := titleStyle.Render("token budget") + "\n\n"

	// Token usage bar
	tokensUsed := m.status.TokensUsed
	tokensRemaining := m.status.TokensRemaining
	totalTokens := tokensUsed + tokensRemaining
	if totalTokens == 0 {
		totalTokens = 100000 // Default
	}

	tokenPercent := float64(tokensUsed) / float64(totalTokens)
	content += labelStyle.Render("tokens used:") + "\n"
	content += m.renderProgressBar(width-8, tokenPercent) + "\n"
	content += fmt.Sprintf("%d / %d\n\n", tokensUsed, totalTokens)

	// Budget usage
	budgetUsed := m.status.BudgetUsed
	budgetRemaining := m.status.BudgetRemaining
	totalBudget := budgetUsed + budgetRemaining
	if totalBudget == 0 {
		totalBudget = 1.0
	}

	budgetPercent := budgetUsed / totalBudget
	content += labelStyle.Render("budget used:") + "\n"
	content += m.renderProgressBar(width-8, budgetPercent) + "\n"
	content += fmt.Sprintf("$%.2f / $%.2f\n", budgetUsed, totalBudget)

	return panelStyle.Render(content)
}

func (m *StatusModel) renderInfoPanel(width int) string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Width(width).
		Height(m.height - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	content := titleStyle.Render("quick actions") + "\n\n"

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Bold(true)

	content += keyStyle.Render("c") + helpStyle.Render(" - chat view") + "\n"
	content += keyStyle.Render("t") + helpStyle.Render(" - tasks view") + "\n"
	content += keyStyle.Render("m") + helpStyle.Render(" - memory view") + "\n"
	content += keyStyle.Render("r") + helpStyle.Render(" - refresh status") + "\n"
	content += keyStyle.Render("q") + helpStyle.Render(" - quit") + "\n"

	return panelStyle.Render(content)
}

func (m *StatusModel) renderProgressBar(width int, percent float64) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	barWidth := max(
		// Account for brackets
		width-2, 10)

	filled := int(float64(barWidth) * percent)
	empty := barWidth - filled

	// Color based on percentage
	fillColor := ColorGreen // Green
	if percent > 0.75 {
		fillColor = ColorAmber // Amber
	}
	if percent > 0.9 {
		fillColor = ColorRed // Red
	}

	fillStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fillColor))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))

	bar := "["
	bar += fillStyle.Render(strings.Repeat("=", filled))
	bar += emptyStyle.Render(strings.Repeat("-", empty))
	bar += "]"

	return bar
}
