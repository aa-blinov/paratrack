// Package cli adapts command-line input and terminal output to application
// workflows.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

// RunWithRuntime executes a CLI command using explicit process streams.
func RunWithRuntime(args []string, rt *Runtime) error {
	if rt == nil {
		rt = NewRuntime(nil, nil, nil)
	}
	rt.commandMu.Lock()
	defer rt.commandMu.Unlock()
	rt.mu.Lock()
	closed := rt.closed
	rt.mu.Unlock()
	if closed {
		return ErrRuntimeClosed
	}
	if rt.Context == nil {
		rt.Context = context.Background()
	}
	if rt.In == nil {
		rt.In = os.Stdin
	}
	if rt.Out == nil {
		rt.Out = os.Stdout
	}
	if rt.Err == nil {
		rt.Err = os.Stderr
	}
	if rt.Now == nil {
		rt.Now = time.Now
	}
	if len(args) == 0 {
		if rt.remoteEnabled() {
			_, err := runRemote(rt, "status", nil)
			return err
		}
		return runStatus(rt, nil)
	}
	// The git hook passes the raw branch; the activity name is made here. The
	// flag is looked for anywhere in the arguments: the hook writes it first,
	// a person may write `focus --name x --branch y`, and a branch that only
	// counts in the first position would be silently ignored.
	if args[0] == "focus" || args[0] == "switch" || args[0] == "sw" {
		rewritten, warn := branchCommand(args)
		args = rewritten
		if warn != nil {
			fmt.Fprintln(rt.Err, "paratrack:", warn)
		}
	}
	// Hooks and completion work on files and the shell, in either mode.
	switch args[0] {
	case "hook":
		return runHook(rt, args[1:])
	case "completion":
		return runCompletion(rt, args[1:])
	case "__activities":
		return runCompletionActivities(rt)
	}
	if len(args) > 0 && args[0] == "auth" {
		// Another account or server: the cached prompt line belongs to the old one.
		rt.dropStatusCache()
		return runAuth(rt, args[1:])
	}
	if rt.remoteEnabled() {
		if handled, err := runRemote(rt, args[0], args[1:]); handled {
			switch args[0] {
			case "status", "st", "stats", "log":
			default:
				if err == nil {
					rt.dropStatusCache()
				}
			}
			return err
		}
		if args[0] != "web" && args[0] != "-h" && args[0] != "--help" && args[0] != "help" {
			return fmt.Errorf("command %q is not available in remote CLI mode (supported: status, start, stop, pause, resume, focus, add, log, stats, auth)", args[0])
		}
	}
	switch args[0] {
	case "status", "st":
		return runStatus(rt, args[1:])
	case "start":
		return runStart(rt, args[1:])
	case "stop", "s":
		return runStop(rt, args[1:])
	case "pause", "p":
		return runPause(rt, args[1:])
	case "resume", "r":
		return runResume(rt, args[1:])
	case "focus", "switch", "sw":
		return runFocus(rt, args[1:])
	case "add", "a":
		return runAdd(rt, args[1:])
	case "log", "l":
		return runLog(rt, args[1:])
	case "stats":
		return runStats(rt, args[1:])
	case "goal", "goals":
		return runGoal(rt, args[1:])
	case "tag", "tags":
		return runTag(rt, args[1:])
	case "project", "projects":
		return runProject(rt, args[1:])
	case "web":
		return runWeb(rt, args[1:])
	case "-h", "--help", "help":
		printUsage(rt)
	default:
		fmt.Fprintf(rt.Err, "unknown command %q\n\n", args[0])
		printUsage(rt)
		return &ExitError{Code: 2}
	}
	return nil
}

func printUsage(rt *Runtime) {
	_, _ = io.WriteString(rt.Out, `paratrack — minimalist time tracker

Usage:
  paratrack                         show active sessions
  paratrack start <activity>        quick start (asks for note)
  paratrack stop [activity]         stop specific or selected sessions
  paratrack pause [activity]        pause active session(s)
  paratrack resume [activity]       resume paused session(s)
  paratrack focus <activity>        pause others, resume/start selected
  paratrack add                     add a past session (interactive)
  paratrack log                     show log for a period
  paratrack stats                   show aggregated stats
  paratrack web [--addr :8000]      launch embedded web UI

Aliases: s=stop, p=pause, r=resume, sw=switch, st=status, a=add, l=log
  paratrack goal set --activity <name> --daily 2h   set a target
  paratrack goal list                               show goals + progress
  paratrack goal unset --activity <name> [--daily]  remove
  paratrack tag add <name>                          create a tag
  paratrack tag list                                show all tags + counts
  paratrack tag attach <session_id> <name>          tag a session
  paratrack tag detach <session_id> <name>          untag
  paratrack project list [--archived] [--team id]   list projects
  paratrack project create [--slug s] [--color #hex] [--team id] <name>  create
  paratrack project show <slug|id>                  show one project
  paratrack project rename <slug|id> <new-name>     rename
  paratrack project color   <slug|id> <#hex>        set color
  paratrack project archive <slug|id>               archive
  paratrack project unarchive <slug|id>             unarchive
  paratrack auth login --url <url> --token <token>  save remote CLI config
  paratrack auth status                             show remote CLI config
  paratrack auth logout                             remove saved remote config
  paratrack hook install|uninstall|show|print       git post-checkout: focus the timer on the branch
  paratrack completion bash|zsh|fish                print a shell completion script`)
}

// dropFlag removes a flag and the value after it from a command's arguments,
// so a branch that names the activity is not fighting another --name.
func dropFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++ // skip the value too, whether or not it looks like a flag
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// branchCommand turns `focus --branch x` into what actually runs. The flag is
// looked for anywhere in the arguments: the hook writes it first, a person may
// write `focus --name x --branch y`, and a branch that counted only in the
// first position was silently ignored. A pause branch, a detached HEAD or a
// name with nothing usable in it ("feat/🔥") becomes `pause`, so the timer
// never stays on the feature that was left; anything else names the activity,
// because a branch "fix/1234" is an activity called 1234 and never the running
// session that happens to have that id.
func branchCommand(args []string) ([]string, error) {
	at := slices.Index(args, "--branch")
	if at < 0 || at+1 >= len(args) {
		return args, nil
	}
	branch := args[at+1]
	pattern := ""
	if values := gitConfigAll("paratrack.ticketPattern"); len(values) > 0 {
		pattern = values[len(values)-1]
	}
	pause, name, warn := branchDecision(branch, splitConfigList(gitConfigAll("paratrack.pauseBranches")), pattern)
	if pause {
		return []string{"pause"}, warn
	}
	rest := dropFlag(append(append([]string{}, args[1:at]...), args[at+2:]...), "--name")
	return append([]string{args[0], "--name", name}, rest...), warn
}
