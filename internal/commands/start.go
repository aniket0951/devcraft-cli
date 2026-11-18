package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/aniket0951/devcraft-cli.git/internal/archive"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

// maxStarterDownload bounds the compressed starter archive size.
const maxStarterDownload int64 = 100 << 20

var assignmentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func startCmd() *cobra.Command {
	var (
		dir   string
		noGit bool
	)

	cmd := &cobra.Command{
		Use:     "start <assignment-id>",
		Short:   "Start an assignment and download its starter workspace",
		Example: "  devcraft start GO-SOLID-001\n  devcraft start GO-SOLID-001 --dir ~/code/solid --no-git",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			assignmentID := args[0]
			if !assignmentIDPattern.MatchString(assignmentID) {
				return fmt.Errorf("invalid assignment ID %q", assignmentID)
			}
			if dir == "" {
				dir = assignmentID
			}
			dest, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			// Check the destination before creating an attempt on the server.
			if err := ensureEmptyOrMissing(dest); err != nil {
				return err
			}
			if ws, err := workspace.Find(filepath.Dir(dest)); err == nil {
				return fmt.Errorf("%s is inside another assignment workspace (%s); choose a different directory", dest, ws.Root)
			}

			c, err := newAPIClient(cmd)
			if err != nil {
				return err
			}
			ctx, stop := commandContext(cmd)
			defer stop()

			attempt, err := c.CreateAttempt(ctx, assignmentID)
			if err != nil {
				return apiError("start assignment", err)
			}
			if attempt.StarterURL == "" {
				return fmt.Errorf("start assignment: API returned no starter download URL")
			}

			cmd.Printf("Created attempt %s for %s (version %d)\n", attempt.ID, assignmentID, attempt.AssignmentVersion)
			cmd.Println("Downloading starter files...")

			if err := setupWorkspace(ctx, c, attempt, assignmentID, dest); err != nil {
				return err
			}

			if !noGit {
				if err := gitInit(dest); err != nil {
					cmd.PrintErrf("warning: could not initialise git repository: %v\n", err)
				}
			}

			rel := dir
			if r, err := filepath.Rel(mustGetwd(), dest); err == nil {
				rel = r
			}
			cmd.Printf("\nWorkspace ready: %s\n\nNext steps:\n  cd %s\n  devcraft test\n", dest, rel)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "directory to create the workspace in (default: ./<assignment-id>)")
	cmd.Flags().BoolVar(&noGit, "no-git", false, "do not initialise a git repository in the workspace")
	return cmd
}

// setupWorkspace downloads and extracts the starter archive into a temporary
// sibling directory, writes metadata, then renames it into place so a failure
// never leaves a half-written workspace behind.
func setupWorkspace(ctx context.Context, c *client.Client, attempt *client.Attempt, assignmentID, dest string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "devcraft-starter-*.tar.gz")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	hasher := sha256.New()
	if _, err := c.Download(ctx, attempt.StarterURL, io.MultiWriter(tmpFile, hasher), maxStarterDownload); err != nil {
		return apiError("download starter files", err)
	}
	if attempt.StarterChecksum != "" {
		if got := hex.EncodeToString(hasher.Sum(nil)); got != attempt.StarterChecksum {
			return fmt.Errorf("starter archive checksum mismatch (got %s, want %s)", got, attempt.StarterChecksum)
		}
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(parent, ".devcraft-start-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }() // no-op after successful rename

	if err := archive.Extract(tmpFile, staging, archive.DefaultLimits); err != nil {
		return fmt.Errorf("extract starter files: %w", err)
	}
	if err := workspace.Write(staging, workspace.Metadata{
		AssignmentID:      assignmentID,
		AssignmentVersion: attempt.AssignmentVersion,
		AttemptID:         attempt.ID,
		WorkspaceFormat:   workspace.CurrentFormat,
	}); err != nil {
		return err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}

	// dest may exist as an empty directory; remove it so rename succeeds.
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(staging, dest)
}

func ensureEmptyOrMissing(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s exists and is not a readable directory", dir)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty; use --dir to choose another location", dir)
	}
	return nil
}

func gitInit(dir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return nil // git is optional
	}
	out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
