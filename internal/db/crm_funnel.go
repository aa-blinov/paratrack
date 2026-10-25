package db

import (
	"context"
	"database/sql"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The funnel report (CRM-17). The caller's role is read in the transaction that reads the
// figures, and the amount sums are kept only for a manager, so a member's report is counts and
// nothing else, whatever the transport does with it. The lead response figures are the CRM-16
// stats query, called here rather than copied.

// CRMFunnelReport builds the funnel of a period for a workspace member.
func (d *DB) CRMFunnelReport(ctx context.Context, query appmodel.CRMFunnelQuery) (model.CRMFunnel, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return model.CRMFunnel{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.CRMFunnel{}, err
	}
	defer func() { _ = tx.Rollback() }()
	role, err := readTeamRole(ctx, tx, query.TeamID, query.CallerID)
	if err != nil {
		return model.CRMFunnel{}, err
	}
	stages, err := crmStagesOf(ctx, tx, query.TeamID)
	if err != nil {
		return model.CRMFunnel{}, err
	}
	funnel := model.CRMFunnel{
		ShowAmounts: role.CanManage(), Pipeline: make([]model.CRMFunnelStage, len(stages)),
		LeadStatuses: make(map[string]int64, len(model.LeadStatuses)),
	}
	for i, stage := range stages {
		funnel.Pipeline[i] = model.CRMFunnelStage{Key: stage.Key, Name: stage.Name, Kind: stage.Kind}
	}
	if err := addFunnelDeals(ctx, tx, query, stages, &funnel); err != nil {
		return model.CRMFunnel{}, err
	}
	if err := addFunnelLeads(ctx, tx, query, &funnel); err != nil {
		return model.CRMFunnel{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMFunnel{}, err
	}
	funnel.Response, err = d.CRMLeadResponseStats(ctx, appmodel.CRMLeadResponseStatsQuery{
		TeamID: query.TeamID, CallerID: query.CallerID, From: query.From, To: query.To,
	})
	if err != nil {
		return model.CRMFunnel{}, err
	}
	return funnel, nil
}

// addFunnelDeals reads the deals once, grouped by stage and currency, and files each group by
// what the report shows of it: the pipeline now, the open deals whose next step has passed, or
// the won and lost deals that entered their stage in the period. A group is classified by its
// stage's kind, so a custom stage counts as the kind it was given.
func addFunnelDeals(ctx context.Context, tx *Tx, query appmodel.CRMFunnelQuery, stages []model.CRMStage, funnel *model.CRMFunnel) error {
	// Rows come in currency order, so each total's amounts do too.
	rows, err := tx.QueryContext(ctx,
		`WITH deal AS (
		   SELECT stage, currency, amount_cents,
		          stage_changed_at::timestamptz >= CAST(? AS timestamptz)
		            AND stage_changed_at::timestamptz < CAST(? AS timestamptz) AS in_period,
		          next_step_at <> '' AND next_step_at < ? AS overdue
		     FROM crm_deals WHERE team_id = ?
		 )
		 SELECT stage, currency, COUNT(*), COALESCE(SUM(amount_cents), 0)::bigint,
		        COUNT(*) FILTER (WHERE in_period), COALESCE(SUM(amount_cents) FILTER (WHERE in_period), 0)::bigint,
		        COUNT(*) FILTER (WHERE overdue), COALESCE(SUM(amount_cents) FILTER (WHERE overdue), 0)::bigint
		   FROM deal GROUP BY stage, currency ORDER BY currency, stage`,
		FormatTime(query.From), FormatTime(query.To), query.Today, query.TeamID)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := make(map[string]int, len(stages))
	for i, stage := range stages {
		index[stage.Key] = i
	}
	for rows.Next() {
		var stage, currency string
		var count, cents, periodCount, periodCents, overdueCount, overdueCents int64
		if err := rows.Scan(&stage, &currency, &count, &cents, &periodCount, &periodCents, &overdueCount, &overdueCents); err != nil {
			return err
		}
		if i, ok := index[stage]; ok {
			addDealTotal(&funnel.Pipeline[i].Total, currency, count, cents, funnel.ShowAmounts)
		}
		switch stageKindOf(stages, stage) {
		case model.StageKindOpen:
			addDealTotal(&funnel.Overdue, currency, overdueCount, overdueCents, funnel.ShowAmounts)
		case model.StageKindWon:
			addDealTotal(&funnel.Won, currency, periodCount, periodCents, funnel.ShowAmounts)
		case model.StageKindLost:
			addDealTotal(&funnel.Lost, currency, periodCount, periodCents, funnel.ShowAmounts)
		}
	}
	return rows.Err()
}

// addDealTotal adds a group's count, and its amount to the currency's entry when the caller may
// see amounts. An amount never joins another currency's sum.
func addDealTotal(total *model.CRMDealTotal, currency string, count, cents int64, showAmounts bool) {
	total.Count += count
	if !showAmounts || count == 0 {
		return
	}
	for i := range total.Amounts {
		if total.Amounts[i].Currency == currency {
			total.Amounts[i].AmountCents += cents
			return
		}
	}
	total.Amounts = append(total.Amounts, model.CRMAmount{Currency: currency, AmountCents: cents})
}

// addFunnelLeads reads the leads: how many are in each status now, and the ones created in the
// period by source, with how many of those were converted. The conversion is over the period's
// own leads, so a lead made in the period and converted later still counts.
func addFunnelLeads(ctx context.Context, tx *Tx, query appmodel.CRMFunnelQuery, funnel *model.CRMFunnel) error {
	statuses, err := tx.QueryContext(ctx, `SELECT status, COUNT(*) FROM crm_leads WHERE team_id = ? GROUP BY status`, query.TeamID)
	if err != nil {
		return err
	}
	defer statuses.Close()
	for statuses.Next() {
		var status string
		var count int64
		if err := statuses.Scan(&status, &count); err != nil {
			return err
		}
		funnel.LeadStatuses[status] = count
	}
	if err := statuses.Err(); err != nil {
		return err
	}
	sources, err := tx.QueryContext(ctx,
		`SELECT source, COUNT(*), COUNT(*) FILTER (WHERE status = 'converted')
		   FROM crm_leads
		  WHERE team_id = ?
		    AND created_at::timestamptz >= CAST(? AS timestamptz)
		    AND created_at::timestamptz < CAST(? AS timestamptz)
		  GROUP BY source ORDER BY COUNT(*) DESC, source`,
		query.TeamID, FormatTime(query.From), FormatTime(query.To))
	if err != nil {
		return err
	}
	defer sources.Close()
	for sources.Next() {
		var source string
		var created, converted int64
		if err := sources.Scan(&source, &created, &converted); err != nil {
			return err
		}
		funnel.LeadSources = append(funnel.LeadSources, model.CRMLeadSource{Source: source, Created: created})
		funnel.LeadsCreated += created
		funnel.LeadsConverted += converted
	}
	return sources.Err()
}
