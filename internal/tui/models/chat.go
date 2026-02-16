// Package models provides the view models for the TUI.
package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatMessage represents a single chat message.
type ChatMessage struct {
	Role      string // "user", "assistant", or "system"
	Content   string
	Timestamp time.Time
}

// ChatModel is the model for the chat view.
type ChatModel struct {
	rpc            RPCClient
	messages       []ChatMessage
	viewport       viewport.Model
	textarea       textarea.Model
	conversationID string
	width          int
	height         int
	loading        bool
	err            error

	// Styles
	userStyle      lipgloss.Style
	assistantStyle lipgloss.Style
	systemStyle    lipgloss.Style
}

// RPCClient interface for the chat model.
type RPCClient interface {
	Chat(message, conversationID string) (string, error)
	IsConnected() bool
}

// NewChatModel creates a new chat model.
func NewChatModel(rpc RPCClient, userStyle, assistantStyle, systemStyle lipgloss.Style) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.CharLimit = 4000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter sends, Shift+Enter for newline

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return &ChatModel{
		rpc:            rpc,
		messages:       []ChatMessage{},
		viewport:       vp,
		textarea:       ta,
		conversationID: generateConversationID(),
		userStyle:      userStyle,
		assistantStyle: assistantStyle,
		systemStyle:    systemStyle,
	}
}

func generateConversationID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}

// SetSize updates the model dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Textarea at bottom (3 lines)
	inputHeight := 3
	viewportHeight := height - inputHeight - 2 // 2 for padding

	m.textarea.SetWidth(width - 4)
	m.viewport.Width = width - 2
	m.viewport.Height = viewportHeight
}

// Init initializes the chat model.
func (m *ChatModel) Init() tea.Cmd {
	if len(m.messages) == 0 {
		m.addMessage("system", "Welcome to Meept! Type a message to begin.")
	}
	return textarea.Blink
}

// ChatResponseMsg carries the chat response.
type ChatResponseMsg struct {
	Reply string
	Err   error
}

// Update handles messages for the chat view.
func (m *ChatModel) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.loading {
				return nil
			}
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return nil
			}
			m.textarea.Reset()
			m.addMessage("user", text)
			m.loading = true
			return m.sendMessage(text)

		case "ctrl+l":
			// Clear chat history
			m.messages = []ChatMessage{}
			m.conversationID = generateConversationID()
			m.updateViewport()
			return nil
		}

	case ChatResponseMsg:
		m.loading = false
		if msg.Err != nil {
			m.addMessage("system", fmt.Sprintf("Error: %v", msg.Err))
		} else {
			m.addMessage("assistant", msg.Reply)
		}
		return nil
	}

	// Update textarea
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	if taCmd != nil {
		cmds = append(cmds, taCmd)
	}

	// Update viewport
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	if vpCmd != nil {
		cmds = append(cmds, vpCmd)
	}

	return tea.Batch(cmds...)
}

func (m *ChatModel) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		reply, err := m.rpc.Chat(text, m.conversationID)
		return ChatResponseMsg{Reply: reply, Err: err}
	}
}

func (m *ChatModel) addMessage(role, content string) {
	m.messages = append(m.messages, ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	m.updateViewport()
}

func (m *ChatModel) updateViewport() {
	var content strings.Builder

	for _, msg := range m.messages {
		var style lipgloss.Style
		var prefix string

		switch msg.Role {
		case "user":
			style = m.userStyle
			prefix = "You: "
		case "assistant":
			style = m.assistantStyle
			prefix = "Meept: "
		case "system":
			style = m.systemStyle
			prefix = ""
		}

		// Format message
		formattedMsg := formatMessage(prefix+msg.Content, m.width-4)
		content.WriteString(style.Render(formattedMsg))
		content.WriteString("\n\n")
	}

	if m.loading {
		content.WriteString(m.systemStyle.Render("thinking..."))
		content.WriteString("\n")
	}

	m.viewport.SetContent(content.String())
	m.viewport.GotoBottom()
}

// formatMessage wraps text to fit within the given width.
func formatMessage(text string, width int) string {
	if width <= 0 {
		return text
	}

	var lines []string
	paragraphs := strings.Split(text, "\n")

	for _, para := range paragraphs {
		if len(para) <= width {
			lines = append(lines, para)
			continue
		}

		// Word wrap
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentLine := words[0]
		for _, word := range words[1:] {
			if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}

// View renders the chat view.
func (m *ChatModel) View() string {
	var b strings.Builder

	// Chat history viewport
	viewportStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Width(m.width - 2).
		Height(m.viewport.Height + 2)

	b.WriteString(viewportStyle.Render(m.viewport.View()))
	b.WriteString("\n")

	// Input textarea
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Width(m.width - 2)

	b.WriteString(inputStyle.Render(m.textarea.View()))

	return b.String()
}

// Reset clears the chat state.
func (m *ChatModel) Reset() {
	m.messages = []ChatMessage{}
	m.conversationID = generateConversationID()
	m.textarea.Reset()
	m.updateViewport()
}
