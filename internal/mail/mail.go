// Package mail sends transactional mail. The default implementation
// writes to a log sink so local / CI deploys work without SMTP; set
// SMTP_* env vars (or wire a custom Sender) for production delivery.
package mail

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strings"
)

// Sender delivers a single message. Implementations must be safe for
// concurrent use.
type Sender interface {
	Send(to, subject, body string) error
}

// LogSender prints the message to the process log. Used when no SMTP
// host is configured — fine for self-hosted / dev, NOT for real SaaS
// delivery (the reset link only lands in the server log).
type LogSender struct{}

func (LogSender) Send(to, subject, body string) error {
	log.Printf("mail → to=%s subject=%q\n%s", to, subject, strings.TrimRight(body, "\n"))
	return nil
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
	addr := s.Host
	if addr == "" {
		return fmt.Errorf("smtp host not configured")
	}
	msg := strings.Join([]string{
		"From: " + s.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, strings.Split(addr, ":")[0])
	}
	return smtp.SendMail(addr, auth, s.From, []string{to}, []byte(msg))
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
