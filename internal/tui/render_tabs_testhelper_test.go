package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderTabsForTest renders the tab bar exactly as the deleted App.renderTabs
// did (deadcode wave-1: the production tab bar is rendered inline; this
// helper survives only for the tab-inventory tests).
func (a *App) renderTabsForTest() string {
	tabs := []struct {
		name string
		view ViewType
	}{
		{"chat", ViewChat},
		{"sessions", ViewSessions},
		{"tasks", ViewTasks},
		{"queue", ViewQueue},
		{"memory", ViewMemory},
		{"plans", ViewPlans},
		{"agents", ViewAgents},
		{"activity", ViewActivity},
	}

	renderedTabs := make([]string, 0, len(tabs))
	for i, t := range tabs {
		style := a.styles.Tab
		if t.view == a.currentView {
			style = a.styles.ActiveTab
			// Clear flash when viewing the tab
			delete(a.tabFlash, t.view)
		}
		label := fmt.Sprintf("[%d] %s", i+1, t.name)
		// Add notification indicator if tab has flash
		if a.tabFlash[t.view] {
			label += " ●"
		}
		renderedTabs = append(renderedTabs, style.Render(label))
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Add separator to fill width
	separatorWidth := max(0, a.width-lipgloss.Width(tabLine))
	separator := strings.Repeat("─", separatorWidth)

	return lipgloss.NewStyle().
		Width(a.width).
		Render(tabLine + a.styles.Muted.Render(separator))
}
