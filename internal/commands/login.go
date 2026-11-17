package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/constants"
	"github.com/spf13/cobra"
)

/*
*		this Login should call a api and generate the redirect URL
*		validate a user and keep a login session here
*		LoginSucess -> Show Success
*		LoginFaild  -> Show faild
*
* */

var redirectURL string = "http://localhost:3000/login"

type CallbackPayload struct {
	AuthCode string
	IDToken  string
}

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:        "Login",
		Aliases:    []string{"login"},
		SuggestFor: []string{"do devcraft login"},
		Short:      "Authentication for DevCraft",
		Example:    "devcraft login",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("Login...")

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return err
			}
			defer func() {
				_ = listener.Close()
			}()

			rURL, err := url.Parse(redirectURL)
			if err != nil {
				return err
			}

			if rURL != nil {
				q := url.Values{}
				q.Set("callback", listener.Addr().String())

				rURL.RawQuery = q.Encode()
				redirectURL = rURL.String()
			}

			codeCh := make(chan CallbackPayload)
			errCh := make(chan error)

			server := &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/callback" {
						return
					}
					code := r.URL.Query().Get("status")
					idToken := r.URL.Query().Get("token")
					cmd.Println("idToken from query params : ", idToken)
					if code == "" || idToken == "" {
						errCh <- fmt.Errorf("required params not in callback")
						http.Error(w, "Login failed", http.StatusBadRequest)
						return
					}
					_, _ = fmt.Fprintln(w, "Login successful! You can close this browser tab.")
					codeCh <- CallbackPayload{
						AuthCode: code,
						IDToken:  idToken,
					}
				}),
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			go func() {
				if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
					errCh <- err
				}
			}()
			defer func() {
				_ = server.Shutdown(ctx)
			}()

			if err := openBrowser(redirectURL); err != nil {
				return err
			}

			var authCode CallbackPayload
			select {
			case authCode = <-codeCh:
			case err := <-errCh:
				return err
			case <-time.After(2 * time.Minute):
				return fmt.Errorf("login timed out")
			case <-ctx.Done():
				return fmt.Errorf("login cancelled or timed out: %w", ctx.Err())
			}

			cmd.Println("authCode : ", authCode.AuthCode)
			cmd.Println("idToken : ", authCode.IDToken)

			return nil
		},
	}
}

func openBrowser(redirectURL string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case constants.SupportedOS.Darwin:
		cmd = exec.Command("open", redirectURL)
	case constants.SupportedOS.Linux:
		cmd = exec.Command("xdg-open", redirectURL)
	case constants.SupportedOS.Windows:
		cmd = exec.Command("rundll32", redirectURL)
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return cmd.Start()
}
