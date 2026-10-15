package main

import (
	"os"

	"github.com/djalmajr/herdr-soho/internal/cli"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], platform.EnvFromOS()))
}
