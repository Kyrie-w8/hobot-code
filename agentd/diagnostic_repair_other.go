//go:build !darwin && !linux

package main

import "fmt"

func chmodPrivatePathNoFollow(path string, directory bool) error {
	return fmt.Errorf("private path repair is unsupported on this platform")
}
