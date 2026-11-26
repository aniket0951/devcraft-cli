package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
)

var (
	errNotLoggedIn    = errors.New("you are not logged in; run `devcraft login` first")
	errSessionExpired = fmt.Errorf("%w: run `devcraft login` again", auth.ErrSessionExpired)
	errSignIn         = errors.New("sign-in failed")
)

// signInFn runs the browser sign-in; tests replace it.
var signInFn = browserLoginWith

// newPrinter builds a command's Printer; tests replace it to answer prompts.
var newPrinter = func(cmd *cobra.Command) *ui.Printer {
	return ui.New(cmd.OutOrStdout(), cmd.InOrStdin())
}

// requireSession returns a usable session. If the saved login is missing or
// expired (or forceExpired, after the API answered 401), it offers to sign
// in right away and carries on with the new session. Without a terminal, or
// if the user declines, it returns errNotLoggedIn or errSessionExpired.
// Ctrl+C returns an error wrapping context.Canceled.
func requireSession(ctx context.Context, p *ui.Printer, forceExpired bool) (*auth.Session, error) {
	s, err := auth.NewAuthHandler().LoadLogin()
	switch {
	case errors.Is(err, auth.ErrNotLoggedIn):
		err = errNotLoggedIn
	case err != nil:
		return nil, fmt.Errorf("load login session: %w", err)
	case forceExpired || s.Expired(time.Now()):
		err = errSessionExpired
	default:
		return s, nil
	}

	if errors.Is(err, errNotLoggedIn) {
		p.Warn("You're not signed in")
	} else {
		p.Warn("Your session expired")
	}
	yes, cerr := p.Confirm(ctx, "Sign in again now?")
	if cerr != nil {
		return nil, cerr
	}
	if !yes {
		return nil, err
	}
	return signInInline(ctx, p)
}

// signInInline runs the browser sign-in on the rail and saves the session.
func signInInline(ctx context.Context, p *ui.Printer) (*auth.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	var step *ui.Step
	s, err := signInFn(ctx, func(loginURL string, openErr error) {
		if openErr == nil {
			p.OK("Opened your browser for Google sign-in")
			p.Line(ui.Dim("If it didn't open, visit " + loginURL))
		} else {
			p.Warn("Couldn't open your browser; open this link to sign in:")
			p.Line(loginURL)
		}
		step = p.Start("Waiting for sign-in")
	})
	if err == nil {
		err = auth.NewAuthHandler().SaveLogin(*s)
	}
	if err != nil {
		if step != nil {
			step.Fail("Sign-in failed")
		}
		return nil, fmt.Errorf("%w: %w", errSignIn, err)
	}
	step.OK("Signed in as " + ui.Bold(s.Email))
	return s, nil
}
