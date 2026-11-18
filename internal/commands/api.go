package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/config"
	"github.com/aniket0951/devcraft-cli.git/internal/version"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

const apiURLFlag = "api-url"

var errNotLoggedIn = errors.New("you are not logged in; run `devcraft login` first")

// newAPIClient builds an authenticated API client from config and the stored session.
func newAPIClient(cmd *cobra.Command) (*client.Client, error) {
	flagURL, _ := cmd.Flags().GetString(apiURLFlag)
	cfg, err := config.Load(flagURL)
	if err != nil {
		return nil, err
	}

	session, err := auth.NewAuthHandler().LoadLogin()
	if errors.Is(err, auth.ErrNotLoggedIn) {
		return nil, errNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("load login session: %w", err)
	}

	return client.New(cfg.APIBaseURL, session.IDToken, client.WithUserAgent(version.UserAgent()))
}

// apiError turns API errors into user-facing messages.
func apiError(action string, err error) error {
	switch {
	case client.IsUnauthorized(err):
		return fmt.Errorf("%s: your session has expired or is invalid; run `devcraft login` again", action)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: cancelled", action)
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}

// commandContext returns a context cancelled on Ctrl-C / SIGTERM.
func commandContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}
