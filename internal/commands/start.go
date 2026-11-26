package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aniket0951/devcraft-cli.git/internal/archive"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
	"github.com/spf13/cobra"
)

// maxStarterDownload bounds the starter ZIP size, whatever the API claims.
const maxStarterDownload int64 = 100 << 20

var (
	assignmentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sha256HexPattern    = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

func startCmd() *cobra.Command {
	var (
		dir   string
		noGit bool
	)

	cmd := &cobra.Command{
		Use:     "start <assignment-code>",
		Short:   "Download an assignment's starter files into a new workspace",
		Example: "  devcraft start GO-SRP-01\n  devcraft start GO-SRP-01 --dir ~/code/srp --no-git",
		Args:    cobra.ExactArgs(1),
		RunE: railRun("start assignment", func(ctx context.Context, cmd *cobra.Command, p *ui.Printer, args []string) error {
			return runStart(ctx, cmd, p, args[0], dir, noGit)
		}),
	}

	cmd.Flags().StringVar(&dir, "dir", "", "exact folder for this assignment (default: <assignments folder>/<CODE>, see devcraft path)")
	cmd.Flags().BoolVar(&noGit, "no-git", false, "do not initialise a git repository in the workspace")
	return cmd
}

// runStart prints the START flow. It shows its own failures where it can say
// more than describe would; anything else is shown by railRun.
func runStart(ctx context.Context, cmd *cobra.Command, p *ui.Printer, arg, dir string, noGit bool) error {
	const action = "start assignment"

	code := strings.ToUpper(arg) // codes are upper-case; the API is case-insensitive
	p.Tag("START", code)
	if !assignmentIDPattern.MatchString(arg) {
		err := fmt.Errorf("invalid assignment code %q", arg)
		return railFail(p, failure{"Invalid assignment code", fmt.Sprintf("%q isn't an assignment code", arg), "codes look like GO-SRP-01"}, action, err)
	}

	// <assignments folder>/<CODE>, asking for the folder the first time;
	// --dir picks the exact folder for this one assignment instead.
	var dest string
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return railFail(p, failure{"Invalid directory", err.Error(), ""}, action, err)
		}
		dest = abs
	} else {
		root, err := assignmentsDir(ctx, p)
		if err != nil {
			return err
		}
		dest = filepath.Join(root, code)
	}

	if ws, err := workspace.Load(dest); err == nil && strings.EqualFold(ws.Metadata.AssignmentID, code) {
		p.Warn(fmt.Sprintf("%s v%s is already downloaded", ws.Metadata.AssignmentID, ws.Metadata.AssignmentVersion))
		p.Done("cd "+displayPath(dest), ui.Dim(dest))
		return nil
	}
	// Check the destination before calling the API.
	if err := ensureEmptyOrMissing(dest); err != nil {
		return railFail(p, failure{"Folder not usable", err.Error(), "use --dir to choose another location"}, action, err)
	}
	if ws, err := workspace.Find(filepath.Dir(dest)); err == nil {
		err := fmt.Errorf("%s is inside another assignment workspace (%s)", dest, ws.Root)
		return railFail(p, failure{"Folder not usable", err.Error(), "use --dir to choose another location"}, action, err)
	}

	baseURL, err := apiBaseURL(cmd)
	if err != nil {
		return err
	}

	dl, err := fetchDownload(ctx, p, baseURL, code)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 && apiErr.Code != "" {
			return railFail(p, failure{"Assignment not found", "there's no published assignment " + code, "check the code with devcraft assignments"}, action, err)
		}
		return err // railRun describes it
	}
	if err := checkDownload(dl.AssignmentDownload); err != nil {
		return railFail(p, failure{"Something went wrong", "DevCraft sent back an incomplete download", hintTryLater}, action, err)
	}

	bar := p.Progress("Downloading starter", dl.SizeBytes)
	zipFile, err := downloadStarter(ctx, dl.client, dl.AssignmentDownload, bar.Writer())
	if err != nil {
		if isInterrupt(err) {
			bar.Fail("Download cancelled")
			return err
		}
		bar.Fail("Download failed")
		return reasonFail(p, describeDownload(err), action, err)
	}
	defer removeTemp(zipFile)
	bar.OK(fmt.Sprintf("Downloaded %s v%s starter", dl.AssignmentCode, dl.Version))

	if err := installWorkspace(zipFile, dl.AssignmentDownload, dest); err != nil {
		p.Fail("Couldn't set up the workspace")
		return reasonFail(p, describeDownload(err), action, err)
	}

	if !noGit {
		if err := gitInit(dest); err != nil {
			p.Warn("Couldn't initialise a git repository: " + err.Error())
		}
	}

	p.Done("cd "+displayPath(dest), ui.Dim(dest))
	return nil
}

// startDownload is a download response and the client that fetched it.
type startDownload struct {
	*client.AssignmentDownload
	client *client.Client
}

// fetchDownload asks the API for the starter download, signing in first if
// needed. If the API rejects a token that looked valid (401), it offers to
// sign in again and retries once.
func fetchDownload(ctx context.Context, p *ui.Printer, baseURL, code string) (*startDownload, error) {
	forceExpired := false
	for attempt := 0; ; attempt++ {
		s, err := requireSession(ctx, p, forceExpired)
		if err != nil {
			return nil, err
		}
		c, err := newClient(baseURL, s)
		if err != nil {
			return nil, err
		}
		dl, err := c.GetAssignmentDownload(ctx, code)
		if client.IsUnauthorized(err) && attempt == 0 {
			forceExpired = true
			continue
		}
		if err != nil {
			return nil, err
		}
		return &startDownload{dl, c}, nil
	}
}

// Errors from setupWorkspace that describeDownload explains to the learner.
var (
	errStarterCorrupt = errors.New("starter archive doesn't match")
	errStarterUnpack  = errors.New("extract starter files")
)

const (
	hintRunAgain  = "run this command again"
	hintTryLater  = "try again in a few minutes"
	hintCheckNet  = "check your internet connection, VPN or firewall, then run this command again"
	whyCantReach  = "couldn't connect to the download server"
	whyCorrupt    = "the downloaded files were incomplete or damaged"
	whyUnpack     = "the starter files couldn't be unpacked"
	whyExpired    = "the download link expired"
	whyInterrupt  = "the download was interrupted"
	whyServerFail = "the download server returned an error"
)

// describeDownload explains a setupWorkspace failure in learner terms: no
// hostnames, IPs or Go error text. The full error is still returned to main
// for the exit code.
func describeDownload(err error) failure {
	var (
		apiErr *client.APIError
		opErr  *net.OpError
		dnsErr *net.DNSError
		netErr net.Error
	)
	switch {
	case errors.Is(err, errStarterCorrupt):
		return failure{why: whyCorrupt, hint: hintRunAgain}
	case errors.Is(err, errStarterUnpack):
		return failure{why: whyUnpack, hint: hintRunAgain}
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden:
		return failure{why: whyExpired, hint: hintRunAgain}
	case errors.As(err, &apiErr):
		return failure{why: whyServerFail, hint: hintTryLater}
	case errors.As(err, &dnsErr), errors.As(err, &opErr) && opErr.Op == "dial":
		return failure{why: whyCantReach, hint: hintCheckNet}
	case errors.As(err, &netErr), errors.Is(err, io.ErrUnexpectedEOF):
		return failure{why: whyInterrupt, hint: "check your internet connection, then run this command again"}
	default: // local problems: disk full, permissions...
		return failure{why: err.Error()}
	}
}

// checkDownload rejects a download response the CLI can't verify.
func checkDownload(dl *client.AssignmentDownload) error {
	switch {
	case dl.DownloadURL == "":
		return errors.New("start assignment: API returned no download URL")
	case dl.AssignmentCode == "" || !assignmentIDPattern.MatchString(dl.AssignmentCode):
		return fmt.Errorf("start assignment: API returned an invalid assignment code %q", dl.AssignmentCode)
	case dl.Version == "":
		return errors.New("start assignment: API returned no version")
	case !sha256HexPattern.MatchString(dl.SHA256Hex):
		return errors.New("start assignment: API returned an invalid checksum")
	case dl.SizeBytes <= 0 || dl.SizeBytes > maxStarterDownload:
		return fmt.Errorf("start assignment: API returned an invalid size (%d bytes)", dl.SizeBytes)
	}
	return nil
}

// downloadStarter downloads the starter ZIP into a temp file, copying every
// byte to progress too, and checks its size and SHA-256 against the API's
// values. The caller removes the file with removeTemp.
func downloadStarter(ctx context.Context, c *client.Client, dl *client.AssignmentDownload, progress io.Writer) (*os.File, error) {
	tmpFile, err := os.CreateTemp("", "devcraft-starter-*.zip")
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			removeTemp(tmpFile)
		}
	}()

	hasher := sha256.New()
	n, err := c.Download(ctx, dl.DownloadURL, io.MultiWriter(tmpFile, hasher, progress), dl.SizeBytes)
	if errors.Is(err, client.ErrDownloadTooLarge) {
		return nil, fmt.Errorf("%w: larger than the expected %d bytes", errStarterCorrupt, dl.SizeBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("download starter files: %w", err)
	}
	if n != dl.SizeBytes {
		return nil, fmt.Errorf("%w: size mismatch (got %d bytes, want %d)", errStarterCorrupt, n, dl.SizeBytes)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, dl.SHA256Hex) {
		return nil, fmt.Errorf("%w: checksum mismatch (got %s, want %s)", errStarterCorrupt, got, strings.ToLower(dl.SHA256Hex))
	}
	ok = true
	return tmpFile, nil
}

func removeTemp(f *os.File) {
	_ = f.Close()
	_ = os.Remove(f.Name())
}

// installWorkspace extracts the verified ZIP into a temporary sibling
// directory, writes metadata, then renames it into place so a failure never
// leaves a half-written workspace behind.
func installWorkspace(zipFile *os.File, dl *client.AssignmentDownload, dest string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".devcraft-start-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }() // no-op after successful rename

	if err := archive.ExtractZip(zipFile, dl.SizeBytes, staging, archive.DefaultLimits); err != nil {
		return fmt.Errorf("%w: %w", errStarterUnpack, err)
	}
	if err := workspace.Write(staging, workspace.Metadata{
		AssignmentID:      dl.AssignmentCode,
		AssignmentVersion: dl.Version,
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
