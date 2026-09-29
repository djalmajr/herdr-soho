//go:build !windows

package cli

import (
	"fmt"
	"syscall"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func execute(program string, args []string, env platform.Env) int {
	argv := append([]string{program}, args...)
	if err := syscall.Exec(program, argv, env.List()); err != nil {
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %v\n", err)
		return 127
	}
	return 0
}
