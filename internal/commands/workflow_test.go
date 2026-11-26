package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
)

func TestVersion(t *testing.T) {
	out, err := runCLI(t, nil, "version")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "devcraft 0.0.1")

	out, _ = runCLI(t, nil, "--version")
	assertContains(t, out, "devcraft 0.0.1")
}

func TestCommands_RequireLogin(t *testing.T) {
	setupTestEnv(t)
	f := newFakeAPI(t)
	newWorkspace(t, map[string]string{"main.go": "package main"})

	// every command fails the same way, through railRun
	for _, tc := range []struct{ cmd, action string }{
		{"assignments", "list assignments"}, {"test", "run tests"}, {"submit", "submit"},
		{"status", "show status"}, {"results", "show results"},
	} {
		out, err := runCLI(t, f, tc.cmd)
		if !errors.Is(err, errNotLoggedIn) {
			t.Errorf("%s: err = %v, want errNotLoggedIn", tc.cmd, err)
		}
		assertContains(t, out, "! You're not signed in", "✗ Not signed in", "hint: run devcraft login", "FAIL", "Couldn't "+tc.action)
		if HandleError(err, io.Discard) != exitFail {
			t.Errorf("%s: want exit 1", tc.cmd)
		}
	}

	// start must run outside a workspace.
	chdirAssignments(t)
	if _, err := runCLI(t, f, "start", "GO-SOLID-001"); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("start: err = %v, want errNotLoggedIn", err)
	}
}

func TestCommands_ExpiredSession(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.unauthorized = true

	out, err := runCLI(t, f, "assignments")
	if !client.IsUnauthorized(err) {
		t.Fatalf("err = %v, want 401", err)
	}
	assertContains(t, out, "✗ Session expired", "hint: run devcraft login, then run this command again", "Couldn't list assignments")
}

func TestCommands_OutsideWorkspace(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	chdirAssignments(t)

	for _, cmd := range []string{"test", "submit", "status", "results"} {
		out, err := runCLI(t, f, cmd)
		if !errors.Is(err, workspace.ErrNotWorkspace) {
			t.Errorf("%s: err = %v, want ErrNotWorkspace", cmd, err)
		}
		assertContains(t, out, "✗ Not in an assignment folder", "hint: cd into your assignment folder")
	}
}

func TestAssignments(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)

	out, err := runCLI(t, f, "assignments")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "ASSIGNMENTS", "CODE", "NAME", "CATEGORY", "LEVEL", "VERSION",
		"GO-SOLID-001", "SOLID in Go", "SOLID Principles", "v1.2.0", "GO-CONC-002", "Worker pool",
		"DONE", "devcraft start <CODE>", "2 available")

	out, err = runCLI(t, f, "assignments", "--category", "solid principles", "--level", "l1")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "GO-SOLID-001", "1 available")
	assertNotContains(t, out, "GO-CONC-002")

	out, err = runCLI(t, f, "assignments", "--language", "rust")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "! No assignments found", "hint: try again without --language, --category or --level")
	assertNotContains(t, out, "DONE")
}

func TestAssignments_MissingEndpoint(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)

	// an older server without GET /v1/assignments answers with a bare 404
	out, err := runCLI(t, nil, "assignments", "--api-url", f.srv.URL+"/old")
	if err == nil {
		t.Fatal("expected error")
	}
	assertContains(t, out, "✗ Not available yet", "FAIL", "Couldn't list assignments")
	assertNotContains(t, out, "api error", "404")
	if HandleError(err, io.Discard) != exitFail {
		t.Fatal("want exit 1")
	}
}

func TestStart_CreatesWorkspace(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	cwd := chdirAssignments(t)

	out, err := runCLI(t, f, "start", "go-solid-001", "--no-git") // codes are case-insensitive
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	dest := filepath.Join(cwd, "GO-SOLID-001")
	assertContains(t, out, "START", "✓ Downloaded GO-SOLID-001 v1.2.0 starter  324 B in ", "DONE", "cd GO-SOLID-001", dest)
	if strings.Contains(out, "secret") {
		t.Fatalf("output leaks the pre-signed URL:\n%s", out)
	}

	// the ZIP's single top-level folder is stripped
	if b, err := os.ReadFile(filepath.Join(dest, "main.go")); err != nil || string(b) != "package main\n" {
		t.Fatalf("starter file: %q %v", b, err)
	}
	ws, err := workspace.Find(dest)
	if err != nil {
		t.Fatal(err)
	}
	want := workspace.Metadata{AssignmentID: "GO-SOLID-001", AssignmentVersion: "1.2.0", WorkspaceFormat: 1}
	if ws.Metadata != want {
		t.Fatalf("metadata = %+v", ws.Metadata)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatal("--no-git should not create .git")
	}
	// No staging directories left behind.
	entries, _ := os.ReadDir(cwd)
	if len(entries) != 1 {
		t.Fatalf("unexpected entries in cwd: %v", entries)
	}
}

func TestStart_InitsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	loginForTest(t)
	f := newFakeAPI(t)
	cwd := chdirAssignments(t)

	if out, err := runCLI(t, f, "start", "GO-SOLID-001", "--dir", "work"); err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(cwd, "work", ".git")); err != nil {
		t.Fatalf("expected git repo: %v", err)
	}
}

func TestStart_RefusesNonEmptyDirWithoutCallingAPI(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	cwd := chdirAssignments(t)
	_ = os.MkdirAll(filepath.Join(cwd, "GO-SOLID-001"), 0o755)
	_ = os.WriteFile(filepath.Join(cwd, "GO-SOLID-001", "mine.txt"), []byte("x"), 0o644)

	_, err := runCLI(t, f, "start", "GO-SOLID-001")
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v", err)
	}
	if f.downloadCalls != 0 {
		t.Fatal("API should not be called when destination is unusable")
	}
}

func TestStart_ChecksumMismatchLeavesNothing(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.starterChecksum = strings.Repeat("0", 64)
	cwd := chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	assertContains(t, out, "✗ Download failed", "the downloaded files were incomplete or damaged", "hint: run this command again")
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestStart_SizeMismatchLeavesNothing(t *testing.T) {
	for name, delta := range map[string]int64{"bigger than reported": -1, "smaller than reported": 1} {
		t.Run(name, func(t *testing.T) {
			loginForTest(t)
			f := newFakeAPI(t)
			f.starterSize += delta
			cwd := chdirAssignments(t)

			_, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
			if err == nil || !strings.Contains(err.Error(), "bytes") {
				t.Fatalf("err = %v", err)
			}
			if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
				t.Fatalf("leftover files: %v", entries)
			}
		})
	}
}

func TestStart_InvalidDownloadResponse(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.starterChecksum = "not-hex"
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err == nil || !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("err = %v", err)
	}
	assertContains(t, out, "DevCraft sent back an incomplete download")
	assertNotContains(t, out, "API", "checksum")
}

func TestStart_NotAZipLeavesNothing(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.starter = []byte("this is not a zip file")
	sum := sha256.Sum256(f.starter)
	f.starterChecksum, f.starterSize = hex.EncodeToString(sum[:]), int64(len(f.starter))
	cwd := chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err == nil || !strings.Contains(err.Error(), "extract starter files") {
		t.Fatalf("err = %v", err)
	}
	assertContains(t, out, "✓ Downloaded", "✗ Couldn't set up the workspace", "the starter files couldn't be unpacked", "Couldn't start assignment")
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestStart_InvalidID(t *testing.T) {
	loginForTest(t)
	if _, err := runCLI(t, newFakeAPI(t), "start", "../etc"); err == nil {
		t.Fatal("expected invalid code error")
	}
}

func TestStart_UnknownAssignment(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	chdirAssignments(t)
	out, err := runCLI(t, f, "start", "NOPE-1", "--no-git")
	if !client.IsNotFound(err) {
		t.Fatalf("err = %v, want 404", err)
	}
	assertContains(t, out, "✗ Assignment not found", "no published assignment NOPE-1", "devcraft assignments", "FAIL", "Couldn't start assignment")
	if HandleError(err, io.Discard) != exitFail {
		t.Fatal("want exit code 1")
	}
}

func TestCommands_WorkspaceWithoutAttempt(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	root := t.TempDir()
	if err := workspace.Write(root, workspace.Metadata{AssignmentID: "GO-SOLID-001", AssignmentVersion: "1.2.0"}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	for _, cmd := range []string{"test", "submit", "status", "results"} {
		if _, err := runCLI(t, f, cmd); !errors.Is(err, errNoAttempt) {
			t.Errorf("%s: err = %v, want errNoAttempt", cmd, err)
		}
	}
}

func TestTest_UploadsSnapshotAndShowsPass(t *testing.T) {
	loginForTest(t)
	fastPolling(t)
	f := newFakeAPI(t)
	f.evalPolls = 2
	newWorkspace(t, map[string]string{
		"main.go":     "package main",
		".env":        "API_KEY=secret",
		".git/HEAD":   "ref: refs/heads/main",
		"tmp/scratch": "x",
	})

	out, err := runCLI(t, f, "test")
	if err != nil {
		t.Fatalf("test: %v\n%s", err, out)
	}
	assertContains(t, out, "Packaged 1 files", "Evaluation eval_1: running", "Evaluation eval_1: completed", "PASS", "score 10/10", "✓ build")

	upload := f.uploads["art_quick_evaluation"]
	if got := tarNames(t, upload); !slices.Equal(got, []string{"main.go"}) {
		t.Fatalf("uploaded entries = %v", got)
	}
	for _, a := range f.uploadAuth {
		if a != "" {
			t.Fatal("API token leaked to upload URL")
		}
	}
}

func TestTest_FailedChecksExitNonZero(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.evaluation = client.Evaluation{
		ID: "eval_1", Kind: "quick", Status: client.EvaluationCompleted, Passed: false, Score: 3, MaxScore: 10,
		Checks:      []client.Check{{Name: "unit tests", Status: client.CheckFailed, Message: "expected 3, got 2"}},
		Suggestions: []string{"Split the Store interface"},
	}
	newWorkspace(t, map[string]string{"main.go": "package main"})

	out, err := runCLI(t, f, "test")
	if !errors.Is(err, errEvaluationFailed) {
		t.Fatalf("err = %v, want errEvaluationFailed", err)
	}
	assertContains(t, out, "FAIL", "✗ unit tests", "→ expected 3, got 2", "Suggestions", "Split the Store interface")
}

func TestTest_InfraErrorIsNotLearnerFailure(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.evaluation = client.Evaluation{
		ID: "eval_1", Status: client.EvaluationInfraError,
		InfraError: &client.InfraError{Code: "sandbox_start_failed", Message: "worker unavailable"},
	}
	newWorkspace(t, map[string]string{"main.go": "package main"})

	out, err := runCLI(t, f, "test")
	if !errors.Is(err, errInfrastructure) {
		t.Fatalf("err = %v, want errInfrastructure", err)
	}
	assertContains(t, out, "infrastructure problem: sandbox_start_failed", "worker unavailable")
	if strings.Contains(out, "FAIL") {
		t.Fatal("infra error must not be shown as a learner failure")
	}
}

func TestTest_BlocksSecrets(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	newWorkspace(t, map[string]string{"main.go": `const k = "AKIAABCDEFGHIJKLMNOP"`})

	_, err := runCLI(t, f, "test")
	if err == nil || !strings.Contains(err.Error(), "possible secrets") {
		t.Fatalf("err = %v, want secrets block", err)
	}
	if len(f.uploads) != 0 {
		t.Fatal("nothing should be uploaded when secrets are found")
	}

	out, err := runCLI(t, f, "test", "--allow-secrets")
	if err != nil {
		t.Fatalf("with --allow-secrets: %v\n%s", err, out)
	}
	assertContains(t, out, "warning: uploading files that look like secrets")
}

func TestSubmit_VerifiedChecksumAndResult(t *testing.T) {
	loginForTest(t)
	fastPolling(t)
	f := newFakeAPI(t)
	f.evaluation.Kind = "formal"
	newWorkspace(t, map[string]string{"main.go": "package main"})

	out, err := runCLI(t, f, "submit")
	if err != nil {
		t.Fatalf("submit: %v\n%s", err, out)
	}
	assertContains(t, out, "Submission sub_1 created", "formal evaluation", "PASS")
	if _, ok := f.uploads["art_submission"]; !ok {
		t.Fatal("submission archive not uploaded")
	}
}

func TestSubmit_NoWait(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	newWorkspace(t, map[string]string{"main.go": "package main"})

	out, err := runCLI(t, f, "submit", "--no-wait")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "devcraft results --submission sub_1")
	if strings.Contains(out, "PASS") {
		t.Fatal("--no-wait should not wait for results")
	}
}

func TestStatusAndResults(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	newWorkspace(t, map[string]string{"main.go": "package main"})

	out, err := runCLI(t, f, "status")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "GO-SOLID-001 (version 1.2.0)", "att_1 (in_progress)", "Submission:  none yet", "Evaluation:  none yet")

	out, err = runCLI(t, f, "results")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "No evaluations yet")

	if _, err := runCLI(t, f, "submit", "--no-wait"); err != nil {
		t.Fatal(err)
	}

	out, err = runCLI(t, f, "status")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "Submission:  sub_1", "Evaluation:  eval_1 (quick, PASSED, score 10/10)")

	for _, args := range [][]string{{"results"}, {"results", "--submission", "sub_1"}, {"results", "--evaluation", "eval_1"}} {
		out, err := runCLI(t, f, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		assertContains(t, out, "PASS", "✓ build")
	}

	if _, err := runCLI(t, f, "results", "--submission", "s", "--evaluation", "e"); err == nil {
		t.Fatal("expected error when both flags are set")
	}
}
