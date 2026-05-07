// Standalone animation demo for iterating on the dispatch visualization.
package main

import (
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/viz"
)

// Demo cycles through different robot states to show all animations.
type model struct {
	viz       *viz.DispatchViz
	width     int
	height    int
	demoPhase int
	demoTick  int
}

func initialModel() model {
	// Create a larger viz for the demo (60 chars wide)
	v := viz.NewDispatchViz(60)
	return model{
		viz:       v,
		width:     80,
		height:    30,
		demoPhase: 0,
		demoTick:  0,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.viz.Init(),
		demoTickCmd(),
	)
}

// demoTickMsg advances the demo state
type demoTickMsg struct{}

func demoTickCmd() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return demoTickMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "1":
			// All idle
			for i := 0; i < 4; i++ {
				m.viz.SetRobotState(i, viz.RobotIdle)
			}
		case "2":
			// All working with different progress
			for i := 0; i < 4; i++ {
				m.viz.SetRobotState(i, viz.RobotWorking)
				m.viz.SetRobotProgress(i, float64(i+1)*0.25)
			}
		case "3":
			// Mixed states
			m.viz.SetRobotState(0, viz.RobotWorking)
			m.viz.SetRobotProgress(0, 0.5)
			m.viz.SetRobotState(1, viz.RobotCarrying)
			m.viz.SetRobotState(2, viz.RobotTaskComplete)
			m.viz.SetRobotState(3, viz.RobotMovingToCenter)
		case "4":
			// Error states
			m.viz.SetRobotState(0, viz.RobotFailed)
			m.viz.SetRobotState(1, viz.RobotProblems)
			m.viz.SetRobotState(2, viz.RobotIdle)
			m.viz.SetRobotState(3, viz.RobotIdle)
		case "5":
			// Moving states
			for i := 0; i < 4; i++ {
				m.viz.SetRobotState(i, viz.RobotMovingToCenter)
			}
		case "6":
			// Dispatching
			for i := 0; i < 4; i++ {
				m.viz.SetRobotState(i, viz.RobotDispatchingSubtask)
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Resize viz to fit window (leave room for chrome)
		vizWidth := msg.Width - 4
		if vizWidth > 80 {
			vizWidth = 80
		}
		if vizWidth < 20 {
			vizWidth = 20
		}
		m.viz.SetSize(vizWidth)
		return m, nil

	case viz.VizTickMsg:
		// Forward to viz and continue animation
		return m, m.viz.Update(msg)

	case demoTickMsg:
		// Cycle through demo phases
		m.demoTick++
		m.demoPhase = (m.demoPhase + 1) % 6

		switch m.demoPhase {
		case 0:
			// All idle
			for i := 0; i < 4; i++ {
				m.viz.SetRobotState(i, viz.RobotIdle)
			}
		case 1:
			// Some working
			m.viz.SetRobotState(0, viz.RobotWorking)
			m.viz.SetRobotProgress(0, 0.3)
			m.viz.SetRobotState(1, viz.RobotWorking)
			m.viz.SetRobotProgress(1, 0.7)
		case 2:
			// Carrying/tool exec
			m.viz.SetRobotState(0, viz.RobotCarrying)
			m.viz.SetRobotState(2, viz.RobotCarrying)
		case 3:
			// Task complete
			m.viz.SetRobotState(0, viz.RobotTaskComplete)
			m.viz.SetRobotState(1, viz.RobotTaskComplete)
			m.viz.SetRobotState(2, viz.RobotIdle)
		case 4:
			// Moving to center
			m.viz.SetRobotState(3, viz.RobotMovingToCenter)
		case 5:
			// Some errors
			m.viz.SetRobotState(2, viz.RobotFailed)
			m.viz.SetRobotState(3, viz.RobotProblems)
		}

		// Increment progress on working robots
		for i := 0; i < 4; i++ {
			if m.viz != nil {
				// This is handled internally by the robot
			}
		}

		return m, demoTickCmd()
	}

	return m, nil
}

func (m model) View() tea.View {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981"))

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2)

	// Current phase name
	phases := []string{
		"idle",
		"working",
		"carrying (tool exec)",
		"task complete",
		"moving to center",
		"error states",
	}
	currentPhase := phases[m.demoPhase]

	// Build the view
	var content string
	content += titleStyle.Render("Dispatch Animation Demo") + "\n\n"
	content += fmt.Sprintf("Phase: %s\n\n", stateStyle.Render(currentPhase))
	content += m.viz.View() + "\n\n"
	content += helpStyle.Render("Keys: 1-6 set states | q quit | Auto-cycling every 2s")

	v := tea.NewView(borderStyle.Render(content))
	v.AltScreen = true
	return v
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
