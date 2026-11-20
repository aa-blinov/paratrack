// The recurring invoice schedule (UX-20) is the invoicing service's worker: in
// a workspace that switched it on, drafts for the previous month are created
// in every billable project, and the people who issue invoices hear "ready to
// issue" in one letter per month.
package invoicing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// RecurringWorkspace is one workspace whose schedule is on.
type RecurringWorkspace = appmodel.RecurringWorkspace

// RecurringStore is the schedule's persistence.
type RecurringStore interface {
	RecurringWorkspaces(context.Context) ([]RecurringWorkspace, error)
	// ClaimRecurringInvoiceLetter answers true exactly once a month, so the
	// "ready to issue" letter goes out once however many ticks the drafts took.
	ClaimRecurringInvoiceLetter(ctx context.Context, request appmodel.RecurringLetterClaimRequest) (bool, error)
}

// DraftSummary is one draft the schedule created in this pass.
type DraftSummary struct {
	Number  string
	Project string
	// AmountCents and Currency say what the draft asks for; money keeps its
	// integer-cents rule.
	AmountCents int
	Currency    string
	// UnapprovedWeeks counts the weeks that rode along unapproved (D-3), so
	// the letter can say what still needs a manager's approval.
	UnapprovedWeeks int
}

// SkippedDraft is a project whose draft this pass did not create, with the
// reason as data — the letter's language belongs to its reader, not here.
type SkippedDraft struct {
	Project string
	// UnapprovedWeeks names the Mondays of the weeks that hold the draft back
	// (D-3). Empty when the reason is something else; a future kind carries
	// its own field.
	UnapprovedWeeks []time.Time
}

// RecurringNotifier announces the drafts the schedule created. The
// implementation decides channels and languages; the durable outbox retries an
// enqueue, so a failed attempt does not repeat the letter's month.
type RecurringNotifier interface {
	ReadyToIssue(ctx context.Context, teamID int64, month string, drafts []DraftSummary, skipped []SkippedDraft)
}

// recurringInvoices is the schedule state every *Service carries; the nil
// service fields mean the graph does not run the schedule.
type recurringSchedule struct {
	store    RecurringStore
	notifier RecurringNotifier
	now      func() time.Time
}

// NewRecurring adds the schedule to the invoice service. The store port rides
// the same persistence adapter as the rest of the workflow; a nil notifier
// keeps the schedule creating drafts silently.
func (s *Service) startRecurring(store RecurringStore, notifier RecurringNotifier, now func() time.Time) error {
	if now == nil {
		return fmt.Errorf("%w: recurring invoices need the injected clock", ErrIncompleteDependencies)
	}
	s.recurring = recurringSchedule{store: store, notifier: notifier, now: now}
	return nil
}

// RunRecurring executes one pass now and then every interval until ctx ends.
func (s *Service) RunRecurring(ctx context.Context, interval time.Duration) {
	if s.recurring.store == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.RecurringPass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RecurringPass(ctx); err != nil && ctx.Err() == nil {
				// A failed pass retries on the next tick; the letters that
				// already went out stay out.
				s.logger.Printf("invoicing: recurring pass failed, retrying next tick: %v", err)
			}
		}
	}
}

// RecurringPass creates the previous month's drafts in every workspace whose
// schedule is on. Every workspace's wait-and-claims are idempotent: the drafts
// deduplicate on their sessions' billing stamps, the letter claims once.
func (s *Service) RecurringPass(ctx context.Context) error {
	if s.recurring.store == nil {
		return nil
	}
	workspaces, err := s.recurring.store.RecurringWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("list scheduled workspaces: %w", err)
	}
	var errs []error
	for _, workspace := range workspaces {
		if err := s.recurringOne(ctx, workspace); err != nil {
			// One broken workspace must not hold the others' invoices; the
			// failure is named in the process log (its worker retries hourly).
			s.logger.Printf("invoicing: recurring pass team %d: %v", workspace.TeamID, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) recurringOne(ctx context.Context, workspace RecurringWorkspace) error {
	local := s.recurring.now().In(workspace.Zone)
	// The month the schedule bills: the one that ended before the current
	// calendar month, computed in the workspace's own zone, so a month is the
	// month the workspace's people lived.
	thisMonth := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, workspace.Zone)
	monthStart := thisMonth.AddDate(0, -1, 0)
	month := monthStart.Format("2006-01")
	// The invoice convention: the period end is exclusive, the same midnight
	// the manual form sends, so the draft covers the whole month in one
	// window and matches the draft a manager made by hand for the same
	// month. Stepping back a second here left the month's last second
	// unbilled and printed the period a day short.
	start, end := monthStart, thisMonth

	projects, err := s.projects.ListProjects(ctx, appmodel.ProjectCatalogQuery{TeamID: workspace.TeamID})
	if err != nil {
		return fmt.Errorf("list the workspace's projects: %w", err)
	}
	var drafts []DraftSummary
	var skipped []SkippedDraft
	for _, project := range projects {
		if project.Archived || !project.Billable || project.BillableRateCents == nil || *project.BillableRateCents <= 0 {
			continue // a project without a rate has nothing that can be billed
		}
		// Unapproved weeks (D-3) hold a draft back until someone approves the
		// week; the letter names that as the next step instead of letting the
		// schedule confirm anything in silence.
		weeks, err := s.UnapprovedWeeks(ctx, workspace.TeamID, project.ID, start, end)
		if err != nil {
			return fmt.Errorf("list unapproved weeks for project %d: %w", project.ID, err)
		}
		if len(weeks) > 0 {
			skipped = append(skipped, SkippedDraft{Project: project.Name, UnapprovedWeeks: weeks})
			continue
		}
		// The pass held every unapproved week back above, so the draft it
		// writes is clean: the count is an honest 0, the same value the
		// manual path records, and the accountant's export can tell it
		// from an invoice made before the count existed (NULL).
		clean := 0
		invoice, lines, err := s.createDraft(ctx, appmodel.InvoiceDraftRequest{
			TeamID: workspace.TeamID, CallerID: workspace.OwnerID,
			Start: start, End: end,
			Options:         appmodel.InvoiceOptions{ProjectID: project.ID},
			UnapprovedWeeks: &clean,
		})
		if err != nil {
			switch {
			case errors.Is(err, model.ErrNoBillableTime), isAlreadyBilledPeriod(err):
				continue // nothing the schedule can add: quiet by design
			case errors.Is(err, model.ErrPeriodClosed):
				// The month is closed by hand, the schedule does not argue (FIN-25).
				continue
			}
			return fmt.Errorf("create the recurring draft for project %d: %w", project.ID, err)
		}
		amount := 0
		for _, line := range lines {
			amount += line.AmountCents
		}
		drafts = append(drafts, DraftSummary{
			Number: invoice.Number, Project: project.Name,
			AmountCents: amount, Currency: invoice.Currency,
			// The draft rode no unapproved week here: the pass held those back.
		})
	}
	if len(drafts) == 0 {
		// Nothing became ready this tick: no letter, and this month's mark
		// stays unclaimed for the tick that finally has something to say.
		return nil
	}
	claimed, err := s.recurring.store.ClaimRecurringInvoiceLetter(ctx, appmodel.RecurringLetterClaimRequest{TeamID: workspace.TeamID, Month: month})
	if err != nil {
		return err
	}
	if !claimed || s.recurring.notifier == nil {
		return nil
	}
	// The outbox is durable; a failed hand-off is named on the process logger
	// and retried by the delivery worker, not by this pass.
	s.recurring.notifier.ReadyToIssue(ctx, workspace.TeamID, month, drafts, skipped)
	return nil
}

// isAlreadyBilledPeriod reports the error the writer gives when the period's
// time already sits on invoices: the schedule's second attempt is a no-op.
func isAlreadyBilledPeriod(err error) bool {
	var billed *model.AlreadyBilledInPeriodError
	return errors.As(err, &billed) || errors.Is(err, appmodel.ErrAlreadyBilled)
}
