package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolate points the home and config dirs at a temp dir.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv(EnvAssignmentsDir, "")
	return home
}

func TestSettings_SaveAndLoad(t *testing.T) {
	isolate(t)

	s, err := LoadSettings()
	if err != nil || s.AssignmentsDir != "" {
		t.Fatalf("missing file: %+v, %v", s, err)
	}
	if err := SaveSettings(Settings{AssignmentsDir: "/work/devcraft"}); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSettings()
	if err != nil || s.AssignmentsDir != "/work/devcraft" {
		t.Fatalf("got %+v, %v", s, err)
	}

	path, _ := SettingsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestSettings_CorruptFile(t *testing.T) {
	isolate(t)
	path, _ := SettingsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := LoadSettings(); err == nil {
		t.Fatal("expected error for corrupt settings")
	}
}

func TestResolveAssignmentsDir_Precedence(t *testing.T) {
	home := isolate(t)

	dir, src, err := ResolveAssignmentsDir()
	if err != nil || dir != "" || src != SourceNone {
		t.Fatalf("unset: %q %v %v", dir, src, err)
	}

	_ = SaveSettings(Settings{AssignmentsDir: "/saved"})
	dir, src, _ = ResolveAssignmentsDir()
	if dir != "/saved" || src != SourceSettings {
		t.Fatalf("saved: %q %v", dir, src)
	}

	t.Setenv(EnvAssignmentsDir, "~/from-env")
	dir, src, _ = ResolveAssignmentsDir()
	if dir != filepath.Join(home, "from-env") || src != SourceEnv {
		t.Fatalf("env: %q %v", dir, src)
	}
}

func TestExpandPathAndTildePath(t *testing.T) {
	home := isolate(t)
	wd, _ := os.Getwd()

	for in, want := range map[string]string{
		"~":               home,
		"~/devcraft":      filepath.Join(home, "devcraft"),
		"  ~/a/../b  ":    filepath.Join(home, "b"),
		"relative/folder": filepath.Join(wd, "relative", "folder"),
	} {
		got, err := ExpandPath(in)
		if err != nil || got != want {
			t.Errorf("ExpandPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ExpandPath("  "); err == nil {
		t.Error("empty path should fail")
	}

	if got := TildePath(filepath.Join(home, "devcraft", "GO-1")); got != filepath.Join("~", "devcraft", "GO-1") {
		t.Errorf("TildePath = %q", got)
	}
	if got := TildePath("/elsewhere"); got != "/elsewhere" {
		t.Errorf("TildePath outside home = %q", got)
	}
	if got := TildePath(home + "x/y"); got != home+"x/y" {
		t.Errorf("sibling of home must not be shortened: %q", got)
	}
}
