//go:build !windows

package main

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"syscall"
)

func newTunDialer(index uint32) *net.Dialer {
	return &net.Dialer{Control: func(n, a string, r syscall.RawConn) error { return errors.New("Windows required") }}
}
func openVPNPath() (string, error)                   { return "", errors.New("Windows required") }
func wintunPresent() bool                            { return false }
func isAdmin() bool                                  { return false }
func hideWindow(cmd *exec.Cmd)                       {}
func ensureTunAdapter(context.Context, string) error { return errors.New("Windows required") }
func clearTunRoutes()                                {}

