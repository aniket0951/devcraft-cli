// Package workspace manages the local assignment workspace metadata stored in
// .devcraft/assignment.json. Credentials are never stored here.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirName       = ".devcraft"
	MetadataFile  = "assignment.json"
	CurrentFormat = 1
)

// ErrNotWorkspace is returned when no workspace is found.
var ErrNotWorkspace = errors.New("not inside a DevCraft assignment workspace (no .devcraft/assignment.json found); run `devcraft start <assignment-id>` first")

// Metadata identifies the assignment version and attempt of a workspace.
type Metadata struct {
	AssignmentID      string `json:"assignment_id"`      // the assignment code, e.g. GO-SRP-01
	AssignmentVersion string `json:"assignment_version"` // semver of the downloaded starter, e.g. 1.0.0
	// AttemptID is empty until the API supports attempts; commands that need
	// one check for it.
	AttemptID       string `json:"attempt_id,omitempty"`
	WorkspaceFormat int    `json:"workspace_format"`
}

// Workspace is a located assignment workspace.
type Workspace struct {
	Root     string
	Metadata Metadata
}

func (m Metadata) validate() error {
	switch {
	case m.AssignmentID == "":
		return errors.New("workspace metadata missing assignment_id")
	case m.WorkspaceFormat < 1:
		return errors.New("workspace metadata missing workspace_format")
	case m.WorkspaceFormat > CurrentFormat:
		return fmt.Errorf("workspace format %d is newer than this CLI supports (%d); please upgrade devcraft", m.WorkspaceFormat, CurrentFormat)
	}
	return nil
}

// Write stores metadata in root/.devcraft/assignment.json.
func Write(root string, m Metadata) error {
	if m.WorkspaceFormat == 0 {
		m.WorkspaceFormat = CurrentFormat
	}
	if err := m.validate(); err != nil {
		return err
	}
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, MetadataFile), append(data, '\n'), 0o644)
}

// Load reads the workspace whose root is exactly root (it doesn't look in
// parent folders). It returns ErrNotWorkspace if root has no metadata.
func Load(root string) (*Workspace, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, DirName, MetadataFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotWorkspace
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Workspace{Root: root, Metadata: m}, nil
}

// Find walks up from start looking for a workspace.
func Find(start string) (*Workspace, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(dir, DirName, MetadataFile)
		data, err := os.ReadFile(path)
		if err == nil {
			var m Metadata
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
			if err := m.validate(); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			return &Workspace{Root: dir, Metadata: m}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotWorkspace
		}
		dir = parent
	}
}
