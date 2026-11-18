package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

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

	var last client.EvaluationStatus
	for {
		ev, err := c.GetEvaluation(ctx, id)
		if err != nil {
			return nil, apiError("get evaluation", err)
		}
		if ev.Status != last {
			_, _ = fmt.Fprintf(w, "Evaluation %s: %s\n", ev.ID, ev.Status)
			last = ev.Status
		}
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
// pass, so the command exits non-zero.
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
		return errInfrastructure
	case client.EvaluationCompleted:
	default:
		p("Evaluation %s is %s. Check again later with `devcraft results --evaluation %s`.\n", ev.ID, ev.Status, ev.ID)
		return nil
	}

	verdict := "FAILED"
	if ev.Passed {
		verdict = "PASSED"
	}
	kind := ev.Kind
	if kind == "" {
		kind = "evaluation"
	}
	p("Result (%s): %s", kind, verdict)
	if ev.MaxScore > 0 {
		p("  score %s/%s", fmtNum(ev.Score), fmtNum(ev.MaxScore))
	}
	p("\n")

	if len(ev.Checks) > 0 {
		p("\nChecks:\n")
		for _, ch := range ev.Checks {
			label := map[string]string{
				client.CheckPassed:  "PASS",
				client.CheckFailed:  "FAIL",
				client.CheckSkipped: "SKIP",
			}[ch.Status]
			if label == "" {
				label = ch.Status
			}
			p("  [%s] %s", label, ch.Name)
			if ch.Message != "" {
				p(": %s", ch.Message)
			}
			p("\n")
		}
	}

	if len(ev.Rubric) > 0 {
		p("\nRubric:\n")
		for _, r := range ev.Rubric {
			p("  %s: %s/%s\n", r.Criterion, fmtNum(r.Score), fmtNum(r.MaxScore))
			if r.Evidence != "" {
				p("    %s\n", r.Evidence)
			}
		}
	}

	if len(ev.Suggestions) > 0 {
		p("\nSuggestions:\n")
		for _, s := range ev.Suggestions {
			p("  - %s\n", s)
		}
	}

	if !ev.Passed {
		return errEvaluationFailed
	}
	return nil
}

func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
