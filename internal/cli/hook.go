package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hookMarker identifies the post-checkout hook this command wrote, so it never
// overwrites or removes a hook somebody else put there.
const hookMarker = "# paratrack-hook"

// hookScript switches the timer to the branch that was checked out. The
// activity is the last part of the branch name ("feat/PT-123-login" becomes
// "PT-123-login"). It runs in the background and never fails the checkout:
// a slow or offline server must not stand between a developer and git.
func hookScript(binary string) string {
	return `#!/bin/sh
` + hookMarker + ` — written by "paratrack hook install", removed by "paratrack hook uninstall".
# Switches the paratrack timer to the branch you check out.
[ "$3" = "1" ] || exit 0
branch=$(git symbolic-ref --quiet --short HEAD) || exit 0
case "$branch" in main|master|develop|trunk) exit 0 ;; esac
name=${branch##*/}
("` + binary + `" focus "$name" >/dev/null 2>&1 &)
exit 0
`
}

func runHook(rt *Runtime, args []string) error {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall" && args[0] != "show") {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack hook install|uninstall|show")}
	}
	dir, err := gitHooksDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "post-checkout")
	current, readErr := os.ReadFile(path)
	ours := readErr == nil && strings.Contains(string(current), hookMarker)
	switch args[0] {
	case "show":
		if readErr != nil {
			fmt.Fprintln(rt.Out, "no post-checkout hook")
			return nil
		}
		fmt.Fprint(rt.Out, string(current))
		return nil
	case "uninstall":
		if readErr != nil {
			fmt.Fprintln(rt.Out, "no post-checkout hook")
			return nil
		}
		if !ours {
			return fmt.Errorf("%s was not written by paratrack; leaving it as it is", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove hook: %w", err)
		}
		fmt.Fprintln(rt.Out, "✓ removed", path)
		return nil
	}
	if readErr == nil && !ours {
		return fmt.Errorf("%s already exists and was not written by paratrack; add this line to it instead:\n  (paratrack focus \"${branch##*/}\" >/dev/null 2>&1 &)", path)
	}
	binary, err := os.Executable()
	if err != nil {
		binary = "paratrack"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create hooks directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(hookScript(binary)), 0o755); err != nil {
		return fmt.Errorf("write hook: %w", err)
	}
	fmt.Fprintln(rt.Out, "✓ installed", path)
	fmt.Fprintln(rt.Out, "  checking out a branch now focuses the timer on its last name part (feat/PT-123-login → PT-123-login)")
	return nil
}

// gitHooksDir asks git where this repository keeps its hooks, which honours
// worktrees and core.hooksPath.
func gitHooksDir() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "", errors.New("not inside a git repository")
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(wd, dir)
	}
	return dir, nil
}
