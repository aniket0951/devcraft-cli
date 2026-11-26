package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aniket0951/devcraft-cli.git/internal/config"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/spf13/cobra"
)

// errAssignmentsDirNotSet: no folder is saved and there is no terminal to ask in.
var errAssignmentsDirNotSet = errors.New("assignments folder is not set; run `devcraft path set <folder>`")

// maxFolderPrompts bounds how often the first-run prompt re-asks after an
// unusable answer.
const maxFolderPrompts = 3

// dirError says why a folder can't hold assignments.
type dirError struct {
	dir string
	why string
}

func (e *dirError) Error() string { return fmt.Sprintf("can't use %s: %s", e.dir, e.why) }

func pathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show the folder your assignments are downloaded to",
		Long: "Every assignment is downloaded to <folder>/<CODE>. `devcraft start` asks for the folder " +
			"the first time; change it with `devcraft path set <folder>`.",
		Example: "  devcraft path\n  devcraft path set ~/code/devcraft",
		Args:    cobra.NoArgs,
		RunE:    railRun("show assignments folder", runPathShow),
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "set <folder>",
		Short:   "Set the folder your assignments are downloaded to",
		Example: "  devcraft path set ~/code/devcraft",
		Args:    cobra.ExactArgs(1),
		RunE:    railRun("set assignments folder", runPathSet),
	})
	return cmd
}

func runPathShow(_ context.Context, _ *cobra.Command, p *ui.Printer, _ []string) error {
	p.Tag("PATH", "Assignments folder")

	dir, source, err := config.ResolveAssignmentsDir()
	if err != nil {
		return err
	}
	if source == config.SourceNone {
		p.Warn("Not set yet")
		p.Reason("", "devcraft start will ask you, or run devcraft path set <folder>")
		return nil
	}

	p.OK(config.TildePath(dir))
	if source == config.SourceEnv {
		p.Line(ui.Dim("from " + config.EnvAssignmentsDir))
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		p.Line(ui.Dim("doesn't exist yet; devcraft start will create it"))
	} else {
		p.Line(ui.Dim(describeWorkspaces(listWorkspaces(dir))))
	}
	p.Done("change it with devcraft path set <folder>")
	return nil
}

func runPathSet(_ context.Context, _ *cobra.Command, p *ui.Printer, args []string) error {
	p.Tag("PATH", "Set assignments folder")

	old, err := config.LoadSettings()
	if err != nil {
		return err
	}
	dir, err := prepareAssignmentsDir(args[0])
	if err != nil {
		return err
	}
	if err := config.SaveSettings(config.Settings{AssignmentsDir: dir}); err != nil {
		return err
	}

	p.OK("Assignments folder set to " + ui.Bold(config.TildePath(dir)))
	if old.AssignmentsDir != "" && old.AssignmentsDir != dir {
		p.Line(ui.Dim("assignments already in " + config.TildePath(old.AssignmentsDir) + " stay there"))
	}
	if os.Getenv(config.EnvAssignmentsDir) != "" {
		p.Warn(config.EnvAssignmentsDir + " is set and is used instead of this folder")
	}
	p.Done("devcraft start <CODE>")
	return nil
}

// assignmentsDir returns the folder to download assignments into, creating it
// if needed. If none is chosen yet it asks once (Enter takes ~/devcraft) and
// saves the answer; without a terminal it returns errAssignmentsDirNotSet.
func assignmentsDir(ctx context.Context, p *ui.Printer) (string, error) {
	dir, source, err := config.ResolveAssignmentsDir()
	if err != nil {
		return "", err
	}
	if source != config.SourceNone {
		return prepareAssignmentsDir(dir) // recreate it if it was deleted
	}
	if !p.Interactive() {
		return "", errAssignmentsDirNotSet
	}

	question := "Where should DevCraft keep your assignments?"
	for range maxFolderPrompts {
		answer, err := p.Ask(ctx, question, "Folder", config.DefaultAssignmentsDir)
		if errors.Is(err, ui.ErrNoInput) {
			return "", errAssignmentsDirNotSet
		}
		if err != nil {
			return "", err
		}
		dir, err := prepareAssignmentsDir(answer)
		var de *dirError
		if errors.As(err, &de) {
			p.Warn("Can't use that folder: " + de.why)
			question = "" // re-ask the folder line only
			continue
		}
		if err != nil {
			return "", err
		}
		if err := config.SaveSettings(config.Settings{AssignmentsDir: dir}); err != nil {
			return "", err
		}
		p.OK("Assignments folder set to " + ui.Bold(config.TildePath(dir)))
		p.Line(ui.Dim("change it any time with devcraft path set <folder>"))
		p.Blank()
		return dir, nil
	}
	return "", errAssignmentsDirNotSet
}

// prepareAssignmentsDir expands raw (~, relative), creates the folder if
// needed and checks it can hold assignments. Problems are *dirError.
func prepareAssignmentsDir(raw string) (string, error) {
	dir, err := config.ExpandPath(raw)
	if err != nil {
		return "", &dirError{dir: raw, why: "that isn't a folder path"}
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return "", &dirError{dir: dir, why: "it's a file, not a folder"}
	}
	if ws, err := workspace.Find(dir); err == nil {
		return "", &dirError{dir: dir, why: "it's inside the assignment " + ws.Metadata.AssignmentID}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", &dirError{dir: dir, why: "it can't be created"}
	}
	probe, err := os.CreateTemp(dir, ".devcraft-write-check-*")
	if err != nil {
		return "", &dirError{dir: dir, why: "you don't have permission to write there"}
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return dir, nil
}

// listWorkspaces returns the assignments directly inside dir, by code.
func listWorkspaces(dir string) []workspace.Metadata {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []workspace.Metadata
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if ws, err := workspace.Load(filepath.Join(dir, e.Name())); err == nil {
			out = append(out, ws.Metadata)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssignmentID < out[j].AssignmentID })
	return out
}

func describeWorkspaces(list []workspace.Metadata) string {
	if len(list) == 0 {
		return "no assignments yet"
	}
	names := make([]string, 0, len(list))
	for _, m := range list {
		names = append(names, fmt.Sprintf("%s (v%s)", m.AssignmentID, m.AssignmentVersion))
	}
	noun := "assignments"
	if len(list) == 1 {
		noun = "assignment"
	}
	return fmt.Sprintf("%d %s: %s", len(list), noun, strings.Join(names, ", "))
}

// displayPath is how to cd into dir from the current folder: relative when
// it is below it, otherwise ~/... or absolute. Paths with spaces are quoted.
func displayPath(dir string) string {
	shown := config.TildePath(dir)
	if rel, err := filepath.Rel(mustGetwd(), dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		shown = rel
	}
	if strings.ContainsAny(shown, " '\"") {
		shown = `"` + shown + `"`
	}
	return shown
}
