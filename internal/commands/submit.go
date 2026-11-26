package commands

import (
	"context"

	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

func submitCmd() *cobra.Command {
	var noWait bool

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a snapshot of the current workspace for formal evaluation",
		Long: "Creates an immutable snapshot of the current working tree, uploads it and " +
			"creates a formal submission. The evaluator grades exactly that snapshot.",
		Example: "  devcraft submit\n  devcraft submit --no-wait",
		Args:    cobra.NoArgs,
		RunE: railRun("submit", func(ctx context.Context, cmd *cobra.Command, p *ui.Printer, args []string) error {
			ws, err := findWorkspace()
			if err != nil {
				return err
			}
			c, err := newAPIClient(ctx, cmd, p)
			if err != nil {
				return err
			}

			cmd.Printf("Submitting %s (attempt %s)\n", ws.Metadata.AssignmentID, ws.Metadata.AttemptID)
			snap, err := uploadSnapshot(ctx, cmd, c, ws, client.PurposeSubmission)
			if err != nil {
				return err
			}

			sub, err := c.CreateSubmission(ctx, ws.Metadata.AttemptID, snap.ArtifactID, snap.Checksum)
			if err != nil {
				return apiError("create submission", err)
			}
			cmd.Printf("Submission %s created (status: %s)\n", sub.ID, sub.Status)

			if noWait {
				cmd.Printf("Check the result later with: devcraft results --submission %s\n", sub.ID)
				return nil
			}

			evalID := sub.EvaluationID
			if evalID == "" {
				if evalID, err = waitForSubmissionEvaluation(ctx, c, sub.ID); err != nil {
					return err
				}
			}
			ev, err := waitForEvaluation(ctx, c, evalID, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return printEvaluation(cmd.OutOrStdout(), ev)
		}),
	}
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "do not wait for the evaluation to finish")
	cmd.Flags().Bool(allowSecretsFlag, false, "upload even if files look like they contain secrets")
	return cmd
}
