package web

import (
	"bytes"
	"fmt"
)

// executeTemplate renders into memory so callers can decide the response
// status before committing headers or a partial body to the client.
func (s *Server) executeTemplate(name string, data templateData) ([]byte, error) {
	var output bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&output, name, data); err != nil {
		return nil, fmt.Errorf("render template %q: %w", name, err)
	}
	return output.Bytes(), nil
}
