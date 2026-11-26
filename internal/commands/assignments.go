package commands

import (
	"context"
	"fmt"

	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

func assignmentsCmd() *cobra.Command {
	var params client.ListAssignmentsParams

	cmd := &cobra.Command{
		Use:     "assignments",
		Aliases: []string{"assignment", "ls"},
		Short:   "List available assignments",
		Example: "  devcraft assignments\n  devcraft assignments --language go --category \"SOLID Principles\" --level L1",
		Args:    cobra.NoArgs,
		RunE: railRun("list assignments", func(ctx context.Context, cmd *cobra.Command, p *ui.Printer, _ []string) error {
			p.Tag("ASSIGNMENTS", "Available assignments")

			c, err := newAPIClient(ctx, cmd, p)
			if err != nil {
				return err
			}
			list, err := c.ListAssignments(ctx, params)
			if err != nil {
				return err
			}

			if len(list) == 0 {
				p.Warn("No assignments found")
				if params != (client.ListAssignmentsParams{}) {
					p.Reason("", "try again without --language, --category or --level")
				}
				return nil
			}

			rows := make([][]string, 0, len(list))
			for _, a := range list {
				rows = append(rows, []string{a.Code, a.Name, a.Language, a.CategoryName, a.Level, "v" + a.CurrentVersion})
			}
			p.Table([]string{"CODE", "NAME", "LANGUAGE", "CATEGORY", "LEVEL", "VERSION"}, rows)
			p.Done("devcraft start <CODE>", ui.Dim(fmt.Sprintf("%d available", len(list))))
			return nil
		}),
	}

	f := cmd.Flags()
	f.StringVar(&params.Language, "language", "", "only this language, e.g. go")
	f.StringVar(&params.Category, "category", "", "only this category, e.g. \"SOLID Principles\"")
	f.StringVar(&params.Level, "level", "", "only this level, e.g. L1")
	return cmd
}
