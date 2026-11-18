package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndFindFromSubdir(t *testing.T) {
	root := t.TempDir()
	m := Metadata{AssignmentID: "GO-SOLID-001", AssignmentVersion: 2, AttemptID: "att_1"}
	if err := Write(root, m); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	ws, err := Find(sub)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRoot, _ := filepath.EvalSymlinks(ws.Root)
	if gotRoot != wantRoot {
		t.Fatalf("root = %s, want %s", ws.Root, root)
	}
	m.WorkspaceFormat = CurrentFormat
	if ws.Metadata != m {
		t.Fatalf("metadata = %+v, want %+v", ws.Metadata, m)
	}

	data, _ := os.ReadFile(filepath.Join(root, DirName, MetadataFile))
	if strings.Contains(strings.ToLower(string(data)), "token") {
		t.Fatal("metadata must not contain tokens")
	}
}

func TestFind_NotWorkspace(t *testing.T) {
	if _, err := Find(t.TempDir()); !errors.Is(err, ErrNotWorkspace) {
		t.Fatalf("err = %v, want ErrNotWorkspace", err)
	}
}

func TestFind_RejectsNewerFormat(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, DirName)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, MetadataFile),
		[]byte(`{"assignment_id":"A","attempt_id":"B","workspace_format":99}`), 0o644)

	if _, err := Find(root); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("err = %v, want upgrade hint", err)
	}
}

func TestWrite_RequiresIDs(t *testing.T) {
	if err := Write(t.TempDir(), Metadata{AttemptID: "x"}); err == nil {
		t.Fatal("expected error for missing assignment_id")
	}
	if err := Write(t.TempDir(), Metadata{AssignmentID: "x"}); err == nil {
		t.Fatal("expected error for missing attempt_id")
	}
}
