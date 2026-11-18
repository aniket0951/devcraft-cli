package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aniket0951/devcraft-cli.git/internal/archive"
	"github.com/aniket0951/devcraft-cli.git/internal/output"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

const allowSecretsFlag = "allow-secrets"

// uploadedSnapshot is a workspace archive that has been uploaded.
type uploadedSnapshot struct {
	ArtifactID string
	Checksum   string
	Size       int64
	FileCount  int
}

// findWorkspace locates the workspace containing the current directory.
func findWorkspace() (*workspace.Workspace, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return workspace.Find(wd)
}

// uploadSnapshot archives the workspace and uploads it for the given purpose.
func uploadSnapshot(ctx context.Context, cmd *cobra.Command, c *client.Client, ws *workspace.Workspace, purpose client.ArtifactPurpose) (*uploadedSnapshot, error) {
	allowSecrets, _ := cmd.Flags().GetBool(allowSecretsFlag)

	tmp, err := os.CreateTemp("", "devcraft-snapshot-*.tar.gz")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	res, err := archive.Create(ws.Root, tmp, archive.DefaultLimits)
	if err != nil {
		return nil, fmt.Errorf("package workspace: %w", err)
	}
	if res.FileCount == 0 {
		return nil, fmt.Errorf("package workspace: no files to upload in %s", ws.Root)
	}
	for _, s := range res.Skipped {
		cmd.PrintErrf("warning: skipped %s (symlinks and special files are not uploaded)\n", s)
	}
	if len(res.Secrets) > 0 {
		var b strings.Builder
		for _, f := range res.Secrets {
			fmt.Fprintf(&b, "  %s: looks like a %s\n", f.Path, f.Reason)
		}
		if !allowSecrets {
			return nil, fmt.Errorf("possible secrets found, upload blocked:\n%sRemove them, or re-run with --%s if these are not real secrets", b.String(), allowSecretsFlag)
		}
		cmd.PrintErrf("warning: uploading files that look like secrets (--%s):\n%s", allowSecretsFlag, b.String())
	}

	cmd.Printf("Packaged %d files (%s), sha256 %s\n", res.FileCount, output.Bytes(res.Size), res.Checksum)

	up, err := c.RequestUploadURL(ctx, client.UploadURLRequest{
		AttemptID: ws.Metadata.AttemptID,
		Purpose:   purpose,
		Checksum:  res.Checksum,
		Size:      res.Size,
	})
	if err != nil {
		return nil, apiError("request upload URL", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return nil, err
	}
	if err := c.UploadArtifact(ctx, up, tmp, res.Size); err != nil {
		return nil, apiError("upload snapshot", err)
	}

	return &uploadedSnapshot{
		ArtifactID: up.ArtifactID,
		Checksum:   res.Checksum,
		Size:       res.Size,
		FileCount:  res.FileCount,
	}, nil
}
