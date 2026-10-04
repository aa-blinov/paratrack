package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestParseFormErrorResponsesAreStable(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "raw parse error",
			body: `func handler(w http.ResponseWriter, r *http.Request) {
				if parseErr := r.ParseForm(); parseErr != nil { http.Error(w, parseErr.Error(), 400) }
			}`,
			wantErr: true,
		},
		{
			name: "stable response",
			body: `func handler(w http.ResponseWriter, r *http.Request) {
				if parseErr := r.ParseForm(); parseErr != nil { http.Error(w, "invalid form", 400) }
			}`,
		},
		{
			name: "domain error remains available",
			body: `func handler(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil { http.Error(w, "invalid form", 400) }
				if err := validate(); err != nil { http.Error(w, err.Error(), 400) }
			}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package web\n"+testCase.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			err = validateParseFormErrorExposure("fixture.go", file)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validateParseFormErrorExposure() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(err.Error(), "stable invalid-form response") {
				t.Fatalf("error = %v, want stable-response guidance", err)
			}
		})
	}
}

func TestJSONResponsesUseExplicitDTOs(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "inline map",
			body:    `func handler(w http.ResponseWriter) { writeJSON(w, map[string]string{"error": "bad request"}) }`,
			wantErr: true,
		},
		{
			name:    "inline map through response receiver",
			body:    `func (s *Server) handler(w http.ResponseWriter) { s.writeJSON(w, map[string]string{"error": "bad request"}) }`,
			wantErr: true,
		},
		{
			name:    "inline status map",
			body:    `func handler(w http.ResponseWriter) { writeJSONStatus(w, 400, map[string]any{"error": "bad request"}) }`,
			wantErr: true,
		},
		{
			name:    "inline custom content type map",
			body:    `func handler(w http.ResponseWriter) { writeJSONContentType(w, "application/manifest+json", map[string]any{"name": "app"}) }`,
			wantErr: true,
		},
		{
			name:    "inline serialized map",
			body:    `func encode() { _, _ = json.Marshal(map[string]string{"target": "board"}) }`,
			wantErr: true,
		},
		{
			name:    "ignored marshal error",
			body:    `func encode(value response) { data, _ := json.Marshal(value); _ = data }`,
			wantErr: true,
		},
		{
			name: "unused encoded bytes with handled error",
			body: `func encode(value response) error { _, err := json.Marshal(value); return err }`,
		},
		{
			name: "named DTO",
			body: `type response struct { Error string ` + "`json:\"error\"`" + ` }
			func handler(w http.ResponseWriter) { writeJSON(w, response{Error: "bad request"}) }`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package web\n"+testCase.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			err = validateJSONSerializationDTOs("fixture.go", file)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validateJSONSerializationDTOs() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}
