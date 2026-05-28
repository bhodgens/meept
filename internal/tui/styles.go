package tui

import (
	"charm.land/lipgloss/v2"
)

// Color palette for dark theme
var (
	ColorPrimary    = lipgloss.Color("#F97316") // Orange
	ColorSecondary  = lipgloss.Color("#10B981") // Green
	ColorAccent     = lipgloss.Color("#F59E0B") // Amber
	ColorError      = lipgloss.Color("#EF4444") // Red
	ColorWarning    = lipgloss.Color("#F59E0B") // Amber
	ColorSuccess    = lipgloss.Color("#10B981") // Green
	ColorMuted      = lipgloss.Color("#6B7280") // Gray
	ColorForeground = lipgloss.Color("#E5E7EB") // Light gray
	ColorBackground = lipgloss.Color("#1F2937") // Dark gray
	ColorBorder     = lipgloss.Color("#374151") // Medium gray
)

// Styles provides a collection of reusable lipgloss styles.
type Styles struct {
	// Base styles
	App       lipgloss.Style
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Paragraph lipgloss.Style
	Muted     lipgloss.Style
	Error     lipgloss.Style
	Success   lipgloss.Style
	Warning   lipgloss.Style

	// Chat styles
	UserMessage      lipgloss.Style
	AssistantMessage lipgloss.Style
	SystemMessage    lipgloss.Style
	InputField       lipgloss.Style

	// Panel styles
	Panel       lipgloss.Style
	PanelTitle  lipgloss.Style
	PanelBorder lipgloss.Style

	// Status styles
	StatusBar     lipgloss.Style
	StatusRunning lipgloss.Style
	StatusStopped lipgloss.Style

	// Table styles
	TableHeader   lipgloss.Style
	TableRow      lipgloss.Style
	TableSelected lipgloss.Style

	// Tab styles
	Tab                  lipgloss.Style
	ActiveTab            lipgloss.Style
	CommandModeTab       lipgloss.Style
	CommandModeIndicator lipgloss.Style

	// Header bar
	HeaderBar lipgloss.Style

	// Help
	HelpKey   lipgloss.Style
	HelpValue lipgloss.Style

	// Progress bar
	ProgressBar  lipgloss.Style
	ProgressFull lipgloss.Style

	// Modal styles
	ModalOverlay      lipgloss.Style
	ModalBox          lipgloss.Style
	ModalTitle        lipgloss.Style
	ModalItem         lipgloss.Style
	ModalItemSelected lipgloss.Style

	// Text selection
	TextSelection lipgloss.Style

	// Queue indicators
	SteerBadge       lipgloss.Style
	FollowUpBadge    lipgloss.Style
	QueueIndicator   lipgloss.Style
	AgentActiveBadge lipgloss.Style

	// Plan state styles
	PlanStatePlanning  lipgloss.Style
	PlanStateDraft     lipgloss.Style
	PlanStatePending   lipgloss.Style
	PlanStateApproved  lipgloss.Style
	PlanStateExecuting lipgloss.Style
	PlanStateCompleted lipgloss.Style
	PlanStateConfirmed lipgloss.Style
	PlanStateFailed    lipgloss.Style
	PlanStateCancelled lipgloss.Style
}

// DefaultStyles returns the default style set.
func DefaultStyles() *Styles {
	s := &Styles{}

	// Base styles
	s.App = lipgloss.NewStyle()

	s.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	s.Subtitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorForeground)

	s.Paragraph = lipgloss.NewStyle().
		Foreground(ColorForeground)

	s.Muted = lipgloss.NewStyle().
		Foreground(ColorMuted)

	s.Error = lipgloss.NewStyle().
		Foreground(ColorError)

	s.Success = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	s.Warning = lipgloss.NewStyle().
		Foreground(ColorWarning)

	// Chat styles
	s.UserMessage = lipgloss.NewStyle().
		Foreground(ColorAccent).
		PaddingLeft(2)

	s.AssistantMessage = lipgloss.NewStyle().
		Foreground(ColorForeground).
		PaddingLeft(2)

	s.SystemMessage = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true).
		PaddingLeft(2)

	s.InputField = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	// Panel styles
	s.Panel = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(1, 2)

	s.PanelTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	s.PanelBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary)

	// Status styles - no background to avoid grey bar overflow
	s.StatusBar = lipgloss.NewStyle().
		Foreground(ColorForeground).
		Padding(0, 1)

	s.StatusRunning = lipgloss.NewStyle().
		Foreground(ColorSuccess).
		Bold(true)

	s.StatusStopped = lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true)

	// Table styles
	s.TableHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(ColorBorder)

	s.TableRow = lipgloss.NewStyle().
		Foreground(ColorForeground)

	s.TableSelected = lipgloss.NewStyle().
		Background(ColorPrimary).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)

	// Tab styles
	s.Tab = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(ColorMuted)

	s.ActiveTab = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(ColorPrimary).
		Bold(true).
		Underline(true)

	s.CommandModeTab = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(ColorAccent).
		Bold(true).
		Background(lipgloss.Color("#374151"))

	s.CommandModeIndicator = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(ColorAccent).
		Bold(true).
		Padding(0, 1)

	// Header bar (orange background, black text)
	s.HeaderBar = lipgloss.NewStyle().
		Background(ColorPrimary).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1)

	// Help styles - orange for keys and special characters
	s.HelpKey = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	s.HelpValue = lipgloss.NewStyle().
		Foreground(ColorForeground)

	// Progress bar
	s.ProgressBar = lipgloss.NewStyle().
		Background(ColorBorder)

	s.ProgressFull = lipgloss.NewStyle().
		Background(ColorPrimary)

	// Modal styles
	s.ModalOverlay = lipgloss.NewStyle().
		Background(lipgloss.Color("#000000"))

	s.ModalBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Padding(1, 2).
		Background(lipgloss.Color("#1F2937"))

	s.ModalTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent).
		MarginBottom(1)

	s.ModalItem = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(ColorForeground)

	s.ModalItemSelected = lipgloss.NewStyle().
		PaddingLeft(2).
		Background(lipgloss.Color("#374151")).
		Foreground(ColorAccent).
		Bold(true)

	// Text selection - reverse colors for visibility
	s.TextSelection = lipgloss.NewStyle().
		Foreground(ColorBackground).
		Background(ColorPrimary)

	// Queue indicators
	s.SteerBadge = lipgloss.NewStyle().
		Background(lipgloss.Color("#EF4444")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 1).
		Bold(true)

	s.FollowUpBadge = lipgloss.NewStyle().
		Background(lipgloss.Color("#10B981")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 1)

	s.QueueIndicator = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	s.AgentActiveBadge = lipgloss.NewStyle().
		Background(lipgloss.Color("#6B7280")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 1).
		Bold(true)

	// Plan state styles
	s.PlanStatePlanning = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3B82F6")) // Blue

	s.PlanStateDraft = lipgloss.NewStyle().
		Foreground(ColorMuted) // Gray

	s.PlanStatePending = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3B82F6")) // Blue

	s.PlanStateApproved = lipgloss.NewStyle().
		Foreground(ColorSuccess) // Green

	s.PlanStateExecuting = lipgloss.NewStyle().
		Foreground(ColorAccent) // Amber/Yellow

	s.PlanStateCompleted = lipgloss.NewStyle().
		Foreground(ColorSuccess) // Green

	s.PlanStateConfirmed = lipgloss.NewStyle().
		Foreground(ColorSuccess). // Green
		Bold(true)

	s.PlanStateFailed = lipgloss.NewStyle().
		Foreground(ColorError) // Red

	s.PlanStateCancelled = lipgloss.NewStyle().
		Foreground(ColorMuted) // Gray

	return s
}

// RenderProgressBar renders a progress bar with the given width and percentage.
func RenderProgressBar(width int, percent float64, styles *Styles) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	filled := int(float64(width) * percent)
	empty := width - filled

	bar := ""
	if filled > 0 {
		bar += styles.ProgressFull.Render(repeatChar('=', filled))
	}
	if empty > 0 {
		bar += styles.ProgressBar.Render(repeatChar('-', empty))
	}

	return "[" + bar + "]"
}

func repeatChar(c rune, n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]rune, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}
