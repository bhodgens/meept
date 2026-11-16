package tui

// kanban_view_render.go holds the kanban board view's rendering half
// (split from kanban_view.go for the <700-line file rule): the View()
// entry plus the legend/prompt/detail renderers. All strings lowercase
// (repo UI rule).

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (p *KanbanPanel) View() string {
	if p.width == 0 {
		return ""
	}
	st := DefaultStyles()
	var b strings.Builder

	// Header: board picker line + column legend with WIP occupancy.
	boardName := "(no boards)"
	if len(p.boards) > 0 && p.boardIdx < len(p.boards) {
		boardName = p.boards[p.boardIdx].Name
	}
	b.WriteString(st.Title.Render("kanban: " + strings.ToLower(boardName)))
	if n := len(p.boards); n > 1 {
		b.WriteString(st.Muted.Render(fmt.Sprintf("  [%d/%d] ( [ / ] )", p.boardIdx+1, n)))
	}
	b.WriteString("\n")
	b.WriteString(st.Muted.Render(p.columnLegend()))
	b.WriteString("\n")

	switch {
	case p.err != nil:
		b.WriteString(st.Error.Render("board error: "+p.err.Error()) + "\n")
	case p.pendingRef != "":
		// C10 surface: the decoded refusal line persists until cleared
		// by the next action.
		b.WriteString(st.Warning.Render(p.pendingRef) + "\n")
	case p.status != "":
		b.WriteString(st.Success.Render(p.status) + "\n")
	case p.loading:
		b.WriteString(st.Muted.Render("loading board…") + "\n")
	}

	if p.subView == kanbanViewDetail {
		b.WriteString(p.renderDetail(st))
		b.WriteString("\n")
		b.WriteString(p.tlTable.View())
		b.WriteString("\n")
		b.WriteString(st.Muted.Render("esc back · r refresh · timeline oldest first"))
		return b.String()
	}

	b.WriteString(p.table.View())
	b.WriteString("\n")
	if p.prompt != kanbanPromptNone {
		b.WriteString(p.renderPrompt(st))
	} else {
		b.WriteString(st.Muted.Render("j/k select · enter details · h/l move · t takeover · a assign · c comment · r refresh · R reconcile"))
	}
	return b.String()
}

func (p *KanbanPanel) columnLegend() string {
	if len(p.columns) == 0 {
		return "no columns"
	}
	counts := make(map[string]int, len(p.cards))
	for _, c := range p.cards {
		counts[c.ColumnID]++
	}
	parts := make([]string, 0, len(p.columns))
	for _, c := range p.columns {
		label := fmt.Sprintf("%s(%d", strings.ToLower(c.Name), counts[c.ID])
		if c.WIP > 0 {
			label += fmt.Sprintf("/%d wip", c.WIP)
		}
		parts = append(parts, label+")")
	}
	return strings.Join(parts, " · ")
}

func (p *KanbanPanel) renderPrompt(st *Styles) string {
	promptLabel := lipgloss.NewStyle().Foreground(Current().Primary)
	switch p.prompt {
	case kanbanPromptAssign:
		return promptLabel.Render("assign employee id> ") + p.inputBuf + st.Muted.Render("  (enter submits · esc cancels)")
	case kanbanPromptComment:
		return promptLabel.Render("comment> ") + p.inputBuf + st.Muted.Render("  (enter submits · esc cancels)")
	case kanbanPromptTakeover:
		return st.Warning.Render("takeover: cancel episode, release claim, audit, then move. y confirm · n cancel")
	}
	return ""
}

func (p *KanbanPanel) renderDetail(st *Styles) string {
	if p.detailCard == nil {
		return st.Muted.Render("no card")
	}
	c := p.detailCard
	_, colName, _ := p.columnIndex(c.ColumnID)
	lines := []string{
		"  title: " + c.Title,
		"  column: " + strings.ToLower(colName),
	}
	if c.RefKind != "" || c.RefID != "" {
		lines = append(lines, "  ref: "+strings.TrimSpace(c.RefKind+" "+c.RefID))
	}
	if c.EmployeeID != "" {
		lines = append(lines, "  employee: "+c.EmployeeID)
	}
	if len(c.Labels) > 0 {
		lines = append(lines, "  labels: "+strings.Join(c.Labels, ", "))
	}
	return strings.Join(lines, "\n")
}
