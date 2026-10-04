// Package mail sends transactional mail. The default implementation
// writes to a log sink so local / CI deploys work without SMTP. The process
// composition root converts SMTP settings into a Sender, or may inject a
// custom Sender for another delivery mechanism.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/mailport"
)

type Sender = mailport.Sender
type Attachment = mailport.Attachment
type Message = mailport.Message
type RichSender = mailport.RichSender

// Available reports whether the injected sender can deliver real messages.
// Custom senders are considered available unless they expose an Available
// method, keeping transport behavior aligned with the dependency it received.
func Available(sender Sender) bool {
	return mailport.Available(sender)
}

// LogSender prints the message to the process log. Used when no SMTP
// host is configured — fine for self-hosted / dev, NOT for real SaaS
// delivery (the reset link only lands in the server log).
type LogSender struct {
	Logger *log.Logger
}

func (LogSender) Available() bool { return false }

func (s LogSender) Send(_ context.Context, to, subject, body string) error {
	if s.Logger == nil {
		return fmt.Errorf("log mail sender requires a logger")
	}
	s.Logger.Printf("mail → to=%s subject=%q\n%s", to, subject, strings.TrimRight(body, "\n"))
	return nil
}

func (l LogSender) Deliver(ctx context.Context, m Message) error {
	body := m.Text
	if m.HTML != "" {
		body += fmt.Sprintf("\n[html version, %d bytes]", len(m.HTML))
	}
	for _, f := range m.Files {
		body += fmt.Sprintf("\n[attachment %s, %d bytes]", f.Name, len(f.Data))
	}
	return l.Send(ctx, m.To, m.Subject, body)
}

// SMTPSender delivers via an SMTP relay. Auth is skipped when username
// is empty (open relay on localhost, e.g. Postfix).
type SMTPSender struct {
	Host     string // "smtp.example.com:587"
	Username string
	Password string
	From     string // "paratrack@example.com"
}

func (s SMTPSender) Available() bool { return s.Host != "" }

func (s SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	return s.Deliver(ctx, Message{To: to, Subject: subject, Text: body})
}

// Deliver sends m over SMTP (see buildMessage for the MIME shape).
func (s SMTPSender) Deliver(ctx context.Context, m Message) error {
	if err := validateMessageHeaders(s.From, m); err != nil {
		return err
	}
	if ctx == nil {
		return ErrNilDeliveryContext
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	addr := s.Host
	if addr == "" {
		return fmt.Errorf("smtp host not configured")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("smtp address %q must include a port: %w", addr, err)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, host)
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(s.From); err != nil {
		return err
	}
	if err := c.Rcpt(m.To); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, bytes.NewReader(buildMessage(s.From, m))); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

var ErrNilDeliveryContext = errors.New("mail delivery context is nil")

// validateMessageHeaders rejects line breaks in values that are written into
// SMTP or MIME headers. Message producers are separate workflows, so the
// transport adapter enforces this boundary even when a future producer accepts
// less-trusted attachment metadata.
func validateMessageHeaders(from string, m Message) error {
	if strings.ContainsAny(from, "\r\n") || strings.ContainsAny(m.To, "\r\n") || strings.ContainsAny(m.Subject, "\r\n") {
		return fmt.Errorf("mail headers must not contain line breaks")
	}
	for i, file := range m.Files {
		if strings.ContainsAny(file.Name, "\r\n") || strings.ContainsAny(file.ContentType, "\r\n") {
			return fmt.Errorf("mail attachment %d headers must not contain line breaks", i)
		}
	}
	return nil
}

// buildMessage is the MIME tree:
//
//	multipart/mixed            (only when there are files)
//	├─ multipart/alternative   (only when there is HTML)
//	│  ├─ text/plain
//	│  └─ text/html
//	└─ files…
//
// Text parts are UTF-8 base64; the subject is RFC 2047 so Cyrillic survives.
func buildMessage(from string, m Message) []byte {
	var buf bytes.Buffer
	head := func(k, v string) { buf.WriteString(k + ": " + v + "\r\n") }
	head("From", from)
	head("To", m.To)
	head("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	head("MIME-Version", "1.0")
	text := func(ct, body string) (textproto.MIMEHeader, []byte) {
		return textproto.MIMEHeader{"Content-Type": {ct + "; charset=UTF-8"}, "Content-Transfer-Encoding": {"base64"}},
			[]byte(wrap64(base64.StdEncoding.EncodeToString([]byte(body))))
	}
	// body writes the text (or text+html alternative) into w.
	body := func(w *multipart.Writer) {
		if m.HTML == "" {
			h, b := text("text/plain", m.Text)
			p, _ := w.CreatePart(h)
			p.Write(b)
			return
		}
		var alt bytes.Buffer
		aw := multipart.NewWriter(&alt)
		for _, part := range [][2]string{{"text/plain", m.Text}, {"text/html", m.HTML}} {
			h, b := text(part[0], part[1])
			p, _ := aw.CreatePart(h)
			p.Write(b)
		}
		aw.Close()
		p, _ := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"multipart/alternative; boundary=" + aw.Boundary()}})
		p.Write(alt.Bytes())
	}
	switch {
	case len(m.Files) == 0 && m.HTML == "":
		h, b := text("text/plain", m.Text)
		for k, v := range h {
			head(k, v[0])
		}
		buf.WriteString("\r\n")
		buf.Write(b)
	case len(m.Files) == 0:
		var alt bytes.Buffer
		aw := multipart.NewWriter(&alt)
		for _, part := range [][2]string{{"text/plain", m.Text}, {"text/html", m.HTML}} {
			h, b := text(part[0], part[1])
			p, _ := aw.CreatePart(h)
			p.Write(b)
		}
		aw.Close()
		head("Content-Type", "multipart/alternative; boundary="+aw.Boundary())
		buf.WriteString("\r\n")
		buf.Write(alt.Bytes())
	default:
		mw := multipart.NewWriter(&buf)
		head("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		buf.WriteString("\r\n")
		body(mw)
		for _, f := range m.Files {
			ct := f.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			name := mime.QEncoding.Encode("utf-8", f.Name)
			fp, _ := mw.CreatePart(textproto.MIMEHeader{
				"Content-Type":              {ct + `; name="` + name + `"`},
				"Content-Disposition":       {`attachment; filename="` + name + `"`},
				"Content-Transfer-Encoding": {"base64"},
			})
			fp.Write([]byte(wrap64(base64.StdEncoding.EncodeToString(f.Data))))
		}
		mw.Close()
	}
	return buf.Bytes()
}

// wrap64 breaks base64 into 76-char lines (RFC 2045).
func wrap64(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s + "\r\n")
	return b.String()
}

// SMTPConfig holds the process-wide SMTP relay settings.
type SMTPConfig struct {
	Host     string
	Username string
	Password string
	From     string
}

// NewSender selects SMTP when configured, otherwise it uses the log sink.
var ErrMissingLogger = errors.New("mail log sender requires a logger")

func NewSender(config SMTPConfig, logger *log.Logger) (Sender, error) {
	if config.Host == "" {
		if logger == nil {
			return nil, ErrMissingLogger
		}
		return LogSender{Logger: logger}, nil
	}
	return SMTPSender{
		Host:     config.Host,
		Username: config.Username,
		Password: config.Password,
		From:     orDefault(config.From, "paratrack@localhost"),
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
