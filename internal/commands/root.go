package commands

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "devcraft",
		Short: "DevCraft CLI",
	}

	rootCmd.AddCommand(loginCmd())

	return rootCmd
}
