package models

import (
	"strconv"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// unboundBucket is the C2 catch-all for sessions with no project binding.
const unboundBucket = "unbound"

// bucketFor returns the C2 project bucket for a session: ProjectID first, then
// ProjectPath, then the "unbound" catch-all. This is the single definition of
// session grouping shared by the tree renderer and the collapse state.
func bucketFor(s types.Session) string {
	if s.ProjectID != "" {
		return s.ProjectID
	}
	if s.ProjectPath != "" {
		return s.ProjectPath
	}
	return unboundBucket
}

// sessionTreeRows is the derived, flat rendering of the grouped session tree.
// Every slice is parallel to rows: sessions[i] is nil for a group header row,
// and buckets[i] is the C2 bucket of both header and child rows.
type sessionTreeRows struct {
	rows     []table.Row
	sessions []*types.Session
	buckets  []string
	headers  map[string]int // bucket -> row index of its group header
}

// buildSessionTreeRows groups the (already sorted) sessions into C2 buckets,
// preserving first-appearance order, and emits one header row per bucket
// followed by that bucket's children when it is expanded. A collapsed bucket
// emits only its header, which hides its child rows from both View() and the
// table's row slice.
func (m *SessionsModel) buildSessionTreeRows() sessionTreeRows {
	result := sessionTreeRows{headers: make(map[string]int)}

	type bucketGroup struct {
		bucket   string
		sessions []*types.Session
	}

	order := make([]string, 0)
	groups := make(map[string]*bucketGroup)
	for i := range m.sessions {
		s := &m.sessions[i]
		b := bucketFor(*s)
		g, ok := groups[b]
		if !ok {
			g = &bucketGroup{bucket: b}
			groups[b] = g
			order = append(order, b)
		}
		g.sessions = append(g.sessions, s)
	}

	// Derive title truncation from the dynamic column width so wider
	// terminals can show more of the session name/description (and of a
	// long ProjectPath bucket name).
	titleW := max(m.width*38/100-2, 15)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary"))

	addRow := func(bucket string, sess *types.Session, cells table.Row) {
		idx := len(result.rows)
		result.rows = append(result.rows, cells)
		result.sessions = append(result.sessions, sess)
		result.buckets = append(result.buckets, bucket)
		if sess == nil {
			result.headers[bucket] = idx
		}
	}

	for _, bucket := range order {
		g := groups[bucket]
		collapsed := m.collapsed[bucket]
		marker := "▾"
		if collapsed {
			marker = "▸"
		}
		label := headerStyle.Render(marker + " " + types.TruncateString(bucket, titleW) + " (" + strconv.Itoa(len(g.sessions)) + ")")
		addRow(bucket, nil, table.Row{label, "", ""})
		if collapsed {
			continue
		}

		for _, sess := range g.sessions {
			// Add designation indicator prefix
			desigPrefix := ""
			if sess.Designation != nil && sess.Designation.Status != "none" {
				switch sess.Designation.Status {
				case "requires_approval":
					desigPrefix = "! "
				case "waiting_human":
					desigPrefix = "? "
				case "human_responded":
					desigPrefix = "R "
				case "bot_thinking":
					desigPrefix = "... "
				}
			}
			// Archived sessions get a visual marker so the dim styling is
			// discoverable even on terminals that ignore foreground color.
			archPrefix := ""
			if sess.Archived {
				archPrefix = "(archived) "
			}
			title := sess.Description
			if title == "" {
				title = sess.Name
			}

			titleCell := desigPrefix + archPrefix + types.TruncateString(title, titleW)
			createdCell := m.formatTime(sess.CreatedAt)
			activityCell := m.formatTimeRelative(sess.LastActivity)

			if sess.Archived {
				titleCell = archivedRowStyle().Render(titleCell)
				createdCell = archivedRowStyle().Render(createdCell)
				activityCell = archivedRowStyle().Render(activityCell)
			}

			addRow(bucket, sess, table.Row{titleCell, createdCell, activityCell})
		}
	}

	return result
}

// bucketAtCursor returns the C2 bucket of the row the table cursor sits on.
// It works for both group header rows and child session rows.
func (m *SessionsModel) bucketAtCursor() string {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rowBuckets) {
		return ""
	}
	return m.rowBuckets[idx]
}

// setBucketCollapsed expands or collapses the bucket under the cursor and
// rebuilds the rows. The cursor is restored to that bucket's header so the
// group stays in place after its children are shown or hidden.
func (m *SessionsModel) setBucketCollapsed(bucket string, collapsed bool) {
	if bucket == "" {
		return
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	if collapsed {
		m.collapsed[bucket] = true
	} else {
		delete(m.collapsed, bucket)
	}

	m.updateSessionsTable()
	if idx, ok := m.headerRows[bucket]; ok {
		m.table.SetCursor(idx)
		m.selected = m.selectedSession()
	}
}
