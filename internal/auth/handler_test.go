package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

var testSession = Session{
	IDToken:          "id-token-123",
	AuthReferencePid: "pid-456",
	Email:            "user@example.com",
	Name:             "Test User",
}

// isolateConfigDir points os.UserConfigDir at a temp dir so tests never
// touch the real credentials file.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	path, err := credentialsPath()
	if err != nil {
		t.Fatalf("credentialsPath: %v", err)
	}
	return path
}

func TestSaveAndLoad_Keyring(t *testing.T) {
	keyring.MockInit()
	credPath := isolateConfigDir(t)
	h := NewAuthHandler()

	if err := h.SaveLogin(testSession); err != nil {
		t.Fatalf("SaveLogin: %v", err)
	}

	// Stored in the keychain, not the fallback file.
	if _, err := keyring.Get(serviceName, accountName); err != nil {
		t.Fatalf("expected keychain entry, got %v", err)
	}
	if _, err := os.Stat(credPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no fallback file, stat err = %v", err)
	}

	got, err := h.LoadLogin()
	if err != nil {
		t.Fatalf("LoadLogin: %v", err)
	}
	if *got != testSession {
		t.Fatalf("LoadLogin = %+v, want %+v", *got, testSession)
	}
}

func TestSave_RemovesStaleFallbackFile(t *testing.T) {
	keyring.MockInit()
	credPath := isolateConfigDir(t)

	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, []byte(`{"email":"old@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := NewAuthHandler().SaveLogin(testSession); err != nil {
		t.Fatalf("SaveLogin: %v", err)
	}
	if _, err := os.Stat(credPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale fallback file removed, stat err = %v", err)
	}
}

func TestSaveAndLoad_FileFallback(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain unavailable"))
	credPath := isolateConfigDir(t)
	h := NewAuthHandler()

	if err := h.SaveLogin(testSession); err != nil {
		t.Fatalf("SaveLogin: %v", err)
	}

	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("expected fallback file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file perm = %o, want 600", perm)
		}
	}

	got, err := h.LoadLogin()
	if err != nil {
		t.Fatalf("LoadLogin: %v", err)
	}
	if *got != testSession {
		t.Fatalf("LoadLogin = %+v, want %+v", *got, testSession)
	}
}

func TestLoad_NotLoggedIn(t *testing.T) {
	keyring.MockInit()
	isolateConfigDir(t)

	_, err := NewAuthHandler().LoadLogin()
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("LoadLogin err = %v, want ErrNotLoggedIn", err)
	}
}

func TestLoad_CorruptData(t *testing.T) {
	keyring.MockInit()
	isolateConfigDir(t)

	if err := keyring.Set(serviceName, accountName, "not-json"); err != nil {
		t.Fatal(err)
	}
	_, err := NewAuthHandler().LoadLogin()
	if err == nil || errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("LoadLogin err = %v, want decode error", err)
	}
}

func TestDelete_RemovesKeyringAndFile(t *testing.T) {
	keyring.MockInit()
	credPath := isolateConfigDir(t)
	h := NewAuthHandler()

	if err := h.SaveLogin(testSession); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.DeleteLogin(); err != nil {
		t.Fatalf("DeleteLogin: %v", err)
	}
	if _, err := keyring.Get(serviceName, accountName); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("keychain entry still present, err = %v", err)
	}
	if _, err := os.Stat(credPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fallback file still present, stat err = %v", err)
	}
	if _, err := h.LoadLogin(); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("LoadLogin after delete err = %v, want ErrNotLoggedIn", err)
	}
}

func TestDelete_WhenNothingStored(t *testing.T) {
	keyring.MockInit()
	isolateConfigDir(t)

	if err := NewAuthHandler().DeleteLogin(); err != nil {
		t.Fatalf("DeleteLogin on empty store: %v", err)
	}
}
