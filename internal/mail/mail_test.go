package mail

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

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
