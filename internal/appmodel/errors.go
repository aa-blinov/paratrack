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
	ErrScheduleWeekNotEmpty      = errors.New("schedule week already has plan")
	ErrScheduleNothingToCopy     = errors.New("schedule previous week is empty")
	ErrScheduleLineHasHours      = errors.New("project line still has planned hours")
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
	ErrNoPayRates                = model.ErrNoPayRates
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
	ErrIntegrationTokenRejected  = errors.New("the provider rejected the token")
	ErrInvalidWebhook            = errors.New("invalid webhook")
	ErrInvalidExternalTask       = errors.New("invalid external task")
	ErrIncompleteTaskSnapshot    = errors.New("external task snapshot reached the synchronization limit")
	ErrIntegrationSyncSuperseded = errors.New("integration sync was superseded by a newer sync")
	ErrInvalidSessionStart       = errors.New("invalid timer start")
	ErrInvalidSessionEdit        = errors.New("invalid session edit request")
	ErrInvalidSessionDelete      = errors.New("invalid session delete request")
	ErrInvalidSessionPeriod      = model.ErrSessionPeriodInvalid
	ErrSessionEndInFuture        = model.ErrSessionEndInFuture
	ErrInvalidSessionLength      = model.ErrSessionLengthInvalid
	ErrInvalidTeamBilling        = errors.New("invalid team billing settings")
	ErrInvalidTeamSettings       = errors.New("invalid team settings")
	ErrInviteAddressLive         = errors.New("invite address already has a live invitation")
	ErrAuthInvalidEmail          = errors.New("invalid email")
	ErrAuthValidation            = errors.New("validation failed")
	ErrAuthNotFound              = errors.New("not found")
	ErrAuthBadPassword           = errors.New("bad password")
	ErrAuthCredentialsInvalid    = errors.New("credentials invalid")
	ErrAuthResetInvalid          = errors.New("reset token invalid")
	// ErrAuthEmailNotVerified refuses a password sign-in on an account whose signup
	// address is not confirmed yet. The answer carries the account, so the caller can
	// send the confirmation again.
	ErrAuthEmailNotVerified = errors.New("email not verified")
	// ErrAuthVerificationInvalid is the one answer for a confirmation link that is unknown,
	// used, or expired.
	ErrAuthVerificationInvalid = errors.New("verification link invalid")
	// ErrAuthTwoFactorRequired means the password was right and a one-time code is
	// still owed; the answer carries the account, so the caller can open the code step.
	ErrAuthTwoFactorRequired = errors.New("two-factor code required")
	// ErrAuthTwoFactorInvalid refuses a wrong code, a used code or a used recovery code.
	ErrAuthTwoFactorInvalid = errors.New("two-factor code invalid")
	// ErrAuthChallengeInvalid is the one answer for a code step that is unknown, expired
	// or out of attempts. The caller must not tell which of these it was.
	ErrAuthChallengeInvalid = errors.New("two-factor challenge invalid")
	// ErrAuthTwoFactorState refuses a set-up step that does not fit the account: a second
	// set-up while one is on, or a confirmation with nothing started.
	ErrAuthTwoFactorState = errors.New("two-factor state does not allow this")
	ErrAuthSessionInvalid = errors.New("session invalid")
	ErrAuthForbidden      = model.ErrForbidden
	ErrAuthTokenInvalid   = model.ErrTokenInvalid
	// ErrAuthEmailTaken marks a profile email change that collides with
	// another account. It is separate from ErrAuthInvalidEmail so the
	// form can say "already in use" instead of "malformed".
	ErrAuthEmailTaken = errors.New("email already in use")

	ErrNoBillableTime              = model.ErrNoBillableTime
	ErrAlreadyBilled               = model.ErrAlreadyBilled
	ErrMixedCurrency               = model.ErrMixedCurrency
	ErrInvoiceNotDraft             = model.ErrInvoiceNotDraft
	ErrStripeSessionMismatch       = model.ErrStripeSessionMismatch
	ErrStripeSessionPending        = model.ErrStripeSessionPending
	ErrInvalidInvoice              = errors.New("invalid invoice transition request")
	ErrInvalidClient               = errors.New("invoice client is required")
	ErrInvalidDue                  = errors.New("invoice due date is not a date")
	ErrInvalidPaymentLink          = errors.New("payment link must be a valid http(s) URL")
	ErrHistoryConfirmationRequired = errors.New("confirmation is required before assigning activity history for billing")
	ErrInvalidReceipt              = errors.New("invoice receipt must be at most 300 characters")
	ErrStripeUnavailable           = errors.New("stripe payment processing is not configured")
	ErrStripeKeyRejected           = errors.New("stripe rejected the configured key")
	ErrInvalidStripeSignature      = errors.New("invalid Stripe webhook signature")
	ErrInvalidStripePayload        = errors.New("invalid Stripe webhook payload")
	ErrInvalidStripeMetadata       = errors.New("invalid Stripe payment metadata")
	ErrMissingStripePaymentStatus  = errors.New("stripe checkout event has no payment status")
)

const (
	SessionTTL = 30 * 24 * time.Hour
	ResetTTL   = 30 * time.Minute
	// VerificationTTL is how long a signup confirmation link works.
	VerificationTTL = 48 * time.Hour
	// ChallengeTTL is how long a password-accepted sign-in waits for its one-time code.
	ChallengeTTL = 5 * time.Minute
)
