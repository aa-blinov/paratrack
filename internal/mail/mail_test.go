package mail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

type availableTestSender struct{}

func (availableTestSender) Send(context.Context, string, string, string) error { return nil }

func TestAvailableUsesInjectedSender(t *testing.T) {
	if Available(nil) {
		t.Fatal("nil sender is available")
	}
	if Available(LogSender{}) {
		t.Fatal("log sender should not report external delivery")
	}
	if !Available(SMTPSender{Host: "smtp.example.test:587"}) {
		t.Fatal("configured SMTP sender should be available")
	}
	if !Available(availableTestSender{}) {
		t.Fatal("custom sender should be available by default")
	}
}

func TestLogSenderRequiresAndUsesInjectedLogger(t *testing.T) {
	if _, err := NewSender(SMTPConfig{}, nil); !errors.Is(err, ErrMissingLogger) {
		t.Fatalf("NewSender error = %v, want %v", err, ErrMissingLogger)
	}
	if err := (LogSender{}).Send(context.Background(), "a@example.test", "subject", "body"); err == nil {
		t.Fatal("zero-value log sender unexpectedly accepted a message")
	}

	var output bytes.Buffer
	sender, err := NewSender(SMTPConfig{}, log.New(&output, "", 0))
	if err != nil {
		t.Fatalf("construct log sender: %v", err)
	}
	if err := sender.Send(context.Background(), "a@example.test", "subject", "body"); err != nil {
		t.Fatalf("send through log sender: %v", err)
	}
	if !strings.Contains(output.String(), "a@example.test") || !strings.Contains(output.String(), "body") {
		t.Fatalf("injected logger output = %q, want message details", output.String())
	}
}

func TestBuildMessageCyrillicAndAttachment(t *testing.T) {
	raw := buildMessage("a@x.t", Message{To: "b@x.t", Subject: "Счёт INV-2026-001", Text: "Здравствуйте!", HTML: "<p>Здравствуйте!</p>",
		Files: []Attachment{{Name: "INV-2026-001.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4")}}})
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if subj != "Счёт INV-2026-001" {
		t.Errorf("subject %q", subj)
	}
	_, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	mr := multipart.NewReader(m.Body, params["boundary"])
	var parts []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		parts = append(parts, p.Header.Get("Content-Type")+"|"+strings.TrimSpace(string(b)))
	}
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "multipart/alternative") || !strings.HasPrefix(parts[1], "application/pdf") {
		t.Fatalf("parts %v", parts)
	}
	if !strings.Contains(parts[0], "text/html") || !strings.Contains(parts[0], "text/plain") {
		t.Errorf("alternative part lacks text or html: %q", parts[0][:min(len(parts[0]), 200)])
	}
}

func TestValidateMessageHeadersRejectsLineBreaks(t *testing.T) {
	tests := []struct {
		name string
		from string
		msg  Message
	}{
		{name: "sender", from: "sender@example.test\r\nBcc: victim@example.test", msg: Message{To: "to@example.test"}},
		{name: "recipient", from: "sender@example.test", msg: Message{To: "to@example.test\nBcc: victim@example.test"}},
		{name: "subject", from: "sender@example.test", msg: Message{To: "to@example.test", Subject: "hello\r\nBcc: victim@example.test"}},
		{name: "attachment name", from: "sender@example.test", msg: Message{To: "to@example.test", Files: []Attachment{{Name: "invoice.pdf\nX-Test: injected"}}}},
		{name: "attachment content type", from: "sender@example.test", msg: Message{To: "to@example.test", Files: []Attachment{{ContentType: "application/pdf\r\nX-Test: injected"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateMessageHeaders(tt.from, tt.msg); err == nil {
				t.Fatal("header line break was accepted")
			}
		})
	}
	if err := validateMessageHeaders("sender@example.test", Message{
		To: "to@example.test", Subject: "invoice", Files: []Attachment{{Name: "invoice.pdf", ContentType: "application/pdf"}},
	}); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
}
