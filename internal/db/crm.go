package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// CRM rules that live in the transaction (platform-crm-calendar-mcp, D3). Any member
// may record contacts and activities. A deal is changed by its owner or a manager;
// only a manager moves a deal to someone else.

// readTeamRole returns the caller's role for a read. It takes no lock: a read does
// not need to queue behind writes the way a change does.
func readTeamRole(ctx context.Context, q queryRower, teamID, callerID int64) (model.TeamRole, error) {
	if teamID <= 0 || callerID <= 0 {
		return "", model.ErrForbidden
	}
	var role string
	err := q.QueryRowContext(ctx, `SELECT role FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, callerID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", model.ErrForbidden
	}
	if err != nil {
		return "", err
	}
	return model.TeamRole(role), nil
}

// clientInTeam confirms a client belongs to the workspace. A missing client is not
// found, the same answer as a client of another workspace.
func clientInTeam(ctx context.Context, q queryRower, teamID, clientID int64) error {
	if clientID == 0 {
		return nil
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM clients WHERE id = ? AND team_id = ?`, clientID, teamID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (d *DB) CreateCRMContact(ctx context.Context, request appmodel.CRMContactCreateRequest) (model.CRMContact, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ClientID <= 0 {
		return model.CRMContact{}, ErrNotFound
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return model.CRMContact{}, fmt.Errorf("contact name cannot be empty")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMContact{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.CRMContact{}, err
	}
	if err := clientInTeam(ctx, tx, request.TeamID, request.ClientID); err != nil {
		return model.CRMContact{}, err
	}
	now := d.currentTime().UTC()
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO crm_contacts (team_id, client_id, name, role, email, phone, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		request.TeamID, request.ClientID, name, strings.TrimSpace(request.Role),
		strings.TrimSpace(request.Email), strings.TrimSpace(request.Phone), request.CallerID, FormatTime(now)).Scan(&id)
	if err != nil {
		return model.CRMContact{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMContact{}, err
	}
	return model.CRMContact{ID: id, TeamID: request.TeamID, ClientID: request.ClientID, Name: name,
		Role: strings.TrimSpace(request.Role), Email: strings.TrimSpace(request.Email),
		Phone: strings.TrimSpace(request.Phone), CreatedBy: request.CallerID, CreatedAt: now}, nil
}

func (d *DB) ListCRMContacts(ctx context.Context, query appmodel.CRMContactListQuery) ([]model.CRMContact, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return nil, err
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, client_id, name, role, email, phone, created_by, created_at FROM crm_contacts
		  WHERE team_id = ? AND client_id = ? ORDER BY id`, query.TeamID, query.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.CRMContact, 0)
	for rows.Next() {
		c := model.CRMContact{TeamID: query.TeamID}
		var created string
		if err := rows.Scan(&c.ID, &c.ClientID, &c.Name, &c.Role, &c.Email, &c.Phone, &c.CreatedBy, &created); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// dealColumns is the one projection every deal read uses.
const dealColumns = `id, title, stage, COALESCE(client_id, 0), COALESCE(contact_id, 0), owner_id,
	amount_cents, currency, probability, close_date, next_step_at, lost_reason, COALESCE(project_id, 0), created_by, created_at, updated_at`

type dealScanner interface {
	Scan(...any) error
}

func scanCRMDeal(teamID int64, row dealScanner) (model.CRMDeal, error) {
	d := model.CRMDeal{TeamID: teamID}
	var created, updated string
	if err := row.Scan(&d.ID, &d.Title, &d.Stage, &d.ClientID, &d.ContactID, &d.OwnerID, &d.AmountCents,
		&d.Currency, &d.Probability, &d.CloseDate, &d.NextStepAt, &d.LostReason, &d.ProjectID, &d.CreatedBy, &created, &updated); err != nil {
		return model.CRMDeal{}, err
	}
	var err error
	if d.CreatedAt, err = ParseTime(created); err != nil {
		return model.CRMDeal{}, err
	}
	if d.UpdatedAt, err = ParseTime(updated); err != nil {
		return model.CRMDeal{}, err
	}
	return d, nil
}

// dealForEdit loads a deal and checks the caller may change it. Members change their
// own deals; a manager changes any.
func dealForEdit(ctx context.Context, tx *Tx, teamID, callerID, dealID int64) (model.CRMDeal, error) {
	_, role, err := lockTeamRole(ctx, tx, teamID, callerID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	row := tx.QueryRowContext(ctx, `SELECT `+dealColumns+` FROM crm_deals WHERE id = ? AND team_id = ?`, dealID, teamID)
	deal, err := scanCRMDeal(teamID, row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CRMDeal{}, ErrNotFound
	}
	if err != nil {
		return model.CRMDeal{}, err
	}
	if !role.CanManage() && deal.OwnerID != callerID {
		return model.CRMDeal{}, model.ErrForbidden
	}
	return deal, nil
}

func (d *DB) CreateCRMDeal(ctx context.Context, request appmodel.CRMDealCreateRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	title := strings.TrimSpace(request.Title)
	if title == "" {
		return model.CRMDeal{}, fmt.Errorf("deal title cannot be empty")
	}
	if request.AmountCents < 0 || request.Probability < 0 || request.Probability > 100 {
		return model.CRMDeal{}, fmt.Errorf("deal amount and probability are out of range")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMDeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	owner := request.OwnerID
	if owner == 0 {
		owner = request.CallerID
	}
	if owner != request.CallerID && !role.CanManage() {
		return model.CRMDeal{}, model.ErrForbidden
	}
	if err := clientInTeam(ctx, tx, request.TeamID, request.ClientID); err != nil {
		return model.CRMDeal{}, err
	}
	if request.ContactID > 0 {
		var ok int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM crm_contacts WHERE id = ? AND team_id = ?`, request.ContactID, request.TeamID).Scan(&ok)
		if errors.Is(err, sql.ErrNoRows) {
			return model.CRMDeal{}, ErrNotFound
		}
		if err != nil {
			return model.CRMDeal{}, err
		}
	}
	now := FormatTime(d.currentTime().UTC())
	stage, err := resolveDealStage(ctx, tx, request.TeamID, request.Stage, now)
	if err != nil {
		return model.CRMDeal{}, err
	}
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO crm_deals (team_id, title, stage, client_id, contact_id, owner_id, amount_cents, currency,
		   probability, close_date, next_step_at, lost_reason, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, NULLIF(?, 0), NULLIF(?, 0), ?, ?, COALESCE(NULLIF(?, ''), (SELECT currency FROM teams WHERE id = ?)), ?, ?, ?, '', ?, ?, ?) RETURNING id`,
		request.TeamID, title, stage, request.ClientID, request.ContactID, owner, request.AmountCents,
		request.Currency, request.TeamID, request.Probability, request.CloseDate, request.NextStepAt, request.CallerID, now, now).Scan(&id)
	if err != nil {
		return model.CRMDeal{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMDeal{}, err
	}
	return d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: request.TeamID, CallerID: request.CallerID, DealID: id})
}

func (d *DB) GetCRMDeal(ctx context.Context, query appmodel.CRMDealLookupQuery) (model.CRMDeal, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return model.CRMDeal{}, err
	}
	deal, err := scanCRMDeal(query.TeamID, d.sql.QueryRowContext(ctx,
		`SELECT `+dealColumns+` FROM crm_deals WHERE id = ? AND team_id = ?`, query.DealID, query.TeamID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.CRMDeal{}, ErrNotFound
	}
	if err != nil {
		return model.CRMDeal{}, err
	}
	stages, err := crmStagesOf(ctx, d.sql, query.TeamID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	deal.StageKind = stageKindOf(stages, deal.Stage)
	return deal, nil
}

func (d *DB) ListCRMDeals(ctx context.Context, query appmodel.CRMDealListQuery) ([]model.CRMDeal, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return nil, err
	}
	stages, err := crmStagesOf(ctx, d.sql, query.TeamID)
	if err != nil {
		return nil, err
	}
	if query.Stage != "" {
		if _, ok := model.FindDealStage(stages, query.Stage); !ok {
			return nil, model.ErrInvalidDealStage
		}
	}
	q := `SELECT ` + dealColumns + ` FROM crm_deals WHERE team_id = ?`
	args := []any{query.TeamID}
	if query.Stage != "" {
		q += ` AND stage = ?`
		args = append(args, query.Stage)
	}
	if query.OwnerID > 0 {
		q += ` AND owner_id = ?`
		args = append(args, query.OwnerID)
	}
	q += ` ORDER BY stage, id`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.CRMDeal, 0)
	for rows.Next() {
		deal, err := scanCRMDeal(query.TeamID, rows)
		if err != nil {
			return nil, err
		}
		deal.StageKind = stageKindOf(stages, deal.Stage)
		if query.Overdue && !deal.Overdue(query.Today) {
			continue
		}
		out = append(out, deal)
	}
	return out, rows.Err()
}

// UpdateCRMDeal changes the fields that are set. Owner changes need a manager.
func (d *DB) UpdateCRMDeal(ctx context.Context, request appmodel.CRMDealUpdateRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.DealID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMDeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := dealForEdit(ctx, tx, request.TeamID, request.CallerID, request.DealID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	sets := []string{"updated_at = ?"}
	args := []any{FormatTime(d.currentTime().UTC())}
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if title == "" {
			return model.CRMDeal{}, fmt.Errorf("deal title cannot be empty")
		}
		sets, args = append(sets, "title = ?"), append(args, title)
	}
	if request.OwnerID != nil && *request.OwnerID != current.OwnerID {
		if !role.CanManage() {
			return model.CRMDeal{}, model.ErrForbidden
		}
		sets, args = append(sets, "owner_id = ?"), append(args, *request.OwnerID)
	}
	if request.ClientID != nil {
		if err := clientInTeam(ctx, tx, request.TeamID, *request.ClientID); err != nil {
			return model.CRMDeal{}, err
		}
		sets, args = append(sets, "client_id = NULLIF(?, 0)"), append(args, *request.ClientID)
	}
	if request.ContactID != nil {
		sets, args = append(sets, "contact_id = NULLIF(?, 0)"), append(args, *request.ContactID)
	}
	if request.AmountCents != nil {
		if *request.AmountCents < 0 {
			return model.CRMDeal{}, fmt.Errorf("deal amount is out of range")
		}
		sets, args = append(sets, "amount_cents = ?"), append(args, *request.AmountCents)
	}
	if request.Probability != nil {
		if *request.Probability < 0 || *request.Probability > 100 {
			return model.CRMDeal{}, fmt.Errorf("deal probability is out of range")
		}
		sets, args = append(sets, "probability = ?"), append(args, *request.Probability)
	}
	if request.CloseDate != nil {
		sets, args = append(sets, "close_date = ?"), append(args, *request.CloseDate)
	}
	if request.NextStepAt != nil {
		sets, args = append(sets, "next_step_at = ?"), append(args, *request.NextStepAt)
	}
	args = append(args, request.DealID, request.TeamID)
	if _, err := tx.ExecContext(ctx, `UPDATE crm_deals SET `+strings.Join(sets, ", ")+` WHERE id = ? AND team_id = ?`, args...); err != nil {
		return model.CRMDeal{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMDeal{}, err
	}
	return d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: request.TeamID, CallerID: request.CallerID, DealID: request.DealID})
}

// MoveCRMDealStage moves a deal to another stage of its workspace. A stage of the
// lost kind needs a reason, and a deal that leaves that kind clears it.
func (d *DB) MoveCRMDealStage(ctx context.Context, request appmodel.CRMDealStageRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.DealID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	reason := strings.TrimSpace(request.LostReason)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMDeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := dealForEdit(ctx, tx, request.TeamID, request.CallerID, request.DealID); err != nil {
		return model.CRMDeal{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	if err := ensureCRMStages(ctx, tx, request.TeamID, now); err != nil {
		return model.CRMDeal{}, err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	target, ok := model.FindDealStage(stages, request.Stage)
	if !ok {
		return model.CRMDeal{}, model.ErrInvalidDealStage
	}
	if target.Kind == model.StageKindLost && reason == "" {
		return model.CRMDeal{}, model.ErrLostReasonRequired
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crm_deals SET stage = ?, lost_reason = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
		request.Stage, reasonFor(target.Kind, reason), now, request.DealID, request.TeamID); err != nil {
		return model.CRMDeal{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMDeal{}, err
	}
	return d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: request.TeamID, CallerID: request.CallerID, DealID: request.DealID})
}

// CRMDealForProject reads a deal for the step that makes its project, with the stage kind
// the step checks. Its transaction takes the workspace lock as a change does, and the
// owner or a manager may read it, the same people who may move the deal.
func (d *DB) CRMDealForProject(ctx context.Context, request appmodel.CRMDealProjectRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.DealID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMDeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	deal, err := dealForEdit(ctx, tx, request.TeamID, request.CallerID, request.DealID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	deal.StageKind = stageKindOf(stages, deal.Stage)
	return deal, nil
}

// LinkCRMDealProject records the project a won deal made. The deal rules run again under
// the workspace lock, so a deal that left the won stage, or got a project meanwhile, is
// refused and nothing is written. The caller then removes the project it just made.
func (d *DB) LinkCRMDealProject(ctx context.Context, request appmodel.CRMDealProjectLinkRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.DealID <= 0 || request.ProjectID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMDeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	deal, err := dealForEdit(ctx, tx, request.TeamID, request.CallerID, request.DealID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return model.CRMDeal{}, err
	}
	if stageKindOf(stages, deal.Stage) != model.StageKindWon {
		return model.CRMDeal{}, appmodel.FieldError{Field: "stage"}
	}
	if deal.ProjectID != 0 {
		return model.CRMDeal{}, appmodel.FieldError{Field: "project"}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crm_deals SET project_id = ?, updated_at = ? WHERE id = ? AND team_id = ? AND project_id IS NULL`,
		request.ProjectID, FormatTime(d.currentTime().UTC()), request.DealID, request.TeamID); err != nil {
		return model.CRMDeal{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMDeal{}, err
	}
	return d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: request.TeamID, CallerID: request.CallerID, DealID: request.DealID})
}

func reasonFor(kind, reason string) string {
	if kind == model.StageKindLost {
		return reason
	}
	return ""
}

func (d *DB) CreateCRMActivity(ctx context.Context, request appmodel.CRMActivityCreateRequest) (model.CRMActivity, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CRMActivity{}, ErrNotFound
	}
	if request.ClientID <= 0 && request.DealID <= 0 && request.LeadID <= 0 {
		return model.CRMActivity{}, ErrNotFound
	}
	kindOK := false
	for _, k := range model.ActivityKinds {
		kindOK = kindOK || k == request.Kind
	}
	if !kindOK {
		return model.CRMActivity{}, model.ErrInvalidActivityKind
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMActivity{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.CRMActivity{}, err
	}
	if err := clientInTeam(ctx, tx, request.TeamID, request.ClientID); err != nil {
		return model.CRMActivity{}, err
	}
	if err := leadInTeam(ctx, tx, request.TeamID, request.LeadID); err != nil {
		return model.CRMActivity{}, err
	}
	clientID := request.ClientID
	if request.DealID > 0 {
		var dealClient sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT client_id FROM crm_deals WHERE id = ? AND team_id = ?`, request.DealID, request.TeamID).Scan(&dealClient)
		if errors.Is(err, sql.ErrNoRows) {
			return model.CRMActivity{}, ErrNotFound
		}
		if err != nil {
			return model.CRMActivity{}, err
		}
		if clientID == 0 && dealClient.Valid {
			clientID = dealClient.Int64
		}
	}
	now := d.currentTime().UTC()
	occurred := request.OccurredAt
	if occurred.IsZero() {
		occurred = now
	}
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO crm_activities (team_id, client_id, deal_id, lead_id, kind, body, result, occurred_at, author_id, created_at)
		 VALUES (?, NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?, ?, ?) RETURNING id`,
		request.TeamID, clientID, request.DealID, request.LeadID, request.Kind, strings.TrimSpace(request.Body),
		strings.TrimSpace(request.Result), FormatTime(occurred.UTC()), request.CallerID, FormatTime(now)).Scan(&id)
	if err != nil {
		return model.CRMActivity{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMActivity{}, err
	}
	return model.CRMActivity{ID: id, TeamID: request.TeamID, ClientID: clientID, DealID: request.DealID, LeadID: request.LeadID,
		Kind: request.Kind, Body: strings.TrimSpace(request.Body), Result: strings.TrimSpace(request.Result),
		OccurredAt: occurred.UTC(), AuthorID: request.CallerID, CreatedAt: now}, nil
}

func (d *DB) ListCRMActivities(ctx context.Context, query appmodel.CRMActivityListQuery) ([]model.CRMActivity, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return nil, err
	}
	q := `SELECT id, COALESCE(client_id, 0), COALESCE(deal_id, 0), COALESCE(lead_id, 0), kind, body, result, occurred_at, author_id, created_at
	        FROM crm_activities WHERE team_id = ?`
	args := []any{query.TeamID}
	if query.DealID > 0 {
		q += ` AND deal_id = ?`
		args = append(args, query.DealID)
	} else if query.ClientID > 0 {
		q += ` AND client_id = ?`
		args = append(args, query.ClientID)
	} else if query.LeadID > 0 {
		q += ` AND lead_id = ?`
		args = append(args, query.LeadID)
	}
	q += ` ORDER BY occurred_at DESC, id DESC`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.CRMActivity, 0)
	for rows.Next() {
		a := model.CRMActivity{TeamID: query.TeamID}
		var occurred, created string
		if err := rows.Scan(&a.ID, &a.ClientID, &a.DealID, &a.LeadID, &a.Kind, &a.Body, &a.Result, &occurred, &a.AuthorID, &created); err != nil {
			return nil, err
		}
		if a.OccurredAt, err = ParseTime(occurred); err != nil {
			return nil, err
		}
		if a.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// The pipeline (CRM-12). A workspace's stages are rows of crm_stages. A workspace with
// no rows reads model.DefaultDealStages; the first write to its stages inserts them.
// Reads never write, so the defaults are not stored until someone changes the stages
// or a deal is created or moved. Every write takes the workspace row lock in
// lockTeamRole, so a stage change and a deal move in one workspace never interleave.

// queryer is the read side that a database handle and a transaction share.
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func defaultCRMStages(teamID int64) []model.CRMStage {
	out := make([]model.CRMStage, len(model.DefaultDealStages))
	for i, stage := range model.DefaultDealStages {
		stage.TeamID, stage.Position = teamID, i+1
		out[i] = stage
	}
	return out
}

// crmStagesOf reads a workspace's pipeline in its order.
func crmStagesOf(ctx context.Context, q queryer, teamID int64) ([]model.CRMStage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT key, name, position, kind FROM crm_stages WHERE team_id = ? ORDER BY position, key`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.CRMStage, 0, len(model.DefaultDealStages))
	for rows.Next() {
		stage := model.CRMStage{TeamID: teamID}
		if err := rows.Scan(&stage.Key, &stage.Name, &stage.Position, &stage.Kind); err != nil {
			return nil, err
		}
		out = append(out, stage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return defaultCRMStages(teamID), nil
	}
	return out, nil
}

// stageKindOf is the kind of a deal's stage. A key the workspace no longer has has no
// kind, so its deals are never overdue.
func stageKindOf(stages []model.CRMStage, key string) string {
	if stage, ok := model.FindDealStage(stages, key); ok {
		return stage.Kind
	}
	return ""
}

func firstOpenStageKey(stages []model.CRMStage) string {
	for _, stage := range stages {
		if stage.Kind == model.StageKindOpen {
			return stage.Key
		}
	}
	return ""
}

// ensureCRMStages stores the default pipeline for a workspace that has none. The
// caller holds the workspace row lock, so the count and the inserts see one state.
func ensureCRMStages(ctx context.Context, tx *Tx, teamID int64, now string) error {
	var stored int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crm_stages WHERE team_id = ?`, teamID).Scan(&stored); err != nil {
		return err
	}
	if stored > 0 {
		return nil
	}
	for _, stage := range defaultCRMStages(teamID) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO crm_stages (team_id, key, name, position, kind, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			teamID, stage.Key, stage.Name, stage.Position, stage.Kind, now); err != nil {
			return err
		}
	}
	return nil
}

// resolveDealStage picks the stage a new deal enters: the given key, or the first open
// stage. A new deal cannot start in a lost stage, because it has no reason yet.
func resolveDealStage(ctx context.Context, tx *Tx, teamID int64, key, now string) (string, error) {
	if err := ensureCRMStages(ctx, tx, teamID, now); err != nil {
		return "", err
	}
	stages, err := crmStagesOf(ctx, tx, teamID)
	if err != nil {
		return "", err
	}
	if key == "" {
		key = firstOpenStageKey(stages)
	}
	stage, ok := model.FindDealStage(stages, key)
	if !ok {
		return "", model.ErrInvalidDealStage
	}
	if stage.Kind == model.StageKindLost {
		return "", model.ErrLostReasonRequired
	}
	return key, nil
}

// checkStageKinds refuses a pipeline without an open, a won or a lost stage, and names
// the field the change would break.
func checkStageKinds(stages []model.CRMStage, field string) error {
	kinds := make(map[string]bool, len(stages))
	for _, stage := range stages {
		kinds[stage.Kind] = true
	}
	if !kinds[model.StageKindOpen] || !kinds[model.StageKindWon] || !kinds[model.StageKindLost] {
		return appmodel.FieldError{Field: field}
	}
	return nil
}

func withoutStage(stages []model.CRMStage, key string) []model.CRMStage {
	out := make([]model.CRMStage, 0, len(stages))
	for _, stage := range stages {
		if stage.Key != key {
			out = append(out, stage)
		}
	}
	return out
}

// renumberStages sets positions 1..n in slice order, so they stay contiguous.
func renumberStages(stages []model.CRMStage) {
	for i := range stages {
		stages[i].Position = i + 1
	}
}

// uniqueStageKey returns base, or base with the first free number after it.
func uniqueStageKey(base string, stages []model.CRMStage) string {
	key := base
	for n := 2; ; n++ {
		if _, taken := model.FindDealStage(stages, key); !taken {
			return key
		}
		key = fmt.Sprintf("%s-%d", base, n)
	}
}

func writeCRMStages(ctx context.Context, tx *Tx, stages []model.CRMStage) error {
	for _, stage := range stages {
		if _, err := tx.ExecContext(ctx,
			`UPDATE crm_stages SET name = ?, kind = ?, position = ? WHERE team_id = ? AND key = ?`,
			stage.Name, stage.Kind, stage.Position, stage.TeamID, stage.Key); err != nil {
			return err
		}
	}
	return nil
}

// lockStageManager takes the workspace lock for a stage change and refuses a caller
// who is not a manager.
func lockStageManager(ctx context.Context, tx *Tx, teamID, callerID int64) error {
	_, role, err := lockTeamRole(ctx, tx, teamID, callerID)
	if err != nil {
		return err
	}
	if !role.CanManage() {
		return model.ErrForbidden
	}
	return nil
}

func (d *DB) ListCRMStages(ctx context.Context, query appmodel.CRMStageListQuery) ([]model.CRMStage, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return nil, err
	}
	return crmStagesOf(ctx, d.sql, query.TeamID)
}

// CreateCRMStage appends a stage to the pipeline under a key derived from its name.
func (d *DB) CreateCRMStage(ctx context.Context, request appmodel.CRMStageCreateRequest) (model.CRMStage, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CRMStage{}, ErrNotFound
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return model.CRMStage{}, appmodel.FieldError{Field: "stage name"}
	}
	if !model.ValidStageKind(request.Kind) {
		return model.CRMStage{}, appmodel.FieldError{Field: "kind"}
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMStage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockStageManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.CRMStage{}, err
	}
	if err := ensureCRMStages(ctx, tx, request.TeamID, FormatTime(d.currentTime().UTC())); err != nil {
		return model.CRMStage{}, err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return model.CRMStage{}, err
	}
	stage := model.CRMStage{
		TeamID: request.TeamID, Key: uniqueStageKey(model.DealStageKey(name), stages),
		Name: name, Position: len(stages) + 1, Kind: request.Kind,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO crm_stages (team_id, key, name, position, kind, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		stage.TeamID, stage.Key, stage.Name, stage.Position, stage.Kind, FormatTime(d.currentTime().UTC())); err != nil {
		return model.CRMStage{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMStage{}, err
	}
	return stage, nil
}

// UpdateCRMStage renames a stage, sets its kind or moves it. The pipeline keeps an open,
// a won and a lost stage, and a stage that leaves the lost kind clears the reasons of
// its deals, as a deal that leaves a lost stage does.
func (d *DB) UpdateCRMStage(ctx context.Context, request appmodel.CRMStageUpdateRequest) (model.CRMStage, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CRMStage{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMStage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockStageManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.CRMStage{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	if err := ensureCRMStages(ctx, tx, request.TeamID, now); err != nil {
		return model.CRMStage{}, err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return model.CRMStage{}, err
	}
	current, ok := model.FindDealStage(stages, request.Key)
	if !ok {
		return model.CRMStage{}, ErrNotFound
	}
	edited := current
	if request.Name != nil {
		edited.Name = strings.TrimSpace(*request.Name)
		if edited.Name == "" {
			return model.CRMStage{}, appmodel.FieldError{Field: "stage name"}
		}
	}
	if request.Kind != nil {
		if !model.ValidStageKind(*request.Kind) {
			return model.CRMStage{}, appmodel.FieldError{Field: "kind"}
		}
		edited.Kind = *request.Kind
	}
	target := current.Position
	if request.Position != nil {
		target = *request.Position
	}
	if target < 1 || target > len(stages) {
		return model.CRMStage{}, appmodel.FieldError{Field: "position"}
	}
	ordered := slices.Insert(withoutStage(stages, current.Key), target-1, edited)
	renumberStages(ordered)
	if err := checkStageKinds(ordered, "kind"); err != nil {
		return model.CRMStage{}, err
	}
	if err := writeCRMStages(ctx, tx, ordered); err != nil {
		return model.CRMStage{}, err
	}
	if current.Kind == model.StageKindLost && edited.Kind != model.StageKindLost {
		if _, err := tx.ExecContext(ctx,
			`UPDATE crm_deals SET lost_reason = '', updated_at = ? WHERE team_id = ? AND stage = ? AND lost_reason <> ''`,
			now, request.TeamID, current.Key); err != nil {
			return model.CRMStage{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.CRMStage{}, err
	}
	return ordered[target-1], nil
}

// DeleteCRMStage removes a stage that has no deals. The pipeline keeps an open, a won
// and a lost stage; a stage with deals is refused, and its deals are not moved.
func (d *DB) DeleteCRMStage(ctx context.Context, request appmodel.CRMStageDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockStageManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if err := ensureCRMStages(ctx, tx, request.TeamID, FormatTime(d.currentTime().UTC())); err != nil {
		return err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return err
	}
	if _, ok := model.FindDealStage(stages, request.Key); !ok {
		return ErrNotFound
	}
	var deals int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM crm_deals WHERE team_id = ? AND stage = ?`, request.TeamID, request.Key).Scan(&deals); err != nil {
		return err
	}
	if deals > 0 {
		return appmodel.FieldError{Field: "stage"}
	}
	remaining := withoutStage(stages, request.Key)
	renumberStages(remaining)
	if err := checkStageKinds(remaining, "kind"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM crm_stages WHERE team_id = ? AND key = ?`, request.TeamID, request.Key); err != nil {
		return err
	}
	if err := writeCRMStages(ctx, tx, remaining); err != nil {
		return err
	}
	return tx.Commit()
}
