package main

import (
	"fmt"
	"os"

	"github.com/aniket0951/devcraft-cli.git/internal/commands"
)

func main() {
	rootCmd := commands.NewRootCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
