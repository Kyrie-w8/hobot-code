//go:build !linux

package main

import (
	"net"
	"os"
	"syscall"
)

func verifyPeer(_ *net.UnixConn) error {
	// The containing directory is 0700 and the socket is 0600. Linux release
	// builds additionally enforce SO_PEERCRED in peer_linux.go.
	return nil
}

func fileOwner(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

func workerSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
