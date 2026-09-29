//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func execute(program string, args []string, env platform.Env) int {
	cmd := exec.Command(program, args...)
	cmd.Env = env.List()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, platform.Stdout, platform.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() >= 0 {
				return exitErr.ExitCode()
			}
		}
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %v\n", err)
		return 127
	}
	return 0
}
