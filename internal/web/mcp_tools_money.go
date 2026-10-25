package web

import (
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpMoneyTools are the manager's writes on invoices, pay runs and the timesheet
// week. Each one calls the workflow the page calls, with the token's caller and
// workspace, so the rule and the transaction behind it decide what is allowed.
// Sending an invoice reaches the client, and paying a run reaches the payees; a
// sent invoice cannot go back to draft, and a paid run cannot be marked unpaid.
var mcpMoneyTools = []mcpTool{
	{
		Name: "invoice_draft_create",
		Description: "Create a draft invoice from the billable time of a period, as the invoice page does. The period is inclusive. " +
			"Refused while the period holds unapproved weeks (approve them with timesheet_week_approve first), and for time " +
			"already on another invoice. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"start":      dateProp("First day of the period"),
			"end":        dateProp("Last day of the period"),
			"client":     stringProp("Client name on the invoice; when blank, the project's client"),
			"project_id": integerProp("Project to bill; without it, every billable project"),
		}, "start", "end"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Start     string `json:"start"`
				End       string `json:"end"`
				Client    string `json:"client"`
				ProjectID *int64 `json:"project_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			start, end, err := mcpPeriod(c, in.Start, in.End)
			if err != nil {
				return nil, err
			}
			// The page prefills the client, the details and the email from the project, and
			// remembers what it sends back on the project, so the same defaults apply here.
			var projectID int64
			var client, details, email string
			if in.ProjectID != nil {
				if *in.ProjectID <= 0 {
					return nil, appmodel.FieldError{Field: "project_id"}
				}
				option, err := s.mcpProjectOption(c, *in.ProjectID)
				if err != nil {
					return nil, err
				}
				projectID = option.Project.ID
				if option.HasClient {
					client, details, email = option.Client.Name, option.Client.Details, option.Client.Email
				}
			}
			if name := strings.TrimSpace(in.Client); name != "" {
				client = name
			}
			if client == "" {
				return nil, appmodel.FieldError{Field: "client"}
			}
			created, err := s.services.Invoicing.Drafts.CreateDraftWithOverlapCheck(operationContext(c.req), appmodel.InvoiceDraftRequest{
				TeamID: c.teamID, CallerID: c.userID, Client: client, Start: start, End: end,
				Options: appmodel.InvoiceOptions{
					ClientDetails: details, ClientEmail: email, ProjectID: projectID, RememberClient: projectID > 0,
				},
			})
			if err != nil {
				return nil, err
			}
			if created.AdvisoryError != nil {
				s.logger.Printf("invoice %d created but overlapping-document check failed: %v", created.Invoice.ID, created.AdvisoryError)
			}
			view, err := s.mcpInvoiceNow(c, created.Invoice.ID)
			if err != nil {
				return nil, err
			}
			overlaps := created.Overlaps
			if overlaps == nil {
				overlaps = []string{}
			}
			return mcpInvoiceCreated{mcpInvoiceView: view, Overlaps: overlaps}, nil
		},
	},
	{
		Name: "invoice_mark_sent",
		Description: "Issue a draft invoice: its status moves from draft to sent, as the invoice page's Mark sent does. " +
			"A sent invoice cannot go back to draft. Managers only.",
		Write:  true,
		Schema: objectSchema(map[string]any{"invoice_id": integerProp("Invoice id")}, "invoice_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				InvoiceID int64 `json:"invoice_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.InvoiceID <= 0 {
				return nil, model.ErrNotFound
			}
			if err := s.services.Invoicing.Drafts.MarkSent(c.ctx, appmodel.InvoiceMutationRequest{
				TeamID: c.teamID, InvoiceID: in.InvoiceID, CallerID: c.userID,
			}); err != nil {
				return nil, err
			}
			return s.mcpInvoiceNow(c, in.InvoiceID)
		},
	},
	{
		Name: "invoice_send",
		Description: "Emails the invoice to the client. This cannot be undone. The letter carries the PDF; the invoice becomes sent " +
			"once the mail is delivered. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"invoice_id": integerProp("Invoice id"),
			"email":      stringProp("Address the letter goes to; by default the invoice's client email"),
		}, "invoice_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				InvoiceID int64  `json:"invoice_id"`
				Email     string `json:"email"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.InvoiceID <= 0 {
				return nil, model.ErrNotFound
			}
			inv, vm, _, err := s.invoiceVMFor(c.req, in.InvoiceID, false)
			if err != nil {
				return nil, err
			}
			if inv.Status != "draft" {
				return nil, model.ErrInvoiceNotDraft
			}
			to := strings.TrimSpace(in.Email)
			if to == "" {
				to = inv.ClientEmail
			}
			if _, err := netmail.ParseAddress(to); err != nil {
				return nil, appmodel.FieldError{Field: "email"}
			}
			if !mailport.Available(s.runtime.Mailer) {
				return nil, errMailNotConfigured
			}
			seller := s.sellerName(c.req)
			pdf, err := renderInvoicePDF(vm, seller, c.lang)
			if err != nil {
				return nil, err
			}
			subject, letter := invoiceEmail(c.lang, vm, seller)
			msg, err := s.buildEmail(c.req, to, subject, letter, mailport.Attachment{Name: invoicePDFName(vm), ContentType: "application/pdf", Data: pdf})
			if err != nil {
				return nil, err
			}
			if err := s.services.MailQueue.EnqueueInvoice(operationContext(c.req), mailport.InvoiceQueueRequest{
				TeamID: c.teamID, InvoiceID: inv.ID, Revision: inv.Revision, InvoiceNumber: inv.Number, Recipient: to, Message: msg,
			}); err != nil {
				return nil, err
			}
			view, err := s.mcpInvoiceNow(c, inv.ID)
			if err != nil {
				return nil, err
			}
			return mcpInvoiceSent{mcpInvoiceView: view, Recipient: to}, nil
		},
	},
	{
		Name: "invoice_record_payment",
		Description: "Record a payment the client made on a sent invoice, as the payment form does. The amount is in cents and must not " +
			"exceed what is still owed. When the payments cover the invoice, it becomes paid. A recorded payment stays in the journal. " +
			"Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"invoice_id":   integerProp("Invoice id"),
			"amount_cents": integerProp("Amount paid, in cents, above zero"),
			"paid_on":      dateProp("Day the money arrived"),
			"note":         stringProp("Optional note, up to 200 characters"),
		}, "invoice_id", "amount_cents", "paid_on"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				InvoiceID   int64  `json:"invoice_id"`
				AmountCents int    `json:"amount_cents"`
				PaidOn      string `json:"paid_on"`
				Note        string `json:"note"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.InvoiceID <= 0 {
				return nil, model.ErrNotFound
			}
			if in.AmountCents <= 0 {
				return nil, appmodel.FieldError{Field: "amount_cents"}
			}
			if _, err := mcpLocalDate("paid_on", in.PaidOn, c.now.Location()); err != nil {
				return nil, err
			}
			settled, err := s.services.Invoicing.Drafts.BookPayment(c.ctx, appmodel.InvoicePaymentRequest{
				TeamID: c.teamID, InvoiceID: in.InvoiceID, CallerID: c.userID,
				PaidOn: in.PaidOn, AmountCents: in.AmountCents, Note: in.Note,
			})
			if err != nil {
				return nil, err
			}
			view, err := s.mcpInvoiceNow(c, in.InvoiceID)
			if err != nil {
				return nil, err
			}
			return mcpInvoicePayment{mcpInvoiceView: view, Settled: settled}, nil
		},
	},
	{
		Name: "payroll_run_create",
		Description: "Create a payroll draft for a period from the approved time of the members who have a pay rate. The period is " +
			"inclusive. Refused while the period holds unapproved time, and where it overlaps a pay run that exists already. " +
			"Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"start": dateProp("First day of the period"),
			"end":   dateProp("Last day of the period"),
			"notes": stringProp("Notes on the run"),
		}, "start", "end"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Start string `json:"start"`
				End   string `json:"end"`
				Notes string `json:"notes"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			start, end, err := mcpPeriod(c, in.Start, in.End)
			if err != nil {
				return nil, err
			}
			run, overlaps, err := s.services.Payroll.CreateRun(operationContext(c.req), appmodel.PayrollDraftRequest{
				TeamID: c.teamID, CallerID: c.userID, Notes: strings.TrimSpace(in.Notes), Start: start, End: end,
			})
			if err != nil {
				return nil, err
			}
			if len(overlaps) > 0 {
				numbers := make([]string, 0, len(overlaps))
				for _, overlap := range overlaps {
					numbers = append(numbers, overlap.Number)
				}
				return nil, mcpOverlapError{Numbers: numbers}
			}
			return s.mcpPayrollRunNow(c, run.ID)
		},
	},
	{
		Name: "payroll_run_pay",
		Description: "Marks the pay run as paid. Pay out money outside the system is not done by this tool. The payees are sent the " +
			"paid notice, and the run cannot be marked unpaid from the tools. Managers only.",
		Write:  true,
		Schema: objectSchema(map[string]any{"run_id": integerProp("Pay run id")}, "run_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				RunID int64 `json:"run_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.RunID <= 0 {
				return nil, model.ErrNotFound
			}
			if err := s.services.PayrollPaid.MarkPaid(operationContext(c.req), appmodel.PayrollMutationRequest{
				TeamID: c.teamID, RunID: in.RunID, CallerID: c.userID,
			}); err != nil {
				return nil, err
			}
			return s.mcpPayrollRunNow(c, in.RunID)
		},
	},
	{
		Name: "timesheet_week_approve",
		Description: "Approve a finished week of the timesheet, as the timesheet's approve button does. week_start is the first day " +
			"of the week, on the workspace's week start. An approved week locks its time for editing, and reopening it stays in " +
			"the timesheet screen. Managers only.",
		Write:  true,
		Schema: objectSchema(map[string]any{"week_start": dateProp("First day of the week")}, "week_start"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				WeekStart string `json:"week_start"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			first, err := mcpLocalDate("week_start", in.WeekStart, c.now.Location())
			if err != nil {
				return nil, err
			}
			// The page approves whole weeks only, so a day in the middle of one is refused.
			if !startOfWeek(c.req, first).Equal(first) {
				return nil, appmodel.FieldError{Field: "week_start"}
			}
			last := first.AddDate(0, 0, 6)
			if err := s.services.Tracking.Commands.ApproveWeek(c.ctx, appmodel.TimesheetApprovalRequest{
				TeamID: c.teamID, CallerID: c.userID, Start: first, End: first.AddDate(0, 0, 7),
			}); err != nil {
				return nil, err
			}
			return struct {
				WeekStart string `json:"week_start"`
				WeekEnd   string `json:"week_end"`
				Approved  bool   `json:"approved"`
			}{first.Format(time.DateOnly), last.Format(time.DateOnly), true}, nil
		},
	},
}

// errMailNotConfigured refuses a send on a server with no SMTP host. The page offers
// the letter as a file instead; the tool has no such way out, so it says so.
var errMailNotConfigured = errors.New("mail is not configured")

// mcpOverlapError names the pay runs a new period would pay for a second time. The
// page asks the manager to confirm with a reason; the tool has no reason field.
type mcpOverlapError struct {
	Numbers []string
}

func (e mcpOverlapError) Error() string {
	return "period overlaps pay runs " + strings.Join(e.Numbers, ", ")
}

// mcpMoneyErrorText reads a refusal of an invoice, pay run or week workflow in the
// words the page uses for it. The page's own mappers choose the sentence, so the
// wording is not repeated here. An error the page does not name returns false, and
// the dispatcher answers it as an internal error.
func (s *Server) mcpMoneyErrorText(c mcpCall, err error) (string, bool) {
	var unapproved *model.UnapprovedWeeksError
	var notApproved *model.PayrollNotApprovedError
	var overlap mcpOverlapError
	switch {
	case errors.As(err, &unapproved):
		return fmt.Sprintf(i18n.T(c.lang, "inv.unapprovedFlash"), strings.Join(unapprovedWeekLabels(c.lang, unapproved.Weeks), ", ")), true
	case errors.As(err, &overlap):
		return fmt.Sprintf(i18n.T(c.lang, "pay.overlap"), strings.Join(overlap.Numbers, ", ")), true
	case errors.As(err, &notApproved), errors.Is(err, model.ErrNoPayRates), errors.Is(err, model.ErrNoPayableTime):
		return s.payrollErrorMessage(c.req, err), true
	case errors.Is(err, model.ErrNoBillableTime):
		return i18n.T(c.lang, "flash.msg.no billable time in that period"), true
	case errors.Is(err, model.ErrMixedCurrency):
		return i18n.T(c.lang, "flash.msg.mixed currency"), true
	case errors.Is(err, model.ErrAlreadyBilled), errors.Is(err, model.ErrInvoiceNotDraft), errors.Is(err, model.ErrInvoiceChanged):
		return s.invoiceErrorMessage(c.req, err), true
	case errors.Is(err, model.ErrPaymentAmount):
		return i18n.T(c.lang, "inv.paymentTooMuch"), true
	case errors.Is(err, model.ErrPaymentDate):
		return i18n.T(c.lang, "inv.paymentDateBad"), true
	case errors.Is(err, model.ErrInvoiceNotPayable):
		return i18n.T(c.lang, "inv.paymentNotSent"), true
	case errors.Is(err, errMailNotConfigured):
		return i18n.T(c.lang, "inv.mailOff"), true
	case errors.Is(err, appmodel.ErrPayrollRunNotDraft):
		return i18n.T(c.lang, "agent.payrollNotDraft"), true
	case errors.Is(err, appmodel.ErrAgentWeekNotApproved):
		return i18n.T(c.lang, "agent.weekNotApproved"), true
	case errors.Is(err, model.ErrWeekNotFinished), errors.Is(err, model.ErrWeekHasRunningTimer), errors.Is(err, model.ErrWeekPaid):
		return approvalMessage(c.lang, err), true
	}
	return "", false
}

// mcpPeriod reads an inclusive period of two days in the workspace zone, and returns
// it the way the invoice and pay run workflows take it: from the first day to the
// day after the last.
func mcpPeriod(c mcpCall, from, to string) (time.Time, time.Time, error) {
	start, err := mcpLocalDate("start", from, c.now.Location())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	last, err := mcpLocalDate("end", to, c.now.Location())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if last.Before(start) {
		return time.Time{}, time.Time{}, appmodel.FieldError{Field: "end"}
	}
	return start, last.AddDate(0, 0, 1), nil
}

// mcpProjectOption finds a project the invoice page offers, with its saved client.
func (s *Server) mcpProjectOption(c mcpCall, projectID int64) (appmodel.InvoiceProjectOption, error) {
	index, err := s.services.Invoicing.Queries.BuildIndex(c.ctx, appmodel.InvoiceIndexRequest{TeamID: c.teamID})
	if err != nil {
		return appmodel.InvoiceProjectOption{}, err
	}
	for _, option := range index.DraftOptions.Projects {
		if option.Project.ID == projectID {
			return option, nil
		}
	}
	return appmodel.InvoiceProjectOption{}, model.ErrNotFound
}

// mcpInvoiceNow reads an invoice the way the page reads it after a write, so the
// agent gets the status and the totals the write left.
func (s *Server) mcpInvoiceNow(c mcpCall, id int64) (mcpInvoiceView, error) {
	snapshot, err := s.services.InvoiceDocuments.Build(c.ctx, appmodel.InvoiceDocumentRequest{TeamID: c.teamID, InvoiceID: id})
	if err != nil {
		return mcpInvoiceView{}, err
	}
	d := snapshot.Details
	return mcpInvoiceViewOf(d.Invoice, d.TotalCents, d.PaidCents, d.TotalHoursHundredths), nil
}

// mcpPayrollRunNow reads a pay run after a write, with its status and totals.
func (s *Server) mcpPayrollRunNow(c mcpCall, id int64) (mcpPayrollRunView, error) {
	details, err := s.services.Payroll.GetRun(c.ctx, c.teamID, id)
	if err != nil {
		return mcpPayrollRunView{}, err
	}
	return mcpPayrollRunViewOf(details.Run, details.TotalCents, details.TotalHoursHundredths), nil
}

// mcpInvoiceCreated is the draft a call made, with the invoices whose lines overlap it.
// The draft stands either way: the page only warns about the overlap.
type mcpInvoiceCreated struct {
	mcpInvoiceView
	Overlaps []string `json:"overlaps"`
}

type mcpInvoiceSent struct {
	mcpInvoiceView
	Recipient string `json:"recipient"`
}

type mcpInvoicePayment struct {
	mcpInvoiceView
	Settled bool `json:"settled"`
}
