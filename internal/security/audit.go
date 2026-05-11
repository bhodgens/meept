package security

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AuditLog provides access to the security decision audit log.
type AuditLog struct {
	db *sql.DB
}

// NewAuditLog creates a new audit log accessor using the engine's database.
func NewAuditLog(db *sql.DB) *AuditLog {
	return &AuditLog{db: db}
}

// LogDecision logs a security decision to the audit log.
func (a *AuditLog) LogDecision(
	action, toolName string,
	detailsJSON string,
	riskLevel RiskLevel,
	decision, reason, ruleSource string,
	overrideID *int64,
	conversationID *string,
) error {
	var oid sql.NullInt64
	if overrideID != nil {
		oid.Int64 = *overrideID
		oid.Valid = true
	}

	var cid sql.NullString
	if conversationID != nil && *conversationID != "" {
		cid.String = *conversationID
		cid.Valid = true
	}

	_, err := a.db.Exec(`
		INSERT INTO decision_log
		(action, tool_name, details_json, risk_level, decision, reason,
		 rule_source, override_id, conversation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		action, toolName, detailsJSON, int(riskLevel),
		decision, reason, ruleSource, oid, cid)

	return err
}

// QueryHistory retrieves audit entries matching the given filters.
func (a *AuditLog) QueryHistory(filters QueryFilters) ([]AuditEntry, error) {
	var query strings.Builder
	query.WriteString(`
		SELECT id, timestamp, action, tool_name, details_json,
		       risk_level, decision, reason, rule_source,
		       override_id, conversation_id
		FROM decision_log`)

	var conditions []string
	var args []any

	if filters.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, filters.Action)
	}
	if filters.Decision != "" {
		conditions = append(conditions, "decision = ?")
		args = append(args, filters.Decision)
	}
	if filters.Since != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filters.Since.Format(time.RFC3339))
	}

	if len(conditions) > 0 {
		query.WriteString(" WHERE ")
		for i, cond := range conditions {
			if i > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(cond)
		}
	}

	query.WriteString(" ORDER BY timestamp DESC")

	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	query.WriteString(fmt.Sprintf(" LIMIT %d", limit))

	rows, err := a.db.Query(query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit log: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var timestampStr string
		var riskLevel int
		var overrideID sql.NullInt64
		var conversationID sql.NullString

		err := rows.Scan(
			&entry.ID, &timestampStr, &entry.Action, &entry.ToolName,
			&entry.DetailsJSON, &riskLevel, &entry.Decision, &entry.Reason,
			&entry.RuleSource, &overrideID, &conversationID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit entry: %w", err)
		}

		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
		entry.RiskLevel = RiskLevel(riskLevel)

		if overrideID.Valid {
			entry.OverrideID = &overrideID.Int64
		}
		if conversationID.Valid {
			entry.ConversationID = &conversationID.String
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit entries: %w", err)
	}

	return entries, nil
}

// GetStats returns aggregate security statistics.
func (a *AuditLog) GetStats() (SecurityStats, error) {
	stats := SecurityStats{
		TopDeniedActions: make(map[string]int64),
	}

	// Total decisions
	err := a.db.QueryRow(`SELECT COUNT(*) FROM decision_log`).Scan(&stats.TotalDecisions)
	if err != nil {
		return stats, err
	}

	// Total allows
	err = a.db.QueryRow(`SELECT COUNT(*) FROM decision_log WHERE decision = 'allow'`).Scan(&stats.TotalAllows)
	if err != nil {
		return stats, err
	}

	// Total denies
	err = a.db.QueryRow(`SELECT COUNT(*) FROM decision_log WHERE decision = 'deny'`).Scan(&stats.TotalDenies)
	if err != nil {
		return stats, err
	}

	// Total escalations
	err = a.db.QueryRow(`SELECT COUNT(*) FROM decision_log WHERE decision = 'escalate'`).Scan(&stats.TotalEscalations)
	if err != nil {
		return stats, err
	}

	// Active overrides
	now := time.Now().UTC().Format(time.RFC3339)
	err = a.db.QueryRow(`
		SELECT COUNT(*) FROM permission_overrides
		WHERE (expires_at IS NULL OR expires_at > ?)
		AND (max_uses = 0 OR usage_count < max_uses)`, now).Scan(&stats.ActiveOverrides)
	if err != nil {
		return stats, err
	}

	// Top denied actions
	rows, err := a.db.Query(`
		SELECT action, COUNT(*) as cnt
		FROM decision_log
		WHERE decision = 'deny'
		GROUP BY action
		ORDER BY cnt DESC
		LIMIT 10`)
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	for rows.Next() {
		var action string
		var count int64
		if err := rows.Scan(&action, &count); err != nil {
			continue
		}
		stats.TopDeniedActions[action] = count
	}

	return stats, nil
}

// GetRecentEntries returns the most recent N audit entries.
func (a *AuditLog) GetRecentEntries(limit int) ([]AuditEntry, error) {
	return a.QueryHistory(QueryFilters{Limit: limit})
}

// GetEntriesForAction returns audit entries for a specific action.
func (a *AuditLog) GetEntriesForAction(action string, limit int) ([]AuditEntry, error) {
	return a.QueryHistory(QueryFilters{Action: action, Limit: limit})
}

// GetDeniedEntries returns entries where permission was denied.
func (a *AuditLog) GetDeniedEntries(limit int) ([]AuditEntry, error) {
	return a.QueryHistory(QueryFilters{Decision: "deny", Limit: limit})
}

// GetEscalatedEntries returns entries that required user confirmation.
func (a *AuditLog) GetEscalatedEntries(limit int) ([]AuditEntry, error) {
	return a.QueryHistory(QueryFilters{Decision: "escalate", Limit: limit})
}

// GetEntriesSince returns entries from the specified time onwards.
func (a *AuditLog) GetEntriesSince(since time.Time, limit int) ([]AuditEntry, error) {
	return a.QueryHistory(QueryFilters{Since: &since, Limit: limit})
}

// PurgeOldEntries deletes audit entries older than the specified duration.
func (a *AuditLog) PurgeOldEntries(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).Format(time.RFC3339)

	result, err := a.db.Exec(`DELETE FROM decision_log WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old entries: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return deleted, nil
}

// CountEntries returns the total number of audit entries.
func (a *AuditLog) CountEntries() (int64, error) {
	var count int64
	err := a.db.QueryRow(`SELECT COUNT(*) FROM decision_log`).Scan(&count)
	return count, err
}

// CountEntriesByDecision returns the count of entries for each decision type.
func (a *AuditLog) CountEntriesByDecision() (map[string]int64, error) {
	rows, err := a.db.Query(`
		SELECT decision, COUNT(*) as cnt
		FROM decision_log
		GROUP BY decision`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var decision string
		var count int64
		if err := rows.Scan(&decision, &count); err != nil {
			continue
		}
		counts[decision] = count
	}

	return counts, nil
}
