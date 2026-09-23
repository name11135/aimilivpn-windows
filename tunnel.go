package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) connectAPI(w http.ResponseWriter, r *http.Request) {
	a.command.Lock()
	defer a.command.Unlock()
	if !isAdmin() {
		apiError(w, errors.New("鍒涘缓 Wintun 闇€瑕佺鐞嗗憳鏉冮檺锛岃杩愯 Start.cmd 骞跺厑璁?UAC"), 503)
		return
	}
	if _, e := openVPNPath(); e != nil {
		apiError(w, e, 503)
		return
	}
	if !wintunPresent() {
		apiError(w, errors.New("missing wintun.dll"), 503)
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
		apiError(w, e, 400)
		return
	}
	a.mu.RLock()
	pool := a.connectionPool(in.ID)
	a.mu.RUnlock()
	sort.SliceStable(pool, func(i, j int) bool {
		if (pool[i].VPNStatus == "passed") != (pool[j].VPNStatus == "passed") {
			return pool[i].VPNStatus == "passed"
		}
		return pool[i].Latency > 0 && (pool[j].Latency == 0 || pool[i].Latency < pool[j].Latency)
	})
	if in.ID != "" {
		found := -1
		for i, n := range pool {
			if n.ID == in.ID {
				found = i
				break
			}
		}
		if found < 0 {
			apiError(w, errors.New("鑺傜偣鏈€氳繃鍥哄畾瀹藉甫鍊欓€?杩為€氶绛涢€?), 400)
			return
		}
		pool[0], pool[found] = pool[found], pool[0]
	}
	if len(pool) == 0 {
		apiError(w, errors.New("鏆傛棤鍙繛鎺ョ殑鍥哄畾瀹藉甫鍊欓€夛紝璇峰埛鏂拌妭鐐?), 503)
		return
	}
	a.stop()
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	gen := a.generation
	a.status = "connecting"
	a.message = "姝ｅ湪寤虹珛闅ч亾"
	a.mu.Unlock()
	go a.runPool(ctx, gen, pool)
	reply(w, map[string]any{"ok": true})
}
func (a *App) runPool(ctx context.Context, generation uint64, pool []Node) {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	for i := 0; ctx.Err() == nil; i++ {
		n := pool[i%len(pool)]
		a.mu.Lock()
		if a.generation != generation {
			a.mu.Unlock()
			return
		}
		a.ready = false
		a.index = 0
		a.active = n.ID
		a.exitIP = ""
		a.status = "connecting"
		a.message = "姝ｅ湪杩炴帴 " + n.Country + " " + n.IP
		a.mu.Unlock()
		err := a.runTunnel(ctx, n, false)
		if ctx.Err() == nil {
			log.Printf("VPN %s: %v", n.ID, err)
		}
		a.mu.Lock()
		if a.generation != generation {
			a.mu.Unlock()
			return
		}
		a.ready = false
		a.index = 0
		a.exitIP = ""
		a.recordVPNLocked(n.ID, "failed", "", err.Error())
		if !a.settings.AutoSwitch {
			a.status = "idle"
			a.message = fmt.Sprintf("%s 杩炴帴澶辫触锛岃嚜鍔ㄥ垏鎹㈠凡鍏抽棴: %v", n.IP, err)
			a.active = ""
			a.mu.Unlock()
			a.closeConnections()
			return
		}
		a.status = "switching"
		a.message = fmt.Sprintf("%s 澶辫触锛屾鍦ㄦ崲绾? %v", n.IP, err)
		a.mu.Unlock()
		a.closeConnections()
		if (i+1)%len(pool) == 0 {
			a.mu.Lock()
			a.status = "waiting"
			a.message = "鏈疆鍊欓€夊叏閮ㄥけ璐ワ紝60 绉掑悗閲嶈瘯锛涘彲鍒锋柊鑺傜偣"
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			a.mu.RLock()
			newPool := a.connectionPool("")
			a.mu.RUnlock()
			if len(newPool) > 0 {
				pool = newPool
				i = -1
			}
		}
	}
}
func (a *App) runTunnel(ctx context.Context, n Node, verifyOnly bool) error {
	exe, err := openVPNPath()
	if err != nil {
		return err
	}
	if err = ensureTunAdapter(ctx, exe); err != nil {
		return err
	}
	clearTunRoutes()
	defer clearTunRoutes()
	// VPNGate's public VPN credentials are separate from the management login.
	if err = os.WriteFile(file("vpngate.auth"), []byte("vpn\nvpn\n"), 0600); err != nil {
		return err
	}
	// Only this interface receives a high-metric default route. The existing
	// physical default remains preferred; IP_UNICAST_IF chooses this route.
	config := n.Config + "\ndev tun\ndev-node AimiliVPN-TUN\nwindows-driver wintun\nroute-nopull\nroute 0.0.0.0 0.0.0.0 vpn_gateway 9999\nroute-metric 9999\nconnect-retry-max 1\nconnect-timeout 6\ntls-timeout 4\nhand-window 15\nverb 3\n"
	if err = os.WriteFile(file("active.ovpn"), []byte(config), 0600); err != nil {
		return err
	}
	logPath := file("logs/openvpn.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	child, kill := context.WithCancel(ctx)
	defer kill()
	cmd := exec.CommandContext(child, exe, "--config", file("active.ovpn"), "--auth-user-pass", file("vpngate.auth"), "--remote-cert-tls", "server", "--data-ciphers", "AES-128-CBC:AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305")
	hideWindow(cmd)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); logFile.Close(); close(done) }()
	defer func() {
		kill()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(28 * time.Second)
	defer deadline.Stop()
	var index uint32
	dns := "1.1.1.1"
wait:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return tunnelExitError(logPath, waitErr)
		case <-deadline.C:
			return errors.New("OpenVPN 鎻℃墜 28 绉掕秴鏃?)
		case <-ticker.C:
			b, _ := os.ReadFile(logPath)
			s := string(b)
			if strings.Contains(s, "Initialization Sequence Completed") {
				iface, e := net.InterfaceByName("AimiliVPN-TUN")
				if e != nil {
					continue
				}
				addresses, e := iface.Addrs()
				if e != nil || len(addresses) == 0 {
					continue
				}
				index = uint32(iface.Index)
				for _, piece := range strings.Split(s, ",") {
					f := strings.Fields(piece)
					if len(f) >= 3 && f[0] == "dhcp-option" && f[1] == "DNS" && net.ParseIP(f[2]) != nil {
						dns = f[2]
						break
					}
				}
				break wait
			}
		}
	}
	d := boundDialer(index, dns)
	tr := &http.Transport{Proxy: nil, DialContext: func(c context.Context, network, address string) (net.Conn, error) {
		return d.DialContext(c, "tcp4", address)
	}, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 7 * time.Second}
	ip, err := probeExitWhenReady(ctx, client)
	if err != nil {
		return fmt.Errorf("闅ч亾宸插缓绔嬶紝浣嗗嚭鍙ｆ祴璇曞け璐? %w", err)
	}
	m, err := classifyExit(ctx, client, ip)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if ctx.Err() != nil {
		a.mu.Unlock()
		return ctx.Err()
	}
	a.recordVPNLocked(n.ID, "passed", ip, "")
	if verifyOnly {
		a.mu.Unlock()
		return nil
	}
	a.index = index
	a.dns = dns
	a.ready = true
	a.exitIP = ip
	a.status = "connected"
	a.message = "鍑哄彛宸茶繛閫?路 " + m.ISP + " 路 鍥哄畾瀹藉甫鍊欓€夛紙鏃犳硶璇佹槑瀹跺涵鐢ㄩ€旓級"
	a.mu.Unlock()
	failures := 0
	a.mu.RLock()
	healthSeconds := a.settings.HealthSeconds
	a.mu.RUnlock()
	if healthSeconds == 0 {
		healthSeconds = 15
	}
	watch := time.NewTicker(time.Duration(healthSeconds) * time.Second)
	defer watch.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return tunnelExitError(logPath, waitErr)
		case <-watch.C:
			a.mu.RLock()
			healthSeconds = a.settings.HealthSeconds
			threshold := a.settings.FailureThreshold
			a.mu.RUnlock()
			if healthSeconds == 0 {
				healthSeconds = 15
			}
			if threshold == 0 {
				threshold = 2
			}
			watch.Reset(time.Duration(healthSeconds) * time.Second)
			healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			e := probeHealth(healthCtx, client)
			cancel()
			if e != nil {
				failures++
			} else {
				failures = 0
				a.mu.Lock()
				a.recordVPNLocked(n.ID, "passed", ip, "")
				a.mu.Unlock()
			}
			if failures >= threshold {
				return fmt.Errorf("杩炵画 %d 娆℃帰娲诲け璐?, threshold)
			}
		}
	}
}

// Windows address/route configuration can finish after OpenVPN reports init.
func probeExitWhenReady(ctx context.Context, client *http.Client) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ip, err := probeExit(probeCtx, client)
		cancel()
		if err == nil {
			return ip, nil
		}
		last = err
		if attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", last
}
func tunnelExitError(path string, err error) error {
	b, _ := os.ReadFile(path)
	s := string(b)
	for _, item := range []struct{ match, message string }{
		{"AUTH_FAILED", "鑺傜偣鎷掔粷 VPN 鐧诲綍锛圓UTH_FAILED锛夛紝宸插彂閫?VPNGate 鐨勫叕寮€璐﹀彿 vpn/vpn"},
		{"Bad encapsulated packet length", "绔彛杩斿洖浜嗛潪 OpenVPN 鏁版嵁鎴栬繛鎺ヨ涓棿璁惧閲嶇疆"},
		{"VERIFY ERROR", "鑺傜偣璇佷功鏍￠獙澶辫触"},
		{"Cannot open TUN/TAP", "鏃犳硶鎵撳紑 VPN 铏氭嫙缃戝崱锛岃妫€鏌ョ鐞嗗憳鏉冮檺鍜?Wintun"},
		{"TLS key negotiation failed", "鑺傜偣 TLS 鎻℃墜瓒呮椂"},
	} {
		if strings.Contains(s, item.match) {
			return errors.New(item.message)
		}
	}
	if err == nil {
		return errors.New("OpenVPN 鎻愬墠閫€鍑猴紝璇﹁ logs/openvpn.log")
	}
	return fmt.Errorf("OpenVPN 閫€鍑? %v锛涜瑙?logs/openvpn.log", err)
}
func probeExit(ctx context.Context, client *http.Client) (string, error) {
	var last error
	for _, u := range []string{"https://api.ipify.org", "https://api4.ipify.org"} {
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		r, e := client.Do(req)
		if e != nil {
			last = e
			continue
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 256))
		r.Body.Close()
		ip := strings.TrimSpace(string(b))
		if e == nil && r.StatusCode == 200 && net.ParseIP(ip) != nil {
			return ip, nil
		}
		last = errors.New("invalid exit IP response")
	}
	return "", last
}
func probeHealth(ctx context.Context, c *http.Client) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
	r, e := c.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 204 {
		return errors.New("probe HTTP " + strconv.Itoa(r.StatusCode))
	}
	return nil
}

