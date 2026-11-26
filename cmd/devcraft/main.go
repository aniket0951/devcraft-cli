package main

import (
	"os"

	"github.com/aniket0951/devcraft-cli.git/internal/commands"
)

func main() {
	err := commands.NewRootCmd().Execute()
	os.Exit(commands.HandleError(err, os.Stderr))
}
