package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
)

// The offline queue sends an Idempotency-Key with every replayed write. A write
// that finished under that key is answered with its first status and not run
// again, so a request whose response was lost in transit cannot apply twice.
const (
	idempotencyKeyHeader    = "Idempotency-Key"
	maxIdempotencyKeyLength = 128
)

// IdempotencyStore keeps the outcome of writes that carry an Idempotency-Key.
type IdempotencyStore interface {
	ClaimIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest) (bool, int, error)
	FinishIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest) error
	ReleaseIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest) error
}

// idempotent wraps a write handler. Without the header, or without a store, the
// handler runs as before. Call it after authentication: the key belongs to the user.
func (s *Server) idempotent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get(idempotencyKeyHeader))
		if key == "" || s.services.Idempotency == nil {
			next(w, r)
			return
		}
		if len(key) > maxIdempotencyKeyLength {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid idempotency key"})
			return
		}
		userID := authenticatedUserID(r)
		request := appmodel.IdempotencyRequest{UserID: userID, Key: key, Now: s.config.Now()}
		done, status, err := s.services.Idempotency.ClaimIdempotencyKey(r.Context(), request)
		if err != nil {
			if errors.Is(err, model.ErrRequestInProgress) {
				s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: "request is already in progress"})
				return
			}
			s.writeInternalJSONError(w, err)
			return
		}
		if done {
			w.WriteHeader(status)
			return
		}
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(recorder, r)
		// The write is settled once it succeeded or redirected. The key then stays
		// with its status. A refusal releases the key, so a corrected retry runs.
		if recorder.status < http.StatusBadRequest {
			request.Status = recorder.status
			s.settleIdempotencyKey(r.Context(), request, s.services.Idempotency.FinishIdempotencyKey)
			return
		}
		s.settleIdempotencyKey(r.Context(), request, func(ctx context.Context, request appmodel.IdempotencyRequest) error {
			return s.services.Idempotency.ReleaseIdempotencyKey(ctx, request)
		})
	}
}

// settleIdempotencyKey records the outcome after the response is out. The client
// may already have gone, so the write runs detached from the request's cancellation.
func (s *Server) settleIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest, settle func(context.Context, appmodel.IdempotencyRequest) error) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := settle(effectCtx, request); err != nil {
		s.logInternalError(err)
	}
}
