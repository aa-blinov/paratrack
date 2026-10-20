package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/workspacefile"
)

// An owner downloads the whole workspace as one named JSON file, and the file
// reads back in the export format. It names no password.
func TestWorkspaceExportDownloadsOneFileForAManager(t *testing.T) {
	e := newAPIEnv(t)
	e.register("workspace-export@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Export work"}}, nil))
	page := reactData[exportPage](t, readBody(t, e.do("GET", "/export", nil, nil)))
	if !page.CanExportWorkspace {
		t.Fatal("the owner is not offered the whole-workspace download")
	}

	resp := e.do("GET", "/export/workspace.json", nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != 200 {
		t.Fatalf("download: status %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `attachment; filename="paratrack-`) {
		t.Fatalf("headers: %q %q", resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}
	export, err := workspacefile.Read(strings.NewReader(body))
	if err != nil {
		t.Fatalf("the download does not read back: %v", err)
	}
	if len(export.Projects) != 1 || export.Projects[0].Name != "Export work" || len(export.Members) != 1 {
		t.Fatalf("the download holds projects %+v and members %+v", export.Projects, export.Members)
	}
	if strings.Contains(body, "password") {
		t.Fatal("the download names a password field")
	}
}
