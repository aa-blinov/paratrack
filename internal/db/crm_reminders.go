package db

import (
	"context"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// CRMStepsDue lists the open deals and the unfinished leads whose owner still
// belongs to the workspace and whose next step is on or before through
// (YYYY-MM-DD). A won or lost deal, a converted or lost lead, and an owner who has
// left are left out here; the reminder pass decides which of the rest fall on today
// in their workspace's zone.
func (d *DB) CRMStepsDue(ctx context.Context, through string) ([]appmodel.CRMStep, error) {
	type dealRow struct {
		step  appmodel.CRMStep
		stage string
	}
	dealRows, err := d.sql.QueryContext(ctx,
		`SELECT d.team_id, d.id, d.title, d.stage, d.owner_id, d.next_step_at, COALESCE(t.timezone, '')
		   FROM crm_deals d JOIN teams t ON t.id = d.team_id
		  WHERE d.owner_id > 0 AND d.next_step_at <> '' AND d.next_step_at <= ?
		    AND (t.owner_id = d.owner_id OR EXISTS (SELECT 1 FROM memberships m WHERE m.team_id = d.team_id AND m.user_id = d.owner_id))
		  ORDER BY d.team_id, d.id`, through)
	if err != nil {
		return nil, fmt.Errorf("list due deals: %w", err)
	}
	var deals []dealRow
	for dealRows.Next() {
		var row dealRow
		row.step.Kind = appmodel.CRMItemDeal
		if err := dealRows.Scan(&row.step.TeamID, &row.step.ItemID, &row.step.Name, &row.stage,
			&row.step.OwnerID, &row.step.NextStepAt, &row.step.Zone); err != nil {
			_ = dealRows.Close()
			return nil, err
		}
		deals = append(deals, row)
	}
	if err := dealRows.Err(); err != nil {
		_ = dealRows.Close()
		return nil, err
	}
	_ = dealRows.Close()

	stagesByTeam := map[int64][]model.CRMStage{}
	var out []appmodel.CRMStep
	for _, row := range deals {
		stages, ok := stagesByTeam[row.step.TeamID]
		if !ok {
			if stages, err = crmStagesOf(ctx, d.sql, row.step.TeamID); err != nil {
				return nil, err
			}
			stagesByTeam[row.step.TeamID] = stages
		}
		if stageKindOf(stages, row.stage) == model.StageKindOpen {
			out = append(out, row.step)
		}
	}

	leadRows, err := d.sql.QueryContext(ctx,
		`SELECT l.team_id, l.id, l.company, l.contact_name, l.owner_id, l.next_step_at, COALESCE(t.timezone, '')
		   FROM crm_leads l JOIN teams t ON t.id = l.team_id
		  WHERE l.owner_id > 0 AND l.status IN ('new', 'contacted', 'qualified')
		    AND l.next_step_at <> '' AND l.next_step_at <= ?
		    AND (t.owner_id = l.owner_id OR EXISTS (SELECT 1 FROM memberships m WHERE m.team_id = l.team_id AND m.user_id = l.owner_id))
		  ORDER BY l.team_id, l.id`, through)
	if err != nil {
		return nil, fmt.Errorf("list due leads: %w", err)
	}
	defer leadRows.Close()
	for leadRows.Next() {
		var step appmodel.CRMStep
		var company, contact string
		step.Kind = appmodel.CRMItemLead
		if err := leadRows.Scan(&step.TeamID, &step.ItemID, &company, &contact,
			&step.OwnerID, &step.NextStepAt, &step.Zone); err != nil {
			return nil, err
		}
		step.Name = company
		if step.Name == "" {
			step.Name = contact
		}
		out = append(out, step)
	}
	return out, leadRows.Err()
}

// ClaimCRMReminder records that an item's reminder went out on a channel for a
// workspace day. It returns true only for the first claim. The claim is stored
// before the reminder is handed over, so a failed hand-over is not retried that day.
func (d *DB) ClaimCRMReminder(ctx context.Context, claim appmodel.CRMReminderClaimRequest) (bool, error) {
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO crm_reminders (team_id, item_kind, item_id, day, channel, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (team_id, item_kind, item_id, day, channel) DO NOTHING`,
		claim.TeamID, claim.Kind, claim.ItemID, claim.Day, claim.Channel, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return false, fmt.Errorf("claim crm reminder: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
