package commands

import (
	"fmt"
	"os/exec"
	"runtime"

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

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:        "Login",
		Aliases:    []string{"login"},
		SuggestFor: []string{"do devcraft login"},
		Short:      "Authentication for DevCraft",
		Example:    "devcraft login",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("Login...")
			openBrowser("https://google.com")
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
