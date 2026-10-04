package appmodel

import (
	"errors"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// Application errors are stable workflow outcomes interpreted by adapters.
var (
	ErrInvalidScheduleCell       = errors.New("invalid schedule cell")
	ErrNoScheduleProjects        = errors.New("schedule requires a project")
	ErrInvalidDefaultProject     = errors.New("default project must belong to the current workspace")
	ErrInvalidPreferences        = errors.New("preferences exceed the allowed size")
	ErrInvalidSavedReport        = errors.New("invalid saved report")
	ErrInvalidGoalTeam           = errors.New("goal team id must be positive")
	ErrInvalidGoalActivity       = errors.New("goal activity is required")
	ErrInvalidGoalPeriod         = errors.New("goal period must be daily, weekly, or monthly")
	ErrInvalidGoalTarget         = errors.New("goal target minutes must be positive")
	ErrInvalidPayrollTeam        = errors.New("payroll team id must be positive")
	ErrInvalidPayrollPeriod      = errors.New("payroll period must end after it starts")
	ErrNoPayableTime             = model.ErrNoPayableTime
	ErrInvalidPushSubscription   = errors.New("invalid push subscription")
	ErrInvalidTagTeam            = errors.New("tag team id must be positive")
	ErrInvalidTag                = errors.New("tag name is required")
	ErrTagNotFound               = model.ErrTagNotFound
	ErrForbidden                 = model.ErrForbidden
	ErrNotFound                  = model.ErrNotFound
	ErrTeamNotFound              = errors.New("team not found")
	ErrTeamDuplicate             = errors.New("team slug already taken")
	ErrTeamValidation            = errors.New("validation failed")
	ErrTeamForbidden             = errors.New("forbidden")
	ErrInvalidProjectTeam        = errors.New("project team id must be positive")
	ErrInvalidProjectRate        = errors.New("project rate cannot be negative")
	ErrInvalidProjectCurrency    = errors.New("project currency must be a three-letter code")
	ErrInvalidProjectEstimate    = errors.New("project estimate cannot be negative")
	ErrInvalidProjectName        = errors.New("project name is required")
	ErrInvalidProjectSlug        = errors.New("project slug contains unsupported characters")
	ErrInvalidProjectColor       = errors.New("project color must be a six-digit hex color")
	ErrInvalidIntegration        = errors.New("invalid integration")
	ErrInvalidWebhook            = errors.New("invalid webhook")
	ErrInvalidExternalTask       = errors.New("invalid external task")
	ErrIncompleteTaskSnapshot    = errors.New("external task snapshot reached the synchronization limit")
	ErrIntegrationSyncSuperseded = errors.New("integration sync was superseded by a newer sync")
	ErrInvalidSessionStart       = errors.New("invalid timer start")
	ErrInvalidSessionEdit        = errors.New("invalid session edit request")
	ErrInvalidSessionDelete      = errors.New("invalid session delete request")
	ErrInvalidSessionPeriod      = errors.New("session end must be after its start")
	ErrInvalidSessionLength      = errors.New("session duration cannot be negative")
	ErrInvalidTeamBilling        = errors.New("invalid team billing settings")
	ErrInvalidTeamSettings       = errors.New("invalid team settings")
	ErrAuthInvalidEmail          = errors.New("invalid email")
	ErrAuthValidation            = errors.New("validation failed")
	ErrAuthNotFound              = errors.New("not found")
	ErrAuthBadPassword           = errors.New("bad password")
	ErrAuthCredentialsInvalid    = errors.New("credentials invalid")
	ErrAuthResetInvalid          = errors.New("reset token invalid")
	ErrAuthSessionInvalid        = errors.New("session invalid")
	ErrAuthForbidden             = model.ErrForbidden
	ErrAuthTokenInvalid          = model.ErrTokenInvalid

	ErrNoBillableTime              = model.ErrNoBillableTime
	ErrAlreadyBilled               = model.ErrAlreadyBilled
	ErrMixedCurrency               = model.ErrMixedCurrency
	ErrInvoiceNotDraft             = model.ErrInvoiceNotDraft
	ErrStripeSessionMismatch       = model.ErrStripeSessionMismatch
	ErrStripeSessionPending        = model.ErrStripeSessionPending
	ErrInvalidInvoice              = errors.New("invalid invoice transition request")
	ErrInvalidClient               = errors.New("invoice client is required")
	ErrInvalidPaymentLink          = errors.New("payment link must be a valid http(s) URL")
	ErrHistoryConfirmationRequired = errors.New("confirmation is required before assigning activity history for billing")
	ErrInvalidReceipt              = errors.New("invoice receipt must be at most 300 characters")
	ErrStripeUnavailable           = errors.New("stripe payment processing is not configured")
	ErrInvalidStripeSignature      = errors.New("invalid Stripe webhook signature")
	ErrInvalidStripePayload        = errors.New("invalid Stripe webhook payload")
	ErrInvalidStripeMetadata       = errors.New("invalid Stripe payment metadata")
	ErrMissingStripePaymentStatus  = errors.New("stripe checkout event has no payment status")
)

const (
	SessionTTL = 30 * 24 * time.Hour
	ResetTTL   = 30 * time.Minute
)
