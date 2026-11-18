package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/zalando/go-keyring"
)

func runLogout(t *testing.T) string {
	t.Helper()
	cmd := logoutCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logout: %v", err)
	}
	return out.String()
}

func TestLogout_NotLoggedIn(t *testing.T) {
	setupTestEnv(t)

	if out := runLogout(t); !strings.Contains(out, "You are not logged in.") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestLogout_RemovesSession(t *testing.T) {
	setupTestEnv(t)
	h := auth.NewAuthHandler()
	if err := h.SaveLogin(auth.Session{IDToken: "t", Email: "user@example.com"}); err != nil {
		t.Fatal(err)
	}

	if out := runLogout(t); !strings.Contains(out, "Logged out user@example.com") {
		t.Fatalf("unexpected output: %q", out)
	}
	if _, err := h.LoadLogin(); !errors.Is(err, auth.ErrNotLoggedIn) {
		t.Fatalf("LoadLogin err = %v, want ErrNotLoggedIn", err)
	}
}

func TestLogout_CorruptSessionStillCleared(t *testing.T) {
	setupTestEnv(t)
	if err := keyring.Set("devcraft-cli", "default-user", "not-json"); err != nil {
		t.Fatal(err)
	}

	if out := runLogout(t); !strings.Contains(out, "Logged out") {
		t.Fatalf("unexpected output: %q", out)
	}
	if _, err := auth.NewAuthHandler().LoadLogin(); !errors.Is(err, auth.ErrNotLoggedIn) {
		t.Fatalf("LoadLogin err = %v, want ErrNotLoggedIn", err)
	}
}
