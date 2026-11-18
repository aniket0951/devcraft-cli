package commands

import (
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

func testCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run a quick evaluation of the current workspace",
		Long: "Uploads a snapshot of the current working tree (no git commit needed) " +
			"and runs a quick evaluation in DevCraft's isolated environment.",
		Example: "  devcraft test",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := findWorkspace()
			if err != nil {
				return err
			}
			c, err := newAPIClient(cmd)
			if err != nil {
				return err
			}
			ctx, stop := commandContext(cmd)
			defer stop()

			cmd.Printf("Testing %s (attempt %s)\n", ws.Metadata.AssignmentID, ws.Metadata.AttemptID)
			snap, err := uploadSnapshot(ctx, cmd, c, ws, client.PurposeQuickEvaluation)
			if err != nil {
				return err
			}

			ev, err := c.CreateQuickEvaluation(ctx, ws.Metadata.AttemptID, snap.ArtifactID)
			if err != nil {
				return apiError("start evaluation", err)
			}
			if !ev.Done() {
				if ev, err = waitForEvaluation(ctx, c, ev.ID, cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			return printEvaluation(cmd.OutOrStdout(), ev)
		},
	}
	cmd.Flags().Bool(allowSecretsFlag, false, "upload even if files look like they contain secrets")
	return cmd
}
