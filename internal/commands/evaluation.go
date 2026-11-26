package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/report"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
)

var (
	// pollInterval and pollTimeout are variables so tests can shorten them.
	pollInterval = 2 * time.Second
	pollTimeout  = 15 * time.Minute

	errEvaluationFailed = errors.New("evaluation failed: some checks did not pass")
	errInfrastructure   = errors.New("evaluation could not be completed due to a DevCraft infrastructure problem; this is not a failure of your solution, please try again later")
)

// waitForEvaluation polls an evaluation until it reaches a terminal state.
func waitForEvaluation(ctx context.Context, c *client.Client, id string, w io.Writer) (*client.Evaluation, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	// On a terminal: one animated line. Otherwise: "Evaluation <id>: <status>"
	// each time the status changes.
	spin := report.NewSpinner(w, isTerminal(w), "Evaluation "+id)
	defer spin.Stop()

	for {
		ev, err := c.GetEvaluation(ctx, id)
		if err != nil {
			return nil, apiError("get evaluation", err)
		}
		spin.Set(string(ev.Status))
		if ev.Done() {
			return ev, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("evaluation %s still running after %s; check later with `devcraft results --evaluation %s`", id, pollTimeout, id)
			}
			return nil, fmt.Errorf("stopped waiting for evaluation %s; check later with `devcraft results --evaluation %s`", id, id)
		case <-time.After(pollInterval):
		}
	}
}

// waitForSubmissionEvaluation polls a submission until an evaluation is attached.
func waitForSubmissionEvaluation(ctx context.Context, c *client.Client, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	for {
		sub, err := c.GetSubmission(ctx, id)
		if err != nil {
			return "", apiError("get submission", err)
		}
		if sub.EvaluationID != "" {
			return sub.EvaluationID, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("submission %s has no evaluation yet; check later with `devcraft results --submission %s`", id, id)
		case <-time.After(pollInterval):
		}
	}
}

// printEvaluation renders an evaluation and returns an error if it did not
// pass, so the command exits non-zero. The error is a reportedError: the
// report already says what happened, so railRun adds nothing.
func printEvaluation(w io.Writer, ev *client.Evaluation) error {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

	p("\n")
	switch ev.Status {
	case client.EvaluationInfraError:
		p("Evaluation %s could not be completed (infrastructure problem", ev.ID)
		if ev.InfraError != nil && ev.InfraError.Code != "" {
			p(": %s", ev.InfraError.Code)
		}
		p(").\n")
		if ev.InfraError != nil && ev.InfraError.Message != "" {
			p("  %s\n", ev.InfraError.Message)
		}
		p("This is not a failure of your solution; please try again in a few minutes.\n")
		return &reportedError{errInfrastructure}
	case client.EvaluationCompleted:
	default:
		p("Evaluation %s is %s. Check again later with `devcraft results --evaluation %s`.\n", ev.ID, ev.Status, ev.ID)
		return nil
	}

	title := "evaluation"
	if ev.Kind != "" {
		title = ev.Kind + " evaluation"
	}
	live := report.NewLive(w, isTerminal(w), title, ev.ID)
	for _, ch := range ev.Checks {
		live.Done(checkResult(ch))
	}

	if len(ev.Rubric) > 0 {
		items := make([]report.Item, 0, len(ev.Rubric))
		for _, r := range ev.Rubric {
			items = append(items, report.Item{
				Text: fmt.Sprintf("%s %s/%s", r.Criterion, fmtNum(r.Score), fmtNum(r.MaxScore)),
				Sub:  r.Evidence,
			})
		}
		live.Section("Rubric", items)
	}
	if len(ev.Suggestions) > 0 {
		items := make([]report.Item, 0, len(ev.Suggestions))
		for _, s := range ev.Suggestions {
			items = append(items, report.Item{Text: "• " + s})
		}
		live.Section("Suggestions", items)
	}

	score := ""
	if ev.MaxScore > 0 {
		score = fmtNum(ev.Score) + "/" + fmtNum(ev.MaxScore)
	}
	var took time.Duration
	if ev.CompletedAt != nil && !ev.CreatedAt.IsZero() {
		took = ev.CompletedAt.Sub(ev.CreatedAt)
	}
	live.FinishWith(ev.Passed, score, took)

	if !ev.Passed {
		return &reportedError{errEvaluationFailed}
	}
	return nil
}

// checkResult converts an API check into a report row. The API sends no
// layer, so rows are shown as a flat list.
func checkResult(ch client.Check) report.Result {
	r := report.Result{Name: ch.Name}
	switch ch.Status {
	case client.CheckPassed:
		r.Status, r.Note = report.Pass, ch.Message
	case client.CheckFailed:
		r.Status, r.Reason = report.Fail, ch.Message
	case client.CheckSkipped:
		r.Status, r.Note = report.Skipped, ch.Message
	default: // unknown status from a newer API: show it, don't hide it
		r.Status, r.Reason = report.Inconclusive, ch.Status
		if ch.Message != "" {
			r.Reason += ": " + ch.Message
		}
	}
	return r
}

// isTerminal reports whether w is an interactive terminal (animations on).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
