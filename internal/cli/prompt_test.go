package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestPromptReturnsTrimmedInput(t *testing.T) {
	r := strings.NewReader("  hello world  \n")
	var w strings.Builder
	got, err := Prompt(r, &w, "Name", "")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
	if !strings.Contains(w.String(), "Name:") {
		t.Fatalf("missing label in prompt: %q", w.String())
	}
}

func TestPromptReturnsDefault(t *testing.T) {
	r := strings.NewReader("\n") // empty reply
	var w strings.Builder
	got, err := Prompt(r, &w, "Activity", "reading")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got != "reading" {
		t.Fatalf("got %q, want default %q", got, "reading")
	}
	if !strings.Contains(w.String(), "[reading]") {
		t.Fatalf("missing default in prompt: %q", w.String())
	}
}

func TestPromptCancelsOnDot(t *testing.T) {
	r := strings.NewReader(".\n")
	var w strings.Builder
	_, err := Prompt(r, &w, "Activity", "")
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %v, want ErrCancelled", err)
	}
}


func TestConfirmYes(t *testing.T) {
	r := strings.NewReader("y\n")
	var w strings.Builder
	got, err := Confirm(r, &w, "Really?", false)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !got {
		t.Fatal("expected yes")
	}
}

func TestConfirmNo(t *testing.T) {
	r := strings.NewReader("n\n")
	var w strings.Builder
	got, err := Confirm(r, &w, "Really?", true)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if got {
		t.Fatal("expected no")
	}
}

func TestConfirmDefault(t *testing.T) {
	r := strings.NewReader("\n")
	var w strings.Builder
	got, err := Confirm(r, &w, "Delete?", true)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !got {
		t.Fatal("empty reply should accept default-yes")
	}
}

func TestChooseDefault(t *testing.T) {
	r := strings.NewReader("\n")
	var w strings.Builder
	options := []string{"a", "b", "c"}
	got, err := Choose(r, &w, "Pick", options, 1)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got != 1 {
		t.Fatalf("got %d, want default 1", got)
	}
}

func TestChooseByNumber(t *testing.T) {
	r := strings.NewReader("3\n")
	var w strings.Builder
	options := []string{"a", "b", "c"}
	got, err := Choose(r, &w, "Pick", options, 0)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}


