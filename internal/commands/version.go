package commands

import (
	"github.com/aniket0951/devcraft-cli.git/internal/version"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the DevCraft CLI version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("devcraft " + version.String())
		},
	}
}
