package commands

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aniket0951/devcraft-cli.git/internal/config"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
)

// homeDir is the test's HOME (set by setupTestEnv).
func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func savedAssignmentsDir(t *testing.T) string {
	t.Helper()
	s, err := config.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	return s.AssignmentsDir
}

/* -------------------------------------------------------------------------- */
/*                                devcraft path                               */
/* -------------------------------------------------------------------------- */

func TestPath_NotSet(t *testing.T) {
	setupTestEnv(t)
	out, err := runCLI(t, nil, "path")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "PATH", "! Not set yet", "hint: devcraft start will ask you, or run devcraft path set <folder>")
}

func TestPathSet_ThenShowListsAssignments(t *testing.T) {
	setupTestEnv(t)
	home := homeDir(t)

	out, err := runCLI(t, nil, "path", "set", "~/code/devcraft")
	if err != nil {
		t.Fatalf("path set: %v\n%s", err, out)
	}
	want := filepath.Join(home, "code", "devcraft")
	assertContains(t, out, "✓ Assignments folder set to "+filepath.Join("~", "code", "devcraft"), "DONE", "devcraft start <CODE>")
	if got := savedAssignmentsDir(t); got != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("folder should be created: %v", err)
	}

	out, _ = runCLI(t, nil, "path")
	assertContains(t, out, "✓ "+filepath.Join("~", "code", "devcraft"), "no assignments yet")

	for code, version := range map[string]string{"GO-SRP-01": "1.0.0", "GO-OCP-02": "1.1.0"} {
		if err := workspace.Write(filepath.Join(want, code), workspace.Metadata{AssignmentID: code, AssignmentVersion: version}); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Mkdir(filepath.Join(want, "notes"), 0o755) // not an assignment

	out, _ = runCLI(t, nil, "path")
	assertContains(t, out, "2 assignments: GO-OCP-02 (v1.1.0), GO-SRP-01 (v1.0.0)", "change it with devcraft path set <folder>")
}

func TestPathSet_ChangingKeepsOldAssignments(t *testing.T) {
	setupTestEnv(t)
	if _, err := runCLI(t, nil, "path", "set", "~/first"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, nil, "path", "set", "~/second")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "assignments already in "+filepath.Join("~", "first")+" stay there")
	if got := savedAssignmentsDir(t); got != filepath.Join(homeDir(t), "second") {
		t.Fatalf("saved %q", got)
	}
}

func TestPathSet_RejectsUnusableFolders(t *testing.T) {
	setupTestEnv(t)
	home := homeDir(t)

	file := filepath.Join(home, "a-file")
	_ = os.WriteFile(file, []byte("x"), 0o644)

	ws := filepath.Join(home, "GO-SRP-01")
	_ = workspace.Write(ws, workspace.Metadata{AssignmentID: "GO-SRP-01", AssignmentVersion: "1.0.0"})

	cases := map[string]struct{ arg, why string }{
		"file":              {file, "it's a file, not a folder"},
		"inside assignment": {filepath.Join(ws, "sub"), "it's inside the assignment GO-SRP-01"},
	}
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		readOnly := filepath.Join(home, "read-only")
		_ = os.Mkdir(readOnly, 0o555)
		t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
		cases["not writable"] = struct{ arg, why string }{readOnly, "you don't have permission to write there"}
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := runCLI(t, nil, "path", "set", tc.arg)
			var de *dirError
			if !errors.As(err, &de) || HandleError(err, io.Discard) != exitFail {
				t.Fatalf("err = %v, want dirError and exit 1", err)
			}
			assertContains(t, out, "✗ Can't use that folder", "→ "+tc.why, "hint: choose another folder", "Couldn't set assignments folder")
			if savedAssignmentsDir(t) != "" {
				t.Fatal("nothing should be saved")
			}
		})
	}
}

func TestPath_EnvOverride(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	t.Setenv(config.EnvAssignmentsDir, dir)

	out, _ := runCLI(t, nil, "path")
	assertContains(t, out, "✓ "+dir, "from "+config.EnvAssignmentsDir)

	out, _ = runCLI(t, nil, "path", "set", "~/saved")
	assertContains(t, out, "! "+config.EnvAssignmentsDir+" is set and is used instead of this folder")
}

/* -------------------------------------------------------------------------- */
/*                      devcraft start and the folder                         */
/* -------------------------------------------------------------------------- */

func TestStart_FirstRunAsksForFolderOnce(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	t.Chdir(t.TempDir())
	answerPrompts(t, "\n") // Enter takes ~/devcraft

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	root := filepath.Join(homeDir(t), "devcraft")
	dest := filepath.Join(root, "GO-SOLID-001")
	assertContains(t, out, "? Where should DevCraft keep your assignments?", "Folder [~/devcraft]:",
		"✓ Assignments folder set to "+filepath.Join("~", "devcraft"), "✓ Downloaded GO-SOLID-001",
		"cd "+filepath.Join("~", "devcraft", "GO-SOLID-001"), dest)
	if savedAssignmentsDir(t) != root {
		t.Fatalf("saved %q, want %q", savedAssignmentsDir(t), root)
	}
	if _, err := workspace.Load(dest); err != nil {
		t.Fatalf("workspace not created in the folder: %v", err)
	}

	// The second start never asks, and doesn't download the same assignment again.
	answerPrompts(t, "")
	out, err = runCLI(t, f, "start", "go-solid-001")
	if err != nil {
		t.Fatalf("second start: %v\n%s", err, out)
	}
	assertContains(t, out, "! GO-SOLID-001 v1.2.0 is already downloaded", "cd "+filepath.Join("~", "devcraft", "GO-SOLID-001"))
	assertNotContains(t, out, "Where should DevCraft", "Downloaded")
	if f.downloadCalls != 1 {
		t.Fatalf("download calls = %d, want 1", f.downloadCalls)
	}
}

func TestStart_FirstRunReasksAfterUnusableFolder(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	t.Chdir(t.TempDir())
	file := filepath.Join(homeDir(t), "a-file")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	answerPrompts(t, file+"\n~/my assignments\n")

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	assertContains(t, out, "! Can't use that folder: it's a file, not a folder",
		`cd "`+filepath.Join("~", "my assignments", "GO-SOLID-001")+`"`) // quoted: has a space
	if strings.Count(out, "Where should DevCraft keep your assignments?") != 1 || strings.Count(out, "Folder [~/devcraft]:") != 2 {
		t.Fatalf("should re-ask only the folder line:\n%s", out)
	}
	if savedAssignmentsDir(t) != filepath.Join(homeDir(t), "my assignments") {
		t.Fatalf("saved %q", savedAssignmentsDir(t))
	}
}

func TestStart_FolderNotSetWithoutTerminal(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	t.Chdir(t.TempDir())

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if !errors.Is(err, errAssignmentsDirNotSet) {
		t.Fatalf("err = %v, want errAssignmentsDirNotSet", err)
	}
	assertContains(t, out, "✗ Assignments folder not set", "hint: run devcraft path set <folder>, then run this command again", "Couldn't start assignment")
	if f.requests != 0 {
		t.Fatal("must not call the API before knowing where to download")
	}
}

func TestStart_UsesSavedFolderFromAnywhere(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	if _, err := runCLI(t, nil, "path", "set", "~/devcraft"); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	if _, err := workspace.Load(filepath.Join(homeDir(t), "devcraft", "GO-SOLID-001")); err != nil {
		t.Fatalf("not downloaded into the saved folder: %v", err)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("nothing should be written to the current folder: %v", entries)
	}
}

func TestStart_DirFlagOverridesFolder(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	cwd := t.TempDir()
	t.Chdir(cwd) // no folder saved, no terminal: --dir alone is enough

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git", "--dir", "here")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	assertContains(t, out, "cd here")
	if _, err := workspace.Load(filepath.Join(cwd, "here")); err != nil {
		t.Fatal(err)
	}
	if savedAssignmentsDir(t) != "" {
		t.Fatal("--dir must not save a folder")
	}
}
