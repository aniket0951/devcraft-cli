package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

// Exit codes.
const (
	exitOK          = 0
	exitFail        = 1
	exitInterrupted = 130 // Ctrl+C
)

// runFunc is a command body run by railRun.
type runFunc func(ctx context.Context, cmd *cobra.Command, p *ui.Printer, args []string) error

// railRun is the RunE for every command. It gives the body a context that is
// cancelled on Ctrl+C and a rail Printer, and shows any error the body
// returns the same way:
//
//	│ ✗ Session expired
//	│   → your saved login is no longer valid
//	│     hint: run devcraft login, then run this command again
//
//	FAIL  Couldn't <action>
//
// Errors the body already showed (reportedError) and Ctrl+C pass through
// untouched; main turns them into exit codes.
func railRun(action string, run runFunc) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		p := newPrinter(cmd)
		ctx, stop := commandContext(cmd)
		defer stop()
		return failOnRail(p, action, run(ctx, cmd, p, args))
	}
}

// failOnRail shows err as a rail failure unless it is nil, a Ctrl+C, or was
// already shown.
func failOnRail(p *ui.Printer, action string, err error) error {
	var reported *reportedError
	if err == nil || isInterrupt(err) || errors.As(err, &reported) {
		return err
	}
	return railFail(p, describe(err), action, err)
}

// failure is a rail-style error: a title, why it happened and what to do.
type failure struct{ title, why, hint string }

// describe turns an error into a learner-facing failure: a short title,
// why it happened and what to do. Every command's errors go through here
// (via railRun) so they read the same way.
func describe(err error) failure {
	var apiErr *client.APIError
	var netErr net.Error
	var urlErr *url.Error
	var dirErr *dirError
	switch {
	case errors.Is(err, errNotLoggedIn):
		return failure{"Not signed in", "there's no saved login on this machine", "run devcraft login, then run this command again"}
	case errors.Is(err, auth.ErrSessionExpired), client.IsUnauthorized(err):
		return failure{"Session expired", "your saved login is no longer valid", "run devcraft login, then run this command again"}
	case errors.Is(err, errSignIn) && errors.Is(err, context.DeadlineExceeded):
		return failure{"Sign-in timed out", fmt.Sprintf("the browser didn't finish signing in within %s", loginTimeout), "run devcraft login, then run this command again"}
	case errors.Is(err, errSignIn):
		return failure{"Sign-in failed", unwrapAll(err), "run devcraft login, then run this command again"}
	case errors.Is(err, workspace.ErrNotWorkspace):
		return failure{"Not in an assignment folder", "this command runs inside a folder created by devcraft start", "cd into your assignment folder, or run devcraft start <CODE>"}
	case errors.Is(err, errAssignmentsDirNotSet):
		return failure{"Assignments folder not set", "DevCraft doesn't know where to put your assignments yet", "run devcraft path set <folder>, then run this command again"}
	case errors.As(err, &dirErr):
		return failure{"Can't use that folder", dirErr.why, "choose another folder"}
	case errors.Is(err, errNoAttempt):
		return failure{"Not available yet", "testing and submitting aren't available for this assignment yet", ""}
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && apiErr.Code == "":
		// The API always sends an err_code; a bare 404 is a route the
		// server doesn't have (an older server, or a wrong --api-url).
		return failure{"Not available yet", "DevCraft can't do this right now", "try again in a few minutes, or update devcraft"}
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound:
		return failure{"Not found", messageOr(apiErr, "DevCraft couldn't find that"), ""}
	case errors.As(err, &apiErr) && apiErr.StatusCode >= 500:
		return failure{"DevCraft is having trouble", "something went wrong on our side", "try again in a few minutes"}
	case errors.As(err, &apiErr):
		return failure{"Request failed", messageOr(apiErr, "DevCraft couldn't complete the request"), ""}
	case errors.As(err, &urlErr), errors.As(err, &netErr):
		return failure{"Can't reach DevCraft", "couldn't connect to DevCraft", "check your internet connection, VPN or firewall, then run this command again"}
	default:
		return failure{"Something went wrong", err.Error(), ""}
	}
}

func messageOr(apiErr *client.APIError, fallback string) string {
	if apiErr.Message != "" {
		return apiErr.Message
	}
	return fallback
}

// unwrapAll returns the innermost error's message.
func unwrapAll(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
}

// reportedError has already been shown to the user; main only sets the exit
// code for it.
type reportedError struct{ err error }

func (e *reportedError) Error() string { return e.err.Error() }
func (e *reportedError) Unwrap() error { return e.err }

// railFail prints a failure on the rail and returns it as a reportedError.
func railFail(p *ui.Printer, f failure, action string, err error) error {
	p.Fail(f.title)
	return reasonFail(p, f, action, err)
}

// reasonFail prints a failure's reason and the FAIL line, for when the ✗
// line has already been printed (by a step or progress bar).
func reasonFail(p *ui.Printer, f failure, action string, err error) error {
	p.Reason(f.why, f.hint)
	p.Failed("Couldn't " + action)
	return &reportedError{err}
}

func isInterrupt(err error) bool { return errors.Is(err, context.Canceled) }

// HandleError prints err to stderr unless it was already shown (or was a
// Ctrl+C) and returns the process exit code: 0 success, 1 failure,
// 130 interrupted.
func HandleError(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	if isInterrupt(err) {
		return exitInterrupted
	}
	var reported *reportedError
	if !errors.As(err, &reported) {
		fmt.Fprintln(stderr, err)
	}
	return exitFail
}
