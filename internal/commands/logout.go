package commands

import (
	"errors"
	"fmt"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/spf13/cobra"
)

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Log out of DevCraft and remove stored credentials",
		Example: "devcraft logout",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			handler := auth.NewAuthHandler()

			session, err := handler.LoadLogin()
			if errors.Is(err, auth.ErrNotLoggedIn) {
				cmd.Println("You are not logged in.")
				return nil
			}
			if err != nil {
				// Stored data is unreadable; still try to clear it.
				session = nil
			}

			if err := handler.DeleteLogin(); err != nil {
				return fmt.Errorf("logout: %w", err)
			}

			if session != nil && session.Email != "" {
				cmd.Printf("Logged out %s\n", session.Email)
			} else {
				cmd.Println("Logged out")
			}
			return nil
		},
	}
}
