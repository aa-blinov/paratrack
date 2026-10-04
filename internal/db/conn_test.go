package db

import (
	"testing"
	"time"
)

func TestCurrentTimeUsesConfiguredClock(t *testing.T) {
	want := time.Date(2026, time.October, 2, 12, 30, 0, 0, time.UTC)
	database := &DB{now: func() time.Time { return want }}
	if got := database.currentTime(); !got.Equal(want) {
		t.Fatalf("database clock = %s, want %s", got, want)
	}
}

func TestRebind(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "binds parameters around SQL literals",
			query: "SELECT ?, '?', E'\\'?\\'', \"?\", \"a\"\"?\" FROM t WHERE id = ?",
			want:  "SELECT $1, '?', E'\\'?\\'', \"?\", \"a\"\"?\" FROM t WHERE id = $2",
		},
		{
			name:  "leaves line and nested block comments intact",
			query: "SELECT ? -- ?\n/* outer ? /* inner ? */ ? */ WHERE id = ?",
			want:  "SELECT $1 -- ?\n/* outer ? /* inner ? */ ? */ WHERE id = $2",
		},
		{
			name:  "leaves tagged and untagged dollar strings intact",
			query: "SELECT $$?$$, $body$ ? $body$, ?",
			want:  "SELECT $$?$$, $body$ ? $body$, $1",
		},
		{
			name:  "does not treat identifier suffixes as dollar strings",
			query: "SELECT column$tag$ + ? FROM records",
			want:  "SELECT column$tag$ + $1 FROM records",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rebind(tt.query); got != tt.want {
				t.Fatalf("rebind() = %q, want %q", got, tt.want)
			}
		})
	}
}
