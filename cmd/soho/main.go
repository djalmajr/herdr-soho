package main

import (
	"os"

	"github.com/djalmajr/soho/internal/cli"
	"github.com/djalmajr/soho/internal/platform"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], platform.EnvFromOS()))
}
