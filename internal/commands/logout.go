package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
)

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Log out of DevCraft and remove stored credentials",
		Example: "devcraft logout",
		Args:    cobra.NoArgs,
		RunE: railRun("sign out", func(_ context.Context, cmd *cobra.Command, p *ui.Printer, _ []string) error {
			p.Tag("AUTH", "Sign out")
			handler := auth.NewAuthHandler()

			session, err := handler.LoadLogin()
			if errors.Is(err, auth.ErrNotLoggedIn) {
				p.Warn("You're not signed in")
				return nil
			}
			if err != nil {
				// Stored data is unreadable; still try to clear it.
				session = nil
			}

			if err := handler.DeleteLogin(); err != nil {
				return fmt.Errorf("remove saved login: %w", err)
			}

			if session != nil && session.Email != "" {
				p.OK("Signed out " + ui.Bold(session.Email))
			} else {
				p.OK("Signed out")
			}
			return nil
		}),
	}
}
