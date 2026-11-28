package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The hook is written once, refuses to replace a hook it did not write, and
// on checking out a branch focuses the timer on the branch's last name part.
func TestGitHookFocusesTheBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "start")
	t.Chdir(repo)

	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	if err := RunWithRuntime([]string{"hook", "install"}, rt); err != nil {
		t.Fatalf("install: %v", err)
	}
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	info, err := os.Stat(hook)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("hook not installed as an executable: %v", err)
	}

	// Run the script as git would, with a stand-in for the binary that
	// records what it was asked to do.
	record := filepath.Join(t.TempDir(), "called")
	stub := filepath.Join(t.TempDir(), "paratrack-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+record+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte(hookScript(stub)), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "checkout", "-q", "-b", "feat/PT-123-login")
	var called []byte
	// The hook runs the binary in the background; the shell creates the file
	// before it writes to it, so wait for the content, not just the file.
	for i := 0; i < 250; i++ {
		if called, err = os.ReadFile(record); err == nil && len(called) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.TrimSpace(string(called)) != "focus PT-123-login" {
		t.Fatalf("the hook called %q, want focus on the branch", called)
	}

	// Someone else's hook is left alone, both ways.
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RunWithRuntime([]string{"hook", "install"}, rt); err == nil {
		t.Fatal("installed over someone else's hook")
	}
	if err := RunWithRuntime([]string{"hook", "uninstall"}, rt); err == nil {
		t.Fatal("removed someone else's hook")
	}
	if err := os.WriteFile(hook, []byte(hookScript("paratrack")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RunWithRuntime([]string{"hook", "uninstall"}, rt); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Fatal("the hook is still there")
	}
}

// The completion scripts load in each shell's own syntax, and the activities
// they offer come from the server, newest first.
func TestCompletion(t *testing.T) {
	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	for shell, want := range map[string]string{"bash": "complete -F _paratrack", "zsh": "compdef _paratrack", "fish": "complete -c paratrack"} {
		out.Reset()
		if err := RunWithRuntime([]string{"completion", shell}, rt); err != nil || !strings.Contains(out.String(), want) {
			t.Fatalf("%s completion: %v\n%s", shell, err, out.String())
		}
	}
	if path, err := exec.LookPath("bash"); err == nil {
		out.Reset()
		_ = RunWithRuntime([]string{"completion", "bash"}, rt)
		if msg, err := exec.Command(path, "-n", "-c", out.String()).CombinedOutput(); err != nil {
			t.Fatalf("the bash script does not parse: %v\n%s", err, msg)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"sessions":[
			{"activity":"Ревью","start":"2026-11-27T10:00:00Z"},
			{"activity":"API","start":"2026-11-28T09:00:00Z"},
			{"activity":"Ревью","start":"2026-11-26T10:00:00Z"}]}`)
	}))
	defer srv.Close()
	rt.RemoteURL, rt.RemoteToken, rt.HTTPClient = srv.URL, "pt_x", srv.Client()
	out.Reset()
	if err := RunWithRuntime([]string{"__activities"}, rt); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "API\nРевью" {
		t.Fatalf("activities offered: %q", got)
	}
}
