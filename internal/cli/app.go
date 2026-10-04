// Package cli adapts command-line input and terminal output to application
// workflows.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
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
		return runStatus(rt)
	}
	switch args[0] {
	case "status", "st":
		return runStatus(rt)
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
  paratrack project delete <slug|id>                delete`)
}
