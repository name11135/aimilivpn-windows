//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func newTunDialer(index uint32) *net.Dialer {
	return &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, raw syscall.RawConn) error {
		if index == 0 {
			return errors.New("missing VPN interface")
		}
		var sockErr error
		err := raw.Control(func(fd uintptr) {
			v := index<<24 | (index&0xff00)<<8 | (index&0xff0000)>>8 | index>>24
			sockErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, 31, int(v))
		})
		if err != nil {
			return err
		}
		return sockErr
	}}
}
func openVPNPath() (string, error) {
	if p := os.Getenv("OPENVPN_EXE"); p != "" {
		return exec.LookPath(p)
	}
	for _, p := range []string{file("runtime/openvpn26/OpenVPN/bin/openvpn.exe"), file("runtime/openvpn/OpenVPN/bin/openvpn.exe"), file("openvpn.exe"), filepath.Join(os.Getenv("ProgramFiles"), "OpenVPN", "bin", "openvpn.exe")} {
		if _, e := os.Stat(p); e == nil {
			return p, nil
		}
	}
	return exec.LookPath("openvpn.exe")
}
func wintunPresent() bool {
	p, e := openVPNPath()
	if e != nil {
		return false
	}
	_, e = os.Stat(filepath.Join(filepath.Dir(p), "wintun.dll"))
	return e == nil
}
func isAdmin() bool {
	r, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin").Call()
	return r != 0
}
func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func ensureTunAdapter(ctx context.Context, openvpn string) error {
	if _, err := net.InterfaceByName("AimiliVPN-TUN"); err == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(openvpn), "tapctl.exe"), "create", "--hwid", "wintun", "--name", "AimiliVPN-TUN")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("鍒涘缓 Aimili 涓撶敤 Wintun 缃戝崱澶辫触: %w (%s)", err, out)
	}
	if _, err := net.InterfaceByName("AimiliVPN-TUN"); err != nil {
		return fmt.Errorf("涓撶敤 Wintun 缃戝崱鏈嚭鐜? %w (%s)", err, out)
	}
	return nil
}
func clearTunRoutes() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netsh.exe", "interface", "ipv4", "delete", "route", "prefix=0.0.0.0/0", "interface=AimiliVPN-TUN", "store=active")
	hideWindow(cmd)
	_ = cmd.Run()
}

