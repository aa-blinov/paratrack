package httpurl

import (
	"errors"
	"testing"
)

func TestResolveSameOrigin(t *testing.T) {
	got, err := ResolveSameOrigin("https://api.example.test/v1/items?page=1", "../items?page=2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.test/items?page=2" {
		t.Fatalf("resolved URL = %q", got)
	}
}

func TestResolveSameOriginRejectsCredentialExfiltrationURLs(t *testing.T) {
	for _, reference := range []string{
		"https://attacker.example.test/collect",
		"//attacker.example.test/collect",
		"https://user@api.example.test/collect",
		"https://api.example.test/collect#fragment",
	} {
		t.Run(reference, func(t *testing.T) {
			if _, err := ResolveSameOrigin("https://api.example.test/v1/items", reference); !errors.Is(err, ErrUnsafeURL) {
				t.Fatalf("ResolveSameOrigin error = %v, want ErrUnsafeURL", err)
			}
		})
	}
}
