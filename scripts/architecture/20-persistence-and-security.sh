# Sourced by scripts/check-architecture.sh; do not execute directly.
workspace_guard_checks=

# Raw connection access exists only to set up integration tests. Runtime code
# must use the typed persistence operations on DB.
if grep -R -n --include='*.go' --exclude='*_test.go' -E '\.TestSQL\(' internal; then
	echo "architecture check: raw database connection access is restricted to tests" >&2
	exit 1
fi

# Database startup performs migrations and must remain tied to the caller's
# cancellable lifecycle. Do not silently replace a missing context with a
# process-wide context.
db_open_context=$(sed -n '/^func OpenConfiguredContext(/,/^}/p' internal/db/db.go)
if ! printf '%s\n' "$db_open_context" | grep -Fq 'if ctx == nil' ||
	! printf '%s\n' "$db_open_context" | grep -Fq 'return nil, ErrNilDatabaseContext' ||
	grep -Fq 'ctx = context.Background()' internal/db/db.go; then
	echo "architecture check: database startup must reject nil context and preserve cancellation" >&2
	exit 1
fi
# Workspace membership policy must not absorb compensation data. Roster pay
# settings are exposed through the payroll workflow, which owns their writes.
if grep -R -Eq 'MemberPayrollSettings|ListMemberPayrollSettings' internal/teams --include='*.go'; then
	echo "architecture check: member compensation belongs to the payroll workflow, not teams" >&2
	exit 1
fi

# Workspace collection reads must never turn a missing team ID into an
# unscoped cross-workspace query, even when a caller bypasses its workflow.
for scoped_read in \
	'internal/db/activities.go:ListActivities' \
	'internal/db/goals.go:ListGoals' \
	'internal/db/saved_reports.go:ListSavedReports'; do
	source=${scoped_read%%:*}
	method=${scoped_read#*:}
	workspace_guard_checks="${workspace_guard_checks}${source}:${method}
"
	operation=$(sed -n "/^func (.*) $method(/,/^}/p" "$source")
	if ! printf '%s\n' "$operation" | grep -Eq 'teamID <= 0' ||
		printf '%s\n' "$operation" | grep -Eq 'if teamID > 0'; then
		echo "architecture check: $source $method must require a positive workspace ID" >&2
		exit 1
	fi
done

# Package-private payment-link mutation is intentionally unavailable as a
# service method; keep its scope guard explicit. Exported workflow methods
# are discovered automatically from the core package list below.
require_positive_workspace_guard() {
	source=$1
	method=$2
	workspace_guard_checks="${workspace_guard_checks}${source}:${method}
"
}
require_positive_workspace_guard internal/invoicing/stripe.go setPaymentLink
for method in Create Delete List RecentDeliveries; do
	require_positive_workspace_guard internal/webhooks/service.go "$method"
done

# The AST checker discovers workspace-scoped workflow methods, requires a
# Close call for each QueryContext result, pairs each BeginTx result with its
# own deferred Rollback, and rejects contextless database operations.

# Legacy tag deletion may only address the legacy unscoped catalog; passing
# teamID zero must never delete a workspace-owned tag by numeric ID.
if grep -R -q --include='*.go' --exclude='*_test.go' -E '^func \(d \*DB\) (CreateLegacySession|CreateActivity|GetOrCreateActivity|GetActivityByName|CreateTag|DeleteTag|AttachTag|DetachTag|SetTagsForSession|ListTagsForSession)\(' internal/db; then
	echo "architecture check: unscoped compatibility operations must stay package-private" >&2
	exit 1
fi
legacy_tag_delete=$(sed -n '/^func (d \*DB) deleteTag(/,/^}/p' internal/db/tags.go)
if ! printf '%s\n' "$legacy_tag_delete" | grep -Fq 'team_id IS NULL'; then
	echo "architecture check: legacy tag deletion must restrict writes to unscoped tags" >&2
	exit 1
fi

# Timesheet cell replacement must retain a concrete actor scope even for
# legacy unscoped records, where no workspace membership row can provide it.
timesheet_cell_write=$(sed -n '/^func (d \*DB) UpsertDayTotal(/,/^}/p' internal/db/timesheet.go)
if ! printf '%s\n' "$timesheet_cell_write" | grep -Fq 'actor := actorID(ctx)' ||
	! printf '%s\n' "$timesheet_cell_write" | grep -Fq 'if actor <= 0' ||
	! printf '%s\n' "$timesheet_cell_write" | grep -Fq 'del += ` AND user_id = ?`'; then
	echo "architecture check: timesheet cell replacement must require and apply an actor scope" >&2
	exit 1
fi

# Session transition transactions must never interpret teamID zero as an
# unfiltered workspace query.
if ! grep -Fq 'requestctx.WithTeamID(ctx, team.ID)' internal/web/middleware.go; then
	echo "architecture check: authenticated request context must carry the verified workspace ID" >&2
	exit 1
fi
if ! grep -Fq 'withSecurePublicOrigin(handler, s.config.PublicURL)' internal/web/routes.go; then
	echo "architecture check: canonical HTTPS origin must inform secure cookie and HSTS policy" >&2
	exit 1
fi
membership_guard=$(sed -n '/^func requireTeamMembership(/,/^}/p' internal/db/scope.go)
if ! printf '%s\n' "$membership_guard" | grep -Fq 'if teamID <= 0' ||
	! printf '%s\n' "$membership_guard" | grep -Fq 'return model.ErrForbidden'; then
	echo "architecture check: session transitions must reject a missing workspace scope" >&2
	exit 1
fi
session_edit_transition=$(sed -n '/^func (d \*DB) UpdateSessionFields(/,/^}/p' internal/db/sessions.go)
if ! printf '%s\n' "$session_edit_transition" | grep -Fq 'lockedSession.wasOpen && update.EndAt != nil' ||
	! printf '%s\n' "$session_edit_transition" | grep -Fq 'recordWebhookEventTx'; then
	echo "architecture check: editing an active session closed must record its stop event transactionally" >&2
	exit 1
fi
session_reopen_transition=$(sed -n '/^func (d \*DB) ReopenSession(/,/^}/p' internal/db/session_transitions.go)
if ! printf '%s\n' "$session_reopen_transition" | grep -Fq 's.EndAt.Equal(request.ExpectedEndAt)' ||
	! sed -n '/^func (s \*Service) Reopen(/,/^}/p' internal/tracking/service.go | grep -Fq 'request.ExpectedEndAt = *current.EndAt'; then
	echo "architecture check: session reopening must revalidate the end time under the persistence row lock" >&2
	exit 1
fi
for method in MarkInvoicePaidOnce MarkInvoicePaidFromStripe; do
	paid_transition=$(sed -n "/^func (d \\*DB) $method(/,/^}/p" internal/db/invoice_payments.go)
	if ! printf '%s\n' "$paid_transition" | grep -Fq "UPDATE invoices SET status = 'paid'" ||
		! printf '%s\n' "$paid_transition" | grep -Fq 'recordWebhookEventTx'; then
		echo "architecture check: invoice paid transition $method must record its event transactionally" >&2
		exit 1
	fi
done
stripe_paid_transition=$(sed -n '/^func (d \*DB) MarkInvoicePaidFromStripe(/,/^}/p' internal/db/invoice_payments.go)
if ! printf '%s\n' "$stripe_paid_transition" | grep -Fq 'FROM invoice_stripe_sessions WHERE stripe_session_id = ?' ||
	! printf '%s\n' "$stripe_paid_transition" | grep -Fq 'sessionTeam != teamID || sessionInvoice != id || active != 1'; then
	echo "architecture check: Stripe payment must match a checkout session stored for that invoice and workspace" >&2
	exit 1
fi
set_invoice_payment_link=$(sed -n '/^func (d \*DB) SetPaymentURL(/,/^}/p' internal/db/invoice_payments.go)
if ! printf '%s\n' "$set_invoice_payment_link" | grep -Fq 'revision != expectedRevision' ||
	! printf '%s\n' "$set_invoice_payment_link" | grep -Fq 'SELECT status, number, revision'; then
	echo "architecture check: checkout creation must reject an invoice changed during the provider request" >&2
	exit 1
fi
invoice_rebuild=$(sed -n '/^func (d \*DB) RebuildInvoice(/,/^}/p' internal/db/invoice_transitions.go)
if ! printf '%s\n' "$invoice_rebuild" | grep -Fq "payment_url = '', stripe_session_id = ''" ||
	! printf '%s\n' "$invoice_rebuild" | grep -Fq 'UPDATE invoice_stripe_sessions SET active = 0 WHERE team_id = ? AND invoice_id = ?'; then
	echo "architecture check: rebuilding an invoice must invalidate any checkout created for its old amount" >&2
	exit 1
fi
stripe_link_creation=$(sed -n '/^func (s \*Service) CreateStripePaymentLink(/,/^}/p' internal/invoicing/stripe.go)
if ! printf '%s\n' "$stripe_link_creation" | grep -Fq 'ExpireCheckout(cleanupCtx, key, sessionID)' ||
	! printf '%s\n' "$stripe_link_creation" | grep -Fq 'postcommit.NewContextWithTimeout(ctx, 5*time.Second)'; then
	echo "architecture check: failed checkout persistence must attempt bounded provider cleanup after request cancellation" >&2
	exit 1
fi
if grep -Eq '^func \(d \*DB\) (MarkInvoiceSent|SetInvoiceByPerson|SetInvoiceProject)\(' internal/db/invoice_transitions.go; then
	echo "architecture check: invoice state and draft-only settings must use the guarded workflow transitions" >&2
	exit 1
fi
if grep -R -q --include='*.go' -E '^func \(d \*DB\) (DeleteTeam|MarkInvoicePaid)\(' internal/db; then
	echo "architecture check: team deletion and paid transitions must use their result-bearing workflows" >&2
	exit 1
fi
if grep -R -q 'UpdateInvoiceStatus' internal/db --include='*.go'; then
	echo "architecture check: generic invoice status writes must use event-aware transitions" >&2
	exit 1
fi
if grep -R -q --include='*.go' -E '^func \(d \*DB\) (CloseMissingExternalTasks|UpsertExternalTask)\(' internal/db; then
	echo "architecture check: integration snapshots must use the scoped, transactional SyncExternalTasks operation" >&2
	exit 1
fi
# Concurrent provider fetches may finish out of order. Reserve a generation
# before fetching and reject any snapshot whose generation is no longer current.
sync_reservation=$(sed -n '/^func (d \*DB) BeginIntegrationSync(/,/^}/p' internal/db/integrations.go)
sync_commit=$(sed -n '/^func (d \*DB) SyncExternalTasks(/,/^}/p' internal/db/integration_sync.go)
if ! printf '%s\n' "$sync_reservation" | grep -Fq 'sync_generation = sync_generation + 1' ||
	! printf '%s\n' "$sync_commit" | grep -Fq 'request.Generation != currentGeneration' ||
	! printf '%s\n' "$sync_commit" | grep -Fq 'ErrIntegrationSyncSuperseded'; then
	echo "architecture check: integration snapshot writes must reject stale provider fetches by generation" >&2
	exit 1
fi
for method in GetIntegrationSummary ListExternalTasks; do
	integration_lookup=$(sed -n "/^func (d \\*DB) $method(/,/^}/p" internal/db/integrations.go)
	if ! printf '%s\n' "$integration_lookup" | grep -Fq 'query appmodel.IntegrationLookupQuery' ||
		! printf '%s\n' "$integration_lookup" | grep -Fq 'query.TeamID <= 0' ||
		! printf '%s\n' "$integration_lookup" | grep -Fq 'query.IntegrationID <= 0'; then
		echo "architecture check: integration read $method must validate workspace and integration scope" >&2
		exit 1
	fi
done
external_task_lookup=$(sed -n '/^func (d \*DB) GetExternalTask(/,/^}/p' internal/db/integrations.go)
if ! printf '%s\n' "$external_task_lookup" | grep -Fq 'query appmodel.ExternalTaskLookupQuery' ||
	! printf '%s\n' "$external_task_lookup" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$external_task_lookup" | grep -Fq 'query.TaskID <= 0'; then
	echo "architecture check: imported task lookup must validate workspace and task scope" >&2
	exit 1
fi
if grep -R -q --include='*.go' --exclude='*_test.go' -E '\.CreateSession\(' internal; then
	echo "architecture check: production session creation must use the explicit tracking workflow operations" >&2
	exit 1
fi
legacy_session_create=$(sed -n '/^func (d \*DB) createLegacySession(/,/^}/p' internal/db/sessions.go)
activity_lock=$(sed -n '/^func lockActivityForSession(/,/^}/p' internal/db/sessions.go)
if ! printf '%s\n' "$legacy_session_create" | grep -Fq 'lockActivityForSession(ctx, tx, teamID, activityID)' ||
	! printf '%s\n' "$legacy_session_create" | grep -Fq 'const teamID int64 = 0' ||
	! printf '%s\n' "$activity_lock" | grep -Fq 'team_id IS NULL'; then
	echo "architecture check: legacy session creation must lock only an unscoped activity" >&2
	exit 1
fi

# Older binaries may write activities during a rolling deploy before setting
# name_key. Workspace resolution must reuse and repair those rows.
member_activity_resolver=$(sed -n '/^func (d \*DB) GetOrCreateActivityForMember(/,/^}/p' internal/db/activities.go)
activity_name_lookup=$(sed -n '/^func (d \*DB) getActivityByName(/,/^}/p' internal/db/activities.go)
if ! printf '%s\n' "$member_activity_resolver" | grep -Fq 'name_key IS NULL AND name = ?' ||
	! printf '%s\n' "$member_activity_resolver" | grep -Fq 'SET name_key = ? WHERE id = ? AND name_key IS NULL' ||
	! printf '%s\n' "$activity_name_lookup" | grep -Fq 'name_key IS NULL AND name = ?'; then
	echo "architecture check: activity lookup must tolerate and repair missing legacy name keys" >&2
	exit 1
fi

# A scoped API token is only valid for a current workspace member. Recheck
# under the workspace lock in persistence so membership removal cannot race
# token creation after the workflow's initial membership read.
scoped_token_create=$(sed -n '/^func (d \*DB) CreateAPIToken(/,/^}/p' internal/db/tokens.go)
if ! printf '%s\n' "$scoped_token_create" | grep -Fq 'lockCurrentTeamMember(ctx, tx, o.TeamID, request.UserID)'; then
	echo "architecture check: scoped API token creation must recheck membership under lock" >&2
	exit 1
fi
if ! printf '%s\n' "$scoped_token_create" | grep -Fq 'if !o.ExpiresAt.After(now)' ||
	! printf '%s\n' "$scoped_token_create" | awk '
	/lockCurrentTeamMember\(ctx, tx, o.TeamID, request.UserID\)/ { lock = NR }
	/err = prepare\(\)/ && lock && !prepare { prepare = NR }
	/err = insert\(tx\)/ && prepare && !insert { insert = NR }
	END { exit !(lock && prepare && insert && lock < prepare && prepare < insert) }
'; then
	echo "architecture check: scoped API token expiry must be revalidated after the membership lock and before insert" >&2
	exit 1
fi

# Enqueuing invoice mail is a manager-only state transition. Its DB write must
# recheck the role before locking the invoice row, matching other invoice
# transitions' team -> invoice lock order.
enqueue_invoice_email=$(sed -n '/^func (d \*DB) EnqueueInvoiceEmail(/,/^}/p' internal/db/mailqueue.go)
if ! printf '%s\n' "$enqueue_invoice_email" | grep -Fq 'lockTeamManager(ctx, tx, teamID, actorID(ctx))'; then
	echo "architecture check: invoice email enqueue must recheck manager role in persistence" >&2
	exit 1
fi
manager_lock_line=$(printf '%s\n' "$enqueue_invoice_email" | grep -nF 'lockTeamManager(ctx, tx, teamID, actorID(ctx))' | cut -d: -f1)
invoice_lock_line=$(printf '%s\n' "$enqueue_invoice_email" | grep -nF 'SELECT status, revision FROM invoices' | cut -d: -f1)
if [ -z "$invoice_lock_line" ] || [ "$manager_lock_line" -ge "$invoice_lock_line" ]; then
	echo "architecture check: invoice email enqueue must lock manager membership before invoice" >&2
	exit 1
fi

# Invoice line stamping accepts session IDs from a caller. The database write
# must verify those IDs still belong to the invoice workspace.
invoice_line_writer=$(sed -n '/^func insertLines(/,/^}/p' internal/db/invoice_transitions.go)
if ! printf '%s\n' "$invoice_line_writer" | grep -Fq 'teamID, invID int64' ||
	! printf '%s\n' "$invoice_line_writer" | grep -Fq 'AND team_id = ?'; then
	echo "architecture check: invoice session stamping must apply its workspace scope" >&2
	exit 1
fi
invoice_line_builder=$(sed -n '/^func buildInvoiceLines(/,/^}/p' internal/db/invoice_lines.go)
if ! printf '%s\n' "$invoice_line_builder" | grep -Fq 'teamID <= 0' ||
	! printf '%s\n' "$invoice_line_builder" | grep -Fq 's.team_id = ?' ||
	! printf '%s\n' "$invoice_line_builder" | grep -Fq 'a.team_id = ? AND p.team_id = ?'; then
	echo "architecture check: invoice line aggregation must reject missing scope and constrain every tenant table" >&2
	exit 1
fi
if ! printf '%s\n' "$invoice_line_builder" | grep -Fq 'if options.lockRows' ||
	! printf '%s\n' "$invoice_line_builder" | grep -Fq 'ORDER BY s.id FOR UPDATE OF s, a, p' ||
	! sed -n '/^func (d \*DB) CreateInvoiceDraft(/,/^}/p' internal/db/invoices.go | grep -Fq 'byPerson: byPerson, lockRows: true' ||
	! sed -n '/^func (d \*DB) RebuildInvoice(/,/^}/p' internal/db/invoice_transitions.go | grep -Fq 'lockRows: true'; then
	echo "architecture check: invoice creation and rebuild must lock billable source rows before snapshotting them" >&2
	exit 1
fi
unbilled_builder=$(sed -n '/^func (d \*DB) Unbilled(/,/^}/p' internal/db/invoice_lines.go)
if ! printf '%s\n' "$unbilled_builder" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$unbilled_builder" | grep -Fq 'a.team_id = ?' ||
	! printf '%s\n' "$unbilled_builder" | grep -Fq 'p.team_id = ?'; then
	echo "architecture check: unbilled invoice totals must require scope and join tenant-owned activities and projects" >&2
	exit 1
fi
payroll_line_builder=$(sed -n '/^func buildPayrollLines(/,/^}/p' internal/db/payroll_calculation.go)
if ! printf '%s\n' "$payroll_line_builder" | grep -Fq 'teamID <= 0' ||
	! printf '%s\n' "$payroll_line_builder" | grep -Fq 's.team_id = ?'; then
	echo "architecture check: payroll aggregation must reject missing scope and constrain sessions to the payroll workspace" >&2
	exit 1
fi
for method in ProjectSpans ProjectActivityCounts ProjectSessions ProjectTrackedTotal; do
	project_query=$(sed -n "/^func (d \*DB) $method(/,/^}/p" internal/db/project_activity_queries.go)
	case "$method" in
		ProjectSpans) scope_check='query.TeamID <= 0 || query.From.IsZero() || query.Through.Before(query.From)' ;;
		ProjectSessions) scope_check='query.TeamID <= 0 || query.ProjectID <= 0' ;;
		ProjectTrackedTotal) scope_check='query.TeamID <= 0 || query.ProjectID <= 0' ;;
		*) scope_check='teamID <= 0' ;;
	esac
	if ! printf '%s\n' "$project_query" | grep -Fq "$scope_check"; then
		echo "architecture check: project usage query $method must validate scope and same-workspace joins" >&2
		exit 1
	fi
	if [ "$method" = ProjectActivityCounts ]; then
		if ! printf '%s\n' "$project_query" | grep -Fq 'p.team_id = a.team_id'; then
			echo "architecture check: project activity counts must join projects within the activity workspace" >&2
			exit 1
		fi
	elif ! printf '%s\n' "$project_query" | grep -Fq 'a.team_id = s.team_id' ||
		! printf '%s\n' "$project_query" | grep -Fq 'p.team_id = s.team_id'; then
		echo "architecture check: project usage query $method must join tenant rows in one workspace" >&2
		exit 1
	fi
done
session_lookup=$(sed -n '/^func (d \*DB) GetSession(/,/^}/p' internal/db/session_queries.go)
if ! printf '%s\n' "$session_lookup" | grep -Fq 'query appmodel.SessionLookupQuery' ||
	! printf '%s\n' "$session_lookup" | grep -Fq 'query.SessionID' ||
	! printf '%s\n' "$session_lookup" | grep -Fq 'query.TeamID'; then
	echo "architecture check: session reads must carry session identity and workspace scope in one query" >&2
	exit 1
fi
session_tags_lookup=$(sed -n '/^func (d \*DB) TagsForSessions(/,/^}/p' internal/db/tags.go)
if ! printf '%s\n' "$session_tags_lookup" | grep -Fq 'query appmodel.SessionTagsQuery' ||
	! printf '%s\n' "$session_tags_lookup" | grep -Fq 'query.TeamID' ||
	! printf '%s\n' "$session_tags_lookup" | grep -Fq 'query.SessionIDs'; then
	echo "architecture check: batched session tag reads must keep workspace and IDs in one query" >&2
	exit 1
fi
activity_name_lookup=$(sed -n '/^func (d \*DB) FindActivityByName(/,/^}/p' internal/db/activities.go)
if ! printf '%s\n' "$activity_name_lookup" | grep -Fq 'query appmodel.ActivityNameQuery' ||
	! printf '%s\n' "$activity_name_lookup" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$activity_name_lookup" | grep -Fq 'query.Name'; then
	echo "architecture check: activity name reads must carry and validate workspace scope" >&2
	exit 1
fi
for method in FindMembershipForUser TeamMemberRole; do
	team_membership_query=$(sed -n "/^func (d \\*DB) $method(/,/^}/p" internal/db/teams.go)
	if ! printf '%s\n' "$team_membership_query" | grep -Fq 'query appmodel.TeamMembershipQuery' ||
		! printf '%s\n' "$team_membership_query" | grep -Fq 'query.TeamID <= 0' ||
		! printf '%s\n' "$team_membership_query" | grep -Fq 'query.UserID <= 0'; then
		echo "architecture check: membership reads must validate explicit workspace and user scope ($method)" >&2
		exit 1
	fi
done
team_is_member=$(sed -n '/^func (s \*Service) IsMember(/,/^}/p' internal/teams/members.go)
team_membership_for_user=$(sed -n '/^func (s \*Service) MembershipForUser(/,/^}/p' internal/teams/teams.go)
team_directory=$(sed -n '/^type TeamDirectory interface {/,/^}/p' internal/web/dependencies.go)
if ! printf '%s\n' "$team_is_member" | grep -Fq 'appmodel.TeamMembershipQuery' ||
	! printf '%s\n' "$team_is_member" | grep -Fq 'TeamMemberRole(ctx, query)' ||
	! printf '%s\n' "$team_membership_for_user" | grep -Fq 'appmodel.TeamMembershipQuery' ||
	! printf '%s\n' "$team_membership_for_user" | grep -Fq 'FindMembershipForUser(ctx, query)' ||
	! printf '%s\n' "$team_directory" | grep -Fq 'IsMember(context.Context, appmodel.TeamMembershipQuery)' ||
	! printf '%s\n' "$team_directory" | grep -Fq 'MembershipForUser(context.Context, appmodel.TeamMembershipQuery)'; then
	echo "architecture check: membership scope must remain typed through teams workflow and HTTP directory ports" >&2
	exit 1
fi
payroll_run_lookup=$(sed -n '/^func (d \*DB) GetPayrollRunDetails(/,/^}/p' internal/db/payroll_queries.go)
if ! printf '%s\n' "$payroll_run_lookup" | grep -Fq 'query appmodel.PayrollRunLookupQuery' ||
	! printf '%s\n' "$payroll_run_lookup" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$payroll_run_lookup" | grep -Fq 'query.RunID <= 0'; then
	echo "architecture check: payroll run details must validate explicit workspace and run scope" >&2
	exit 1
fi
payroll_run=$(sed -n '/^func (d \*DB) GetPayrollRun(/,/^}/p' internal/db/payroll_queries.go)
if ! printf '%s\n' "$payroll_run" | grep -Fq 'query appmodel.PayrollRunLookupQuery' ||
	! printf '%s\n' "$payroll_run" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$payroll_run" | grep -Fq 'query.RunID <= 0'; then
	echo "architecture check: payroll run reads must validate explicit workspace and run scope" >&2
	exit 1
fi
invoice_lookup=$(sed -n '/^func (d \*DB) GetInvoiceDetails(/,/^}/p' internal/db/invoices.go)
if ! printf '%s\n' "$invoice_lookup" | grep -Fq 'query appmodel.InvoiceLookupQuery' ||
	! printf '%s\n' "$invoice_lookup" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$invoice_lookup" | grep -Fq 'query.InvoiceID <= 0'; then
	echo "architecture check: invoice details must validate explicit workspace and invoice scope" >&2
	exit 1
fi
invoice=$(sed -n '/^func (d \*DB) GetInvoice(/,/^}/p' internal/db/invoices.go)
if ! printf '%s\n' "$invoice" | grep -Fq 'query appmodel.InvoiceLookupQuery' ||
	! printf '%s\n' "$invoice" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$invoice" | grep -Fq 'query.InvoiceID <= 0'; then
	echo "architecture check: invoice reads must validate explicit workspace and invoice scope" >&2
	exit 1
fi
webhook_lookup=$(sed -n '/^func (d \*DB) GetWebhook(/,/^}/p' internal/db/webhooks.go)
if ! printf '%s\n' "$webhook_lookup" | grep -Fq 'query appmodel.WebhookLookupQuery' ||
	! printf '%s\n' "$webhook_lookup" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$webhook_lookup" | grep -Fq 'query.WebhookID <= 0'; then
	echo "architecture check: webhook reads must validate explicit workspace and endpoint scope" >&2
	exit 1
fi
invoice_overlap=$(sed -n '/^func (d \*DB) OverlappingInvoices(/,/^}/p' internal/db/invoice_transitions.go)
if ! printf '%s\n' "$invoice_overlap" | grep -Fq 'query appmodel.InvoiceOverlapQuery' ||
	! printf '%s\n' "$invoice_overlap" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$invoice_overlap" | grep -Fq 'query.ExcludeInvoiceID <= 0' ||
	! printf '%s\n' "$invoice_overlap" | grep -Fq '!query.End.After(query.Start)'; then
	echo "architecture check: invoice overlap reads must validate workspace, exclusion and period together" >&2
	exit 1
fi
unbilled_query=$(sed -n '/^func (d \*DB) Unbilled(/,/^}/p' internal/db/invoice_lines.go)
if ! printf '%s\n' "$unbilled_query" | grep -Fq 'query appmodel.UnbilledProjectQuery' ||
	! printf '%s\n' "$unbilled_query" | grep -Fq 'query.TeamID <= 0' ||
	! printf '%s\n' "$unbilled_query" | grep -Fq 'query.ProjectID != nil' ||
	! printf '%s\n' "$unbilled_query" | grep -Fq 'if query.ProjectID != nil'; then
	echo "architecture check: unbilled time reads must use explicit workspace scope and optional project filtering" >&2
	exit 1
fi
for method in ListTags ListAllTagsWithCounts; do
	tag_query=$(sed -n "/^func (d \*DB) $method(/,/^}/p" internal/db/tags.go)
	if ! printf '%s\n' "$tag_query" | grep -Fq 'teamID <= 0'; then
		echo "architecture check: tag query $method must require one workspace scope" >&2
		exit 1
	fi
	if [ "$method" = ListTags ]; then
		scope_clause='WHERE team_id = ?'
	else
		scope_clause='WHERE t.team_id = ?'
	fi
	if ! printf '%s\n' "$tag_query" | grep -Fq "$scope_clause"; then
		echo "architecture check: tag query $method must constrain rows to the requested workspace" >&2
		exit 1
	fi
done
tag_counts=$(sed -n '/^func (d \*DB) ListAllTagsWithCounts(/,/^}/p' internal/db/tags.go)
if ! printf '%s\n' "$tag_counts" | grep -Fq 's.team_id = t.team_id' ||
	! printf '%s\n' "$tag_counts" | grep -Fq 'COUNT(s.id)'; then
	echo "architecture check: tag usage counts must include sessions from the tag workspace only" >&2
	exit 1
fi
timesheet_query=$(sed -n '/^func (d \*DB) ListTimesheet(/,/^}/p' internal/db/timesheet.go)
if ! printf '%s\n' "$timesheet_query" | grep -Fq 'teamID <= 0' ||
	! printf '%s\n' "$timesheet_query" | grep -Fq 's.team_id = ?' ||
	! printf '%s\n' "$timesheet_query" | grep -Fq 'a.team_id = s.team_id'; then
	echo "architecture check: timesheet aggregation must require a workspace and same-workspace activity join" >&2
	exit 1
fi
session_select=$(sed -n '/^const sessionSelect =/,/^`/p' internal/db/session_queries.go)
if ! printf '%s\n' "$session_select" | grep -Fq 'a.team_id IS NOT DISTINCT FROM s.team_id'; then
	echo "architecture check: shared session reads must exclude cross-workspace activity relationships" >&2
	exit 1
fi
unassigned_activities=$(sed -n '/^func (d \*DB) UnassignedActivities(/,/^}/p' internal/db/project_assignments.go)
if ! printf '%s\n' "$unassigned_activities" | grep -Fq 's.team_id = a.team_id'; then
	echo "architecture check: unassigned activity totals must include only sessions from the activity workspace" >&2
	exit 1
fi
if [ "$(rg -c 'a.team_id = sessions.team_id AND p.team_id = sessions.team_id' internal/db/invoice_legacy.go)" -ne 2 ]; then
	echo "architecture check: legacy invoice stamping must require activity and project workspace ownership" >&2
	exit 1
fi

# Provider protocol tests belong to the provider adapter package. HTTP tests
# may exercise importer workflow wiring, but should not bypass it by importing
# low-level integration provider clients directly.
web_test_imports=$(go list -test -f '{{join .TestImports " "}}' "$module/internal/web")
if printf '%s\n' "$web_test_imports" | tr ' ' '\n' | grep -Fxq "$module/internal/integrations/providers"; then
	echo "architecture check: web tests must exercise integration providers through the workflow boundary" >&2
	exit 1
fi

# Runtime packages never depend on test helpers or the testing API. Check all
# Go packages, including process entrypoints outside internal/.
for package in $package_paths; do
	case "$package" in
		"$module/internal/testutil")
			;;
		*)
			check_forbidden_imports "$package" internal/testutil
			dependencies=$(package_deps "$package")
			case " $dependencies " in
				*" testing "*)
				echo "architecture check: runtime package $package must not depend on testing" >&2
				exit 1
				;;
			esac
			;;
	esac
done

# Commands are process composition roots: they may wire approved adapters,
# but feature workflows must be assembled through internal/app.
if grep -Eq 'os\.(Getenv|LookupEnv)\(' cmd/paratrack/main.go; then
	echo "architecture check: the process entrypoint must read settings through cmd/paratrack/config.go" >&2
	exit 1
fi
if ! grep -Fq 'loadCLITimezone()' cmd/paratrack/main.go ||
	! grep -Fq 'time.Now().In(location)' cmd/paratrack/main.go ||
	grep -Fq 'time.Local' internal/timeparse/timeparse.go ||
	grep -R -Fq '.Local()' internal/cli --include='*.go' --exclude='*_test.go'; then
	echo "architecture check: CLI parsing and display must use the configured process timezone" >&2
	exit 1
fi
for package in $package_paths; do
	case "$package" in "$module/cmd/"*) ;; *) continue ;; esac
	for dependency in $(package_imports "$package"); do
		case "$dependency" in
			"$module/internal/app"|"$module/internal/cliport"|"$module/internal/cli"|"$module/internal/db"|"$module/internal/mail"|"$module/internal/netclients"|"$module/internal/oidcclient"|"$module/internal/web")
				;;
			"$module/internal/"*)
				echo "architecture check: command $package must compose workflows through internal/app (found $dependency)" >&2
				exit 1
				;;
		esac
	done

done

# Progress summaries and timesheet invoice guards use batch reads. A query in
# these entity loops would reintroduce per-goal or per-invoice N+1 behavior.
goal_progress_operation=$(sed -n '/^func (d \*DB) ProgressForGoals(/,/^}/p' internal/db/goals.go)
if printf '%s\n' "$goal_progress_operation" | grep -Eq 'activityMinutesInRange|QueryRowContext'; then
	echo "architecture check: goal progress must batch activity and session reads" >&2
	exit 1
fi
timesheet_invoice_lock=$(sed -n '/^func lockTimesheetCellInvoices(/,/^}/p' internal/db/timesheet.go)
if printf '%s\n' "$timesheet_invoice_lock" | grep -q 'QueryRowContext'; then
	echo "architecture check: timesheet invoice states must be locked in one batch query" >&2
	exit 1
fi
import_operation=$(sed -n '/^func (d \*DB) ImportEntries(/,/^}/p' internal/db/imports.go)
if printf '%s\n' "$import_operation" | grep -Eq 'Query(Row)?Context|ExecContext'; then
	echo "architecture check: import persistence must delegate SQL to batch helpers" >&2
	exit 1
fi
import_transaction=$(sed -n '/^func (d \*DB) importEntries(/,/^}/p' internal/db/imports.go)
import_activities=$(sed -n '/^func (d \*DB) ensureImportActivities(/,/^}/p' internal/db/imports.go)
if [ "$(printf '%s\n' "$import_transaction" | grep -Fc 'd.currentTime()')" -ne 1 ] ||
	! printf '%s\n' "$import_transaction" | grep -Fq 'ensureImportActivities(ctx, tx, teamID, accepted, now)' ||
	! printf '%s\n' "$import_transaction" | grep -Fq 'Imported: result.Imported}, now)' ||
	! printf '%s\n' "$import_activities" | grep -Fq 'createdAt := FormatTime(now.UTC())'; then
	echo "architecture check: import activity rows and completion event must share one transaction timestamp" >&2
	exit 1
fi
create_integration=$(sed -n '/^func (d \*DB) CreateIntegration(/,/^}/p' internal/db/integrations.go)
if [ "$(printf '%s\n' "$create_integration" | grep -Fc 'd.currentTime()')" -ne 1 ] ||
	! printf '%s\n' "$create_integration" | grep -Fq 'CreatedAt: createdAt'; then
	echo "architecture check: integration creation must return its persisted creation timestamp" >&2
	exit 1
fi

# DB.SQL is retained for integration-test setup and diagnostics. Production
# code must use typed persistence operations so SQL and transaction policy
# remain inside the database adapter.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	if grep -Eq '\.SQL\(\)' "$source"; then
		echo "architecture check: runtime source $source must not access DB.SQL()" >&2
		exit 1
	fi
done

# Tag-filtered session reads belong to the reports workflow, which batches tag
# decoration with the session query. Do not reintroduce the obsolete unscoped
# DB entrypoint that bypassed that path.
if grep -R -Eq '^func \(d \*DB\) ListSessionsByTag\(' internal/db --include='*.go'; then
	echo "architecture check: tag-filtered history must use the reports workflow" >&2
	exit 1
fi

# Global audit scope is reserved for authentication events that happen before
# workspace selection. Other workflows must record under a positive workspace.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	case "$source" in
		internal/auth/users.go|internal/audit/service.go)
			continue
			;;
	esac
	if grep -Eq '\.RecordGlobal\(' "$source"; then
		echo "architecture check: global audit writes are reserved for authentication ($source)" >&2
		exit 1
	fi
done

# Legacy integration-task writes are retained only for old fixtures. Runtime
# code must synchronize a complete provider snapshot through the integration
# workflow and its atomic DB operation.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	case "$source" in
		internal/db/*) continue ;;
	esac
	if grep -Eq '\.(UpsertExternalTask|CloseMissingExternalTasks)\(' "$source"; then
		echo "architecture check: runtime source $source must synchronize external tasks through the integration workflow" >&2
		exit 1
	fi
done

# The SQL driver API belongs to the persistence adapter. Keep workflow,
# transport and process code on typed ports instead of opening connections.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	case "$source" in
		internal/db/*|internal/testutil/*)
			continue
			;;
	esac
	if grep -Eq '"database/sql(/[^\"]*)?"|sql\.Open\(' "$source"; then
		echo "architecture check: SQL driver access belongs in the database adapter ($source)" >&2
		exit 1
	fi
done

# Startup schema changes and compatibility backfills share one transaction-
# scoped advisory lock. This keeps concurrent replicas from racing DDL,
# secret-key validation, or one-time data transformations.
for migration in \
	'internal/db/schema.go:applySchemaContext' \
	'internal/db/schema.go:migrateActivityKeys' \
	'internal/db/secrets.go:sealExistingSecretsContext' \
	'internal/db/migrations.go:runDataMigration'; do
	source=${migration%%:*}
	method=${migration#*:}
	operation=$(sed -n "/^func (.*) $method(/,/^}/p" "$source")
	if ! printf '%s\n' "$operation" | grep -Fq 'lockMigrations(ctx, tx)'; then
		echo "architecture check: $source $method must acquire the shared migration lock" >&2
		exit 1
	fi
done

# Session transition operations receive the authoritative transition instant
# from tracking workflows; do not mix those timestamps with persistence wall time.
for source in internal/db/sessions.go internal/db/session_transitions.go internal/db/tracking.go; do
	if grep -Eq 'time\.Now\(' "$source"; then
		echo "architecture check: session transition timestamps must use caller-supplied time ($source)" >&2
		exit 1
	fi
done

# Persistence metadata and lease calculations use the process clock supplied
# by db.Config. The adapter must require that clock rather than selecting a
# process-global fallback.
for source in $(find internal/db -type f -name '*.go' ! -name '*_test.go' ! -name 'db.go'); do
	if grep -Eq 'time\.(Now|Since|Until)\(' "$source"; then
		echo "architecture check: persistence code must use the injected database clock ($source)" >&2
		exit 1
	fi
done
if grep -Eq 'time\.Now\(|now = time\.Now' internal/db/db.go || ! grep -q 'config.Now == nil' internal/db/db.go || ! grep -q 'ErrMissingClock' internal/db/db.go; then
	echo "architecture check: database construction must require the injected process clock" >&2
	exit 1
fi
if grep -q 'log.Default()' internal/db/db.go || ! grep -q 'config.Logger == nil' internal/db/db.go || ! grep -q 'ErrMissingLogger' internal/db/db.go; then
	echo "architecture check: database construction must require the injected process logger" >&2
	exit 1
fi

# The exported SQL accessor is a setup seam for integration tests. Production
# code must stay on named persistence operations and workflow ports.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	if grep -Eq '\.SQL\(\)' "$source"; then
		echo "architecture check: production code must not bypass named DB operations through $source" >&2
		exit 1
	fi
done

# Runtime environment reads belong at explicit configuration boundaries.
# Test utilities are exempt because they configure their own local fixtures.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	case "$source" in
		cmd/paratrack/*|internal/testutil/*)
			continue
			;;
	esac
	if grep -Eq 'os\.(Getenv|LookupEnv)\(' "$source"; then
		echo "architecture check: runtime env reads belong in configuration boundaries ($source)" >&2
		exit 1
	fi
done

# Verify workspace guards structurally as well as textually, so comments or
# harmless formatting changes cannot make the scope gate pass accidentally.
{
	printf '%s' "$workspace_guard_checks"
	for workflow in $core_packages; do
		printf 'package:internal/%s\n' "$workflow"
	done
} | go run ./scripts/architecture
