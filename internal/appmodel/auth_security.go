package appmodel

import "time"

// EmailVerificationIssueRequest mints a confirmation link. The store refuses a second
// link inside MinInterval, so a retried sign-in cannot flood the inbox.
type EmailVerificationIssueRequest struct {
	UserID      int64
	TokenHash   string
	Now         time.Time
	TTL         time.Duration
	MinInterval time.Duration
}

// EmailVerificationConsumeRequest confirms the address behind a link, once.
type EmailVerificationConsumeRequest struct {
	TokenHash string
	At        time.Time
}

// TwoFactorEnableRequest turns the second factor on: it stores the confirmed secret,
// the step the confirming code was read from, and the hashes of the recovery codes.
type TwoFactorEnableRequest struct {
	UserID     int64
	Secret     string
	Step       int64
	CodeHashes []string
}

// TwoFactorState is what the second factor of an account is, as the workflow reads it.
type TwoFactorState struct {
	Enabled  bool
	Secret   string
	Pending  string
	LastStep int64
}

// TwoFactorChallengeCreateRequest opens a code step after a correct password.
type TwoFactorChallengeCreateRequest struct {
	UserID    int64
	TokenHash string
	Now       time.Time
	TTL       time.Duration
}

// TwoFactorChallenge is an open code step: the account it belongs to and the wrong codes so far.
type TwoFactorChallenge struct {
	UserID   int64
	Attempts int64
}

// RecoveryCodeConsumeRequest spends one recovery code of an account.
type RecoveryCodeConsumeRequest struct {
	UserID   int64
	CodeHash string
	At       time.Time
}

// RegistrationOutcome is what a signup leaves behind. With the email check on, it carries
// the confirmation token to mail and no session; without the check, a session.
type RegistrationOutcome struct {
	Session     AuthSessionCredential
	TeamID      int64
	Identity    UserIdentity
	VerifyToken string
}

// TwoFactorSetup is what a set-up hands the person: the secret to type into an authenticator
// app, and the link that opens the app on a phone.
type TwoFactorSetup struct {
	Secret string
	URI    string
}

// TwoFactorConfirmRequest confirms a set-up with a code from the app.
type TwoFactorConfirmRequest struct {
	UserID int64
	Code   string
}

// TwoFactorDisableRequest turns the second factor off. It needs the password and a current code.
type TwoFactorDisableRequest struct {
	UserID   int64
	Password string
	Code     string
}

// TwoFactorLoginRequest answers the code step of a sign-in: the open challenge and the code.
type TwoFactorLoginRequest struct {
	Challenge string
	Code      string
}

// TwoFactorPendingRequest keeps a secret of a set-up that no code has confirmed yet.
type TwoFactorPendingRequest struct {
	UserID int64
	Secret string
}

// TwoFactorStepRequest records the step a correct code was read from, so it cannot be used again.
type TwoFactorStepRequest struct {
	UserID int64
	Step   int64
}

// TwoFactorUserRequest names the account whose second factor is turned off.
type TwoFactorUserRequest struct {
	UserID int64
}

// TwoFactorChallengeRequest names an open code step by the hash of its token.
type TwoFactorChallengeRequest struct {
	TokenHash string
}
