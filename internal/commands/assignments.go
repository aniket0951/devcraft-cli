package commands

import (
	"strconv"

	"github.com/aniket0951/devcraft-cli.git/internal/output"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

func assignmentsCmd() *cobra.Command {
	var params client.ListAssignmentsParams

	cmd := &cobra.Command{
		Use:     "assignments",
		Aliases: []string{"assignment", "ls"},
		Short:   "List available assignments",
		Example: "  devcraft assignments\n  devcraft assignments --language go --difficulty easy",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient(cmd)
			if err != nil {
				return err
			}
			ctx, stop := commandContext(cmd)
			defer stop()

			list, err := c.ListAssignments(ctx, params)
			if err != nil {
				return apiError("list assignments", err)
			}
			if len(list.Items) == 0 {
				cmd.Println("No assignments found.")
				return nil
			}

			rows := make([][]string, 0, len(list.Items))
			for _, a := range list.Items {
				rows = append(rows, []string{a.ID, a.Title, a.Language, a.Category, a.Difficulty})
			}
			if err := output.Table(cmd.OutOrStdout(), []string{"ID", "TITLE", "LANGUAGE", "CATEGORY", "DIFFICULTY"}, rows); err != nil {
				return err
			}

			if list.NextCursor != "" {
				next := "devcraft assignments --cursor " + list.NextCursor
				if params.Limit > 0 {
					next += " --limit " + strconv.Itoa(params.Limit)
				}
				cmd.Printf("\nMore results: %s\n", next)
			}
			cmd.Println("\nStart one with: devcraft start <ID>")
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&params.Language, "language", "", "filter by language")
	f.StringVar(&params.Category, "category", "", "filter by category")
	f.StringVar(&params.Difficulty, "difficulty", "", "filter by difficulty")
	f.IntVar(&params.Limit, "limit", 0, "maximum number of results per page")
	f.StringVar(&params.Cursor, "cursor", "", "pagination cursor from a previous page")
	return cmd
}
