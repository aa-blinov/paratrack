package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	amount_cents, currency, probability, close_date, next_step_at, lost_reason, created_by, created_at, updated_at`

type dealScanner interface {
	Scan(...any) error
}

func scanCRMDeal(teamID int64, row dealScanner) (model.CRMDeal, error) {
	d := model.CRMDeal{TeamID: teamID}
	var created, updated string
	if err := row.Scan(&d.ID, &d.Title, &d.Stage, &d.ClientID, &d.ContactID, &d.OwnerID, &d.AmountCents,
		&d.Currency, &d.Probability, &d.CloseDate, &d.NextStepAt, &d.LostReason, &d.CreatedBy, &created, &updated); err != nil {
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
	stage := request.Stage
	if stage == "" {
		stage = model.DealStageNew
	}
	if !model.IsDealStage(stage) {
		return model.CRMDeal{}, model.ErrInvalidDealStage
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
	return deal, err
}

func (d *DB) ListCRMDeals(ctx context.Context, query appmodel.CRMDealListQuery) ([]model.CRMDeal, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return nil, err
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
	if query.Overdue && query.Today != "" {
		q += ` AND stage NOT IN ('won', 'lost') AND next_step_at <> '' AND substr(next_step_at, 1, 10) < ?`
		args = append(args, query.Today)
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

// MoveCRMDealStage moves a deal to another stage. A lost deal keeps its reason, and
// a deal that is moved out of lost clears it.
func (d *DB) MoveCRMDealStage(ctx context.Context, request appmodel.CRMDealStageRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.DealID <= 0 {
		return model.CRMDeal{}, ErrNotFound
	}
	if !model.IsDealStage(request.Stage) {
		return model.CRMDeal{}, model.ErrInvalidDealStage
	}
	reason := strings.TrimSpace(request.LostReason)
	if request.Stage == model.DealStageLost && reason == "" {
		return model.CRMDeal{}, model.ErrLostReasonRequired
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMDeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := dealForEdit(ctx, tx, request.TeamID, request.CallerID, request.DealID); err != nil {
		return model.CRMDeal{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crm_deals SET stage = ?, lost_reason = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
		request.Stage, reasonFor(request.Stage, reason), FormatTime(d.currentTime().UTC()), request.DealID, request.TeamID); err != nil {
		return model.CRMDeal{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMDeal{}, err
	}
	return d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: request.TeamID, CallerID: request.CallerID, DealID: request.DealID})
}

func reasonFor(stage, reason string) string {
	if stage == model.DealStageLost {
		return reason
	}
	return ""
}

func (d *DB) CreateCRMActivity(ctx context.Context, request appmodel.CRMActivityCreateRequest) (model.CRMActivity, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CRMActivity{}, ErrNotFound
	}
	if request.ClientID <= 0 && request.DealID <= 0 {
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
		`INSERT INTO crm_activities (team_id, client_id, deal_id, kind, body, result, occurred_at, author_id, created_at)
		 VALUES (?, NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?, ?, ?) RETURNING id`,
		request.TeamID, clientID, request.DealID, request.Kind, strings.TrimSpace(request.Body),
		strings.TrimSpace(request.Result), FormatTime(occurred.UTC()), request.CallerID, FormatTime(now)).Scan(&id)
	if err != nil {
		return model.CRMActivity{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMActivity{}, err
	}
	return model.CRMActivity{ID: id, TeamID: request.TeamID, ClientID: clientID, DealID: request.DealID,
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
	q := `SELECT id, COALESCE(client_id, 0), COALESCE(deal_id, 0), kind, body, result, occurred_at, author_id, created_at
	        FROM crm_activities WHERE team_id = ?`
	args := []any{query.TeamID}
	if query.DealID > 0 {
		q += ` AND deal_id = ?`
		args = append(args, query.DealID)
	} else if query.ClientID > 0 {
		q += ` AND client_id = ?`
		args = append(args, query.ClientID)
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
		if err := rows.Scan(&a.ID, &a.ClientID, &a.DealID, &a.Kind, &a.Body, &a.Result, &occurred, &a.AuthorID, &created); err != nil {
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
