//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

func verifyPeer(connection *net.UnixConn) error {
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var credentials *syscall.Ucred
	var socketError error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketError = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if socketError != nil {
		return socketError
	}
	if credentials == nil || int(credentials.Uid) != os.Getuid() {
		return fmt.Errorf("agentd accepts only uid %d", os.Getuid())
	}
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
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}
