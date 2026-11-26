package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
)

// expiredToken is a JWT-shaped token whose exp is an hour ago.
func expiredToken() string {
	enc := base64.RawURLEncoding.EncodeToString
	payload := fmt.Sprintf(`{"exp":%d}`, time.Now().Add(-time.Hour).Unix())
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(payload)) + ".sig"
}

// loginWithToken stores a session with token.
func loginWithToken(t *testing.T, token string) {
	t.Helper()
	setupTestEnv(t)
	if err := auth.NewAuthHandler().SaveLogin(auth.Session{IDToken: token, Email: "old@example.com"}); err != nil {
		t.Fatal(err)
	}
}

// answerPrompts makes commands interactive and feeds input to their prompts.
func answerPrompts(t *testing.T, input string) {
	t.Helper()
	orig := newPrinter
	t.Cleanup(func() { newPrinter = orig })
	newPrinter = func(cmd *cobra.Command) *ui.Printer {
		return ui.New(cmd.OutOrStdout(), strings.NewReader(input), ui.WithInteractive(true))
	}
}

// fakeSignIn replaces the browser sign-in. It returns the number of calls;
// before succeeding it runs onSuccess (e.g. to make the fake API accept the
// new token).
func fakeSignIn(t *testing.T, err error, onSuccess func()) *int {
	t.Helper()
	calls := 0
	orig := signInFn
	t.Cleanup(func() { signInFn = orig })
	signInFn = func(ctx context.Context, onOpen func(string, error)) (*auth.Session, error) {
		calls++
		onOpen("http://localhost:3000/login?state=x", nil)
		if err != nil {
			return nil, err
		}
		if onSuccess != nil {
			onSuccess()
		}
		return &auth.Session{IDToken: "test-token", Email: "user@example.com", Name: "User", AuthReferencePid: "a"}, nil
	}
	return &calls
}

func storedToken(t *testing.T) string {
	t.Helper()
	s, err := auth.NewAuthHandler().LoadLogin()
	if err != nil {
		t.Fatal(err)
	}
	return s.IDToken
}

func TestStart_ExpiredSession_NoTerminal(t *testing.T) {
	loginWithToken(t, expiredToken())
	calls := fakeSignIn(t, nil, nil)
	f := newFakeAPI(t)
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	assertContains(t, out, "! Your session expired", "✗ Session expired", "your saved login is no longer valid",
		"hint: run devcraft login, then run this command again", "FAIL", "Couldn't start assignment")
	if strings.Contains(out, "Sign in again now?") || *calls != 0 || f.downloadCalls != 0 {
		t.Fatalf("must not prompt, sign in or call the API without a terminal (calls=%d downloads=%d)\n%s", *calls, f.downloadCalls, out)
	}
	var stderr bytes.Buffer
	if code := HandleError(err, &stderr); code != exitFail || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q: want 1 and nothing printed twice", code, stderr.String())
	}
}

func TestStart_ExpiredSession_SignInAndContinue(t *testing.T) {
	loginWithToken(t, expiredToken())
	answerPrompts(t, "\n") // Enter means yes
	calls := fakeSignIn(t, nil, nil)
	f := newFakeAPI(t)
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	assertContains(t, out, "! Your session expired", "Sign in again now? [Y/n]",
		"✓ Opened your browser for Google sign-in", "✓ Signed in as user@example.com",
		"✓ Downloaded GO-SOLID-001 v1.2.0", "DONE", "cd GO-SOLID-001")
	if *calls != 1 || storedToken(t) != "test-token" {
		t.Fatalf("calls=%d token=%q: want one sign-in and the new session saved", *calls, storedToken(t))
	}
}

func TestStart_NotLoggedIn_SignInAndContinue(t *testing.T) {
	setupTestEnv(t)
	answerPrompts(t, "yes\n")
	fakeSignIn(t, nil, nil)
	f := newFakeAPI(t)
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	assertContains(t, out, "! You're not signed in", "✓ Signed in as user@example.com", "✓ Downloaded")
}

func TestStart_ExpiredSession_Declined(t *testing.T) {
	loginWithToken(t, expiredToken())
	answerPrompts(t, "n\n")
	calls := fakeSignIn(t, nil, nil)
	f := newFakeAPI(t)
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if !errors.Is(err, auth.ErrSessionExpired) || *calls != 0 {
		t.Fatalf("err=%v calls=%d", err, *calls)
	}
	assertContains(t, out, "Sign in again now? [Y/n]", "✗ Session expired", "then run this command again", "Couldn't start assignment")
}

func TestStart_SignInFails(t *testing.T) {
	for name, tc := range map[string]struct {
		err   error
		title string
	}{
		"timeout": {fmt.Errorf("login cancelled or timed out: %w", context.DeadlineExceeded), "✗ Sign-in timed out"},
		"error":   {errors.New("invalid state in login callback"), "✗ Sign-in failed"},
	} {
		t.Run(name, func(t *testing.T) {
			loginWithToken(t, expiredToken())
			answerPrompts(t, "y\n")
			fakeSignIn(t, tc.err, nil)
			f := newFakeAPI(t)
			chdirAssignments(t)

			out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
			if err == nil || HandleError(err, io.Discard) != exitFail {
				t.Fatalf("err = %v, want exit 1", err)
			}
			assertContains(t, out, "✓ Opened your browser", tc.title, "Couldn't start assignment")
			if f.downloadCalls != 0 {
				t.Fatal("API must not be called without a session")
			}
		})
	}
}

func TestStart_Unauthorized_SignInAndRetry(t *testing.T) {
	loginForTest(t) // a token without a readable expiry: only the API can reject it
	answerPrompts(t, "y\n")
	f := newFakeAPI(t)
	f.unauthorized = true
	calls := fakeSignIn(t, nil, func() {
		f.mu.Lock()
		f.unauthorized = false
		f.mu.Unlock()
	})
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	assertContains(t, out, "! Your session expired", "✓ Signed in as user@example.com", "✓ Downloaded")
	if *calls != 1 || f.requests != 2 || f.downloadCalls != 1 {
		t.Fatalf("calls=%d requests=%d downloads=%d: want one sign-in, then one successful retry", *calls, f.requests, f.downloadCalls)
	}
}

func TestStart_Unauthorized_NoTerminal(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.unauthorized = true
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	assertContains(t, out, "✗ Session expired", "Couldn't start assignment")
	if f.requests != 1 {
		t.Fatalf("requests = %d, want no retry", f.requests)
	}
}

func TestStart_MissingEndpoint(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	chdirAssignments(t)

	// a server without the download route answers with a bare 404
	out, err := runCLI(t, nil, "start", "GO-SOLID-001", "--no-git", "--api-url", f.srv.URL+"/old")
	if err == nil {
		t.Fatal("expected error")
	}
	assertContains(t, out, "✗ Not available yet", "update devcraft")
	assertNotContains(t, out, "API", "backend", "404")
}

func TestAssignments_ExpiredSession_SignsInInline(t *testing.T) {
	loginWithToken(t, expiredToken())
	answerPrompts(t, "y\n")
	fakeSignIn(t, nil, nil)
	f := newFakeAPI(t)

	out, err := runCLI(t, f, "assignments")
	if err != nil {
		t.Fatalf("assignments: %v\n%s", err, out)
	}
	assertContains(t, out, "✓ Signed in as user@example.com", "GO-SOLID-001")
}

func TestHandleError(t *testing.T) {
	var stderr bytes.Buffer
	if code := HandleError(nil, &stderr); code != exitOK {
		t.Errorf("nil: %d", code)
	}
	if code := HandleError(fmt.Errorf("download: %w", context.Canceled), &stderr); code != exitInterrupted || stderr.Len() != 0 {
		t.Errorf("Ctrl+C: code=%d stderr=%q, want 130 and quiet", code, stderr.String())
	}
	if code := HandleError(errors.New("boom"), &stderr); code != exitFail || stderr.String() != "boom\n" {
		t.Errorf("plain error: code=%d stderr=%q", code, stderr.String())
	}
}
