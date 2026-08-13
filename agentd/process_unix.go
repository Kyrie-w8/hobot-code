//go:build !windows

package main

import "syscall"

func replaceProcess(command string, args, environment []string) error {
	return syscall.Exec(command, append([]string{command}, args...), environment)
}
