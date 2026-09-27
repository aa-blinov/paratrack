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

// AttachSender can send files too. Both built-in senders implement it.
type AttachSender interface {
	SendWith(to, subject, body string, files ...Attachment) error
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

func (l LogSender) SendWith(to, subject, body string, files ...Attachment) error {
	for _, f := range files {
		body += fmt.Sprintf("\n[attachment %s, %d bytes]", f.Name, len(f.Data))
	}
	return l.Send(to, subject, body)
}

// SMTPSender delivers via an SMTP relay. Auth is skipped when username
// is empty (open relay on localhost, e.g. Postfix).
type SMTPSender struct {
	Host     string // "smtp.example.com:587"
	Username string
	Password string
	From     string // "paratrack@example.com"
}

func (s SMTPSender) Send(to, subject, body string) error { return s.SendWith(to, subject, body) }

// SendWith builds a MIME message: UTF-8 text, RFC 2047 subject (Cyrillic
// survives), files as base64 parts.
func (s SMTPSender) SendWith(to, subject, body string, files ...Attachment) error {
	addr := s.Host
	if addr == "" {
		return fmt.Errorf("smtp host not configured")
	}
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, strings.Split(addr, ":")[0])
	}
	return smtp.SendMail(addr, auth, s.From, []string{to}, buildMessage(s.From, to, subject, body, files))
}

func buildMessage(from, to, subject, body string, files []Attachment) []byte {
	var buf bytes.Buffer
	head := func(k, v string) { buf.WriteString(k + ": " + v + "\r\n") }
	head("From", from)
	head("To", to)
	head("Subject", mime.QEncoding.Encode("utf-8", subject))
	head("MIME-Version", "1.0")
	if len(files) == 0 {
		head("Content-Type", "text/plain; charset=UTF-8")
		head("Content-Transfer-Encoding", "base64")
		buf.WriteString("\r\n" + wrap64(base64.StdEncoding.EncodeToString([]byte(body))))
	} else {
		mw := multipart.NewWriter(&buf)
		head("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		buf.WriteString("\r\n")
		tp, _ := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": {"text/plain; charset=UTF-8"}, "Content-Transfer-Encoding": {"base64"}})
		tp.Write([]byte(wrap64(base64.StdEncoding.EncodeToString([]byte(body)))))
		for _, f := range files {
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
