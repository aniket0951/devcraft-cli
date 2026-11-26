package commands

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/constants"
	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
)

const (
	loginPageURL = "http://localhost:3000/login"
	loginTimeout = 5 * time.Minute
)

// openBrowserFn is a variable so tests can replace the real browser launch.
var openBrowserFn = openBrowser

type callbackResult struct {
	session auth.Session
	err     error
}

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:        "Login",
		Aliases:    []string{"login"},
		SuggestFor: []string{"do devcraft login"},
		Short:      "Authentication for DevCraft",
		Example:    "devcraft login",
		RunE: railRun("sign in", func(ctx context.Context, cmd *cobra.Command, p *ui.Printer, _ []string) error {
			p.Tag("AUTH", "Sign in to DevCraft")
			if _, err := signInInline(ctx, p); err != nil {
				return err
			}
			p.Done("devcraft assignments", ui.Dim("see what you can start"))
			return nil
		}),
	}
}

// browserLoginWith opens the sign-in page and waits for its callback.
// onOpen is called once, after trying to open the browser (openErr is
// non-nil if that failed), before waiting.
func browserLoginWith(ctx context.Context, onOpen func(loginURL string, openErr error)) (*auth.Session, error) {
	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start callback listener: %w", err)
	}

	loginURL, err := buildLoginURL(listener.Addr().String(), state)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	// Buffered with non-blocking sends: only the first callback is accepted,
	// later requests (retries, favicon, etc.) never block the handler.
	resultCh := make(chan callbackResult, 1)
	deliver := func(res callbackResult) {
		select {
		case resultCh <- res:
		default:
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Login failed: invalid state", http.StatusBadRequest)
			deliver(callbackResult{err: errors.New("invalid state in login callback")})
			return
		}

		session := auth.Session{
			IDToken:          q.Get("token"),
			AuthReferencePid: q.Get("auth_reference_pid"),
			Email:            q.Get("email"),
			Name:             q.Get("name"),
		}
		status := q.Get("status")
		if status == "" || session.IDToken == "" || session.AuthReferencePid == "" ||
			session.Email == "" || session.Name == "" {
			http.Error(w, "Login failed", http.StatusBadRequest)
			deliver(callbackResult{err: errors.New("required params not in callback")})
			return
		}

		_, _ = fmt.Fprintln(w, "Login successful! You can close this browser tab.")
		deliver(callbackResult{session: session})
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			deliver(callbackResult{err: fmt.Errorf("callback server: %w", err)})
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	onOpen(loginURL, openBrowserFn(loginURL))

	select {
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		return &res.session, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("login cancelled or timed out: %w", ctx.Err())
	}
}

func buildLoginURL(callbackAddr, state string) (string, error) {
	u, err := url.Parse(loginPageURL)
	if err != nil {
		return "", fmt.Errorf("parse login URL: %w", err)
	}
	q := u.Query()
	q.Set("callback", callbackAddr)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(target string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case constants.SupportedOS.Darwin:
		cmd = exec.Command("open", target)
	case constants.SupportedOS.Linux:
		cmd = exec.Command("xdg-open", target)
	case constants.SupportedOS.Windows:
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return cmd.Start()
}
