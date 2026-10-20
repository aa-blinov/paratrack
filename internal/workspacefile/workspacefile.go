// Package workspacefile owns the workspace export file: how it is written, and
// how a reader decides it can take a file in. The data comes from the database
// layer through Store, so the file format stays apart from SQL.
package workspacefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ErrUnsupportedFile means the file is not a paratrack workspace export this
// version of the server can read.
var ErrUnsupportedFile = appmodel.ErrUnsupportedWorkspaceFile

// Store reads one workspace's export and writes a restored workspace. Both move
// records one at a time through a sink, so neither side holds a whole workspace.
type Store interface {
	ExportWorkspace(ctx context.Context, request appmodel.WorkspaceExportRequest) error
	ResolveWorkspaceEmails(ctx context.Context, emails []string) (map[string]int64, []string, error)
	RestoreWorkspace(ctx context.Context, request appmodel.WorkspaceRestoreRequest) (appmodel.WorkspaceRestoreResult, error)
}

// Service writes and reads workspace export files.
type Service struct {
	store Store
}

// New builds the export service around a store.
func New(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workspace export store is required")
	}
	return &Service{store: store}, nil
}

// Export streams the workspace to w as one file. begin hears the team's slug and
// the export time before the first byte goes out, so the caller can set the
// download's headers. A refused export never calls begin and writes nothing.
func (s *Service) Export(ctx context.Context, request appmodel.WorkspaceExportRequest, w io.Writer, begin func(slug string, exportedAt time.Time)) error {
	if request.TeamID <= 0 {
		return model.ErrNotFound
	}
	out := newFileWriter(w, begin)
	request.Sink = out
	if err := s.store.ExportWorkspace(ctx, request); err != nil {
		return err
	}
	return out.Close()
}

// Restore creates a workspace the caller owns from an export read from body. The
// body is read twice, once to learn which people the file names and once to write
// it, so it is first kept in a temporary file. Nothing is written unless every
// person it names already has an account.
func (s *Service) Restore(ctx context.Context, callerID int64, body io.Reader) (appmodel.WorkspaceRestoreResult, error) {
	spool, err := os.CreateTemp("", "paratrack-restore-*.json")
	if err != nil {
		return appmodel.WorkspaceRestoreResult{}, fmt.Errorf("spool workspace file: %w", err)
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()
	if _, err := io.Copy(spool, body); err != nil {
		return appmodel.WorkspaceRestoreResult{}, fmt.Errorf("read workspace file: %w", err)
	}

	people := &emailCollector{}
	if err := readSpool(spool, people); err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	users, missing, err := s.store.ResolveWorkspaceEmails(ctx, people.list())
	if err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	if len(missing) > 0 {
		return appmodel.WorkspaceRestoreResult{}, fmt.Errorf("%w: %s", appmodel.ErrRestorePeopleMissing, strings.Join(missing, ", "))
	}

	return s.store.RestoreWorkspace(ctx, appmodel.WorkspaceRestoreRequest{
		CallerID: callerID,
		Users:    users,
		Write:    func(sink appmodel.WorkspaceSink) error { return readSpool(spool, sink) },
	})
}

// readSpool decodes the kept copy of a file from its start into sink.
func readSpool(spool *os.File, sink appmodel.WorkspaceSink) error {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("read workspace file: %w", err)
	}
	return Decode(spool, sink)
}
