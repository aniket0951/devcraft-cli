package commands

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/config"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
)

// setupTestEnv mocks the keychain and isolates the config dir.
func setupTestEnv(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv(config.EnvAssignmentsDir, "") // a real one would leak into tests
}

// fakeBrowser replaces openBrowserFn with a function that plays the role of
// the frontend: it reads callback/state from the login URL and calls back
// with the query params produced by mkParams.
func fakeBrowser(t *testing.T, mkParams func(state string) url.Values) {
	t.Helper()
	orig := openBrowserFn
	t.Cleanup(func() { openBrowserFn = orig })

	openBrowserFn = func(loginURL string) error {
		u, err := url.Parse(loginURL)
		if err != nil {
			return err
		}
		q := u.Query()
		callback := q.Get("callback")
		state := q.Get("state")
		if callback == "" || state == "" {
			t.Errorf("login URL missing callback/state: %s", loginURL)
		}
		target := "http://" + callback + "/callback?" + mkParams(state).Encode()
		go func() {
			resp, err := http.Get(target)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
}

func validParams(state string) url.Values {
	return url.Values{
		"state":              {state},
		"status":             {"success"},
		"token":              {"id-token-123"},
		"auth_reference_pid": {"pid-456"},
		"email":              {"user@example.com"},
		"name":               {"Test User"},
	}
}

func newTestCmd() (*cobra.Command, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd, buf
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestBuildLoginURL(t *testing.T) {
	got, err := buildLoginURL("127.0.0.1:5555", "abc")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "localhost:3000" || u.Path != "/login" {
		t.Fatalf("unexpected base URL: %s", got)
	}
	if u.Query().Get("callback") != "127.0.0.1:5555" || u.Query().Get("state") != "abc" {
		t.Fatalf("unexpected query: %s", u.RawQuery)
	}
}

func TestRandomState(t *testing.T) {
	a, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := randomState()
	if len(a) != 32 {
		t.Fatalf("state length = %d, want 32", len(a))
	}
	if a == b {
		t.Fatal("two states should differ")
	}
}

func TestBrowserLogin_Success(t *testing.T) {
	fakeBrowser(t, validParams)
	cmd, _ := newTestCmd()

	got, err := browserLogin(testCtx(t), cmd)
	if err != nil {
		t.Fatalf("browserLogin: %v", err)
	}
	want := auth.Session{
		IDToken:          "id-token-123",
		AuthReferencePid: "pid-456",
		Email:            "user@example.com",
		Name:             "Test User",
	}
	if *got != want {
		t.Fatalf("session = %+v, want %+v", *got, want)
	}
}

func TestBrowserLogin_InvalidState(t *testing.T) {
	fakeBrowser(t, func(string) url.Values { return validParams("wrong-state") })
	cmd, _ := newTestCmd()

	_, err := browserLogin(testCtx(t), cmd)
	if err == nil || !strings.Contains(err.Error(), "invalid state") {
		t.Fatalf("err = %v, want invalid state", err)
	}
}

func TestBrowserLogin_MissingParams(t *testing.T) {
	for _, field := range []string{"status", "token", "auth_reference_pid", "email", "name"} {
		t.Run(field, func(t *testing.T) {
			fakeBrowser(t, func(state string) url.Values {
				p := validParams(state)
				p.Del(field)
				return p
			})
			cmd, _ := newTestCmd()

			_, err := browserLogin(testCtx(t), cmd)
			if err == nil || !strings.Contains(err.Error(), "required params") {
				t.Fatalf("err = %v, want required params error", err)
			}
		})
	}
}

func TestBrowserLogin_Timeout(t *testing.T) {
	orig := openBrowserFn
	t.Cleanup(func() { openBrowserFn = orig })
	openBrowserFn = func(string) error { return nil } // browser never calls back

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd, _ := newTestCmd()

	_, err := browserLogin(ctx, cmd)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestBrowserLogin_BrowserOpenFailsStillWorks(t *testing.T) {
	orig := openBrowserFn
	t.Cleanup(func() { openBrowserFn = orig })
	fakeBrowser(t, validParams)
	callback := openBrowserFn
	openBrowserFn = func(u string) error {
		_ = callback(u) // user opened the printed URL manually
		return errors.New("no browser")
	}
	setupTestEnv(t)
	out := &bytes.Buffer{}

	if _, err := signInInline(testCtx(t), ui.New(out, strings.NewReader(""))); err != nil {
		t.Fatalf("signInInline: %v", err)
	}
	if !strings.Contains(out.String(), "! Couldn't open your browser; open this link to sign in:") {
		t.Fatalf("expected browser warning in output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "http://localhost:3000/login") {
		t.Fatalf("expected login URL printed, got %q", out.String())
	}
}

func TestBrowserLogin_DuplicateCallbackDoesNotBlock(t *testing.T) {
	orig := openBrowserFn
	t.Cleanup(func() { openBrowserFn = orig })
	openBrowserFn = func(loginURL string) error {
		u, _ := url.Parse(loginURL)
		q := u.Query()
		target := "http://" + q.Get("callback") + "/callback?" + validParams(q.Get("state")).Encode()
		go func() {
			for range 3 {
				resp, err := http.Get(target)
				if err == nil {
					_ = resp.Body.Close()
				}
			}
		}()
		return nil
	}
	cmd, _ := newTestCmd()

	if _, err := browserLogin(testCtx(t), cmd); err != nil {
		t.Fatalf("browserLogin: %v", err)
	}
}

// End-to-end: run the real login command and confirm the session is stored.
func TestLoginCmd_StoresSession(t *testing.T) {
	setupTestEnv(t)
	fakeBrowser(t, validParams)

	cmd := loginCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(nil)
	cmd.SetContext(testCtx(t))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(out.String(), "✓ Signed in as user@example.com") || !strings.Contains(out.String(), "DONE") {
		t.Fatalf("unexpected output: %q", out.String())
	}
	if strings.Contains(out.String(), "id-token-123") {
		t.Fatal("token must not be printed")
	}

	got, err := auth.NewAuthHandler().LoadLogin()
	if err != nil {
		t.Fatalf("LoadLogin: %v", err)
	}
	if got.IDToken != "id-token-123" || got.Email != "user@example.com" {
		t.Fatalf("stored session = %+v", *got)
	}
}

func TestLoginCmd_FailureStoresNothing(t *testing.T) {
	setupTestEnv(t)
	fakeBrowser(t, func(string) url.Values { return validParams("wrong-state") })

	cmd := loginCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(testCtx(t))

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected login error")
	}
	if _, err := auth.NewAuthHandler().LoadLogin(); !errors.Is(err, auth.ErrNotLoggedIn) {
		t.Fatalf("LoadLogin err = %v, want ErrNotLoggedIn", err)
	}
}

// browserLogin runs the sign-in flow without printing anything.
func browserLogin(ctx context.Context, _ *cobra.Command) (*auth.Session, error) {
	return browserLoginWith(ctx, func(string, error) {})
}
