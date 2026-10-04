package httpjson

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeEnforcesLimitAndCompleteJSON(t *testing.T) {
	t.Run("accepts payload at limit", func(t *testing.T) {
		var value map[string]int
		if err := Decode(strings.NewReader(`{"x":1}`), 7, &value); err != nil {
			t.Fatal(err)
		}
		if value["x"] != 1 {
			t.Fatalf("decoded value = %v", value)
		}
	})

	t.Run("rejects oversized payload", func(t *testing.T) {
		var value map[string]int
		err := Decode(strings.NewReader(`{"x":1} `), 7, &value)
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("Decode error = %v, want ErrPayloadTooLarge", err)
		}
	})

	t.Run("rejects trailing JSON values", func(t *testing.T) {
		var value map[string]int
		if err := Decode(strings.NewReader(`{"x":1} {"y":2}`), 32, &value); err == nil {
			t.Fatal("Decode accepted multiple top-level JSON values")
		}
	})
}
