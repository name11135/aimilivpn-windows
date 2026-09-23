package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

func boundDialer(index uint32, dns string) *net.Dialer {
	d := newTunDialer(index)
	if dns == "" {
		dns = "1.1.1.1"
	}
	d.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		return newTunDialer(index).DialContext(ctx, "udp4", net.JoinHostPort(dns, "53"))
	}}
	return d
}
func (a *App) dial(ctx context.Context, network, address string) (net.Conn, error) {
	a.mu.RLock()
	index, dns, ready := a.index, a.dns, a.ready
	a.mu.RUnlock()
	if !ready || index == 0 {
		return nil, errors.New("VPN not ready; direct connection blocked")
	}
	c, e := boundDialer(index, dns).DialContext(ctx, "tcp4", address)
	if e != nil {
		return nil, e
	}
	a.mu.RLock()
	valid := a.ready && a.index == index
	a.mu.RUnlock()
	if !valid {
		c.Close()
		return nil, errors.New("VPN changed")
	}
	return c, nil
}
func (a *App) listenProxy(ln net.Listener) {
	slots := make(chan struct{}, 256)
	for {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		a.mu.Lock()
		a.connections[c] = struct{}{}
		a.mu.Unlock()
		go func() {
			defer func() { c.Close(); a.mu.Lock(); delete(a.connections, c); a.mu.Unlock(); <-slots }()
			a.proxyConn(c)
		}()
	}
}
func (a *App) proxyConn(c net.Conn) {
	c.SetDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(c)
	b, e := br.Peek(1)
	if e != nil {
		return
	}
	if b[0] == 5 {
		a.socks5(c, br)
		return
	}
	req, e := http.ReadRequest(br)
	if e != nil {
		return
	}
	if req.Body != nil {
		defer req.Body.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if req.Method == "CONNECT" {
		if _, _, e = net.SplitHostPort(req.Host); e != nil {
			proxyError(c, e)
			return
		}
		out, e := a.dial(ctx, "tcp4", req.Host)
		if e != nil {
			proxyError(c, e)
			return
		}
		defer out.Close()
		fmt.Fprint(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
		relay(c, out, br)
		return
	}
	if req.URL.Scheme != "http" || req.URL.Host == "" {
		proxyError(c, errors.New("absolute HTTP URL required"))
		return
	}
	req.RequestURI = ""
	req.Close = true
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")
	tr := &http.Transport{DialContext: a.dial, DisableKeepAlives: true, ResponseHeaderTimeout: 20 * time.Second}
	defer tr.CloseIdleConnections()
	c.SetDeadline(time.Now().Add(2 * time.Minute))
	resp, e := tr.RoundTrip(req)
	if e != nil {
		proxyError(c, e)
		return
	}
	defer resp.Body.Close()
	resp.Write(c)
}
func proxyError(c net.Conn, e error) {
	msg := "Proxy unavailable: " + e.Error() + "\n"
	fmt.Fprintf(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nConnection: close\r\nContent-Length: %d\r\n\r\n%s", len(msg), msg)
}
func (a *App) socks5(c net.Conn, br *bufio.Reader) {
	h := make([]byte, 2)
	if _, e := io.ReadFull(br, h); e != nil || h[0] != 5 || h[1] == 0 {
		return
	}
	methods := make([]byte, int(h[1]))
	if _, e := io.ReadFull(br, methods); e != nil {
		return
	}
	found := false
	for _, m := range methods {
		if m == 0 {
			found = true
		}
	}
	if !found {
		c.Write([]byte{5, 255})
		return
	}
	c.Write([]byte{5, 0})
	h = make([]byte, 4)
	if _, e := io.ReadFull(br, h); e != nil || h[0] != 5 || h[2] != 0 {
		return
	}
	fail := func(code byte) { c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
	if h[1] != 1 {
		fail(7)
		return
	}
	var host string
	switch h[3] {
	case 1:
		b := make([]byte, 4)
		if _, e := io.ReadFull(br, b); e != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		n, e := br.ReadByte()
		if e != nil || n == 0 {
			return
		}
		b := make([]byte, int(n))
		if _, e = io.ReadFull(br, b); e != nil {
			return
		}
		host = string(b)
	default:
		fail(8)
		return
	}
	p := make([]byte, 2)
	if _, e := io.ReadFull(br, p); e != nil {
		return
	}
	port := int(p[0])<<8 | int(p[1])
	if port == 0 {
		fail(1)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, e := a.dial(ctx, "tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
	if e != nil {
		fail(5)
		return
	}
	defer out.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	relay(c, out, br)
}
func relay(in, out net.Conn, r io.Reader) {
	in.SetDeadline(time.Time{})
	out.SetDeadline(time.Time{})
	done := make(chan struct{})
	go func() {
		io.Copy(out, r)
		if t, ok := out.(*net.TCPConn); ok {
			t.CloseWrite()
		}
		close(done)
	}()
	io.Copy(in, out)
	in.Close()
	out.Close()
	<-done
}

