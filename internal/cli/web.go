package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"time"
)

func runWeb(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8000", "address to listen on")
	open := fs.Bool("open", false, "open the UI in the default browser once ready")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	if rt.WebRunner == nil {
		return fmt.Errorf("web server is unavailable in this CLI runtime")
	}

	fmt.Fprintf(rt.Out, "paratrack web: http://%s\n", *addr)
	if *open {
		url := "http://" + *addr
		ctx := rt.Context
		if ctx == nil {
			ctx = context.Background()
		}
		go func() {
			timer := time.NewTimer(300 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				openBrowser(url)
			}
		}()
	}
	return rt.WebRunner(rt.Context, *addr)
}

// openBrowser asks the OS to open a URL. Best-effort: if it fails
// (e.g. on a headless server), we silently continue — the server is
// still reachable from the printed URL.
func openBrowser(u string) {
	exe, err := lookupBrowserCmd()
	if err != nil || exe == "" {
		return
	}
	_ = exec.Command(exe, u).Start()
}

func lookupBrowserCmd() (string, error) {
	for _, cand := range []string{"open", "xdg-open", "wslview"} {
		if p, err := exec.LookPath(cand); err == nil {
			return p, nil
		}
	}
	return "", nil
}
