package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/stt"
	"github.com/caimlas/meept/internal/tts"
	"github.com/caimlas/meept/internal/tui/components"
	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/viz"
	"github.com/tailscale/hujson"
)

// LayoutMode determines how the TUI arranges panels based on terminal size.
type LayoutMode int

const (
	LayoutCompact  LayoutMode = iota // < 80 cols: no sidebar, single panel
	LayoutStandard                   // 80-120 cols: narrow sidebar (25 chars)
	LayoutWide                       // > 120 cols: normal sidebar (35 chars)
)

// ViewType represents the different views in the TUI.
type ViewType int

const (
	ViewChat ViewType = iota
	ViewSessions
	ViewTasks
	ViewQueue
	ViewMemory
	ViewPlans
	ViewSearch
)

// VerbosityLevel controls how much progress detail to show in the TUI.
type VerbosityLevel int

const (
	VerbosityQuiet   VerbosityLevel = 0
	VerbosityNormal  VerbosityLevel = 1
	VerbosityVerbose VerbosityLevel = 2
)

func (v VerbosityLevel) String() string {
	switch v {
	case VerbosityQuiet:
		return "quiet"
	case VerbosityNormal:
		return "normal"
	case VerbosityVerbose:
		return "verbose"
	default:
		return "unknown"
	}
}

func parseVerbosity(s string) VerbosityLevel {
	switch s {
	case "quiet":
		return VerbosityQuiet
	case "verbose":
		return VerbosityVerbose
	default:
		return VerbosityNormal
	}
}

// AppFocus tracks which component has focus.
type AppFocus int

const (
	FocusChat AppFocus = iota
	FocusSidebar
)

// App is the main bubbletea model for the TUI.
type App struct {
	width        int
	height       int
	sidebarWidth int // cached for status bar width calculation
	layoutMode   LayoutMode
	styles       *Styles
	rpc          *RPCClient
	eventRPC     *RPCClient // Separate connection for event polling
	currentView  ViewType

	// Sub-models for each view
	chat     *models.ChatModel
	sessions *models.SessionsModel
	tasks    *models.TasksModel
	queue    *models.QueueModel
	memory   *models.MemoryModel
	plans    *models.PlansModel
	search   *models.SearchModel

	// Sidebar
	sidebar *SidebarModel

	// Focus management
	appFocus AppFocus

	// Key bindings
	keys KeyMap

	// Client configuration
	clientConfig *ClientConfig

	// Text-to-speech manager
	ttsManager *tts.Manager

	// Modal state
	activeModal    ModalType
	commandPalette *Modal
	sessionPicker  *SessionPickerModal
	sessionRename  *SessionRenameModal
	confirmModal   *ConfirmModal
	fuzzyFinder    *FuzzyFinderModal
	branchPicker   *BranchPickerModal
	projectPicker  *ProjectPickerModal

	// Current session
	currentSession *types.Session

	// Current project
	currentProjectID    string
	currentProjectName  string
	currentProjectMode  string
	currentProjectDirty bool
	currentProjectBranch string

	// SessionManager is the shared session manager used by both TUI and meept-lite
	sessionMgr *sharedclient.SessionManager

	// Project directory
	projectDir string

	// Status message (for clipboard feedback)
	statusMessage     string
	statusMessageTime time.Time

	// Double-press tracking for Ctrl-C/Ctrl-D
	lastCtrlC      time.Time
	lastCtrlD      time.Time
	doublePressTTL time.Duration

	// Tab flash indicator (shows notification dot on tab)
	tabFlash     map[ViewType]bool
	tabFlashTime time.Time

	// Slash command handling
	commandHandler    *CommandHandler
	slashAutocomplete *SlashAutocomplete

	// Error state
	err error

	// Verbosity level for agent progress display
	verbosity VerbosityLevel

	// Toast notifications
	notifications *components.NotificationManager
}

// KeyMap defines the key bindings.
type KeyMap struct {
	Quit      key.Binding
	Enter     key.Binding
	Escape    key.Binding
	Help      key.Binding
	Command   key.Binding // Ctrl+X prefix
	ToggleTTS key.Binding // Ctrl+T
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Enter: key.NewBinding(
			key.WithKeys(KeyEnter),
			key.WithHelp(KeyEnter, "submit"),
		),
		Escape: key.NewBinding(
			key.WithKeys(KeyEsc),
			key.WithHelp(KeyEsc, "cancel"),
		),
		Help: key.NewBinding(
			key.WithKeys("ctrl+?"),
			key.WithHelp("ctrl+?", "help"),
		),
		Command: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("ctrl+x", "command mode"),
		),
		ToggleTTS: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "toggle TTS"),
		),
	}
}

// NewApp creates a new TUI application.
func NewApp(socketPath string) *App {
	rpc := NewRPCClient(socketPath)
	// Separate RPC client for event stream polling so it doesn't block
	// on the main client's callMu while a Chat call is in-flight
	eventRPC := NewRPCClient(socketPath)
	styles := DefaultStyles()

	// Load client configuration
	clientConfig, _ := LoadClientConfig()

	// Get current working directory for display
	projectDir, _ := os.Getwd()

	// Create input behavior config from client config
	inputConfig := models.InputBehaviorConfig{
		EnterBehavior: clientConfig.Input.EnterBehavior,
		AutoExpand:    clientConfig.Input.AutoExpand,
	}

	app := &App{
		styles:      styles,
		rpc:         rpc,
		sessionMgr:  sharedclient.NewSessionManager(rpc, clientConfig.Session.DefaultName),
		eventRPC:    eventRPC,
		currentView: ViewChat,
		chat: models.NewChatModelWithConfig(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage, clientConfig.Keybindings.EscapeBehavior, inputConfig, models.ChatConfig{
			AutoCopyOnRelease: clientConfig.Chat.AutoCopyOnRelease,
			ScrollSpeed:       clientConfig.Chat.ScrollSpeed,
		}),
		tasks:          models.NewTasksModel(rpc),
		sessions:       models.NewSessionsModel(rpc),
		queue:          models.NewQueueModel(rpc),
		memory:         models.NewMemoryModel(rpc),
		plans:          models.NewPlansModel(rpc),
		search:         models.NewSearchModel(rpc, slog.Default()),
		sidebar:        NewSidebarModel(rpc, eventRPC, styles, clientConfig.Rendering.SidebarAnimation),
		keys:           DefaultKeyMap(),
		clientConfig:   clientConfig,
		projectDir:     projectDir,
		activeModal:    ModalNone,
		doublePressTTL: 500 * time.Millisecond,
		tabFlash:       make(map[ViewType]bool),
	}

	// Create modals
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)
	app.sessionRename = NewSessionRenameModal(styles)
	app.fuzzyFinder = NewFuzzyFinderModal(styles, rpc)
	app.branchPicker = NewBranchPickerModal(styles, rpc)
	app.projectPicker = NewProjectPickerModal(styles, rpc)

	// Initialize slash autocomplete
	app.slashAutocomplete = NewSlashAutocomplete(styles)

	// Initialize command handler for slash commands
	handlerOpts := []CommandHandlerOption{
		WithChatModelGetter(func() *models.ChatModel {
			return app.chat
		}),
	}
	// Wire /skill command via RPC-backed lister (nil-guard for defense in depth).
	if rpc != nil {
		handlerOpts = append(handlerOpts, WithSkillLister(rpc))
	}
	// Initialize notification manager before constructing the command handler
	// so /dnd can wire the toggler.
	app.notifications = components.NewNotificationManager()
	// Apply TUI-local DoNotDisturb from client config (independent of
	// daemon-wide DND: this only gates local toast rendering).
	if clientConfig.Notifications.DoNotDisturb {
		app.notifications.SetDoNotDisturb(true)
	}
	handlerOpts = append(handlerOpts, WithNotificationToggler(app.notifications))
	app.commandHandler = NewCommandHandler(rpc, handlerOpts...)

	// Initialize STT (speech-to-text) from client config
	if clientConfig.STT.Enabled {
		app.chat.InitSTT(
			stt.Config{
				Engine:   clientConfig.STT.Engine,
				Language: clientConfig.STT.Language,
			},
			true,
			clientConfig.STT.AutoSend,
		)
	}

	// Initialize TTS (text-to-speech) with config merging
	// Priority: client.json5 overrides meept.json5 for Enabled/Engine/Voice
	// Playback and Behavior come from meept.json5 (or defaults)
	if clientConfig.TTS.Enabled {
		// Start with main config TTS settings as base
		mainCfg := loadMainConfigForTTS()
		ttsCfg := tts.Config{
			Engine:    clientConfig.TTS.Engine, // client.json5 overrides
			Voice:     clientConfig.TTS.Voice,  // client.json5 overrides
			VoicePath: "",
			Playback:  mainCfg.Playback, // from meept.json5
			Behavior:  mainCfg.Behavior, // from meept.json5
		}
		// Client config can override playback/behavior if specified
		if clientConfig.TTS.Playback.Volume != 0 {
			ttsCfg.Playback.Volume = clientConfig.TTS.Playback.Volume
		}
		if clientConfig.TTS.Playback.Rate != 0 {
			ttsCfg.Playback.Rate = clientConfig.TTS.Playback.Rate
		}
		if clientConfig.TTS.Behavior.InterruptOnNewMsg {
			ttsCfg.Behavior.InterruptOnNewMsg = clientConfig.TTS.Behavior.InterruptOnNewMsg
		}
		if clientConfig.TTS.Behavior.MaxQueueSize != 0 {
			ttsCfg.Behavior.MaxQueueSize = clientConfig.TTS.Behavior.MaxQueueSize
		}

		// Set default voice path
		voicePath, err := tts.DefaultVoicePath(ttsCfg.Voice)
		if err == nil {
			ttsCfg.Piper.ModelPath = voicePath
			ttsCfg.Piper.ConfigPath = voicePath + ".json"
		}

		if mgr, err := tts.NewManager(ttsCfg); err == nil {
			app.ttsManager = mgr
			app.chat.InitTTS(mgr, true)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: TTS initialization failed: %v\n", err)
		}
	}

	// Initialize verbosity from client config
	app.verbosity = parseVerbosity(clientConfig.Chat.Verbosity)

	return app
}

// loadMainConfigForTTS loads the main meept.json5 config and returns TTS settings.
// Used for config merging: meept.json5 provides Playback and Behavior defaults,
// while client.json5 can override Enabled, Engine, and Voice.
func loadMainConfigForTTS() tts.Config {
	// Default TTS config
	defaultCfg := tts.Config{
		Playback: tts.PlaybackConfig{
			Volume: 1.0,
			Rate:   1.0,
		},
		Behavior: tts.BehaviorConfig{
			InterruptOnNewMsg: true,
			MaxQueueSize:      5,
		},
	}

	// Try project-local first
	mainPath := ".meept/meept.json5"
	if cfg := loadMainConfigFile(mainPath); cfg != nil {
		return *cfg
	}

	// Try user home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		homePath := filepath.Join(homeDir, ".meept", "meept.json5")
		if cfg := loadMainConfigFile(homePath); cfg != nil {
			return *cfg
		}
	}

	return defaultCfg
}

// loadMainConfigFile loads TTS config from main meept.json5 file.
// loadMainConfigFile loads TTS config from main meept.json5 file.
func loadMainConfigFile(path string) *tts.Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	// Parse JSON5
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil
	}

	// Unmarshal into main config schema to get TTS section
	var mainCfg config.Config
	if err := json.Unmarshal(standardJSON, &mainCfg); err != nil {
		return nil
	}

	// Convert main config TTS to tts.Config (type conversion)
	return &tts.Config{
		Engine: mainCfg.TTS.Engine,
		Voice:  mainCfg.TTS.Voice,
		Playback: tts.PlaybackConfig{
			Volume: mainCfg.TTS.Playback.Volume,
			Rate:   mainCfg.TTS.Playback.Rate,
		},
		Behavior: tts.BehaviorConfig{
			ReadOwnMessages:   mainCfg.TTS.Behavior.ReadOwnMessages,
			InterruptOnNewMsg: mainCfg.TTS.Behavior.InterruptOnNewMsg,
			QueueMessages:     mainCfg.TTS.Behavior.QueueMessages,
			MaxQueueSize:      mainCfg.TTS.Behavior.MaxQueueSize,
		},
	}
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.connectDaemon,
		a.loadSession,
	)
}

// loadSession attempts to load or create a session using SessionManager.
func (a *App) loadSession() tea.Msg {
	// Wait for connection first - this will be called after connectDaemon
	if !a.rpc.IsConnected() {
		return nil
	}

	// Auto-resume: try to load most recent session before using default
	if a.clientConfig.Session.AutoResume {
		session, err := a.rpc.GetMostRecentSession()
		if err == nil && session != nil {
			a.sessionMgr.SetSession(session)
			return SessionLoadedMsg{Session: session, IsNew: false}
		}
	}

	// Fall back to creating a new session (same logic as SessionManager would do)
	session, err := a.rpc.CreateSession(a.clientConfig.Session.DefaultName)
	if err != nil {
		return SessionLoadedMsg{Session: nil, Err: err, IsNew: false}
	}
	a.sessionMgr.SetSession(session)
	return SessionLoadedMsg{Session: session, IsNew: true}
}

// SessionLoadedMsg indicates a session was loaded or created.
type SessionLoadedMsg struct {
	Session *types.Session
	IsNew   bool
	Err     error
}

// connectDaemon is a command that connects to the daemon.
func (a *App) connectDaemon() tea.Msg {
	if err := a.rpc.Connect(); err != nil {
		return ConnectErrorMsg{Err: err}
	}
	// Connect the event stream RPC client on its own connection.
	// Event streaming is non-critical; if this fails we still report success
	// on the main connection but log the failure for debugging.
	if a.eventRPC != nil {
		if err := a.eventRPC.Connect(); err != nil {
			slog.Debug("event RPC connect failed (event streaming may be degraded)", "error", err)
		}
	}
	return ConnectSuccessMsg{}
}

// ConnectSuccessMsg indicates successful daemon connection.
type ConnectSuccessMsg struct{}

// ConnectErrorMsg indicates a connection error.
type ConnectErrorMsg struct {
	Err error
}

// TemplateNamesLoadedMsg carries template names fetched from the daemon for
// autocomplete integration.
type TemplateNamesLoadedMsg struct {
	Names []string
}

// fetchTemplateNames fetches available template names from the daemon and
// returns them as a TemplateNamesLoadedMsg for the autocomplete system.
func (a *App) fetchTemplateNames() tea.Msg {
	if a.rpc == nil || !a.rpc.IsConnected() {
		return TemplateNamesLoadedMsg{}
	}

	templates, err := a.rpc.ListTemplates()
	if err != nil {
		// Non-fatal: template names are optional for autocomplete
		return TemplateNamesLoadedMsg{}
	}

	names := make([]string, len(templates))
	for i, t := range templates {
		names[i] = t.Name
	}

	return TemplateNamesLoadedMsg{Names: names}
}

// fetchCurrentProject fetches the current project info from the daemon and
// updates the project indicator fields on the App.
func (a *App) fetchCurrentProject() tea.Msg {
	if a.rpc == nil || !a.rpc.IsConnected() {
		return ProjectInfoUpdatedMsg{}
	}

	projects, err := a.rpc.ListProjects()
	if err != nil {
		return ProjectInfoUpdatedMsg{}
	}

	// Find the first active project
	for _, p := range projects.Projects {
		if p.Status == "active" {
			dirty := false
			branch := ""
			if p.Mode == "git" {
				status, err := a.rpc.ProjectStatus(p.ID)
				if err == nil {
					dirty = status.Dirty
					branch = status.Branch
				}
			}
			return ProjectInfoUpdatedMsg{
				ProjectID:   p.ID,
				ProjectName: p.Name,
				ProjectMode: string(p.Mode),
				Dirty:       dirty,
				Branch:      branch,
			}
		}
	}

	return ProjectInfoUpdatedMsg{}
}

// ProjectInfoUpdatedMsg carries updated project info to the App model.
type ProjectInfoUpdatedMsg struct {
	ProjectID   string
	ProjectName string
	ProjectMode string
	Dirty       bool
	Branch      string
}

// Update handles messages and updates the model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		// Calculate responsive layout based on terminal width
		a.calculateLayout()

		// Calculate reserved height for chrome (header + status bar)
		chromeHeight := 1 // status bar
		headerOffset := 0
		if a.clientConfig.Rendering.ShowHeader {
			chromeHeight = 2 // header + status bar
			headerOffset = 2 // header line + newline
		}

		// Calculate sidebar width based on layout mode
		a.sidebarWidth = 0
		if a.sidebar.IsVisible() && a.layoutMode != LayoutCompact {
			switch a.layoutMode {
			case LayoutStandard:
				a.sidebarWidth = 25
			case LayoutWide:
				a.sidebarWidth = max(min(msg.Width*30/100, 35), 25)
			}
		}
		a.sidebar.SetSize(a.sidebarWidth, msg.Height-chromeHeight)

		// Update sub-models with remaining width
		mainWidth := msg.Width - a.sidebarWidth
		a.chat.SetSize(mainWidth, msg.Height-chromeHeight)
		a.chat.SetScreenYOffset(headerOffset)
		a.sessions.SetSize(mainWidth, msg.Height-chromeHeight)
		a.tasks.SetSize(mainWidth, msg.Height-chromeHeight)
		a.queue.SetSize(mainWidth, msg.Height-chromeHeight)
		a.memory.SetSize(mainWidth, msg.Height-chromeHeight)
		a.plans.SetSize(mainWidth, msg.Height-chromeHeight)
		if a.search != nil {
			a.search.SetSize(mainWidth, msg.Height-chromeHeight)
		}

		return a, nil

	case tea.KeyPressMsg:
		// Handle Ctrl+C - copy selection or double-press to exit
		if msg.String() == "ctrl+c" {
			// First check: if chat viewport has active text selection, copy it
			if a.chat != nil && a.currentView == ViewChat && a.chat.HasSelection() {
				return a, a.chat.CopySelection()
			}

			now := time.Now()
			if now.Sub(a.lastCtrlC) < a.doublePressTTL {
				a.sidebar.Cleanup()
				a.rpc.Close()
				if a.eventRPC != nil {
					a.eventRPC.Close()
				}
				if a.ttsManager != nil {
					a.ttsManager.Close()
				}
				return a, tea.Quit
			}
			a.lastCtrlC = now

			// If chat is loading, stop current work
			if a.chat != nil && a.chat.IsLoading() {
				cmd := a.stopCurrentWork()
				return a, cmd
			}

			// Show hint message
			a.statusMessage = "press ctrl+c again to exit"
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}

		// Handle Ctrl+D - double-press to exit
		if msg.String() == "ctrl+d" {
			now := time.Now()
			if now.Sub(a.lastCtrlD) < a.doublePressTTL {
				a.sidebar.Cleanup()
				a.rpc.Close()
				if a.eventRPC != nil {
					a.eventRPC.Close()
				}
				if a.ttsManager != nil {
					a.ttsManager.Close()
				}
				return a, tea.Quit
			}
			a.lastCtrlD = now
			a.statusMessage = "press ctrl+d again to exit"
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}

		// Handle modal key input first
		if a.activeModal != ModalNone {
			return a.handleModalKey(msg)
		}

		// Check for Ctrl+X to open command palette
		if key.Matches(msg, a.keys.Command) {
			a.activeModal = ModalCommandPalette
			a.commandPalette.Show()
			return a, nil
		}

		// Check for Ctrl+T to toggle TTS
		if key.Matches(msg, a.keys.ToggleTTS) {
			if a.ttsManager != nil {
				// Toggle TTS enabled state
				enabled := a.chat.ToggleTTS()
				status := "off"
				if enabled {
					status = "on"
				}
				a.statusMessage = fmt.Sprintf("TTS: %s", status)
				a.statusMessageTime = time.Now()
			}
			return a, nil
		}

		// Check for Ctrl+S: toggle steer mode when agent active, otherwise navigate to sessions tab
		if msg.String() == "ctrl+s" {
			if a.chat != nil && a.chat.IsAgentActive() {
				// Agent is running - toggle steer mode
				newState := a.chat.ToggleSteerMode()
				status := "off"
				if newState {
					status = "on"
				}
				a.chat.AddSystemMessage(fmt.Sprintf("steer mode: %s", status))
				return a, nil
			}
			// Navigate to sessions tab
			a.currentView = ViewSessions
			a.statusMessage = "sessions tab (create: n, delete: d)"
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			return a, clearCmd
		}

		// Check for Ctrl+V: cycle verbosity level
		if msg.String() == "ctrl+v" {
			a.verbosity = (a.verbosity + 1) % 3
			a.statusMessage = fmt.Sprintf("verbosity: %s", a.verbosity)
			a.statusMessageTime = time.Now()
			return a, nil
		}

		// Check for Ctrl+P to open fuzzy finder
		if msg.String() == "ctrl+p" {
			if a.fuzzyFinder == nil {
				a.statusMessage = "fuzzy finder not initialized"
				a.statusMessageTime = time.Now()
				return a, nil
			}
			a.activeModal = ModalFuzzyFinder
			a.fuzzyFinder.Show()
			return a, tea.Batch(a.fuzzyFinder.FetchSessions(), a.fuzzyFinder.FetchTasks())
		}

		// Check for Ctrl+B to show branch picker
		if msg.String() == "ctrl+b" {
			if a.currentSession == nil {
				a.statusMessage = "no active session"
				a.statusMessageTime = time.Now()
				return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
					return StatusMessageClearMsg{}
				})
			}
			a.activeModal = ModalBranchPicker
			a.branchPicker.Show()
			if a.currentSession.Name != "" {
				a.branchPicker.SetActiveBranchID(a.currentSession.Name)
			}
			return a, a.branchPicker.RefreshBranches(a.currentSession.ID)
		}

		// Global escape handler
		if msg.String() == KeyEsc {
			// If sidebar is focused, unfocus and return to chat
			if a.appFocus == FocusSidebar {
				a.appFocus = FocusChat
				a.sidebar.SetFocused(false)
				if a.currentView == ViewChat {
					return a, a.chat.HandleEscape()
				}
				return a, nil
			}
			// If on non-chat view, switch to chat
			if a.currentView != ViewChat {
				a.currentView = ViewChat
				a.chat.SetFocus(models.FocusInput)
				cmd := a.initCurrentView()
				return a, cmd
			}
			// Delegate to chat's escape handling
			return a, a.chat.HandleEscape()
		}

		// Delegate based on focus
		if a.appFocus == FocusSidebar && a.sidebar.IsVisible() {
			// If it's a printable character, redirect focus to chat input
			if a.currentView == ViewChat && isPrintableKey(msg) {
				a.appFocus = FocusChat
				a.sidebar.SetFocused(false)
				a.chat.SetFocusFromSidebar()
				// Forward the key to the chat view
				cmd := a.chat.Update(msg)
				return a, cmd
			}
			cmd := a.sidebar.Update(msg)
			return a, cmd
		}

		// Slash command autocomplete handling (checked before direct Enter
		// interception so autocomplete selection takes priority when visible)
		if a.currentView == ViewChat && a.appFocus == FocusChat && a.slashAutocomplete != nil {
			// Check if autocomplete is visible
			if a.slashAutocomplete.IsVisible() {
				result, cmd := a.slashAutocomplete.HandleKey(msg.String())
				if result == HandleKeyNavigated {
					// Navigation keys (including escape) - consume and return
					return a, cmd
				}
				if result == HandleKeyInsert && cmd != nil {
					return a, cmd
				}
				// PassThrough - continue to normal input handling
			}

			// Check for slash command trigger: "/" at start of input
			// Show popup immediately when "/" is typed on empty input
			if msg.String() == "/" {
				input := a.chat.GetInputValue()
				// Debug logging - write to file for inspection
				DebugLog(fmt.Sprintf("SLASH /: input=%q (len=%d), appFocus=%v, currentView=%v", input, len(input), a.appFocus, a.currentView))

				if input == "" || strings.HasPrefix(input, "/") {
					a.slashAutocomplete.Show("")
					DebugLog(fmt.Sprintf("SLASH Show(): visible=%v, filtered=%d commands", a.slashAutocomplete.IsVisible(), len(a.slashAutocomplete.GetFilteredCommands())))
				} else {
					DebugLog("SLASH: NOT showing popup, input condition failed")
				}
			}

			// Handle Enter for slash command execution (when autocomplete is
			// not visible or autocomplete returned PassThrough for Enter)
			if msg.String() == KeyEnter {
				input := a.chat.GetInputValue()
				if strings.HasPrefix(strings.TrimSpace(input), "/") {
					cmd := ParseSlash(input)
					if cmd != nil {
						a.chat.SetInputValue("")
						a.slashAutocomplete.Hide()
						return a, a.commandHandler.Execute(cmd)
					}
				}
			}
		} else if a.currentView == ViewChat && a.appFocus == FocusChat && msg.String() == KeyEnter {
			// Slash command detection - intercept Enter when input starts with "/"
			// (only when autocomplete is not initialized)
			input := a.chat.GetInputValue()
			if strings.HasPrefix(strings.TrimSpace(input), "/") {
				// Parse the slash command
				cmd := ParseSlash(input)
				if cmd != nil {
					// Clear the input
					a.chat.SetInputValue("")
					// Execute the command
					return a, a.commandHandler.Execute(cmd)
				}
			}
		}

		// Otherwise fall through to delegate to current view

	case ConnectSuccessMsg:
		a.err = nil
		// Initialize the current view, sidebar, load session, and fetch templates
		return a, tea.Batch(a.initCurrentView(), a.sidebar.Init(), a.loadSession, a.fetchTemplateNames, a.fetchCurrentProject)

	case SessionLoadedMsg:
		if msg.Err != nil {
			a.statusMessage = fmt.Sprintf("session error: %v", msg.Err)
			a.statusMessageTime = time.Now()
		} else if msg.Session != nil {
			a.currentSession = msg.Session
			// Wire up session ID for tasks FilterMine feature
			a.tasks.SetCurrentSession(msg.Session.ID)
			// Wire up session ID for plans filtering
			a.plans.SetSession(msg.Session.ID)
			sessionCmd := a.chat.SetSession(msg.Session)
			if msg.IsNew {
				a.statusMessage = fmt.Sprintf("created session: %s", msg.Session.Name)
			} else {
				a.statusMessage = fmt.Sprintf("resumed session: %s", msg.Session.Name)
			}
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			if sessionCmd != nil {
				return a, tea.Batch(sessionCmd, clearCmd)
			}
			return a, clearCmd
		}
		return a, nil

	case SessionListMsg:
		// Update session picker with session list
		if msg.Err == nil {
			a.sessionPicker.SetSessions(msg.Sessions)
			// Auto-select current session
			if a.currentSession != nil {
				a.sessionPicker.SetCurrentSession(a.currentSession.ID)
			}
			// Fetch plan counts for all sessions (async)
			if len(msg.Sessions) > 0 {
				return a, a.sessionPicker.FetchPlanCounts(msg.Sessions)
			}
		}
		return a, nil

	case PlanCountsMsg:
		if msg.Counts != nil {
			a.sessionPicker.SetPlanCounts(msg.Counts)
		}
		return a, nil

	case SessionSwitchMsg:
		// Switch to selected session
		if msg.Err != nil {
			a.statusMessage = fmt.Sprintf("session switch failed: %v", msg.Err)
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}
		if msg.Session != nil {
			a.currentSession = msg.Session
			a.sessionMgr.SetSession(msg.Session)
			// Wire up session ID for tasks FilterMine feature
			a.tasks.SetCurrentSession(msg.Session.ID)
			// Wire up session ID for plans filtering
			a.plans.SetSession(msg.Session.ID)
			sessionCmd := a.chat.SetSession(msg.Session)
			a.statusMessage = fmt.Sprintf("switched to: %s", msg.Session.Name)
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			// Switch to chat view if requested
			if msg.SwitchToChat {
				a.currentView = ViewChat
			}
			if sessionCmd != nil {
				return a, tea.Batch(sessionCmd, clearCmd)
			}
			return a, clearCmd
		}
		return a, nil

	case models.SessionSwitchToChatMsg:
		// Switch to selected session AND switch view to chat
		if msg.Session != nil {
			a.currentSession = msg.Session
			a.sessionMgr.SetSession(msg.Session)
			// Wire up session ID for tasks FilterMine feature
			a.tasks.SetCurrentSession(msg.Session.ID)
			// Wire up session ID for plans filtering
			a.plans.SetSession(msg.Session.ID)
			sessionCmd := a.chat.SetSession(msg.Session)
			a.statusMessage = fmt.Sprintf("switched to: %s", msg.Session.Name)
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			// Switch to chat view
			a.currentView = ViewChat
			if sessionCmd != nil {
				return a, tea.Batch(sessionCmd, clearCmd)
			}
			return a, clearCmd
		}
		return a, nil

	case SessionCreateMsg:
		// Create a new session
		cmd := a.createSession(msg.Name)
		return a, cmd

	case SessionDeleteMsg:
		// Delete a session
		cmd := a.deleteSession(msg.SessionID)
		return a, cmd

	case models.OpenCreateSessionModalMsg:
		// Open rename modal for creating a new session (uses default name)
		a.activeModal = ModalSessionRename
		a.sessionRename.Show("", a.clientConfig.Session.DefaultName)
		return a, nil

	case models.OpenSearchViewMsg:
		// Switch to the global search view
		a.currentView = ViewSearch
		if a.search != nil {
			return a, a.search.Init()
		}
		return a, nil

	case models.CloseSearchViewMsg:
		// Return to the sessions view
		a.currentView = ViewSessions
		return a, nil

	case models.NavigateToSessionMsg:
		// Navigate from search results to a session in chat view.
		// For MVP, loading the session is sufficient; scrolling to a
		// specific message is logged for future enhancement.
		if msg.SessionID != "" {
			// Find the session by ID and load it into chat
			return a, a.switchToSessionByID(msg.SessionID, msg.MessageID)
		}
		return a, nil

	case OpenRenameModalMsg:
		// Open rename modal for a session
		a.activeModal = ModalSessionRename
		a.sessionRename.Show(msg.SessionID, msg.CurrentName)
		return a, nil

	case SessionRenameMsg:
		if msg.SessionID == "" {
			// Create new session (sessionID is empty when opened from create modal)
			cmd := a.createSession(msg.NewName)
			return a, cmd
		}
		// Rename a session (update description)
		cmd := a.renameSession(msg.SessionID, msg.NewName)
		return a, cmd

	case SidebarDataMsg:
		// Delegate to sidebar
		return a, a.sidebar.Update(msg)

	case FuzzyFinderSessionsMsg:
		if a.fuzzyFinder != nil {
			a.fuzzyFinder.SetSessions(msg.Sessions)
		}
		return a, nil

	case FuzzyFinderTasksMsg:
		if a.fuzzyFinder != nil {
			a.fuzzyFinder.SetTasks(msg.Tasks)
		}
		return a, nil

	case EventStreamTickMsg:
		// Forward event stream tick to sidebar for polling
		return a, a.sidebar.Update(msg)

	case EventStreamDataMsg:
		// Process progress events directly to update chat model
		var cmds []tea.Cmd
		for _, e := range msg.Events {
			switch e.Topic {
			case "agent.progress":
				// Extract progress data and update chat directly
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					progressMsg := models.ProgressUpdateMsg{}
					if v, ok := payloadMap["agent_id"].(string); ok {
						progressMsg.AgentID = v
					} else if v, ok := payloadMap["conversation_id"].(string); ok {
						progressMsg.AgentID = v
					}
					if v, ok := payloadMap["stage"].(string); ok {
						progressMsg.Stage = v
					}
					if v, ok := payloadMap["detail"].(string); ok {
						progressMsg.CurrentTool = v
					}
					if v, ok := payloadMap["percent"].(float64); ok {
						progressMsg.Percent = v
					} else if iteration, ok := payloadMap["iteration"].(float64); ok {
						progressMsg.Percent = iteration * 10.0
						if progressMsg.Percent > 100 {
							progressMsg.Percent = 100
						}
					}
					if v, ok := payloadMap["token_count"].(float64); ok {
						progressMsg.TokensUsed = int(v)
					}
					// Update chat directly
					if cmd := a.chat.Update(progressMsg); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case "llm.tokens.used":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if totalTokens, ok := payloadMap["total_tokens"].(float64); ok {
						progressMsg := models.ProgressUpdateMsg{TokensUsed: int(totalTokens)}
						a.chat.Update(progressMsg)
					}
				}
			case "conversation.reset":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if resets, ok := payloadMap["resets"].(float64); ok {
						progressMsg := models.ProgressUpdateMsg{ContextResets: int(resets)}
						a.chat.Update(progressMsg)
					}
				}
			case "task.progress":
				// Extract chat_visible flag and update chat if visible
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					chatVisible := true // Default to visible for backwards compatibility
					if cv, ok := payloadMap["chat_visible"].(bool); ok {
						chatVisible = cv
					}

					// Only update chat if chat_visible is true
					if chatVisible {
						currentStep, _ := payloadMap["current_step"].(string)
						if currentStep != "" {
							progressMsg := models.ProgressUpdateMsg{
								Stage:       "task progress",
								CurrentTool: currentStep,
							}
							if completed, ok := payloadMap["completed_jobs"].(float64); ok {
								total, _ := payloadMap["total_jobs"].(float64)
								if total > 0 {
									progressMsg.Percent = (completed / total) * 100
								}
							}
							if cmd := a.chat.Update(progressMsg); cmd != nil {
								cmds = append(cmds, cmd)
							}
						}
					}
				}
			case "task.error":
				// Handle step errors - display in chat immediately
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					errMsg, _ := payloadMap["error"].(string)
					stepDesc, _ := payloadMap["step_id"].(string)
					taskID, _ := payloadMap["task_id"].(string)

					if errMsg != "" {
						// Display error in chat as a task result message
						resultMsg := models.ChatTaskResultMsg{
							State:         "failed",
							TaskID:        taskID,
							ResultSummary: fmt.Sprintf("step %s failed: %s", stepDesc, errMsg),
						}
						if cmd := a.chat.Update(resultMsg); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}
			case "step.review_completed", "task.review_completed":
				// Handle review events - show rejection/revision feedback in chat
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					reviewStatus, _ := payloadMap["status"].(string)
					feedback, _ := payloadMap["feedback"].(string)
					taskID, _ := payloadMap["task_id"].(string)
					stepID, _ := payloadMap["step_id"].(string)
					revisionCount := 0
					if rc, ok := payloadMap["revision_count"].(float64); ok {
						revisionCount = int(rc)
					}

					switch reviewStatus {
					case "rejected":
						// Show rejection with feedback and revision info
						summary := fmt.Sprintf("step %s: review rejected", stepID)
						if revisionCount > 0 {
							summary = fmt.Sprintf("step %s: review rejected (revision #%d)", stepID, revisionCount)
						}
						if feedback != "" {
							summary += fmt.Sprintf(" -- %s", feedback)
						}
						resultMsg := models.ChatTaskResultMsg{
							State:         "reviewing",
							TaskID:        taskID,
							ResultSummary: summary,
						}
						if cmd := a.chat.Update(resultMsg); cmd != nil {
							cmds = append(cmds, cmd)
						}
					case "needs_info":
						// Show needs-info feedback
						summary := fmt.Sprintf("step %s: review needs more info", stepID)
						if feedback != "" {
							summary += fmt.Sprintf(" -- %s", feedback)
						}
						resultMsg := models.ChatTaskResultMsg{
							State:         "reviewing",
							TaskID:        taskID,
							ResultSummary: summary,
						}
						if cmd := a.chat.Update(resultMsg); cmd != nil {
							cmds = append(cmds, cmd)
						}
					case "approved":
						// Show approval (lighter touch - just a progress update)
						progressMsg := models.ProgressUpdateMsg{
							Stage:       "review approved",
							CurrentTool: fmt.Sprintf("step %s approved", stepID),
							Percent:     100,
							ChatVisible: true,
						}
						if cmd := a.chat.Update(progressMsg); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}
			case "tool.execution.progress":
				// Extract tool-level streaming progress
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					progressMsg := models.ProgressUpdateMsg{}
					if v, ok := payloadMap["tool_name"].(string); ok {
						progressMsg.ToolName = v
						progressMsg.CurrentTool = v
					}
					if v, ok := payloadMap["message"].(string); ok {
						progressMsg.ToolMessage = v
						progressMsg.Stage = v
					}
					if v, ok := payloadMap["percent"].(float64); ok {
						progressMsg.ToolPercent = int(v)
						progressMsg.Percent = v
					}
					if v, ok := payloadMap["agent_id"].(string); ok {
						progressMsg.AgentID = v
					}
					if cmd := a.chat.Update(progressMsg); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case "task.completed", EventTaskFailed:
				// Inject task result message into chat
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					resultMsg := models.ChatTaskResultMsg{
						State: "completed",
					}
					if e.Topic == EventTaskFailed {
						resultMsg.State = "failed"
					}
					if v, ok := payloadMap["task_id"].(string); ok {
						resultMsg.TaskID = v
					}
					if v, ok := payloadMap["name"].(string); ok {
						resultMsg.TaskName = v
					}
					if v, ok := payloadMap["completed_jobs"].(float64); ok {
						resultMsg.CompletedSteps = int(v)
					}
					if v, ok := payloadMap["total_jobs"].(float64); ok {
						resultMsg.TotalSteps = int(v)
					}
					if v, ok := payloadMap["result"].(string); ok {
						resultMsg.ResultSummary = v
					}
					if cmd := a.chat.Update(resultMsg); cmd != nil {
						cmds = append(cmds, cmd)
					}

					// Push toast notification
					if a.notifications != nil && resultMsg.TaskName != "" {
						level := components.NotifySuccess
						title := "task completed"
						msg := resultMsg.TaskName
						if e.Topic == EventTaskFailed {
							level = components.NotifyError
							title = "task failed"
						}
						_, expiryCmd := a.notifications.Push(level, title, msg)
						if expiryCmd != nil {
							cmds = append(cmds, expiryCmd)
						}
					}
				}
				// Flash the Tasks tab indicator if not currently viewing tasks
				if a.currentView != ViewTasks {
					a.tabFlash[ViewTasks] = true
					a.tabFlashTime = time.Now()
				}

			// Agent lifecycle events
			case bus.EventAgentStarted:
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if convID, ok := payloadMap["conversation_id"].(string); ok {
						if cmd := a.chat.Update(models.AgentLifecycleMsg{Active: true, ConversationID: convID}); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}
			case bus.EventAgentEnded:
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if convID, ok := payloadMap["conversation_id"].(string); ok {
						if cmd := a.chat.Update(models.AgentLifecycleMsg{Active: false, ConversationID: convID}); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}

			// Steering queue events
			case bus.EventQueueSteerInjected:
				if cmd := a.chat.Update(models.SteeringInjectedMsg{}); cmd != nil {
					cmds = append(cmds, cmd)
				}

			// Follow-up queue events
			case bus.EventQueueFollowUpInjected:
				if cmd := a.chat.Update(models.FollowUpInjectedMsg{}); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case bus.EventQueueFollowUpRestored:
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if count, ok := payloadMap["count"].(float64); ok {
						if cmd := a.chat.Update(models.FollowUpRestoredMsg{Count: int(count)}); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}

			// Queue status update events
			case bus.EventQueueStatus:
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					status := &types.QueueStatusResponse{}
					if sd, ok := payloadMap["steering_depth"].(float64); ok {
						status.SteeringDepth = int(sd)
					}
					if fd, ok := payloadMap["followup_depth"].(float64); ok {
						status.FollowUpDepth = int(fd)
					}
					if ia, ok := payloadMap["is_active"].(bool); ok {
						status.IsActive = ia
					}
					if gen, ok := payloadMap["generation"].(float64); ok {
						status.Generation = uint64(gen)
					}
					a.chat.UpdateQueueStatus(status)
				}

			// Multi-client participant messages
			case "chat.message.received":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					sourceClient, _ := payloadMap["source_client"].(string)
					content, _ := payloadMap["content"].(string)
					if sourceClient != "" && sourceClient != "tui" && content != "" && a.chat != nil {
						a.chat.AddParticipantMessage(sourceClient, content)
					}
				}

			// Synthesized progress events (filtered by verbosity)
			case "agent.progress.synthesized":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					tier := VerbosityNormal
					if v, ok := payloadMap["tier"].(float64); ok {
						tier = VerbosityLevel(int(v))
					}
					message, _ := payloadMap["message"].(string)
					if tier <= a.verbosity && message != "" && a.chat != nil {
						a.chat.AddSystemMessage(message)
					}
				}

			// Plan lifecycle events - inline chat notifications
			case "plan.submitting", "plan.completed", "plan.confirmed", "plan.rejected":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					planMsg := models.PlanNotificationMsg{
						Timestamp: e.Timestamp,
					}
					if v, ok := payloadMap["plan_id"].(string); ok {
						planMsg.PlanID = v
					}
					if v, ok := payloadMap["title"].(string); ok {
						planMsg.Title = v
					}
					if v, ok := payloadMap["phase_count"].(float64); ok {
						planMsg.PhaseCount = int(v)
					}
					if v, ok := payloadMap["step_count"].(float64); ok {
						planMsg.StepCount = int(v)
					}
					if v, ok := payloadMap["by"].(string); ok {
						planMsg.By = v
					}
					if v, ok := payloadMap["reason"].(string); ok {
						planMsg.Reason = v
					}
					// Derive state from topic
					switch e.Topic {
					case "plan.submitting":
						planMsg.State = "submitting"
					case "plan.completed":
						planMsg.State = "completed"
					case "plan.confirmed":
						planMsg.State = "confirmed"
					case "plan.rejected":
						planMsg.State = "rejected"
					}
					if cmd := a.chat.Update(planMsg); cmd != nil {
						cmds = append(cmds, cmd)
					}
					// Flash the Plans tab indicator if not currently viewing plans
					if a.currentView != ViewPlans {
						a.tabFlash[ViewPlans] = true
						a.tabFlashTime = time.Now()
					}
				}
			}
		}
		// Also forward to sidebar for activity feed updates
		if sidebarCmd := a.sidebar.Update(msg); sidebarCmd != nil {
			cmds = append(cmds, sidebarCmd)
		}
		if len(cmds) > 0 {
			return a, tea.Batch(cmds...)
		}
		return a, nil

	case models.ProgressUpdateMsg:
		// Forward progress updates to chat model
		return a, a.chat.Update(msg)

	case viz.VizTickMsg:
		// Forward viz tick to sidebar
		if a.sidebar.IsVisible() {
			return a, a.sidebar.Update(msg)
		}
		return a, nil

	case models.ChatFocusSidebarMsg:
		// Chat wants to move focus to sidebar
		if a.sidebar.IsVisible() {
			a.appFocus = FocusSidebar
			a.sidebar.SetFocused(true)
		} else {
			// No sidebar, cycle back to chat input
			a.chat.SetFocusFromSidebar()
		}
		return a, nil

	case SidebarFocusChatMsg:
		// Sidebar wants to move focus back to chat
		a.appFocus = FocusChat
		a.sidebar.SetFocused(false)
		a.chat.SetFocusFromSidebar()
		return a, nil

	case ConnectErrorMsg:
		a.err = msg.Err
		return a, nil

	case models.CopyToClipboardMsg:
		// Handle clipboard copy request from chat view
		return a, doCopy(msg.Text)

	case models.ChatToastMsg:
		// Handle toast notification request from chat view
		if a.notifications != nil {
			level := components.NotifyInfo
			if msg.Title == "stt error" || msg.Title == "tts error" {
				level = components.NotifyError
			}
			_, expiryCmd := a.notifications.Push(level, msg.Title, msg.Message)
			return a, expiryCmd
		}
		return a, nil

	case CopySuccessMsg:
		// Copy silently - do not display a "Copied: ..." status message.
		return a, nil

	case CopyErrorMsg:
		a.statusMessage = fmt.Sprintf("copy failed: %v", msg.Err)
		a.statusMessageTime = time.Now()

	case RenameErrorMsg:
		a.statusMessage = fmt.Sprintf("rename failed: %v", msg.Err)
		a.statusMessageTime = time.Now()

	case CommandResultMsg:
		// Handle slash command result
		if msg.Result != nil {
			// Display the output in the chat transcript (not status bar)
			if msg.Result.Output != "" {
				a.chat.AddSystemMessage(msg.Result.Output)
			}

			// Handle special actions
			if msg.Result.ClearConversation {
				a.chat.ClearConversation()
			}

			// Handle retry - re-send the last message
			if msg.Result.RetryLast {
				// The chat model already has the input set to the last user message
				// from RetryLast(), so we just need to trigger send
				return a, a.chat.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			}

			// Handle vim toggle - mode was already toggled by the command handler.
			// (ToggleVimMode consumed in command handler; no-op here)

			// Handle project switch
			if msg.Result.SetProjectID != "" {
				if a.currentSession != nil {
					sessionID := a.currentSession.ID
					projectID := msg.Result.SetProjectID
					return a, tea.Batch(
						func() tea.Msg {
							err := a.rpc.SetProject(sessionID, projectID)
							return SetProjectResultMsg{Err: err}
						},
						tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
							return StatusMessageClearMsg{}
						}),
						a.fetchCurrentProject,
					)
				}
			}

			// Clear status message after delay
			return a, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}
		return a, nil

	case StopSessionResultMsg:
		a.activeModal = ModalNone
		if msg.Error != nil {
			a.statusMessage = fmt.Sprintf("stop failed: %v", msg.Error)
		} else {
			workers := len(msg.Response.WorkersStopped)
			a.statusMessage = fmt.Sprintf("stopped %d worker(s)", workers)
			// Reset loading state in chat
			if a.chat != nil {
				a.chat.ClearLoading()
			}
		}
		a.statusMessageTime = time.Now()
		return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})

	case StatusMessageClearMsg:
		if time.Since(a.statusMessageTime) >= 2*time.Second {
			a.statusMessage = ""
		}
		return a, nil

	case StopWorkChildTasksMsg:
		return a, a.handleStopWorkChildTasks(msg)

	case BranchInfoMsg:
		if a.activeModal == ModalBranchPicker {
			// Populate the branch picker modal
			if msg.Err != nil {
				a.branchPicker.SetBranches(nil)
				a.statusMessage = fmt.Sprintf("branch error: %v", msg.Err)
				a.statusMessageTime = time.Now()
			} else {
				a.branchPicker.SetBranches(msg.Branches)
			}
			return a, nil
		}
		// Fallback: show branch info as status message
		switch {
		case msg.Err != nil:
			a.statusMessage = fmt.Sprintf("branch error: %v", msg.Err)
		case len(msg.Branches) == 0:
			a.statusMessage = "no branches"
		default:
			names := make([]string, len(msg.Branches))
			for i, b := range msg.Branches {
				names[i] = b.ID
			}
			a.statusMessage = fmt.Sprintf("branches: %s", strings.Join(names, ", "))
		}
		a.statusMessageTime = time.Now()
		return a, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})

	case BranchNavigateMsg:
		a.activeModal = ModalNone
		if msg.Err != nil {
			a.statusMessage = fmt.Sprintf("navigate failed: %v", msg.Err)
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}
		// Navigate to the selected branch via RPC
		if a.currentSession != nil {
			sessionID := a.currentSession.ID
			targetLeafID := msg.Branch.LeafID
			branchID := msg.Branch.ID
			return a, func() tea.Msg {
				err := a.rpc.NavigateBranch(sessionID, targetLeafID)
				if err != nil {
					return BranchNavigateMsg{Err: err}
				}
				return BranchNavigateResultMsg{BranchID: branchID}
			}
		}
		return a, nil

	case BranchNavigateResultMsg:
		a.branchPicker.SetActiveBranchID(msg.BranchID)
		a.statusMessage = fmt.Sprintf("switched to branch: %s", msg.BranchID)
		a.statusMessageTime = time.Now()
		return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})

	case components.NotificationExpiredMsg:
		if a.notifications != nil {
			a.notifications.Update(msg)
		}
		return a, nil

	case models.SessionDescriptionUpdatedMsg:
		// Update the app-level session name and description so tab bar and picker reflect it
		if a.currentSession != nil && msg.SessionID == a.currentSession.ID {
			if msg.Name != "" {
				a.currentSession.Name = msg.Name
			}
			a.currentSession.Description = msg.Description
		}
		// Sync SessionManager so GetSessionName reflects the new description
		if a.sessionMgr != nil {
			if a.currentSession != nil && a.currentSession.ID == msg.SessionID {
				a.sessionMgr.SetSession(a.currentSession)
			}
		}
		// Still delegate to chat model

	case SlashAutocompleteMsg:
		// Insert the selected command and execute it
		if a.currentView == ViewChat && a.appFocus == FocusChat {
			a.chat.SetInputValue(msg.Command + " ")
			a.slashAutocomplete.Hide()
			// Parse and execute the command
			cmd := ParseSlash(msg.Command + " ")
			if cmd != nil {
				return a, a.commandHandler.Execute(cmd)
			}
		}
		return a, nil

	case TemplateNamesLoadedMsg:
		// Merge template names into the autocomplete command list
		if a.slashAutocomplete != nil && len(msg.Names) > 0 {
			a.slashAutocomplete.MergeCommands(msg.Names)
		}
		return a, nil

	case ProjectInfoUpdatedMsg:
		a.currentProjectID = msg.ProjectID
		a.currentProjectName = msg.ProjectName
		a.currentProjectMode = msg.ProjectMode
		a.currentProjectDirty = msg.Dirty
		a.currentProjectBranch = msg.Branch
		return a, nil

	case ProjectListMsg:
		// Update project picker with project list
		if msg.Err == nil {
			a.projectPicker.SetProjects(msg.Projects)
		}
		return a, nil

	case ProjectSelectMsg:
		// Bind selected project to current session
		if a.currentSession != nil && msg.ProjectID != "" {
			sessionID := a.currentSession.ID
			projectID := msg.ProjectID
			a.statusMessage = fmt.Sprintf("project set: %s", msg.ProjectID)
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			return a, tea.Batch(
				func() tea.Msg {
					err := a.rpc.SetProject(sessionID, projectID)
					return SetProjectResultMsg{Err: err}
				},
				clearCmd,
				a.fetchCurrentProject,
			)
		}
		return a, nil

	case SetProjectResultMsg:
		if msg.Err != nil {
			a.statusMessage = fmt.Sprintf("failed to set project: %v", msg.Err)
			a.statusMessageTime = time.Now()
		}
		return a, nil
	}

	// Handle slash autocomplete filter updates when typing
	if a.slashAutocomplete != nil && a.currentView == ViewChat && a.appFocus == FocusChat && a.slashAutocomplete.IsVisible() {
		// Check for key press or check if we just showed the popup
		currentInput := a.chat.GetInputValue()
		if after, ok := strings.CutPrefix(currentInput, "/"); ok {
			// Normal case: input has "/" prefix
			filter := after
			filter = strings.TrimSpace(filter)
			a.slashAutocomplete.SetFilter(filter)
			if len(a.slashAutocomplete.GetFilteredCommands()) == 0 {
				a.slashAutocomplete.Hide()
			}
		} else if currentInput == "" && a.slashAutocomplete.IsVisible() {
			// Special case: "/" was just typed but not yet in textarea
			// Show all commands
			a.slashAutocomplete.SetFilter("")
		}
	}

	// Delegate to current view
	var cmd tea.Cmd
	switch a.currentView {
	case ViewChat:
		cmd = a.chat.Update(msg)
	case ViewSessions:
		cmd = a.sessions.Update(msg)
	case ViewTasks:
		cmd = a.tasks.Update(msg)
	case ViewQueue:
		cmd = a.queue.Update(msg)
	case ViewMemory:
		cmd = a.memory.Update(msg)
	case ViewPlans:
		cmd = a.plans.Update(msg)
	case ViewSearch:
		if a.search != nil {
			cmd = a.search.Update(msg)
		}
	}
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

// handleModalKey processes key input when a modal is active.
func (a *App) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()

	switch a.activeModal {
	case ModalCommandPalette:
		action := a.commandPalette.HandleKey(keyStr)
		if !a.commandPalette.IsVisible() {
			a.activeModal = ModalNone
		}

		// Handle command palette actions
		keys := a.clientConfig.Keybindings.CommandPalette
		switch action {
		case keys.ViewChat:
			a.currentView = ViewChat
			cmd := a.initCurrentView()
			return a, cmd
		case keys.ViewSessions:
			a.currentView = ViewSessions
			cmd := a.initCurrentView()
			return a, cmd
		case keys.ViewTasks:
			a.currentView = ViewTasks
			cmd := a.initCurrentView()
			return a, cmd
		case keys.ViewQueue:
			a.currentView = ViewQueue
			cmd := a.initCurrentView()
			return a, cmd
		case keys.ViewMemory:
			a.currentView = ViewMemory
			cmd := a.initCurrentView()
			return a, cmd
		case keys.ViewPlans:
			a.currentView = ViewPlans
			cmd := a.initCurrentView()
			return a, cmd
		case keys.Sidebar:
			a.sidebar.Toggle()
			return a, func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			}
		case keys.Sessions:
			// Navigate to sessions tab (Issue 9: replaces deprecated ModalSessionPicker).
			a.currentView = ViewSessions
			a.statusMessage = "sessions tab (create: n, delete: d)"
			a.statusMessageTime = time.Now()
			return a, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		case keys.NewSession:
			// Create a new session directly with default name
			cmd := a.createSession(a.clientConfig.Session.DefaultName)
			return a, cmd
		case keys.RenameSession:
			// Rename current session
			if a.currentSession != nil {
				currentName := a.currentSession.Description
				if currentName == "" {
					currentName = a.currentSession.Name
				}
				a.activeModal = ModalSessionRename
				a.sessionRename.Show(a.currentSession.ID, currentName)
			}
			return a, nil
		case keys.Projects:
			a.activeModal = ModalProjectPicker
			a.projectPicker.Show()
			return a, a.projectPicker.RefreshProjects()
		}
		return a, nil

	case ModalSessionPicker:
		cmd := a.sessionPicker.HandleKey(keyStr)
		if !a.sessionPicker.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd

	case ModalSessionRename:
		cmd := a.sessionRename.HandleKey(keyStr)
		if !a.sessionRename.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd

	case ModalConfirm:
		if a.confirmModal != nil {
			cmd := a.confirmModal.HandleKey(keyStr)
			if !a.confirmModal.IsVisible() {
				a.activeModal = ModalNone
			}
			return a, cmd
		}

	case ModalFuzzyFinder:
		action := a.fuzzyFinder.HandleKey(keyStr)
		if !a.fuzzyFinder.Visible() {
			a.activeModal = ModalNone
		}
		// Handle selection from fuzzy finder
		if action == "select" {
			if sess := a.fuzzyFinder.GetSelectedSession(); sess != nil {
				// Switch to selected session
				a.currentSession = sess
				a.sessionMgr.SetSession(sess)
				a.tasks.SetCurrentSession(sess.ID)
				a.plans.SetSession(sess.ID)
				sessionCmd := a.chat.SetSession(sess)
				a.statusMessage = fmt.Sprintf("switched to: %s", sess.Name)
				a.statusMessageTime = time.Now()
				clearCmd := tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
					return StatusMessageClearMsg{}
				})
				return a, tea.Batch(sessionCmd, clearCmd)
			}
			if task := a.fuzzyFinder.GetSelectedTask(); task != nil {
				// Switch to tasks view and select the task
				a.currentView = ViewTasks
				a.tasks.SetFilter(models.FilterAll)
				cmd := a.initCurrentView()
				return a, cmd
			}
		}
		return a, nil

	case ModalBranchPicker:
		cmd := a.branchPicker.HandleKey(keyStr)
		if !a.branchPicker.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd

	case ModalProjectPicker:
		cmd := a.projectPicker.HandleKey(keyStr)
		if !a.projectPicker.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd
	}

	return a, nil
}

// createSession creates a new session via SessionManager.
func (a *App) createSession(name string) tea.Cmd {
	return func() tea.Msg {
		err := a.sessionMgr.CreateSession(context.TODO(), name)
		if err != nil {
			return SessionLoadedMsg{Session: nil, Err: err}
		}
		updatedSession := a.sessionMgr.GetCurrentSession()
		return SessionSwitchMsg{Session: updatedSession}
	}
}

// switchToSessionByID loads a session by ID into the chat view. Used by the
// global search view to navigate to a message result. For MVP, scrolling to
// a specific message ID is logged for future enhancement.
func (a *App) switchToSessionByID(sessionID string, messageID int64) tea.Cmd {
	return func() tea.Msg {
		if !a.rpc.IsConnected() {
			return SessionSwitchMsg{Err: fmt.Errorf("not connected")}
		}
		// Fetch the session by scanning the session list. The RPC layer
		// does not have a GetSessionByID method, so we use ListSessions
		// and find by ID. This is acceptable for MVP since search
		// navigation is user-initiated and infrequent.
		resp, err := a.rpc.ListSessions()
		if err != nil {
			return SessionSwitchMsg{Err: err}
		}
		for _, sess := range resp.Sessions {
			if sess.ID == sessionID {
				return SessionSwitchMsg{Session: &sess, SwitchToChat: true}
			}
		}
		return SessionSwitchMsg{Err: fmt.Errorf("session not found: %s", sessionID)}
	}
}

// deleteSession deletes a session via SessionManager.
func (a *App) deleteSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := a.sessionMgr.DeleteSession(context.TODO(), sessionID)
		if err != nil {
			// Just refresh the list to show current state
		}
		// Refresh session list
		sessions, _ := a.sessionMgr.ListSessions(context.TODO())
		if len(sessions) > 0 {
			return SessionListMsg{Sessions: sessions}
		}
		return SessionListMsg{Sessions: nil}
	}
}

// renameSession renames a session via RPC (updates description).
// We call RPC directly because SessionManager.UpdateSessionDescription
// always operates on the current session, but the rename modal can target
// any session from the picker.
func (a *App) renameSession(sessionID, newName string) tea.Cmd {
	return func() tea.Msg {
		err := a.rpc.UpdateSessionDescription(sessionID, newName)
		if err != nil {
			return RenameErrorMsg{Err: err}
		}
		// Refresh session list so names stay in sync
		if _, err := a.sessionMgr.ListSessions(context.TODO()); err != nil {
			slog.Debug("failed to refresh session list", "error", err)
		}
		return models.SessionDescriptionUpdatedMsg{
			SessionID:   sessionID,
			Description: newName,
		}
	}
}

// initCurrentView returns a command to initialize the current view.
func (a *App) initCurrentView() tea.Cmd {
	switch a.currentView {
	case ViewChat:
		return a.chat.Init()
	case ViewSessions:
		return a.sessions.Init()
	case ViewTasks:
		return a.tasks.Init()
	case ViewQueue:
		return a.queue.Init()
	case ViewMemory:
		return a.memory.Init()
	case ViewPlans:
		return a.plans.Init()
	case ViewSearch:
		if a.search != nil {
			return a.search.Init()
		}
	}
	return nil
}

// View renders the application.
func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("loading...")
	}

	// Render modal overlay if active
	if a.activeModal != ModalNone {
		v := tea.NewView(a.renderModalOverlay())
		v.AltScreen = true
		v.WindowTitle = a.getWindowTitle()
		v.MouseMode = tea.MouseModeAllMotion
		return v
	}

	var b strings.Builder

	// Render header bar (orange with session info) if enabled
	if a.clientConfig.Rendering.ShowHeader {
		b.WriteString(a.renderHeader())
		b.WriteString("\n")
	}

	// Render main content area (view + optional sidebar)
	var mainView string
	if a.err != nil {
		mainView = a.renderError()
	} else {
		switch a.currentView {
		case ViewChat:
			mainView = a.chat.View()
		case ViewSessions:
			mainView = a.sessions.View()
		case ViewTasks:
			mainView = a.tasks.View()
		case ViewQueue:
			mainView = a.queue.View()
		case ViewMemory:
			mainView = a.memory.View()
		case ViewPlans:
			mainView = a.plans.View()
		case ViewSearch:
			if a.search != nil {
				mainView = a.search.View()
			} else {
				mainView = "search unavailable"
			}
		}
	}

	// If sidebar is visible, render it alongside the main view
	if a.sidebar.IsVisible() {
		sidebarView := a.sidebar.View()
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, mainView, sidebarView)
	}

	// Render main view
	b.WriteString(mainView)

	// Render status bar
	b.WriteString("\n")
	b.WriteString(a.renderStatusBar())

	// Render toast notifications overlay (positioned above status bar)
	if a.notifications != nil && a.notifications.HasActive() {
		notifView := a.notifications.View(a.width - a.sidebarWidth)
		if notifView != "" {
			b.WriteString("\n")
			b.WriteString(notifView)
		}
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = a.getWindowTitle()
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

// renderModalOverlay renders the active modal centered on a dimmed background.
func (a *App) renderModalOverlay() string {
	switch a.activeModal {
	case ModalCommandPalette:
		return a.commandPalette.View(a.width, a.height)
	case ModalSessionPicker:
		return a.sessionPicker.View(a.width, a.height)
	case ModalSessionRename:
		return a.sessionRename.View(a.width, a.height)
	case ModalConfirm:
		if a.confirmModal != nil {
			return a.confirmModal.View(a.width, a.height)
		}
	case ModalFuzzyFinder:
		return a.fuzzyFinder.View(a.width, a.height)
	case ModalBranchPicker:
		return a.branchPicker.View(a.width, a.height)
	case ModalProjectPicker:
		return a.projectPicker.View(a.width, a.height)
	}
	return ""
}

func (a *App) renderHeader() string {
	// Check if header is disabled
	if !a.clientConfig.Rendering.ShowHeader {
		return ""
	}

	// Ensure we have a valid width
	width := a.width
	if width < 20 {
		width = 80 // fallback
	}

	// Session name - hide "default"
	sessionName := ""
	if a.currentSession != nil && a.currentSession.Name != "" && a.currentSession.Name != "default" {
		sessionName = a.currentSession.Name
	}

	// Session description (always show if present)
	desc := ""
	if a.currentSession != nil && a.currentSession.Description != "" {
		desc = a.currentSession.Description
	}

	// Build header content
	var content string
	switch {
	case sessionName != "" && desc != "":
		// Both name and description
		maxDescWidth := width - len(sessionName) - 5
		if maxDescWidth > 10 && len(desc) > maxDescWidth {
			desc = desc[:maxDescWidth-3] + "..."
		}
		content = sessionName + " │ " + desc
	case sessionName != "":
		// Just session name
		content = sessionName
	case desc != "":
		// Just description (for "default" session)
		if len(desc) > width-2 {
			desc = desc[:width-5] + "..."
		}
		content = desc
	default:
		// Nothing to show
		content = "meept"
	}

	// Pad content to fill width
	if len(content) < width {
		content += strings.Repeat(" ", width-len(content))
	} else if len(content) > width {
		content = content[:width]
	}

	// Plan badges line (to be populated from session plan counts)
	// plansLine := a.renderPlanBadges()

	// Orange background, black text - render to exact width
	return a.styles.HeaderBar.
		Width(width).
		MaxWidth(width).
		Render(content)
}

// calculateLayout determines the layout mode based on terminal width.
// Compact: < 80 cols (no sidebar, single panel)
// Standard: 80-120 cols (narrow sidebar 25 chars)
// Wide: > 120 cols (normal sidebar up to 35 chars)
func (a *App) calculateLayout() {
	switch {
	case a.width < 80:
		a.layoutMode = LayoutCompact
	case a.width <= 120:
		a.layoutMode = LayoutStandard
	default:
		a.layoutMode = LayoutWide
	}

	// Auto-hide sidebar in compact mode
	if a.layoutMode == LayoutCompact && a.sidebar.IsVisible() {
		a.sidebar.SetVisible(false)
	}
}

// getWindowTitle returns the terminal title string for the tea.View WindowTitle field.
func (a *App) getWindowTitle() string {
	title := "meept"
	if a.currentSession != nil {
		if a.currentSession.Description != "" {
			title = "meept - " + a.currentSession.Description
		} else if a.currentSession.Name != "" && a.currentSession.Name != "default" {
			title = "meept - " + a.currentSession.Name
		}
	}
	return title
}

func (a *App) renderTabs() string {
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

func (a *App) renderStatusBar() string {
	// Connection status
	connectionStatus := "●"
	statusStyle := a.styles.StatusStopped
	if a.rpc.IsConnected() {
		statusStyle = a.styles.StatusRunning
	}

	// Project directory (shortened)
	projectDisplay := a.projectDir
	maxProjectLen := 25
	if len(projectDisplay) > maxProjectLen {
		projectDisplay = "..." + projectDisplay[len(projectDisplay)-maxProjectLen+3:]
	}

	// Build single line: status dot | mouse mode | context-sensitive keybindings | directory
	var parts []string

	// Status message takes priority if present
	if a.statusMessage != "" {
		parts = append(parts, a.styles.StatusRunning.Render(a.statusMessage))
	} else {
		parts = append(parts, statusStyle.Render(connectionStatus))

		// Show branch indicator if session has a leaf message set
		if a.currentSession != nil && a.currentSession.LeafMessageID != nil {
			parts = append(parts, a.styles.Muted.Render("branch: "+a.currentSession.Name))
		}

		// Add context-sensitive quick actions
		quickActions := a.getQuickActions()
		parts = append(parts, quickActions...)

		// Add project indicator
		if a.currentProjectName != "" {
			projectIndicator := a.renderProjectIndicator()
			parts = append(parts, projectIndicator)
		}

		parts = append(parts, a.styles.Muted.Render(projectDisplay))
	}

	// Verbosity indicator
	parts = append(parts, a.styles.Muted.Render(fmt.Sprintf("verbosity: %s", a.verbosity)))

	content := strings.Join(parts, " │ ")

	// Status bar spans only the main content area (excludes sidebar)
	statusWidth := a.width - a.sidebarWidth
	return a.styles.StatusBar.
		Width(statusWidth).
		MaxWidth(statusWidth).
		Render(content)
}

// getQuickActions returns context-sensitive keybinding hints based on current view and mode.
func (a *App) getQuickActions() []string {
	var actions []string

	// Always show menu, sessions, find, branches, and quit
	actions = append(actions,
		a.styles.HelpKey.Render("^X")+" "+a.styles.HelpValue.Render("menu"),
		a.styles.HelpKey.Render("^S")+" "+a.styles.HelpValue.Render("sessions"),
		a.styles.HelpKey.Render("^P")+" "+a.styles.HelpValue.Render("find"),
		a.styles.HelpKey.Render("^B")+" "+a.styles.HelpValue.Render("branches"),
		a.styles.HelpKey.Render("^C")+" "+a.styles.HelpValue.Render("quit"),
	)

	switch a.currentView {
	case ViewChat:
		// Chat view actions depend on chat mode
		if a.chat != nil {
			// Show copy hint when there's an active text selection
			if a.chat.HasSelection() {
				actions = append(actions, a.styles.HelpKey.Render("c")+" "+a.styles.HelpValue.Render("copy selection"))
			}

			chatMode := a.chat.GetMode()
			switch chatMode {
			case "insert":
				actions = append(actions,
					a.styles.HelpKey.Render(KeyEsc)+" "+a.styles.HelpValue.Render("normal"),
					a.styles.HelpKey.Render(KeyEnter)+" "+a.styles.HelpValue.Render("send"),
				)
			case "visual":
				actions = append(actions,
					a.styles.HelpKey.Render(KeyEsc)+" "+a.styles.HelpValue.Render("normal"),
					a.styles.HelpKey.Render("y")+" "+a.styles.HelpValue.Render("copy"),
				)
			default: // normal mode
				actions = append(actions,
					a.styles.HelpKey.Render("i")+" "+a.styles.HelpValue.Render("insert"),
					a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("scroll"),
					a.styles.HelpKey.Render("/")+" "+a.styles.HelpValue.Render("search"),
					a.styles.HelpKey.Render("^V")+" "+a.styles.HelpValue.Render("verbosity"),
				)
			}
		} else {
			actions = append(actions, a.styles.HelpKey.Render(KeyEsc)+" "+a.styles.HelpValue.Render("input"))
		}

	case ViewTasks:
		actions = append(actions,
			a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"),
			a.styles.HelpKey.Render(KeyEnter)+" "+a.styles.HelpValue.Render("details"),
			a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"),
			a.styles.HelpKey.Render("tab")+" "+a.styles.HelpValue.Render("toggle view"),
		)

	case ViewQueue:
		actions = append(actions,
			a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"),
			a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"),
			a.styles.HelpKey.Render("tab")+" "+a.styles.HelpValue.Render("toggle view"),
		)

	case ViewMemory:
		actions = append(actions,
			a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"),
			a.styles.HelpKey.Render("/")+" "+a.styles.HelpValue.Render("search"),
			a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"),
		)

	case ViewPlans:
		actions = append(actions,
			a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"),
			a.styles.HelpKey.Render(KeyEnter)+" "+a.styles.HelpValue.Render("details"),
			a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"),
			a.styles.HelpKey.Render("/")+" "+a.styles.HelpValue.Render("filter"),
		)

	case ViewSearch:
		actions = append(actions,
			a.styles.HelpKey.Render(KeyEnter)+" "+a.styles.HelpValue.Render("open"),
			a.styles.HelpKey.Render(KeyEsc)+" "+a.styles.HelpValue.Render("back"),
			a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"),
			a.styles.HelpKey.Render("tab")+" "+a.styles.HelpValue.Render("scope"),
		)
	}

	// Add sidebar toggle hint if sidebar is hidden
	if !a.sidebar.IsVisible() {
		actions = append(actions, a.styles.HelpKey.Render("^X y")+" "+a.styles.HelpValue.Render("sidebar"))
	}

	return actions
}

// renderProjectIndicator returns a styled string showing the current project.
// For git projects: [name branch*] where * means dirty
// For local: [local:path]
func (a *App) renderProjectIndicator() string {
	if a.currentProjectName == "" {
		return ""
	}

	name := a.currentProjectName
	maxNameLen := 16
	if len(name) > maxNameLen {
		name = name[:maxNameLen-3] + "..."
	}

	switch a.currentProjectMode {
	case "git":
		// Branch and dirty state are cached from fetchCurrentProject()
		// background updates — never perform RPC in the render path.
		branch := ""
		if a.currentProjectBranch != "" {
			branch = " " + a.currentProjectBranch
		}
		dirty := ""
		if a.currentProjectDirty {
			dirty = "*"
		}
		return a.styles.HelpKey.Render(fmt.Sprintf("[%s%s%s]", name, branch, dirty))
	default:
		return a.styles.Muted.Render(fmt.Sprintf("[local:%s]", name))
	}
}

func (a *App) renderError() string {
	errMsg := fmt.Sprintf("error: %v", a.err)
	return a.styles.Panel.
		Width(a.width - 4).
		Render(
			a.styles.Error.Render("connection error") + "\n\n" +
				a.styles.Paragraph.Render(errMsg) + "\n\n" +
				a.styles.Muted.Render("Make sure the meept daemon is running:\n  meept daemon start"),
		)
}

// isPrintableKey returns true if the key message represents a printable character
// that should trigger auto-focus to the text input.
func isPrintableKey(msg tea.KeyPressMsg) bool {
	return msg.Text != ""
}

// CopySuccessMsg indicates clipboard copy succeeded.
type CopySuccessMsg struct {
	Text string
}

// CopyErrorMsg indicates clipboard copy failed.
type CopyErrorMsg struct {
	Err error
}

// RenameErrorMsg indicates a session rename operation failed.
type RenameErrorMsg struct {
	Err error
}

// doCopy is a command that copies text to clipboard using bubbletea's built-in clipboard support.
func doCopy(text string) tea.Cmd {
	return tea.Batch(
		tea.SetClipboard(text),
		func() tea.Msg {
			return CopySuccessMsg{Text: text}
		},
	)
}

// StatusMessageClearMsg clears the status message.
type StatusMessageClearMsg struct{}

// BranchInfoMsg carries branch information fetched for the current session.
type BranchInfoMsg struct {
	Branches []BranchInfo
	Err      error
}

// BranchNavigateResultMsg carries the result of a successful branch navigation.
type BranchNavigateResultMsg struct {
	BranchID string
}

// stopCurrentWork stops the current session's work and prompts for child tasks.
func (a *App) stopCurrentWork() tea.Cmd {
	if a.currentSession == nil {
		a.statusMessage = "no active session"
		a.statusMessageTime = time.Now()
		return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})
	}

	sessionID := a.currentSession.ID

	// Check if there are child tasks (async to avoid blocking Update)
	return func() tea.Msg {
		tasks, _ := a.rpc.GetSessionChildTasks(sessionID)
		return StopWorkChildTasksMsg{SessionID: sessionID, ChildTaskCount: len(tasks)}
	}
}

// handleStopWorkChildTasks processes the result of the async child task count
// fetch and shows the confirm modal or stops immediately.
func (a *App) handleStopWorkChildTasks(msg StopWorkChildTasksMsg) tea.Cmd {
	if msg.ChildTaskCount > 0 {
		// Show confirm modal to ask about child tasks
		if a.confirmModal == nil {
			a.confirmModal = NewConfirmModal(a.styles)
		}
		a.activeModal = ModalConfirm
		sessionID := msg.SessionID
		a.confirmModal.Show(
			"stop work",
			fmt.Sprintf("Stop current work? There are %d active tasks.", msg.ChildTaskCount),
			func() tea.Cmd {
				return a.doStopSession(sessionID)
			},
			func() tea.Cmd {
				a.activeModal = ModalNone
				return nil
			},
		)
		return nil
	}

	// No child tasks, stop immediately
	return a.doStopSession(msg.SessionID)
}

// doStopSession performs the actual session stop RPC call.
func (a *App) doStopSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := a.rpc.StopSession(sessionID)
		if err != nil {
			return StopSessionResultMsg{Error: err}
		}
		return StopSessionResultMsg{Response: resp}
	}
}

// StopSessionResultMsg carries the result of stopping a session.
type StopSessionResultMsg struct {
	Response *types.StopSessionResponse
	Error    error
}

// SetProjectResultMsg carries the result of an async SetProject RPC call.
type SetProjectResultMsg struct {
	Err error
}

// StopWorkChildTasksMsg carries the child task count for async stop-work flow.
type StopWorkChildTasksMsg struct {
	SessionID     string
	ChildTaskCount int
}
