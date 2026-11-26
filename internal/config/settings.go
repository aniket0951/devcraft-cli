package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvAssignmentsDir overrides the saved assignments folder (CI, tests).
const EnvAssignmentsDir = "DEVCRAFT_ASSIGNMENTS_DIR"

// DefaultAssignmentsDir is what the first-run prompt suggests.
const DefaultAssignmentsDir = "~/devcraft"

const (
	configDirName = "devcraft"
	settingsFile  = "config.json"
)

// Settings are the user's saved CLI preferences.
type Settings struct {
	// AssignmentsDir is the parent folder every assignment is downloaded
	// into, as <AssignmentsDir>/<CODE>. Absolute; empty until chosen.
	AssignmentsDir string `json:"assignments_dir,omitempty"`
}

// SettingsPath is where Settings are stored:
// <user config dir>/devcraft/config.json.
func SettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(dir, configDirName, settingsFile), nil
}

// LoadSettings reads the saved settings; a missing file is empty Settings.
func LoadSettings() (Settings, error) {
	var s Settings
	path, err := SettingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read settings: %w", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("read settings %s: %w", path, err)
	}
	return s, nil
}

// SaveSettings writes s atomically (temp file + rename), readable only by
// the user.
func SaveSettings(s Settings) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after rename
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save settings: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// AssignmentsDirSource says where the assignments folder came from.
type AssignmentsDirSource int

const (
	SourceNone     AssignmentsDirSource = iota // not chosen yet
	SourceEnv                                  // DEVCRAFT_ASSIGNMENTS_DIR
	SourceSettings                             // saved with `devcraft path set` or the first-run prompt
)

// ResolveAssignmentsDir returns the assignments folder: the environment
// variable if set, else the saved setting, else "" with SourceNone. The
// result is expanded (~) and absolute but not checked on disk.
func ResolveAssignmentsDir() (string, AssignmentsDirSource, error) {
	if v := strings.TrimSpace(os.Getenv(EnvAssignmentsDir)); v != "" {
		dir, err := ExpandPath(v)
		if err != nil {
			return "", SourceEnv, fmt.Errorf("%s: %w", EnvAssignmentsDir, err)
		}
		return dir, SourceEnv, nil
	}
	s, err := LoadSettings()
	if err != nil {
		return "", SourceNone, err
	}
	if s.AssignmentsDir == "" {
		return "", SourceNone, nil
	}
	return s.AssignmentsDir, SourceSettings, nil
}

// ExpandPath expands a leading ~ to the home directory and returns the
// absolute, cleaned path.
func ExpandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("folder is empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home folder: %w", err)
		}
		p = filepath.Join(home, p[1:])
	}
	return filepath.Abs(p)
}

// TildePath shortens a path under the home directory to ~/..., for display.
func TildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "~" + string(filepath.Separator) + rel
	}
	return p
}
