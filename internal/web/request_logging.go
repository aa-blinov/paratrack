package web

import (
	"log"
	"net/http"
	"time"
)

// logRequests records the matched route pattern rather than the raw URL path,
// which can contain invitation tokens or other caller-provided secrets.
func logRequests(next http.Handler, logger *log.Logger, routes *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		_, route := routes.Handler(r)
		if route == "" {
			route = "<unmatched>"
		}
		ww := &statusRecorder{ResponseWriter: w, status: 200}
		defer func() {
			if recovered := recover(); recovered != nil {
				if !ww.wroteHeader {
					ww.status = http.StatusInternalServerError
				}
				logger.Printf("%s %s %d %s", r.Method, route, ww.status, time.Since(start))
				panic(recovered)
			}
			logger.Printf("%s %s %d %s", r.Method, route, ww.status, time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		s.ResponseWriter.WriteHeader(code)
		return
	}
	if s.wroteHeader {
		return
	}
	s.wroteHeader = true
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

// Unwrap preserves optional ResponseWriter capabilities for ResponseController.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
