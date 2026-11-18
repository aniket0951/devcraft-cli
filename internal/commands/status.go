package commands

import (
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show the status of the current attempt and its latest submission",
		Example: "  devcraft status",
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

			attempt, err := c.GetAttempt(ctx, ws.Metadata.AttemptID)
			if err != nil {
				return apiError("get attempt", err)
			}

			cmd.Printf("Assignment:  %s (version %d)\n", ws.Metadata.AssignmentID, ws.Metadata.AssignmentVersion)
			cmd.Printf("Attempt:     %s (%s)\n", attempt.ID, attempt.Status)
			cmd.Printf("Workspace:   %s\n", ws.Root)

			if attempt.LatestSubmissionID == "" {
				cmd.Println("Submission:  none yet (run `devcraft submit`)")
			} else {
				sub, err := c.GetSubmission(ctx, attempt.LatestSubmissionID)
				if err != nil {
					return apiError("get submission", err)
				}
				cmd.Printf("Submission:  %s (%s, %s)\n", sub.ID, sub.Status, sub.CreatedAt.Local().Format("2006-01-02 15:04"))
			}

			if attempt.LatestEvaluationID == "" {
				cmd.Println("Evaluation:  none yet")
				return nil
			}
			ev, err := c.GetEvaluation(ctx, attempt.LatestEvaluationID)
			if err != nil {
				return apiError("get evaluation", err)
			}
			summary := string(ev.Status)
			if ev.Status == "completed" {
				summary = "FAILED"
				if ev.Passed {
					summary = "PASSED"
				}
				if ev.MaxScore > 0 {
					summary += ", score " + fmtNum(ev.Score) + "/" + fmtNum(ev.MaxScore)
				}
			}
			cmd.Printf("Evaluation:  %s (%s, %s)\n", ev.ID, ev.Kind, summary)
			return nil
		},
	}
}
