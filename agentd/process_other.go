//go:build windows

package main

import (
	"fmt"
	"runtime"
)

func replaceProcess(_ string, _, _ []string) error {
	return fmt.Errorf("interactive TUI process replacement is unsupported on %s", runtime.GOOS)
}
