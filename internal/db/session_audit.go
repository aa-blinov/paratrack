package db

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// sessionEditAudit records what one edit changed: one audit row per changed field,
// written in the edit's own transaction, so a rejected edit leaves no row behind.
// The meta is "field: before -> after".
func sessionEditAudit(ctx context.Context, tx *Tx, teamID, actor int64, ip string, sessionID int64,
	before sessionEditState, update appmodel.SessionUpdate, now string) error {
	type change struct{ field, from, to string }
	var changes []change
	if update.StartAt != nil && FormatTime(*update.StartAt) != before.startAt {
		changes = append(changes, change{"start_at", before.startAt, FormatTime(*update.StartAt)})
	}
	if update.EndAt != nil {
		from := ""
		if before.endAt.Valid {
			from = before.endAt.String
		}
		if to := FormatTime(*update.EndAt); to != from {
			changes = append(changes, change{"end_at", from, to})
		}
	}
	if update.AccumulatedSeconds != nil && int64(*update.AccumulatedSeconds) != before.accumulatedSeconds {
		changes = append(changes, change{"accumulated_seconds",
			strconv.FormatInt(before.accumulatedSeconds, 10), strconv.Itoa(*update.AccumulatedSeconds)})
	}
	if update.ProjectID != nil && *update.ProjectID != before.ownProject {
		changes = append(changes, change{"project_id",
			strconv.FormatInt(before.ownProject, 10), strconv.FormatInt(*update.ProjectID, 10)})
	}
	from := ""
	if before.note.Valid {
		from = before.note.String
	}
	if update.Note != from {
		changes = append(changes, change{"note", from, update.Note})
	}
	target := fmt.Sprintf("session:%d", sessionID)
	for _, c := range changes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO audit_log (team_id, user_id, action, target, meta, ip, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			teamID, actor, "session.edit", target, c.field+": "+c.from+" -> "+c.to, ip, now); err != nil {
			return err
		}
	}
	return nil
}
