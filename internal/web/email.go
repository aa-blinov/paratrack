package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/postcommit"
)

// emailVM is one letter, rendered twice: templates/email.html for HTML
// clients and emailText for plain ones (both carry the same content).
type emailVM struct {
	Lang        string
	Preheader   string // inbox preview line
	Heading     string
	Paras       []string
	AmountLabel string
	Amount      string // big figure (invoices)
	Facts       []emailFact
	ButtonLabel string
	ButtonURL   string
	ButtonHint  string // "or open:" before the raw link
	After       []string
	Footer      string
}

func (emailVM) isTemplateData() {}

type emailFact struct {
	Key, Value string
	Mono       bool
}

func (s *Server) buildEmail(to, subject string, vm emailVM, files ...mailport.Attachment) (mailport.Message, error) {
	html, err := s.executeTemplate("email", vm)
	if err != nil {
		return mailport.Message{}, err
	}
	return mailport.Message{To: to, Subject: subject, Text: emailText(vm), HTML: string(html), Files: files}, nil
}

// humanTTL is a localized duration for an email.
func humanTTL(lang i18n.Lang, d time.Duration) string {
	return fmtDurL(lang, int(d.Seconds()))
}

// deliver sends a rich letter, falling back to plain text for a sender
// that can't do more.
func (s *Server) deliver(ctx context.Context, m mailport.Message) error {
	if rs, ok := s.runtime.Mailer.(mailport.RichSender); ok {
		return rs.Deliver(ctx, m)
	}
	return s.runtime.Mailer.Send(ctx, m.To, m.Subject, m.Text)
}

// deliverPostcommit lets a short SMTP attempt finish when the HTTP client
// disconnects after the application has already committed its state change.
func (s *Server) deliverPostcommit(ctx context.Context, message mailport.Message) error {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	return s.deliver(effectCtx, message)
}

func emailText(vm emailVM) string {
	var b strings.Builder
	b.WriteString(vm.Heading + "\n\n")
	for _, p := range vm.Paras {
		b.WriteString(p + "\n\n")
	}
	if vm.Amount != "" {
		b.WriteString(vm.AmountLabel + ": " + vm.Amount + "\n")
	}
	for _, f := range vm.Facts {
		b.WriteString(f.Key + ": " + f.Value + "\n")
	}
	if vm.Amount != "" || len(vm.Facts) > 0 {
		b.WriteString("\n")
	}
	if vm.ButtonURL != "" {
		b.WriteString(vm.ButtonLabel + ": " + vm.ButtonURL + "\n\n")
	}
	for _, p := range vm.After {
		b.WriteString(p + "\n\n")
	}
	b.WriteString("— paratrack\n")
	return b.String()
}

// invoiceEmail is the letter that carries an invoice PDF to the client.
func invoiceEmail(lang i18n.Lang, vm invoiceVM, seller string) (string, emailVM) {
	T := func(k string) string { return i18n.T(lang, k) }
	subj := T("inv.invoice") + " " + vm.Number + ", " + seller
	ev := emailVM{
		Lang:        string(lang),
		Preheader:   fmt.Sprintf(T("mail.inv.pre"), vm.Number, vm.Total),
		Heading:     T("inv.invoice") + " " + vm.Number,
		Paras:       []string{T("mail.inv.hello"), fmt.Sprintf(T("mail.inv.body"), seller)},
		AmountLabel: T("mail.inv.amount"),
		Amount:      vm.Total,
		Facts: []emailFact{
			{Key: T("inv.period"), Value: vm.PeriodLabel},
			{Key: T("inv.hours"), Value: vm.Hours, Mono: true},
			{Key: T("pdf.from"), Value: seller},
			{Key: T("pdf.billedTo"), Value: vm.ClientName},
		},
		After:  []string{T("mail.inv.pdf")},
		Footer: fmt.Sprintf(T("mail.footer.inv"), seller),
	}
	if vm.VATNote != "" {
		ev.Facts = append(ev.Facts, emailFact{Key: T("mail.inv.vat"), Value: vm.VATNote})
	}
	if vm.PaymentURL != "" {
		ev.ButtonLabel, ev.ButtonURL, ev.ButtonHint = T("inv.payOnline"), vm.PaymentURL, T("mail.orOpen")
	}
	return subj, ev
}

func resetEmail(lang i18n.Lang, name, email, link, ttl string) (string, emailVM) {
	T := func(k string) string { return i18n.T(lang, k) }
	return T("mail.reset.subject"), emailVM{
		Lang:        string(lang),
		Preheader:   T("mail.reset.pre"),
		Heading:     T("mail.reset.heading"),
		Paras:       []string{fmt.Sprintf(T("mail.hi"), name), fmt.Sprintf(T("mail.reset.body"), email)},
		ButtonLabel: T("mail.reset.button"), ButtonURL: link, ButtonHint: T("mail.orOpen"),
		After:  []string{fmt.Sprintf(T("mail.reset.ttl"), ttl), T("mail.reset.ignore")},
		Footer: T("mail.footer"),
	}
}

func inviteEmail(lang i18n.Lang, inviter, team, link string) (string, emailVM) {
	T := func(k string) string { return i18n.T(lang, k) }
	return fmt.Sprintf(T("mail.invite.subject"), team), emailVM{
		Lang:        string(lang),
		Preheader:   fmt.Sprintf(T("mail.invite.pre"), inviter, team),
		Heading:     fmt.Sprintf(T("mail.invite.heading"), team),
		Paras:       []string{fmt.Sprintf(T("mail.invite.body"), inviter, team), T("mail.invite.what")},
		ButtonLabel: T("mail.invite.button"), ButtonURL: link, ButtonHint: T("mail.orOpen"),
		After:  []string{T("mail.invite.ignore")},
		Footer: T("mail.footer"),
	}
}

// handleEmailPreview shows each letter as a mail client would, with
// sample data, so the templates can be checked without SMTP.
// GET /settings/email-preview?kind=invoice|reset|invite[&text=1]
func (s *Server) handleEmailPreview(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	var ev emailVM
	switch r.URL.Query().Get("kind") {
	case "reset":
		_, ev = resetEmail(lang, "Анна", "anna@example.ru", s.publicBaseURL(r)+"/reset-password?token=sample", humanTTL(lang, time.Hour))
	case "invite":
		_, ev = inviteEmail(lang, "Анна Фрилансер", "Студия Ромашка", s.publicBaseURL(r)+"/invites/sample")
	default:
		_, ev = invoiceEmail(lang, invoiceVM{
			Number: "INV-2026-001", PeriodLabel: "1 сен 2026 – 27 сен 2026", Hours: fmtHoursL(lang, 350),
			Total: moneyL(lang, 875175, "RUB"), ClientName: "ООО «Ромашка»", VATNote: "НДС не облагается (УСН)",
			PaymentURL: "https://pay.example.ru/inv-2026-001",
		}, "Анна Фрилансер")
	}
	if r.URL.Query().Get("text") == "1" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, emailText(ev))
		return
	}
	html, err := s.executeTemplate("email", ev)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(html); err != nil {
		s.logger.Printf("web: write email preview: %v", err)
	}
}
