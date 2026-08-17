//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
)

func replaceProcess(command string, args, environment []string, token string) error {
	environment = safeChildEnvironment(environment)
	if token != "" {
		reader, writer, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("create gateway credential pipe: %w", err)
		}
		if _, err := io.WriteString(writer, token); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			return fmt.Errorf("write gateway credential pipe: %w", err)
		}
		if err := writer.Close(); err != nil {
			_ = reader.Close()
			return fmt.Errorf("close gateway credential pipe: %w", err)
		}
		credentialFD, err := syscall.Dup(int(reader.Fd()))
		if err != nil {
			_ = reader.Close()
			return fmt.Errorf("duplicate gateway credential descriptor: %w", err)
		}
		_ = reader.Close()
		for index, arg := range args {
			if arg == gatewayTokenFDPlaceholder {
				args[index] = strconv.Itoa(credentialFD)
			}
		}
		environment = append(environment, fmt.Sprintf("%s=%d", gatewayTokenFDEnvironment, credentialFD))
		if err := syscall.Exec(command, append([]string{command}, args...), environment); err != nil {
			_ = syscall.Close(credentialFD)
			return err
		}
		return nil
	}
	return syscall.Exec(command, append([]string{command}, args...), environment)
}
