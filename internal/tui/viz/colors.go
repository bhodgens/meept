package viz

import "github.com/charmbracelet/lipgloss"

// Visualization-specific colors (matching the main TUI palette)
var (
	ColorIdle       = lipgloss.Color("#E5E7EB") // Light gray - idle/foreground
	ColorWorking    = lipgloss.Color("#F97316") // Orange - primary/working
	ColorSuccess    = lipgloss.Color("#10B981") // Green - task complete
	ColorMuted      = lipgloss.Color("#6B7280") // Gray - dispatching subtask
	ColorCarrying   = lipgloss.Color("#3B82F6") // Blue - carrying/working halo
	ColorError      = lipgloss.Color("#EF4444") // Red - failed/problems
	ColorDispatcher = lipgloss.Color("#F59E0B") // Amber - dispatcher block
	ColorDotLine    = lipgloss.Color("#374151") // Dark gray - dotted lines
)
