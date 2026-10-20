// Package workspacefile owns the workspace export file: how it is written, and
// how a reader decides it can take a file in. The data comes from the database
// layer through Store, so the file format stays apart from SQL.
package workspacefile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ErrUnsupportedFile means the file is not a paratrack workspace export this
// version of the server can read.
var ErrUnsupportedFile = appmodel.ErrUnsupportedWorkspaceFile

// Store reads one workspace's export snapshot and writes a restored workspace.
type Store interface {
	ExportWorkspace(ctx context.Context, request appmodel.WorkspaceExportRequest) (appmodel.WorkspaceExport, error)
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

// Export returns the export as the bytes of a file. The file is built in full
// first, so a failure never leaves a half-written download behind.
func (s *Service) Export(ctx context.Context, request appmodel.WorkspaceExportRequest) (appmodel.WorkspaceExport, []byte, error) {
	if request.TeamID <= 0 {
		return appmodel.WorkspaceExport{}, nil, model.ErrNotFound
	}
	export, err := s.store.ExportWorkspace(ctx, request)
	if err != nil {
		return appmodel.WorkspaceExport{}, nil, err
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(encodeWorkspaceExport(export)); err != nil {
		return appmodel.WorkspaceExport{}, nil, fmt.Errorf("encode workspace export: %w", err)
	}
	return export, buf.Bytes(), nil
}

// Restore creates a workspace from an export, owned by the caller.
func (s *Service) Restore(ctx context.Context, request appmodel.WorkspaceRestoreRequest) (appmodel.WorkspaceRestoreResult, error) {
	return s.store.RestoreWorkspace(ctx, request)
}

// Decode reads a workspace export file, refusing anything this server cannot take in.
func (s *Service) Decode(r io.Reader) (appmodel.WorkspaceExport, error) {
	return Read(r)
}

// Read decodes a workspace export file. It refuses anything that is not this
// format at this version, before any of its content is trusted.
func Read(r io.Reader) (appmodel.WorkspaceExport, error) {
	var file WorkspaceExport
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return appmodel.WorkspaceExport{}, fmt.Errorf("%w: %w", ErrUnsupportedFile, err)
	}
	export := decodeWorkspaceExport(file)
	if export.Format != appmodel.WorkspaceExportFormat || export.Version != appmodel.WorkspaceExportVersion {
		return appmodel.WorkspaceExport{}, fmt.Errorf("%w: format %q version %d", ErrUnsupportedFile, export.Format, export.Version)
	}
	return export, nil
}
