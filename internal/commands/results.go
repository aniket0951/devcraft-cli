package commands

import (
	"errors"

	"github.com/spf13/cobra"
)

func resultsCmd() *cobra.Command {
	var submissionID, evaluationID string

	cmd := &cobra.Command{
		Use:   "results",
		Short: "Show evaluation results and feedback",
		Example: "  devcraft results\n" +
			"  devcraft results --submission sub_123\n" +
			"  devcraft results --evaluation eval_123",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if submissionID != "" && evaluationID != "" {
				return errors.New("use only one of --submission and --evaluation")
			}

			// Resolve the workspace before checking login so errors are in a sensible order.
			var attemptID string
			if submissionID == "" && evaluationID == "" {
				ws, err := findWorkspace()
				if err != nil {
					return err
				}
				attemptID = ws.Metadata.AttemptID
			}

			c, err := newAPIClient(cmd)
			if err != nil {
				return err
			}
			ctx, stop := commandContext(cmd)
			defer stop()

			switch {
			case evaluationID != "":
			case submissionID != "":
				sub, err := c.GetSubmission(ctx, submissionID)
				if err != nil {
					return apiError("get submission", err)
				}
				if sub.EvaluationID == "" {
					cmd.Printf("Submission %s has no evaluation yet (status: %s).\n", sub.ID, sub.Status)
					return nil
				}
				evaluationID = sub.EvaluationID
			default:
				attempt, err := c.GetAttempt(ctx, attemptID)
				if err != nil {
					return apiError("get attempt", err)
				}
				if attempt.LatestEvaluationID == "" {
					cmd.Println("No evaluations yet. Run `devcraft test` or `devcraft submit`.")
					return nil
				}
				evaluationID = attempt.LatestEvaluationID
			}

			ev, err := c.GetEvaluation(ctx, evaluationID)
			if err != nil {
				return apiError("get evaluation", err)
			}
			return printEvaluation(cmd.OutOrStdout(), ev)
		},
	}
	cmd.Flags().StringVar(&submissionID, "submission", "", "show results for this submission ID")
	cmd.Flags().StringVar(&evaluationID, "evaluation", "", "show results for this evaluation ID")
	return cmd
}
