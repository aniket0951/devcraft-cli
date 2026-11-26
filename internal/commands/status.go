package commands

import (
	"context"

	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show the status of the current attempt and its latest submission",
		Example: "  devcraft status",
		Args:    cobra.NoArgs,
		RunE: railRun("show status", func(ctx context.Context, cmd *cobra.Command, p *ui.Printer, args []string) error {
			ws, err := findWorkspace()
			if err != nil {
				return err
			}
			c, err := newAPIClient(ctx, cmd, p)
			if err != nil {
				return err
			}

			attempt, err := c.GetAttempt(ctx, ws.Metadata.AttemptID)
			if err != nil {
				return apiError("get attempt", err)
			}

			cmd.Printf("Assignment:  %s (version %s)\n", ws.Metadata.AssignmentID, ws.Metadata.AssignmentVersion)
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
		}),
	}
}
