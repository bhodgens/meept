package llm

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ModelPickerMode defines the current picker mode.
type ModelPickerMode int

const (
	ModeSelectProvider ModelPickerMode = iota
	ModeSelectModel
)

// modelProviderItem represents a provider in the list.
type modelProviderItem struct {
	def ProviderDef
}

func (m modelProviderItem) Title() string       { return m.def.Name }
func (m modelProviderItem) Description() string { return m.def.ID }
func (m modelProviderItem) FilterValue() string { return m.def.Name + " " + m.def.ID }

// modelModelItem represents a model in the list.
type modelModelItem struct {
	entry ModelCatalogEntry
}

func (m modelModelItem) Title() string { return m.entry.Name }
func (m modelModelItem) Description() string {
	caps := strings.Join(m.entry.Capabilities, ", ")
	ctxK := m.entry.ContextWindow / 1000
	priceStr := fmt.Sprintf("$%.2f/$%.2f", m.entry.InputCost, m.entry.OutputCost)
	return fmt.Sprintf("%s | Context: %dK | %s | %s", m.entry.ModelID, ctxK, priceStr, caps)
}
func (m modelModelItem) FilterValue() string { return m.entry.Name + " " + m.entry.ModelID }

// ModelPickerConfig holds configuration for the picker.
type ModelPickerConfig struct {
	Title             string
	ShowHelp          bool
	AllowCustom       bool
	PreselectProvider string
	PreselectModel    string
	ProvidersConfig   *ProvidersConfig // If set, custom providers from config are merged in
}

// ModelPicker is the TUI model picker.
type ModelPicker struct {
	mode             ModelPickerMode
	config           ModelPickerConfig
	providerList     list.Model
	modelList        list.Model
	providers        []ProviderDef
	providersCfg     *ProvidersConfig
	models           []ModelCatalogEntry
	selectedProvider *ProviderDef
	selectedModel    *ModelCatalogEntry
	quitting         bool
	cancelled        bool
	width            int
	height           int
}

// NewModelPicker creates a new model picker.
func NewModelPicker(config ModelPickerConfig) *ModelPicker {
	// Merge canonical providers with config-sourced providers
	allProviders := buildProviderList(config.ProvidersConfig)

	// Build provider items
	providerItems := make([]list.Item, len(allProviders))
	for i, p := range allProviders {
		providerItems[i] = modelProviderItem{def: p}
	}

	providerDelegate := list.NewDefaultDelegate()
	providerDelegate.ShowDescription = true

	providerList := list.New(providerItems, providerDelegate, 40, 10)
	providerList.Title = "Select Provider"
	providerList.SetShowStatusBar(true)
	providerList.SetFilteringEnabled(true)

	// Build model list (empty initially, populated after provider selection)
	modelDelegate := list.NewDefaultDelegate()
	modelDelegate.ShowDescription = true

	modelList := list.New(nil, modelDelegate, 50, 15)
	modelList.Title = "Select Model"
	modelList.SetShowStatusBar(true)
	modelList.SetFilteringEnabled(true)

	mp := &ModelPicker{
		mode:           ModeSelectProvider,
		config:         config,
		providerList:   providerList,
		modelList:      modelList,
		providers:      allProviders,
		providersCfg:   config.ProvidersConfig,
	}

	// Pre-select provider if specified
	if config.PreselectProvider != "" {
		for i, p := range allProviders {
			if p.ID != config.PreselectProvider {
				continue
			}
			mp.providerList.Select(i)
			mp.selectedProvider = &allProviders[i]
			mp.loadModelsForProvider(&allProviders[i])
			mp.mode = ModeSelectModel
			break
		}
	}

	return mp
}

func (m *ModelPicker) loadModelsForProvider(provider *ProviderDef) {
	var models []ModelCatalogEntry

	// Check static catalog first
	if catalog, ok := ProviderModels[provider.ID]; ok {
		models = append(models, catalog...)
	}

	// Check config-sourced models
	if m.providersCfg != nil {
		if pc, ok := m.providersCfg.Providers[provider.ID]; ok {
			for modelID, md := range pc.Models {
				// Skip if already in catalog (avoid duplicates)
				found := false
				for _, existing := range models {
					if existing.ModelID == modelID || existing.ModelID == md.Name {
						found = true
						break
					}
				}
				if found {
					continue
				}
				models = append(models, ModelCatalogEntry{
					ModelID:       modelID,
					Name:          md.Name,
					ProviderID:    provider.ID,
					ContextWindow: md.ContextLimit,
					MaxOutput:     md.MaxOutput,
					InputCost:     md.InputCost,
					OutputCost:    md.OutputCost,
					Capabilities:  md.Capabilities,
				})
			}
		}
	}

	if len(models) == 0 {
		m.modelList.SetItems(nil)
		return
	}

	items := make([]list.Item, len(models))
	for i, model := range models {
		items[i] = modelModelItem{entry: model}
	}
	m.modelList.SetItems(items)
	m.models = models
}

// buildProviderList merges canonical providers with config-sourced providers.
// Config providers that don't match a canonical ID are appended.
// Config providers that match a canonical ID override its BaseURL/APIKey.
func buildProviderList(cfg *ProvidersConfig) []ProviderDef {
	canonical := make([]ProviderDef, len(CanonicalProviders))
	copy(canonical, CanonicalProviders)

	if cfg == nil || len(cfg.Providers) == 0 {
		return canonical
	}

	// Build lookup for fast override
	canonicalMap := make(map[string]int, len(canonical))
	for i, p := range canonical {
		canonicalMap[p.ID] = i
	}

	// Disabled providers
	disabled := make(map[string]bool, len(cfg.DisabledProviders))
	for _, d := range cfg.DisabledProviders {
		disabled[d] = true
	}

	for id, pc := range cfg.Providers {
		if disabled[id] {
			continue
		}

		if idx, ok := canonicalMap[id]; ok {
			// Override canonical provider settings from config
			if pc.Options.BaseURL != "" {
				canonical[idx].BaseURL = pc.Options.BaseURL
			}
			continue
		}

		// User-defined provider
		def := ProviderDef{
			ID:        id,
			Name:      id,
			Transport: TransportOpenAIChat,
			AuthType:  AuthEnvVar,
			BaseURL:   pc.Options.BaseURL,
			Supports:  []string{CapStreaming},
		}
		if pc.Options.APIKey != "" {
			def.AuthType = AuthAPIKey
		}
		if pc.Lifecycle != nil {
			def.Supports = append(def.Supports, "local")
		}
		canonical = append(canonical, def)
	}

	return canonical
}

// Init initializes the model picker.
func (m *ModelPicker) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model.
func (m *ModelPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.mode == ModeSelectModel {
				// Go back to provider selection
				m.mode = ModeSelectProvider
				m.selectedProvider = nil
				return m, nil
			}
			m.quitting = true
			m.cancelled = true
			return m, tea.Quit

		case "esc":
			if m.mode == ModeSelectModel {
				// Go back to provider selection
				m.mode = ModeSelectProvider
				m.selectedProvider = nil
				return m, nil
			}
			m.quitting = true
			m.cancelled = true
			return m, tea.Quit

		case "enter":
			switch m.mode {
			case ModeSelectProvider:
				// Provider selected
				if item, ok := m.providerList.SelectedItem().(modelProviderItem); ok {
					m.selectedProvider = &item.def
					m.loadModelsForProvider(&item.def)
					m.mode = ModeSelectModel
				}
			case ModeSelectModel:
				// Model selected
				if item, ok := m.modelList.SelectedItem().(modelModelItem); ok {
					m.selectedModel = &item.entry
					m.quitting = true
					return m, tea.Quit
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		h, v := listStyle.GetFrameSize()
		m.providerList.SetSize(msg.Width-h, msg.Height-v)
		m.modelList.SetSize(msg.Width-h, msg.Height-v)
	}

	// Delegate to appropriate list based on mode
	var cmd tea.Cmd
	if m.mode == ModeSelectProvider {
		m.providerList, cmd = m.providerList.Update(msg)
	} else {
		m.modelList, cmd = m.modelList.Update(msg)
	}
	return m, cmd
}

// View renders the model picker.
func (m *ModelPicker) View() tea.View {
	if m.quitting {
		if m.cancelled {
			return tea.NewView("Cancelled\n")
		}
		if m.selectedProvider != nil && m.selectedModel != nil {
			return tea.NewView(fmt.Sprintf("Selected: %s / %s\n", m.selectedProvider.Name, m.selectedModel.Name))
		}
		return tea.NewView("")
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(m.config.Title))
	b.WriteString("\n\n")

	if m.mode == ModeSelectProvider {
		b.WriteString(helpStyle.Render("Use arrow keys to navigate, type to filter, enter to select, q/esc to cancel\n\n"))
		b.WriteString(m.providerList.View())
	} else {
		fmt.Fprintf(&b, "Provider: %s\n\n", highlightStyle.Render(m.selectedProvider.Name))
		b.WriteString(helpStyle.Render("Use arrow keys to navigate, type to filter, enter to select, esc to go back, q to cancel\n\n"))
		b.WriteString(m.modelList.View())
	}

	return tea.NewView(b.String())
}

// GetSelectedProvider returns the selected provider.
func (m *ModelPicker) GetSelectedProvider() *ProviderDef {
	return m.selectedProvider
}

// GetSelectedModel returns the selected model.
func (m *ModelPicker) GetSelectedModel() *ModelCatalogEntry {
	return m.selectedModel
}

// WasCancelled returns true if the user cancelled.
func (m *ModelPicker) WasCancelled() bool {
	return m.cancelled
}

var (
	listStyle      = lipgloss.NewStyle().Padding(1, 2)
	titleStyle     = lipgloss.NewStyle().Bold(true)
	helpStyle      = lipgloss.NewStyle().Faint(true)
	highlightStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
)

// RunModelPicker runs the model picker TUI and returns the selected provider/model.
func RunModelPicker(config ModelPickerConfig) (*ProviderDef, *ModelCatalogEntry, error) {
	picker := NewModelPicker(config)

	p := tea.NewProgram(picker)
	result, err := p.Run()
	if err != nil {
		return nil, nil, err
	}

	mp, ok := result.(*ModelPicker)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected result type")
	}

	if mp.WasCancelled() {
		return nil, nil, nil // User cancelled
	}

	return mp.GetSelectedProvider(), mp.GetSelectedModel(), nil
}
