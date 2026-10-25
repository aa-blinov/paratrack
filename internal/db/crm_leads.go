package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Leads (CRM-14). A lead is an enquiry before it is a client and a deal. Any member records
// and edits a lead and logs its activities. Its owner or a manager moves its status and
// converts it, the rule a deal follows, and only a manager hands a lead to someone else. A
// converted or lost lead does not convert again. An enquiry from the website comes in with
// no owner and no member behind it.

const leadColumns = `id, company, contact_name, phone, email, source, message, owner_id, status, lost_reason,
	next_step_at, COALESCE(converted_deal_id, 0), COALESCE(converted_client_id, 0), created_by, created_at, updated_at`

func scanCRMLead(teamID int64, row dealScanner) (model.CRMLead, error) {
	l := model.CRMLead{TeamID: teamID}
	var created, updated string
	if err := row.Scan(&l.ID, &l.Company, &l.ContactName, &l.Phone, &l.Email, &l.Source, &l.Message, &l.OwnerID,
		&l.Status, &l.LostReason, &l.NextStepAt, &l.ConvertedDealID, &l.ConvertedClientID, &l.CreatedBy, &created, &updated); err != nil {
		return model.CRMLead{}, err
	}
	var err error
	if l.CreatedAt, err = ParseTime(created); err != nil {
		return model.CRMLead{}, err
	}
	if l.UpdatedAt, err = ParseTime(updated); err != nil {
		return model.CRMLead{}, err
	}
	return l, nil
}

// loadLead reads one lead of the workspace. A lead of another workspace is not found.
func loadLead(ctx context.Context, q queryRower, teamID, leadID int64) (model.CRMLead, error) {
	lead, err := scanCRMLead(teamID, q.QueryRowContext(ctx,
		`SELECT `+leadColumns+` FROM crm_leads WHERE id = ? AND team_id = ?`, leadID, teamID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.CRMLead{}, ErrNotFound
	}
	return lead, err
}

// leadForStatus loads a lead under the workspace lock and checks the caller may change its
// status: its owner or a manager. A member who is not the owner is refused.
func leadForStatus(ctx context.Context, tx *Tx, teamID, callerID, leadID int64) (model.CRMLead, error) {
	_, role, err := lockTeamRole(ctx, tx, teamID, callerID)
	if err != nil {
		return model.CRMLead{}, err
	}
	lead, err := loadLead(ctx, tx, teamID, leadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if !role.CanManage() && lead.OwnerID != callerID {
		return model.CRMLead{}, model.ErrForbidden
	}
	return lead, nil
}

// leadInTeam confirms a lead belongs to the workspace, for an activity logged on it.
func leadInTeam(ctx context.Context, q queryRower, teamID, leadID int64) error {
	if leadID == 0 {
		return nil
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM crm_leads WHERE id = ? AND team_id = ?`, leadID, teamID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// memberInTeam confirms a user is a member of the workspace, so a lead is never handed to
// someone outside it. A user who is not a member is a field error on the owner.
func memberInTeam(ctx context.Context, q queryRower, teamID, userID int64) error {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.FieldError{Field: "owner_id"}
	}
	return err
}

// leadName is the name the client takes: the company, else the contact, else the way to
// reach the lead.
func leadName(lead model.CRMLead) string {
	for _, value := range []string{lead.Company, lead.ContactName, lead.Email, lead.Phone} {
		if value != "" {
			return value
		}
	}
	return ""
}

func (d *DB) CreateCRMLead(ctx context.Context, request appmodel.CRMLeadCreateRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CRMLead{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMLead{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return model.CRMLead{}, err
	}
	owner := request.OwnerID
	if owner == 0 {
		owner = request.CallerID
	}
	if owner != request.CallerID && !role.CanManage() {
		return model.CRMLead{}, model.ErrForbidden
	}
	if err := memberInTeam(ctx, tx, request.TeamID, owner); err != nil {
		return model.CRMLead{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO crm_leads (team_id, company, contact_name, phone, email, source, message, owner_id, status,
		   next_step_at, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'new', ?, ?, ?, ?) RETURNING id`,
		request.TeamID, request.Company, request.ContactName, request.Phone, request.Email, request.Source,
		request.Message, owner, request.NextStepAt, request.CallerID, now, now).Scan(&id)
	if err != nil {
		return model.CRMLead{}, err
	}
	lead, err := loadLead(ctx, tx, request.TeamID, id)
	if err != nil {
		return model.CRMLead{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMLead{}, err
	}
	return lead, nil
}

func (d *DB) ListCRMLeads(ctx context.Context, query appmodel.CRMLeadListQuery) ([]model.CRMLead, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return nil, err
	}
	if query.Status != "" && !model.ValidLeadStatus(query.Status) {
		return nil, appmodel.FieldError{Field: "status"}
	}
	q := `SELECT ` + leadColumns + ` FROM crm_leads WHERE team_id = ?`
	args := []any{query.TeamID}
	if query.Status != "" {
		q += ` AND status = ?`
		args = append(args, query.Status)
	}
	if query.OwnerID > 0 {
		q += ` AND owner_id = ?`
		args = append(args, query.OwnerID)
	}
	q += ` ORDER BY id DESC`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.CRMLead, 0)
	for rows.Next() {
		lead, err := scanCRMLead(query.TeamID, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lead)
	}
	return out, rows.Err()
}

// UpdateCRMLead changes the fields that are given. Any member may edit a lead's details;
// only a manager sets the owner. The lead still needs a company, a contact, a phone or an
// email after the change, checked against the stored fields.
func (d *DB) UpdateCRMLead(ctx context.Context, request appmodel.CRMLeadUpdateRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.LeadID <= 0 {
		return model.CRMLead{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMLead{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return model.CRMLead{}, err
	}
	lead, err := loadLead(ctx, tx, request.TeamID, request.LeadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if request.OwnerID != nil && *request.OwnerID != lead.OwnerID {
		if !role.CanManage() {
			return model.CRMLead{}, model.ErrForbidden
		}
		if *request.OwnerID != 0 {
			if err := memberInTeam(ctx, tx, request.TeamID, *request.OwnerID); err != nil {
				return model.CRMLead{}, err
			}
		}
		lead.OwnerID = *request.OwnerID
	}
	setText := func(target *string, value *string) {
		if value != nil {
			*target = strings.TrimSpace(*value)
		}
	}
	setText(&lead.Company, request.Company)
	setText(&lead.ContactName, request.ContactName)
	setText(&lead.Phone, request.Phone)
	setText(&lead.Email, request.Email)
	setText(&lead.Source, request.Source)
	setText(&lead.Message, request.Message)
	setText(&lead.NextStepAt, request.NextStepAt)
	if leadName(lead) == "" {
		return model.CRMLead{}, appmodel.FieldError{Field: "contact"}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crm_leads SET company = ?, contact_name = ?, phone = ?, email = ?, source = ?, message = ?,
		   next_step_at = ?, owner_id = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
		lead.Company, lead.ContactName, lead.Phone, lead.Email, lead.Source, lead.Message, lead.NextStepAt,
		lead.OwnerID, FormatTime(d.currentTime().UTC()), request.LeadID, request.TeamID); err != nil {
		return model.CRMLead{}, err
	}
	updated, err := loadLead(ctx, tx, request.TeamID, request.LeadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMLead{}, err
	}
	return updated, nil
}

// MoveCRMLeadStatus sets a lead's status. A lost lead needs a reason, and leaving lost clears
// it. A converted lead is closed: its status does not change, and converted is only set by
// ConvertCRMLead.
func (d *DB) MoveCRMLeadStatus(ctx context.Context, request appmodel.CRMLeadStatusRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.LeadID <= 0 {
		return model.CRMLead{}, ErrNotFound
	}
	reason := strings.TrimSpace(request.LostReason)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMLead{}, err
	}
	defer func() { _ = tx.Rollback() }()
	lead, err := leadForStatus(ctx, tx, request.TeamID, request.CallerID, request.LeadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if lead.Status == model.LeadStatusConverted || request.Status == model.LeadStatusConverted || !model.ValidLeadStatus(request.Status) {
		return model.CRMLead{}, appmodel.FieldError{Field: "status"}
	}
	lostReason := ""
	if request.Status == model.LeadStatusLost {
		if reason == "" {
			return model.CRMLead{}, model.ErrLostReasonRequired
		}
		lostReason = reason
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crm_leads SET status = ?, lost_reason = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
		request.Status, lostReason, FormatTime(d.currentTime().UTC()), request.LeadID, request.TeamID); err != nil {
		return model.CRMLead{}, err
	}
	updated, err := loadLead(ctx, tx, request.TeamID, request.LeadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMLead{}, err
	}
	return updated, nil
}

// ConvertCRMLead makes a client from an open lead, a deal in the workspace's first open
// stage and, when the lead names a contact, a contact of that client. The deal takes the
// lead's next step and the lead's message as its first note. Everything is one transaction
// under the workspace lock, and the lead keeps the ids it made. A client name the workspace
// already has is a field error, and nothing is written.
func (d *DB) ConvertCRMLead(ctx context.Context, request appmodel.CRMLeadConvertRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.LeadID <= 0 {
		return model.CRMLead{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CRMLead{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// A conversion creates a client, and only a manager may create a client (the clients
	// rule in CreateClient). The owner of the lead is not enough.
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.CRMLead{}, err
	}
	lead, err := leadForStatus(ctx, tx, request.TeamID, request.CallerID, request.LeadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if lead.Status == model.LeadStatusConverted || lead.Status == model.LeadStatusLost {
		return model.CRMLead{}, appmodel.FieldError{Field: "status"}
	}
	name := leadName(lead)
	if name == "" {
		return model.CRMLead{}, appmodel.FieldError{Field: "contact"}
	}
	now := FormatTime(d.currentTime().UTC())
	if err := ensureCRMStages(ctx, tx, request.TeamID, now); err != nil {
		return model.CRMLead{}, err
	}
	stages, err := crmStagesOf(ctx, tx, request.TeamID)
	if err != nil {
		return model.CRMLead{}, err
	}
	stage := firstOpenStageKey(stages)
	if stage == "" {
		return model.CRMLead{}, model.ErrInvalidDealStage
	}

	var clientID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at)
		 VALUES (?, ?, ?, '', '', '', 0, ?, ?) RETURNING id`,
		request.TeamID, name, lead.Email, now, now).Scan(&clientID)
	if err != nil {
		if isUniqueViolation(err) {
			return model.CRMLead{}, appmodel.FieldError{Field: "client name"}
		}
		return model.CRMLead{}, err
	}
	var contactID int64
	if lead.ContactName != "" {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO crm_contacts (team_id, client_id, name, role, email, phone, created_by, created_at)
			 VALUES (?, ?, ?, '', ?, ?, ?, ?) RETURNING id`,
			request.TeamID, clientID, lead.ContactName, lead.Email, lead.Phone, request.CallerID, now).Scan(&contactID); err != nil {
			return model.CRMLead{}, err
		}
	}
	owner := lead.OwnerID
	if owner == 0 {
		owner = request.CallerID
	}
	var dealID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO crm_deals (team_id, title, stage, client_id, contact_id, owner_id, amount_cents, currency,
		   probability, close_date, next_step_at, lost_reason, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, NULLIF(?, 0), ?, 0, (SELECT currency FROM teams WHERE id = ?), 0, '', ?, '', ?, ?, ?) RETURNING id`,
		request.TeamID, name, stage, clientID, contactID, owner, request.TeamID, lead.NextStepAt,
		request.CallerID, now, now).Scan(&dealID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if lead.Message != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO crm_activities (team_id, client_id, deal_id, kind, body, result, occurred_at, author_id, created_at)
			 VALUES (?, ?, ?, 'note', ?, '', ?, ?, ?)`,
			request.TeamID, clientID, dealID, lead.Message, now, request.CallerID, now); err != nil {
			return model.CRMLead{}, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crm_leads SET status = 'converted', lost_reason = '', converted_client_id = ?, converted_deal_id = ?,
		   updated_at = ? WHERE id = ? AND team_id = ?`,
		clientID, dealID, now, request.LeadID, request.TeamID); err != nil {
		return model.CRMLead{}, err
	}
	converted, err := loadLead(ctx, tx, request.TeamID, request.LeadID)
	if err != nil {
		return model.CRMLead{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CRMLead{}, err
	}
	return converted, nil
}

// CreateCRMIntakeKey makes the workspace's intake key and returns the raw key once. Only its
// SHA-256 is stored. The new key replaces the old one in the same transaction, so a workspace
// has one active key.
func (d *DB) CreateCRMIntakeKey(ctx context.Context, request appmodel.CRMIntakeKeyRequest) (string, model.CRMIntakeKey, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return "", model.CRMIntakeKey{}, ErrNotFound
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", model.CRMIntakeKey{}, err
	}
	raw := "lk_" + hex.EncodeToString(b[:])
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", model.CRMIntakeKey{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return "", model.CRMIntakeKey{}, err
	}
	now := d.currentTime().UTC()
	if _, err := tx.ExecContext(ctx, `DELETE FROM crm_intake_keys WHERE team_id = ?`, request.TeamID); err != nil {
		return "", model.CRMIntakeKey{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO crm_intake_keys (key_hash, team_id, created_by, created_at) VALUES (?, ?, ?, ?)`,
		HashAPIToken(raw), request.TeamID, request.CallerID, FormatTime(now)); err != nil {
		return "", model.CRMIntakeKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", model.CRMIntakeKey{}, err
	}
	return raw, model.CRMIntakeKey{TeamID: request.TeamID, CreatedBy: request.CallerID, CreatedAt: now}, nil
}

// RevokeCRMIntakeKey removes the workspace's intake key. A workspace with none is not found.
func (d *DB) RevokeCRMIntakeKey(ctx context.Context, request appmodel.CRMIntakeKeyRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM crm_intake_keys WHERE team_id = ?`, request.TeamID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// CRMIntakeKeyStatus reads the workspace's intake key without its secret. A workspace with
// no key returns a zero CreatedAt.
func (d *DB) CRMIntakeKeyStatus(ctx context.Context, query appmodel.CRMIntakeKeyQuery) (model.CRMIntakeKey, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return model.CRMIntakeKey{}, ErrNotFound
	}
	if _, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID); err != nil {
		return model.CRMIntakeKey{}, err
	}
	key := model.CRMIntakeKey{TeamID: query.TeamID}
	var created string
	err := d.sql.QueryRowContext(ctx,
		`SELECT created_by, created_at FROM crm_intake_keys WHERE team_id = ?`, query.TeamID).Scan(&key.CreatedBy, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return key, nil
	}
	if err != nil {
		return model.CRMIntakeKey{}, err
	}
	if key.CreatedAt, err = ParseTime(created); err != nil {
		return model.CRMIntakeKey{}, err
	}
	return key, nil
}

// IntakeKeyTeam returns the workspace an intake key belongs to. A missing, wrong or revoked key
// is model.ErrIntakeKeyInvalid. The key is compared as its hash.
func (d *DB) IntakeKeyTeam(ctx context.Context, raw string) (int64, error) {
	if raw == "" {
		return 0, model.ErrIntakeKeyInvalid
	}
	var teamID int64
	err := d.sql.QueryRowContext(ctx, `SELECT team_id FROM crm_intake_keys WHERE key_hash = ?`, HashAPIToken(raw)).Scan(&teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, model.ErrIntakeKeyInvalid
	}
	return teamID, err
}

// CreateIntakeLead records an enquiry that came in with a valid intake key. It has no owner and
// starts as new. No member stands behind the request, so there is no workspace lock to take.
func (d *DB) CreateIntakeLead(ctx context.Context, request appmodel.CRMLeadIntakeRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 {
		return model.CRMLead{}, ErrNotFound
	}
	now := FormatTime(d.currentTime().UTC())
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO crm_leads (team_id, company, contact_name, phone, email, source, message, owner_id, status,
		   created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'new', 0, ?, ?) RETURNING id`,
		request.TeamID, request.Company, request.ContactName, request.Phone, request.Email, request.Source,
		request.Message, now, now).Scan(&id)
	if err != nil {
		return model.CRMLead{}, err
	}
	return loadLead(ctx, d.sql, request.TeamID, id)
}
