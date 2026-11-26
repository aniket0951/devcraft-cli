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
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/aniket0951/devcraft-cli.git/internal/version"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

const apiURLFlag = "api-url"

// newAPIClient builds an authenticated API client from config and the stored
// session, offering to sign in again if the session is missing or expired.
func newAPIClient(ctx context.Context, cmd *cobra.Command, p *ui.Printer) (*client.Client, error) {
	baseURL, err := apiBaseURL(cmd)
	if err != nil {
		return nil, err
	}
	session, err := requireSession(ctx, p, false)
	if err != nil {
		return nil, err
	}
	return newClient(baseURL, session)
}

// apiBaseURL resolves the API URL from --api-url, the environment and config.
func apiBaseURL(cmd *cobra.Command) (string, error) {
	flagURL, _ := cmd.Flags().GetString(apiURLFlag)
	cfg, err := config.Load(flagURL)
	if err != nil {
		return "", err
	}
	return cfg.APIBaseURL, nil
}

func newClient(baseURL string, session *auth.Session) (*client.Client, error) {
	return client.New(baseURL, session.IDToken, client.WithUserAgent(version.UserAgent()))
}

// apiError turns API errors into user-facing messages.
func apiError(action string, err error) error {
	switch {
	case client.IsUnauthorized(err):
		return fmt.Errorf("%s: your session has expired or is invalid; run `devcraft login` again", action)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: %w", action, context.Canceled)
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
