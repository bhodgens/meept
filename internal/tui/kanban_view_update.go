package tui

// kanban_view_update.go holds the kanban board view's event loop (split
// from kanban_view.go for the <700-line file rule): message application
// and the keybinding -> human-command mapping the C6 contract pins. All
// user-facing strings lowercase.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (p *KanbanPanel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case kanbanBoardsMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return nil
		}
		p.err = nil
		p.boards = msg.boards
		if p.boardIdx >= len(p.boards) {
			p.boardIdx = 0
		}
		return p.fetchBoardData()

	case kanbanBoardDataMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return nil
		}
		p.err = nil
		p.columns = msg.columns
		p.cards = msg.cards
		p.updateBoardTable()
		// Clamp/reset the cursor: an empty-rows clear leaves it at -1,
		// and a shrink can point it past the last row.
		if len(p.cards) > 0 {
			switch cur := p.table.Cursor(); {
			case cur < 0:
				p.table.SetCursor(0)
			case cur >= len(p.cards):
				p.table.SetCursor(len(p.cards) - 1)
			}
		}
		return nil

	case kanbanDetailMsg:
		if msg.err != nil {
			p.err = msg.err
			return nil
		}
		p.err = nil
		p.detailCard = msg.card
		p.timeline = msg.timeline
		p.updateTimelineTable()
		return nil

	case kanbanActionMsg:
		if msg.err != nil {
			// Refusals stay on-screen until the next action (C10: the
			// lease/WIP line must be readable while the user decides on
			// takeover). Success clears them.
			p.err = nil
			p.status = ""
			p.pendingRef = msg.note
			p.pendingIsLease = msg.isLease
			if msg.isLease {
				p.pendingCard = msg.cardID
				p.pendingToCol = msg.toColumnID
			} else {
				p.pendingCard = ""
				p.pendingToCol = ""
			}
		} else {
			p.pendingRef = ""
			p.pendingIsLease = false
			p.pendingCard = ""
			p.pendingToCol = ""
			p.status = msg.note
		}
		return p.fetchAfterAction()

	case tea.KeyPressMsg:
		return p.handleKey(msg)

	default:
		// Non-key events (mouse clicks on rows): forward to the active
		// table when no prompt owns the input (repo rule: default to
		// clickable elements).
		if p.prompt == kanbanPromptNone {
			if p.subView == kanbanViewBoard {
				var cmd tea.Cmd
				p.table, cmd = p.table.Update(msg)
				return cmd
			}
		}
	}
	return nil
}

// fetchAfterAction refreshes the right scope after a mutation: detail
// keeps the timeline fresh, board re-pulls columns + cards.
func (p *KanbanPanel) fetchAfterAction() tea.Cmd {
	if p.subView == kanbanViewDetail && p.selectedID != "" {
		return p.fetchDetail(p.selectedID)
	}
	return p.fetchBoardData()
}

func (p *KanbanPanel) setNotice(s string) {
	p.status = s
	p.pendingRef = ""
}

func (p *KanbanPanel) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()

	// Prompt input takes precedence over every board binding.
	if p.prompt != kanbanPromptNone {
		return p.handlePromptKey(key)
	}

	if p.subView == kanbanViewDetail {
		switch key {
		case KeyEsc:
			p.subView = kanbanViewBoard
			p.detailCard = nil
			p.timeline = nil
			p.updateTimelineTable()
			return p.fetchBoardData()
		case "r":
			return p.fetchDetail(p.selectedID)
		default:
			var cmd tea.Cmd
			p.tlTable, cmd = p.tlTable.Update(msg)
			return cmd
		}
	}

	switch key {
	case "h", KeyLeft:
		if card, ok := p.selectedCard(); ok {
			return p.moveCard(card, -1)
		}
		return nil
	case "l", KeyRight:
		if card, ok := p.selectedCard(); ok {
			return p.moveCard(card, 1)
		}
		return nil
	case KeyEnter:
		card, ok := p.selectedCard()
		if !ok {
			return nil
		}
		p.selectedID = card.ID
		p.subView = kanbanViewDetail
		p.err = nil
		return p.fetchDetail(card.ID)
	case "t":
		// Takeover confirm. The target is the refused move when one is
		// pending (lease rule A replay); otherwise the selected card in
		// place.
		if card, ok := p.selectedCard(); ok && p.selectedID == "" {
			p.selectedID = card.ID
		}
		p.prompt = kanbanPromptTakeover
		return nil
	case "a":
		if card, ok := p.selectedCard(); ok {
			p.selectedID = card.ID
			p.prompt = kanbanPromptAssign
			p.inputBuf = ""
		}
		return nil
	case "c":
		if card, ok := p.selectedCard(); ok {
			p.selectedID = card.ID
			p.prompt = kanbanPromptComment
			p.inputBuf = ""
		}
		return nil
	case "r":
		p.loading = true
		return p.fetchBoardData()
	case "R":
		return p.reconcileNow()
	case "[":
		p.cycleBoard(-1)
		return p.fetchBoardData()
	case "]":
		p.cycleBoard(1)
		return p.fetchBoardData()
	default:
		var cmd tea.Cmd
		p.table, cmd = p.table.Update(msg)
		return cmd
	}
}

func (p *KanbanPanel) cycleBoard(delta int) {
	if len(p.boards) == 0 {
		return
	}
	p.boardIdx = (p.boardIdx + delta + len(p.boards)) % len(p.boards)
	p.subView = kanbanViewBoard
	p.columns = nil
	p.cards = nil
	p.pendingRef = ""
	p.status = ""
	p.updateBoardTable()
	p.loading = true
}

// handlePromptKey drives the line-input prompts and the takeover confirm.
func (p *KanbanPanel) handlePromptKey(key string) tea.Cmd {
	if p.prompt == kanbanPromptTakeover {
		switch key {
		case "y":
			p.prompt = kanbanPromptNone
			if p.pendingIsLease && p.pendingCard != "" {
				// Replay the refused move as the bypass (C10: takeover
				// is the only bypass).
				return p.takeoverCard(p.pendingCard, p.pendingToCol)
			}
			id := p.selectedID
			if id == "" {
				if card, ok := p.selectedCard(); ok {
					id = card.ID
				}
			}
			return p.takeoverCard(id, "")
		case "n", KeyEsc:
			p.prompt = kanbanPromptNone
			return nil
		}
		return nil
	}

	switch key {
	case KeyEsc:
		p.prompt = kanbanPromptNone
		p.inputBuf = ""
		return nil
	case "backspace", "ctrl+h":
		if p.inputBuf != "" {
			p.inputBuf = p.inputBuf[:len(p.inputBuf)-1]
		}
		return nil
	case "ctrl+u":
		p.inputBuf = ""
		return nil
	case KeyEnter:
		val := strings.TrimSpace(p.inputBuf)
		id := p.selectedID
		wasAssign := p.prompt == kanbanPromptAssign
		p.prompt = kanbanPromptNone
		p.inputBuf = ""
		if wasAssign {
			return p.assignCard(id, val)
		}
		return p.commentCard(id, val)
	default:
		if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
			p.inputBuf += key
		}
	}
	return nil
}
