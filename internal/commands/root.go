package commands

import (
	"github.com/aniket0951/devcraft-cli.git/internal/config"
	"github.com/aniket0951/devcraft-cli.git/internal/version"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "devcraft",
		Short:   "DevCraft CLI",
		Version: version.String(),
		// Errors are printed once by main; don't dump usage for runtime errors.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetVersionTemplate("devcraft {{.Version}}\n")

	rootCmd.PersistentFlags().String(apiURLFlag, "",
		"DevCraft API base URL (env "+config.EnvAPIURL+")")

	rootCmd.AddCommand(
		versionCmd(),
		loginCmd(),
		logoutCmd(),
		assignmentsCmd(),
		startCmd(),
		testCmd(),
		submitCmd(),
		statusCmd(),
		resultsCmd(),
	)

	return rootCmd
}
