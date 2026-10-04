package web

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrIncompleteServices = errors.New("HTTP application dependencies are incomplete")

// Dependencies are the workflow ports consumed by the HTTP adapter.
type Dependencies struct {
	Billing              BillingWorkflow
	Auth                 AuthenticationDependencies
	TokenAdmin           APITokenManagementBuilding
	AuditLog             AuditWorkflow
	Teams                TeamDependencies
	TeamOps              TeamOperations
	Tracking             TrackingDependencies
	TrackingOps          TrackingOperations
	ImportedTaskTracking ImportedTaskTimerStarting
	Imports              ImportWorkflow
	Integrations         IntegrationDependencies
	Invoicing            InvoiceDependencies
	InvoiceDocuments     InvoiceDocumentBuilding
	Payroll              PayrollWorkflow
	MemberAdmin          TeamMemberManagementBuilding
	PayrollPaid          PayrollPaymentWorkflow
	Preferences          PreferenceWorkflow
	Scheduling           SchedulingWorkflow
	Projects             ProjectDependencies
	ProjectPages         ProjectPageBuilding
	Reports              SavedReportWorkflow
	ReportBuilder        ReportBuilding
	Dashboard            DashboardBuilding
	SessionDecorations   SessionDecorationBuilding
	Push                 PushWorkflow
	Tagging              TagDependencies
	Goals                GoalWorkflow
	Webhooks             WebhookWorkflow
	MailQueue            InvoiceMailQueue
}

// AuthenticationDependencies groups the HTTP consumer contracts for account
// and identity flows while retaining least-capability ports per route area.
type AuthenticationDependencies struct {
	Identity  IdentityWorkflow
	SignIn    SignInWorkflow
	Recovery  PasswordRecoveryWorkflow
	Profile   ProfileWorkflow
	APITokens APITokenWorkflow
}

// TeamDependencies groups workspace consumer contracts by responsibility.
type TeamDependencies struct {
	Directory      TeamDirectory
	Invitations    TeamInvitations
	Settings       TeamSettingsManagement
	Administration TeamAdministration
}

// ProjectDependencies separates project reads from project mutations.
type ProjectDependencies struct {
	Queries  ProjectQueries
	Commands ProjectCommands
}

// AuditWorkflow is the audit query and recording surface used by HTTP routes.
type AuditWorkflow interface {
	List(context.Context, int64, int) ([]model.AuditEntry, error)
	Record(context.Context, model.AuditRecord) error
}

// BillingWorkflow processes provider payment events and coordinates their
// application side effects.
type BillingWorkflow interface {
	ProcessStripeWebhook(context.Context, appmodel.StripeWebhookRequest) (appmodel.StripeWebhookResult, error)
	RecordManualPayment(context.Context, appmodel.ManualPaymentRequest) error
}

// ImportWorkflow is the provider import surface exposed to HTTP routes.
type ImportWorkflow interface {
	Preview(context.Context, appmodel.ProviderImportPreviewRequest) ([]importport.ImportedEntry, error)
	RunFromProvider(context.Context, appmodel.ProviderImportRunRequest) (appmodel.ImportResult, error)
}

// SavedReportWorkflow manages saved report definitions.
type SavedReportWorkflow interface {
	Create(context.Context, appmodel.SavedReportCreateRequest) (model.SavedReport, error)
	Delete(context.Context, appmodel.SavedReportDeleteRequest) error
	List(context.Context, int64) ([]model.SavedReport, error)
}

// PushWorkflow exposes subscription controls and the device count required by
// notification settings. Delivery key material stays inside the workflow.
type PushWorkflow interface {
	PublicKey(context.Context) (string, error)
	Subscribe(context.Context, appmodel.PushSubscribeRequest) error
	SubscriptionCount(context.Context, int64) (int, error)
	UnsubscribeForMember(context.Context, appmodel.PushUnsubscribeRequest) error
}

// WebhookWorkflow exposes endpoint management and delivery-history reads to
// HTTP routes. Event dispatch and worker lifecycle stay behind application
// workflows and runtime dependencies.
type WebhookWorkflow interface {
	Create(context.Context, appmodel.WebhookCreateRequest) (webhookport.WebhookSummary, error)
	Delete(context.Context, appmodel.WebhookDeleteRequest) error
	Management(context.Context, int64) (appmodel.WebhookManagementSnapshot, error)
}

// InvoiceMailQueue enqueues frozen invoice messages for durable delivery.
type InvoiceMailQueue interface {
	EnqueueInvoice(context.Context, mailport.InvoiceQueueRequest) error
}

// InvoiceDocumentBuilding assembles cross-workflow data shared by invoice
// pages and generated documents.
type InvoiceDocumentBuilding interface {
	Build(context.Context, appmodel.InvoiceDocumentRequest) (appmodel.InvoiceDocumentSnapshot, error)
}

// ReportBuilding is the query surface used by the HTTP adapter. Keeping the
// graph on this consumer-facing port lets it use report builders without
// requiring the concrete implementation in application wiring.
type ReportBuilding interface {
	Build(context.Context, appmodel.ReportBuildQuery) (appmodel.ReportAggregateResult, error)
	BuildStats(context.Context, appmodel.ReportStatsQuery) (appmodel.ReportStatsResult, error)
	BuildGraph(context.Context, appmodel.ReportGraphQuery) (appmodel.ReportGraphResult, error)
	BuildExport(context.Context, appmodel.ExportBuildQuery) (appmodel.ExportSnapshot, error)
}

// GoalWorkflow is the manager and display surface for team goals.
type GoalWorkflow interface {
	Management(context.Context, appmodel.GoalProgressQuery) (appmodel.GoalManagementSnapshot, error)
	DeleteForManager(context.Context, appmodel.GoalDeleteRequest) error
	List(context.Context, int64) ([]model.Goal, error)
	Progress(context.Context, appmodel.GoalProgressQuery) ([]model.GoalProgress, error)
	UpsertForManager(context.Context, appmodel.GoalUpsertRequest) (model.Goal, error)
}

// PreferenceWorkflow exposes a user's persisted interface preferences.
type PreferenceWorkflow interface {
	Load(context.Context, int64) (appmodel.UserPreferences, error)
	Save(context.Context, appmodel.PreferencesSaveRequest) error
}

// SchedulingWorkflow exposes schedule reads and cell updates to HTTP routes.
type SchedulingWorkflow interface {
	List(context.Context, int64, time.Time) (appmodel.ScheduleSnapshot, error)
	SetCell(context.Context, appmodel.ScheduleCellRequest) error
}

// TagDependencies separates tag reads from mutations at the HTTP boundary.
type TagDependencies struct {
	Queries  TagQueries
	Commands TagCommands
}

// TagQueries provides the tag views used by HTTP routes.
type TagQueries interface {
	List(context.Context, int64) ([]model.Tag, error)
	ListWithCounts(context.Context, int64) ([]model.TagWithCount, error)
}

// TagCommands manages tags and their session assignments.
type TagCommands interface {
	AttachForMember(context.Context, appmodel.SessionTagRequest) error
	CreateForMember(context.Context, appmodel.TagCreateRequest) (model.Tag, error)
	Delete(context.Context, appmodel.TagDeleteRequest) error
	DetachForMember(context.Context, appmodel.SessionTagRequest) error
}

// SessionDecorationBuilding batches optional tag and project metadata for session rows.
type SessionDecorationBuilding interface {
	Build(context.Context, appmodel.SessionDecorationRequest) (appmodel.SessionDecorationSnapshot, error)
	BuildRow(context.Context, int64, int64) (appmodel.SessionDecorationRowSnapshot, error)
}

// ProjectQueries provides project and activity data to HTTP views.
type ProjectQueries interface {
	Detail(context.Context, appmodel.ProjectDetailRequest) (model.ProjectDetail, error)
	GetBySlug(context.Context, appmodel.ProjectSlugQuery) (model.Project, error)
	List(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error)
	ListWithUsage(context.Context, appmodel.ProjectUsageQuery) (appmodel.ProjectListSnapshot, error)
}

// ProjectCommands owns project and activity assignment mutations.
type ProjectCommands interface {
	AssignActivity(context.Context, appmodel.AssignActivityProjectRequest) error
	Create(context.Context, appmodel.ProjectCreateRequest) (model.Project, error)
	Delete(context.Context, appmodel.ProjectMutationRequest) error
	DeleteBySlug(context.Context, appmodel.ProjectSlugMutationRequest) error
	Update(context.Context, appmodel.ProjectUpdateRequest) (model.Project, error)
	UpdateBySlug(context.Context, appmodel.ProjectSlugUpdateRequest) (model.Project, error)
	UpdateRate(context.Context, appmodel.ProjectRateRequest) error
}

// ProjectPageBuilding assembles cross-workflow data for project detail views.
type ProjectPageBuilding interface {
	Build(context.Context, appmodel.ProjectPageRequest) (appmodel.ProjectPageSnapshot, error)
}

// PayrollWorkflow is the payroll operation set consumed by HTTP routes.
type PayrollWorkflow interface {
	CreateRun(context.Context, appmodel.PayrollDraftRequest) (model.PayrollRun, []model.PayrollRun, error)
	DeleteDraft(context.Context, appmodel.PayrollMutationRequest) error
	GetRun(context.Context, int64, int64) (appmodel.PayrollRunDetail, error)
	ListRuns(context.Context, int64) ([]appmodel.PayrollRunSummary, error)
	MemberSettings(context.Context, int64) ([]model.MemberPayrollSettings, error)
	UpdateMemberPay(context.Context, appmodel.PayrollMemberPayRequest) error
}

// PayrollPaymentWorkflow includes payment-transition side effects used by the
// HTTP adapter as one application operation.
type PayrollPaymentWorkflow interface {
	MarkPaid(context.Context, appmodel.PayrollMutationRequest) error
}

// InvoiceDependencies separates invoice queries, draft operations and payment
// link operations at the HTTP boundary.
type InvoiceDependencies struct {
	Queries      InvoiceQueries
	Drafts       InvoiceDrafts
	PaymentLinks InvoicePaymentLinks
}

// InvoiceQueries provides the read capabilities used by invoice pages and
// payment handlers.
type InvoiceQueries interface {
	BuildIndex(context.Context, appmodel.InvoiceIndexRequest) (appmodel.InvoiceIndexSnapshot, error)
	Get(context.Context, int64, int64) (appmodel.InvoiceDetailResult, error)
	StripeReady(context.Context, int64) (bool, error)
	UnbilledProjectTime(context.Context, appmodel.UnbilledProjectQuery) ([]model.UnbilledProject, error)
}

// InvoiceDrafts owns draft lifecycle and invoice history assignment.
type InvoiceDrafts interface {
	AssignHistoryToBillableProject(context.Context, appmodel.AssignBillableHistoryRequest) error
	CreateDraftWithOverlapCheck(context.Context, appmodel.InvoiceDraftRequest) (appmodel.InvoiceDraftCreation, error)
	DeleteDraft(context.Context, appmodel.InvoiceMutationRequest) error
	MarkSent(context.Context, appmodel.InvoiceMutationRequest) error
	RebuildDraft(context.Context, appmodel.InvoiceMutationRequest) error
	SetReceipt(context.Context, appmodel.InvoiceReceiptRequest) error
	UpdateDraftDetails(context.Context, appmodel.InvoiceDraftUpdateRequest) error
}

// InvoicePaymentLinks contains the provider and manual payment-link actions.
type InvoicePaymentLinks interface {
	CreateStripePaymentLink(context.Context, appmodel.InvoiceStripeLinkRequest) (appmodel.InvoiceStripePaymentLink, error)
	SetManualPaymentLink(context.Context, appmodel.InvoiceManualLinkRequest) error
}

// IntegrationDependencies separates integration reads from provider commands
// at the HTTP boundary.
type IntegrationDependencies struct {
	Queries  IntegrationQueries
	Commands IntegrationCommands
}

// IntegrationQueries serves integration and imported-task views.
type IntegrationQueries interface {
	Management(context.Context, int64) (appmodel.IntegrationManagementSnapshot, error)
	Detail(context.Context, appmodel.IntegrationLookupQuery) (appmodel.IntegrationDetailSnapshot, error)
	List(context.Context, int64) ([]model.IntegrationSummary, error)
	Summary(context.Context, appmodel.IntegrationLookupQuery) (model.IntegrationSummary, error)
	Task(context.Context, appmodel.ExternalTaskLookupQuery) (model.ExternalTask, error)
	Tasks(context.Context, appmodel.IntegrationLookupQuery) ([]model.ExternalTask, error)
	TasksForTeam(context.Context, int64) ([]model.ExternalTaskWithProvider, error)
}

// IntegrationCommands manages provider connections and synchronization.
// Provider access and credentials remain behind this contract.
type IntegrationCommands interface {
	ConnectAndSync(context.Context, appmodel.IntegrationCreateRequest) (appmodel.IntegrationConnectResult, error)
	Delete(context.Context, appmodel.IntegrationMutationRequest) error
	SyncProvider(context.Context, appmodel.IntegrationMutationRequest) (int, error)
}

// TrackingDependencies separates session reads from direct tracking commands.
type TrackingDependencies struct {
	Queries  TrackingQueries
	Commands TrackingCommands
}

// TrackingQueries provides session, activity and timesheet reads to HTTP views.
type TrackingQueries interface {
	ActiveSessions(context.Context, int64) ([]model.ActiveSession, error)
	Activity(context.Context, int64, int64) (model.Activity, error)
	ClosedSessions(context.Context, int64, time.Time, time.Time, *int64) ([]model.ActiveSession, error)
	FindActivity(context.Context, int64, string) (model.Activity, error)
	HasAnySession(context.Context, int64) (bool, error)
	Session(context.Context, int64, int64) (model.Session, error)
	SessionHistoryPage(context.Context, int64, time.Time, time.Time, *model.SessionCursor, int) (model.SessionPage, error)
	Timesheet(context.Context, appmodel.TimesheetRequest) (model.TimesheetWeek, error)
}

// TrackingCommands handles direct session and timesheet mutations that do not
// require the cross-cutting transition coordinator.
type TrackingCommands interface {
	Pause(context.Context, appmodel.TimerSessionRequest) (model.Session, error)
	PauseAll(context.Context, appmodel.TimerStopAllRequest) ([]int64, error)
	Resume(context.Context, appmodel.TimerSessionRequest) (model.Session, error)
	SetDayTotal(context.Context, appmodel.TimesheetCellUpdateRequest) error
	UpdateFields(context.Context, appmodel.SessionUpdateRequest) error
}

// TrackingOperations coordinates named timer actions and session transitions
// that span tracking, projects, or cross-cutting side effects.
type TrackingOperations interface {
	AddClosedActivity(context.Context, appmodel.TimerAddByNameRequest) (model.Activity, model.Session, error)
	Focus(context.Context, appmodel.TimerFocusRequest) (appmodel.FocusResult, error)
	FocusActivity(context.Context, appmodel.TimerFocusByNameRequest) (model.Activity, appmodel.FocusResult, error)
	Delete(context.Context, appmodel.SessionDeleteRequest) error
	Reopen(context.Context, appmodel.TimerReopenRequest) (model.Session, error)
	StartActivity(context.Context, appmodel.TimerStartByNameRequest) (model.Activity, model.Session, error)
	Stop(context.Context, appmodel.TimerStopRequest) (appmodel.TimerStopResult, error)
	StopAll(context.Context, appmodel.TimerStopAllRequest) ([]int64, error)
}

type ImportedTaskTimerStarting interface {
	Start(context.Context, appmodel.ImportedTaskStartRequest) (model.Activity, model.Session, error)
}

// IdentityWorkflow resolves authenticated users and maintains their sessions.
type IdentityWorkflow interface {
	APITokenByRaw(context.Context, appmodel.APITokenLookupRequest) (appmodel.APITokenIdentity, error)
	Logout(context.Context, appmodel.LogoutRequest) error
	IdentityByID(context.Context, int64) (appmodel.UserIdentity, error)
	AuthenticateSessionToken(context.Context, string) (appmodel.UserIdentity, error)
	Touch(context.Context, string)
}

// SignInWorkflow handles password, SSO and account-registration sign-in.
type SignInWorkflow interface {
	AuthenticatePassword(context.Context, appmodel.PasswordLoginRequest) (appmodel.UserIdentity, appmodel.AuthSessionCredential, error)
	AuthenticateSSO(context.Context, appmodel.SSOAuthenticationRequest) (appmodel.UserIdentity, appmodel.AuthSessionCredential, bool, error)
	RegisterAndStartSession(context.Context, appmodel.RegistrationRequest) (appmodel.AuthSessionCredential, int64, error)
}

// PasswordRecoveryWorkflow owns password reset request and completion.
type PasswordRecoveryWorkflow interface {
	CompletePasswordReset(context.Context, appmodel.PasswordResetCompletionRequest) (appmodel.UserIdentity, appmodel.AuthSessionCredential, error)
	RequestPasswordReset(context.Context, appmodel.PasswordResetRequest) (appmodel.UserIdentity, string, error)
}

// ProfileWorkflow exposes signed-in profile changes.
type ProfileWorkflow interface {
	UpdateName(context.Context, appmodel.ProfileNameRequest) error
	ChangePassword(context.Context, appmodel.PasswordChangeRequest) error
}

// APITokenWorkflow manages a user's personal API tokens.
type APITokenWorkflow interface {
	CreateAPIToken(context.Context, appmodel.APITokenCreateRequest) (string, appmodel.APITokenSummary, error)
	DeleteAPIToken(context.Context, appmodel.APITokenDeleteRequest) error
	ListAPITokens(context.Context, appmodel.APITokenListRequest) ([]appmodel.APITokenSummary, error)
}

type APITokenManagementBuilding interface {
	Management(context.Context, appmodel.APITokenListRequest) (appmodel.APITokenManagementSnapshot, error)
}

// TeamDirectory is the workspace and membership query surface consumed by
// HTTP routes.
type TeamDirectory interface {
	FindByID(context.Context, int64) (model.Team, error)
	IsMember(context.Context, appmodel.TeamMembershipQuery) (model.TeamRole, bool, error)
	Members(context.Context, int64) ([]model.TeamMember, error)
	MembershipForUser(context.Context, appmodel.TeamMembershipQuery) (model.TeamMembership, bool, error)
	MembershipsForUser(context.Context, int64) ([]model.TeamMembership, error)
}

type TeamMemberManagementBuilding interface {
	Management(context.Context, int64) (appmodel.TeamMemberManagementSnapshot, error)
}

// TeamInvitations handles workspace invitation lifecycle operations.
type TeamInvitations interface {
	AcceptInvite(context.Context, appmodel.TeamInviteAcceptRequest) (model.Team, error)
	FindInvite(context.Context, string) (appmodel.TeamInviteResult, error)
	InvitePage(context.Context, string) (appmodel.TeamInvitePageSnapshot, error)
	InvitesForTeam(context.Context, int64) ([]appmodel.TeamInviteResult, error)
	NewInvite(context.Context, appmodel.TeamInviteCreateRequest) (appmodel.TeamInviteResult, error)
	RevokeInvite(context.Context, appmodel.TeamInviteRevokeRequest) error
}

// TeamSettingsManagement exposes workspace configuration reads and writes.
type TeamSettingsManagement interface {
	BillingRules(context.Context, int64) (model.BillingRules, error)
	Currency(context.Context, int64) (string, error)
	SectionModules(context.Context, int64) (map[string]bool, error)
	Settings(context.Context, int64) (model.TeamSettings, error)
	UpdateLogo(context.Context, appmodel.TeamLogoRequest) error
	UpdateModules(context.Context, appmodel.TeamModulesRequest) error
	UpdateRequisites(context.Context, appmodel.TeamRequisitesRequest) error
}

// TeamAdministration contains workspace and membership mutations that do not
// require the coordinated audit operations in TeamOperations.
type TeamAdministration interface {
	Create(context.Context, appmodel.TeamCreateRequest) (model.Team, error)
	RemoveMember(context.Context, appmodel.TeamMemberRemovalRequest) error
	Rename(context.Context, appmodel.TeamRenameRequest) error
}

// TeamOperations contains workspace mutations whose audit records are part of
// the same application operation.
type TeamOperations interface {
	DeleteWorkspace(context.Context, appmodel.WorkspaceDeleteRequest) ([]int64, error)
	SetRole(context.Context, appmodel.TeamMemberRoleRequest) error
	SetStripeCredentials(context.Context, appmodel.TeamStripeCredentialsRequest) error
	TransferOwnership(context.Context, appmodel.TeamOwnershipTransferRequest) error
	UpdateBilling(context.Context, appmodel.TeamBillingRequest) error
	UpdateCurrency(context.Context, appmodel.TeamCurrencyRequest) error
}

// DashboardBuilding is the dashboard query surface used by the HTTP adapter.
type DashboardBuilding interface {
	Build(context.Context, appmodel.DashboardQuery) (appmodel.DashboardSnapshot, error)
	BuildActiveList(context.Context, int64) (appmodel.ActiveListSnapshot, error)
}

// Validate reports missing required workflows before an adapter starts
// serving requests.
func (s Dependencies) Validate() error {
	return s.validate(
		dependency{"billing", depcheck.IsNil(s.Billing)},
		dependency{"identity", depcheck.IsNil(s.Auth.Identity)},
		dependency{"sign in", depcheck.IsNil(s.Auth.SignIn)},
		dependency{"password recovery", depcheck.IsNil(s.Auth.Recovery)},
		dependency{"profile", depcheck.IsNil(s.Auth.Profile)},
		dependency{"API tokens", depcheck.IsNil(s.Auth.APITokens)},
		dependency{"API token management", depcheck.IsNil(s.TokenAdmin)},
		dependency{"audit", depcheck.IsNil(s.AuditLog)},
		dependency{"team directory", depcheck.IsNil(s.Teams.Directory)},
		dependency{"team invitations", depcheck.IsNil(s.Teams.Invitations)},
		dependency{"team settings", depcheck.IsNil(s.Teams.Settings)},
		dependency{"team administration", depcheck.IsNil(s.Teams.Administration)},
		dependency{"team operations", depcheck.IsNil(s.TeamOps)},
		dependency{"team member administration", depcheck.IsNil(s.MemberAdmin)},
		dependency{"tracking queries", depcheck.IsNil(s.Tracking.Queries)},
		dependency{"tracking commands", depcheck.IsNil(s.Tracking.Commands)},
		dependency{"tracking operations", depcheck.IsNil(s.TrackingOps)},
		dependency{"imported task tracking", depcheck.IsNil(s.ImportedTaskTracking)},
		dependency{"imports", depcheck.IsNil(s.Imports)},
		dependency{"integration queries", depcheck.IsNil(s.Integrations.Queries)},
		dependency{"integration commands", depcheck.IsNil(s.Integrations.Commands)},
		dependency{"invoice queries", depcheck.IsNil(s.Invoicing.Queries)},
		dependency{"invoice drafts", depcheck.IsNil(s.Invoicing.Drafts)},
		dependency{"invoice payment links", depcheck.IsNil(s.Invoicing.PaymentLinks)},
		dependency{"invoice documents", depcheck.IsNil(s.InvoiceDocuments)},
		dependency{"payroll", depcheck.IsNil(s.Payroll)},
		dependency{"payroll payment operations", depcheck.IsNil(s.PayrollPaid)},
		dependency{"preferences", depcheck.IsNil(s.Preferences)},
		dependency{"scheduling", depcheck.IsNil(s.Scheduling)},
		dependency{"project queries", depcheck.IsNil(s.Projects.Queries)},
		dependency{"project commands", depcheck.IsNil(s.Projects.Commands)},
		dependency{"project page builder", depcheck.IsNil(s.ProjectPages)},
		dependency{"reports", depcheck.IsNil(s.Reports)},
		dependency{"report builder", depcheck.IsNil(s.ReportBuilder)},
		dependency{"dashboard builder", depcheck.IsNil(s.Dashboard)},
		dependency{"push", depcheck.IsNil(s.Push)},
		dependency{"tag queries", depcheck.IsNil(s.Tagging.Queries)},
		dependency{"tag commands", depcheck.IsNil(s.Tagging.Commands)},
		dependency{"session decorations", depcheck.IsNil(s.SessionDecorations)},
		dependency{"goals", depcheck.IsNil(s.Goals)},
		dependency{"webhooks", depcheck.IsNil(s.Webhooks)},
		dependency{"mail queue", depcheck.IsNil(s.MailQueue)},
	)
}

type dependency struct {
	name  string
	isNil bool
}

func (s Dependencies) validate(required ...dependency) error {
	for _, dependency := range required {
		if dependency.isNil {
			return fmt.Errorf("%w: %s", ErrIncompleteServices, dependency.name)
		}
	}
	return nil
}
