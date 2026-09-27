// Package mail sends transactional mail. The default implementation
// writes to a log sink so local / CI deploys work without SMTP; set
// SMTP_* env vars (or wire a custom Sender) for production delivery.
package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
)

// Sender delivers a single message. Implementations must be safe for
// concurrent use.
type Sender interface {
	Send(to, subject, body string) error
}

// Attachment is a file sent along (an invoice PDF).
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Message is a full letter: plain text (always), optional HTML version
// of the same content, optional files.
type Message struct {
	To, Subject, Text, HTML string
	Files                   []Attachment
}

// RichSender delivers a Message. Both built-in senders implement it.
type RichSender interface {
	Deliver(m Message) error
}

// Configured reports whether real delivery is set up (SMTP host given).
func Configured() bool { return os.Getenv("PARATRACK_SMTP_HOST") != "" }

// LogSender prints the message to the process log. Used when no SMTP
// host is configured — fine for self-hosted / dev, NOT for real SaaS
// delivery (the reset link only lands in the server log).
type LogSender struct{}

func (LogSender) Send(to, subject, body string) error {
	log.Printf("mail → to=%s subject=%q\n%s", to, subject, strings.TrimRight(body, "\n"))
	return nil
}

func (l LogSender) Deliver(m Message) error {
	body := m.Text
	if m.HTML != "" {
		body += fmt.Sprintf("\n[html version, %d bytes]", len(m.HTML))
	}
	for _, f := range m.Files {
		body += fmt.Sprintf("\n[attachment %s, %d bytes]", f.Name, len(f.Data))
	}
	return l.Send(m.To, m.Subject, body)
}

// SMTPSender delivers via an SMTP relay. Auth is skipped when username
// is empty (open relay on localhost, e.g. Postfix).
type SMTPSender struct {
	Host     string // "smtp.example.com:587"
	Username string
	Password string
	From     string // "paratrack@example.com"
}

func (s SMTPSender) Send(to, subject, body string) error {
	return s.Deliver(Message{To: to, Subject: subject, Text: body})
}

// Deliver sends m over SMTP (see buildMessage for the MIME shape).
func (s SMTPSender) Deliver(m Message) error {
	addr := s.Host
	if addr == "" {
		return fmt.Errorf("smtp host not configured")
	}
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, strings.Split(addr, ":")[0])
	}
	return smtp.SendMail(addr, auth, s.From, []string{m.To}, buildMessage(s.From, m))
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

// FromEnv picks SMTP when PARATRACK_SMTP_HOST is set, else the log sink.
// PARATRACK_SMTP_USER / PARATRACK_SMTP_PASS / PARATRACK_MAIL_FROM
// complete the relay config.
func FromEnv() Sender {
	host := os.Getenv("PARATRACK_SMTP_HOST")
	if host == "" {
		return LogSender{}
	}
	return SMTPSender{
		Host:     host,
		Username: os.Getenv("PARATRACK_SMTP_USER"),
		Password: os.Getenv("PARATRACK_SMTP_PASS"),
		From:     orDefault(os.Getenv("PARATRACK_MAIL_FROM"), "paratrack@localhost"),
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
