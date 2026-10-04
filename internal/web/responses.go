package web

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type jsonResponse interface {
	isJSONResponse()
}

// writeInternalError keeps infrastructure details in server logs while
// returning a stable response that does not expose SQL/driver internals.
func (s *Server) writeInternalError(w http.ResponseWriter, err error) {
	s.writeInternalErrorMessage(w, err, "internal server error")
}

func (s *Server) writeInternalErrorMessage(w http.ResponseWriter, err error, message string) {
	s.logInternalError(err)
	http.Error(w, message, http.StatusInternalServerError)
}

func (s *Server) logInternalError(err error) {
	if err != nil {
		s.logger.Printf("web: internal request error: %v", err)
	}
}

func (s *Server) writeInternalJSONError(w http.ResponseWriter, err error) {
	if err != nil {
		s.logger.Printf("web: internal request error: %v", err)
	}
	s.writeJSONStatus(w, http.StatusInternalServerError, apiErrorResponse{Error: "internal server error"})
}

// isHTMX reports whether the request came from an HTMX swap target.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// writeJSONStatus sends v with a non-200 status. The header must go
// before the body, or the client sees 200.
func (s *Server) writeJSONStatus(w http.ResponseWriter, code int, v jsonResponse) {
	s.writeJSONResponse(w, "application/json; charset=utf-8", code, v, false)
}

func (s *Server) writeJSON(w http.ResponseWriter, v jsonResponse) {
	s.writeJSONContentTypeIndented(w, "application/json; charset=utf-8", v)
}

func (s *Server) writeJSONContentType(w http.ResponseWriter, contentType string, v jsonResponse) {
	s.writeJSONResponse(w, contentType, http.StatusOK, v, false)
}

func (s *Server) writeJSONContentTypeIndented(w http.ResponseWriter, contentType string, v jsonResponse) {
	s.writeJSONResponse(w, contentType, http.StatusOK, v, true)
}

func (s *Server) writeJSONResponse(w http.ResponseWriter, contentType string, status int, v jsonResponse, indent bool) {
	body, err := encodeJSON(v, indent)
	if err != nil {
		s.logInternalError(fmt.Errorf("encode JSON response: %w", err))
		status = http.StatusInternalServerError
		body = []byte("{\"error\":\"internal server error\"}\n")
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func encodeJSON(v jsonResponse, indent bool) ([]byte, error) {
	var (
		body []byte
		err  error
	)
	if indent {
		body, err = json.MarshalIndent(v, "", "  ")
	} else {
		body, err = json.Marshal(v)
	}
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}
