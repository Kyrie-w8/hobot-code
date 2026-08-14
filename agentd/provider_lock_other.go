//go:build windows

package main

import "fmt"

func lockProviderConfiguration(_ string) (func(), error) {
	return nil, fmt.Errorf("managed provider configuration is unsupported on Windows")
}
