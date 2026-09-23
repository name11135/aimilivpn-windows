package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var directClient = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 30 * time.Second}

type IPInfo struct {
	Status  string    `json:"status"`
	Query   string    `json:"query"`
	ISP     string    `json:"isp"`
	Org     string    `json:"org"`
	Mobile  *bool     `json:"mobile"`
	Hosting *bool     `json:"hosting"`
	Country string    `json:"country"`
	Code    string    `json:"countryCode"`
	Checked time.Time `json:"checked"`
}

// nodeCacheEntry keeps the sanitized OpenVPN config, which Node intentionally
// omits from its public JSON representation.
type nodeCacheEntry struct {
	Node
	Config string `json:"config"`
}

func saveNodeCache(nodes []Node) {
	entries := make([]nodeCacheEntry, 0, len(nodes))
	for _, n := range nodes {
		entries = append(entries, nodeCacheEntry{Node: n, Config: n.Config})
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err == nil {
		_ = os.WriteFile(file("nodes-cache.json"), b, 0600)
	}
}

func loadNodeCache() map[string]Node {
	b, err := os.ReadFile(file("nodes-cache.json"))
	if err != nil {
		return nil
	}
	var entries []nodeCacheEntry
	if json.Unmarshal(b, &entries) != nil {
		return nil
	}
	result := make(map[string]Node, len(entries))
	for _, entry := range entries {
		entry.Node.Config = entry.Config
		if entry.ID != "" && entry.Config != "" {
			result[entry.ID] = entry.Node
		}
	}
	return result
}

func (m IPInfo) classification() string {
	if m.Status != "success" || m.Mobile == nil || m.Hosting == nil {
		return "unknown"
	}
	if *m.Hosting || *m.Mobile {
		return "excluded"
	}
	s := strings.ToLower(m.ISP + " " + m.Org)
	for _, bad := range []string{"university", "academic", "tsukuba", "softether", "hosting", "datacenter", "data center", "cloud", "amazon", "microsoft", "digitalocean", "ovh", "vultr", "research"} {
		if strings.Contains(s, bad) {
			return "excluded"
		}
	}
	return "broadband-candidate"
}
func parseNodes(data []byte) ([]Node, error) {
	rd := csv.NewReader(bytes.NewReader(data))
	rd.LazyQuotes = true
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, err
	}
	nodes := []Node{}
	seen := map[string]bool{}
	for _, r := range rows {
		if len(r) != 15 || strings.HasPrefix(r[0], "#") || strings.HasPrefix(r[0], "*") {
			continue
		}
		ip := net.ParseIP(r[1])
		if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			continue
		}
		raw, e := base64.StdEncoding.DecodeString(r[14])
		if e != nil {
			continue
		}
		config, host, port, proto, e := sanitizeConfig(string(raw))
		if e != nil || host != r[1] {
			continue
		}
		id := fmt.Sprintf("%s:%d/%s", host, port, proto)
		if seen[id] {
			continue
		}
		seen[id] = true
		nodes = append(nodes, Node{ID: id, Country: r[5], Code: r[6], IP: r[1], Port: port, Protocol: proto, Classification: "unknown", Status: "untested", Config: config})
	}
	if len(nodes) == 0 {
		return nil, errors.New("VPNGate 娌℃湁杩斿洖鏈夋晥鑺傜偣")
	}
	return nodes, nil
}

// Feed files are data: never allow downloaded configs to execute plugins/scripts
// or change host routes. Only the protocol options below are retained.
func sanitizeConfig(raw string) (config, host string, port int, proto string, err error) {
	var out strings.Builder
	block := ""
	proto = "tcp"
	port = 1194
	allowed := map[string]bool{"client": true, "nobind": true, "persist-key": true, "persist-tun": true, "cipher": true, "auth": true, "data-ciphers": true, "data-ciphers-fallback": true, "remote-cert-tls": true, "verify-x509-name": true, "key-direction": true, "tls-version-min": true, "tls-cipher": true}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if block != "" {
			out.WriteString(line + "\n")
			if line == "</"+block+">" {
				block = ""
			}
			continue
		}
		if strings.HasPrefix(line, "<") {
			b := strings.Trim(line, "<>")
			if b != "ca" && b != "cert" && b != "key" && b != "tls-auth" && b != "tls-crypt" {
				return "", "", 0, "", errors.New("unsupported inline block")
			}
			block = b
			out.WriteString(line + "\n")
			continue
		}
		f := strings.Fields(line)
		key := f[0]
		switch key {
		case "remote":
			if host != "" {
				continue
			}
			if len(f) < 3 || net.ParseIP(f[1]) == nil {
				return "", "", 0, "", errors.New("invalid VPN remote")
			}
			host = f[1]
			port, err = strconv.Atoi(f[2])
			if err != nil || port < 1 || port > 65535 {
				return "", "", 0, "", errors.New("invalid VPN port")
			}
		case "proto":
			if len(f) != 2 {
				return "", "", 0, "", errors.New("invalid protocol")
			}
			proto = strings.TrimSuffix(f[1], "-client")
			if proto != "tcp" && proto != "udp" {
				return "", "", 0, "", errors.New("unsupported protocol")
			}
		default:
			if allowed[key] {
				out.WriteString(line + "\n")
			}
		}
	}
	if host == "" || block != "" || !strings.Contains(raw, "<ca>") {
		return "", "", 0, "", errors.New("incomplete VPN config")
	}
	actual := proto
	if proto == "tcp" {
		actual = "tcp-client"
	}
	out.WriteString(fmt.Sprintf("remote %s %d\nproto %s\ndev-type tun\nresolv-retry 3\nauth-nocache\n", host, port, actual))
	return out.String(), host, port, proto, nil
}
func (a *App) refresh() {
	a.mu.Lock()
	if a.refreshing {
		a.mu.Unlock()
		return
	}
	a.refreshing = true
	timeout := time.Duration(a.settings.CheckTimeoutMS) * time.Millisecond
	countries := append([]string(nil), a.settings.DiscoveryCountries...)
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.refreshing = false; a.mu.Unlock() }()
	// Merge the same public feeds used by the original project. Feeds are
	// untrusted data; parse and sanitize every config before accepting it.
	feeds := []string{
		"https://www.vpngate.net/api/iphone/",
		"http://www.vpngate.net/api/iphone/",
		"https://baoweise-bot.github.io/aimili-vpngate/vpngate.csv",
		"https://raw.githubusercontent.com/baoweise-bot/aimili-vpngate/main/mirror/vpngate.csv",
	}
	type feedResult struct {
		data []byte
		err  error
	}
	results := make(chan feedResult, len(feeds))
	var fetchWG sync.WaitGroup
	for _, endpoint := range feeds {
		fetchWG.Add(1)
		go func(endpoint string) {
			defer fetchWG.Done()
			req, reqErr := http.NewRequest("GET", endpoint, nil)
			if reqErr != nil {
				results <- feedResult{err: reqErr}
				return
			}
			res, reqErr := directClient.Do(req)
			if reqErr != nil {
				results <- feedResult{err: reqErr}
				return
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				results <- feedResult{err: fmt.Errorf("%s: HTTP %d", endpoint, res.StatusCode)}
				return
			}
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 12<<20))
			results <- feedResult{data: body, err: readErr}
		}(endpoint)
	}
	fetchWG.Wait()
	close(results)
	merged := map[string]Node{}
	var primary []byte
	var lastErr error
	for result := range results {
		if result.err != nil {
			lastErr = result.err
			continue
		}
		parsed, parseErr := parseNodes(result.data)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		if len(primary) == 0 {
			primary = result.data
		}
		for _, node := range parsed {
			if _, exists := merged[node.ID]; !exists {
				merged[node.ID] = node
			}
		}
	}
	var data []byte
	if len(merged) == 0 {
		data, lastErr = os.ReadFile(file("source.csv"))
		if lastErr == nil {
			parsed, parseErr := parseNodes(data)
			if parseErr == nil {
				for _, node := range parsed {
					merged[node.ID] = node
				}
			} else {
				lastErr = parseErr
			}
		}
	}
	if len(merged) == 0 {
		for id, node := range loadNodeCache() {
			merged[id] = node
		}
	}
	if len(merged) == 0 {
		a.mu.Lock()
		a.message = "鑺傜偣鎷夊彇澶辫触: " + lastErr.Error()
		a.mu.Unlock()
		return
	}
	nodes := make([]Node, 0, len(merged))
	for _, node := range merged {
		nodes = append(nodes, node)
	}
	if len(countries) > 0 {
		allowed := map[string]bool{}
		for _, code := range countries {
			allowed[code] = true
		}
		filtered := []Node{}
		for _, n := range nodes {
			if allowed[n.Code] {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}
	if len(primary) > 0 {
		_ = os.WriteFile(file("source.csv"), primary, 0600)
	}
	meta := map[string]IPInfo{}
	if b, e := os.ReadFile(file("ip-info.json")); e == nil {
		json.Unmarshal(b, &meta)
	}
	missing := []string{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if time.Since(meta[n.IP].Checked) > 24*time.Hour && !seen[n.IP] {
			missing = append(missing, n.IP)
			seen[n.IP] = true
		}
	}
	for start := 0; start < len(missing); start += 100 {
		end := start + 100
		if end > len(missing) {
			end = len(missing)
		}
		b, _ := json.Marshal(missing[start:end])
		resp, e := directClient.Post("http://ip-api.com/batch?fields=status,query,country,countryCode,isp,org,mobile,hosting", "application/json", bytes.NewReader(b))
		if e != nil {
			break
		}
		entries := []IPInfo{}
		if resp.StatusCode == 200 {
			json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&entries)
		}
		resp.Body.Close()
		for _, m := range entries {
			if m.Status == "success" {
				m.Checked = time.Now()
				meta[m.Query] = m
			}
		}
		if end < len(missing) {
			time.Sleep(5 * time.Second)
		}
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(file("ip-info.json"), b, 0600)
	for i := range nodes {
		m := meta[nodes[i].IP]
		nodes[i].Classification = m.classification()
		nodes[i].ISP = m.ISP
		if m.Country != "" {
			nodes[i].Country = m.Country
			nodes[i].Code = m.Code
		}
		if nodes[i].Classification == "excluded" {
			nodes[i].Status = "excluded"
		}
	}
	// Bounded TCP preflight is only reachability, never a claim of a working VPN.
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				n := &nodes[j]
				if n.Classification != "broadband-candidate" {
					continue
				}
				n.CheckedAt = time.Now()
				if n.Protocol == "udp" {
					n.Status = "udp-untested"
					continue
				}
				start := time.Now()
				c, e := net.DialTimeout("tcp4", net.JoinHostPort(n.IP, strconv.Itoa(n.Port)), timeout)
				if e != nil {
					n.Status = "tcp-timeout"
				} else {
					c.Close()
					n.Status = "tcp-reachable"
					n.Latency = time.Since(start).Milliseconds() + 1
				}
			}
		}()
	}
	for i := range nodes {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	a.mu.Lock()
	previous := map[string]Node{}
	for _, n := range a.nodes {
		previous[n.ID] = n
	}
	// Keep previously classified candidates when a feed rotates them out.
	// The next refresh can replace their metadata, but a transient feed change
	// must not make the user's historical pool disappear.
	for id, old := range previous {
		if _, exists := merged[id]; exists || old.Classification != "broadband-candidate" || old.Config == "" {
			continue
		}
		nodes = append(nodes, old)
	}
	for i := range nodes {
		old := previous[nodes[i].ID]
		nodes[i].VPNStatus = old.VPNStatus
		nodes[i].VPNCheckedAt = old.VPNCheckedAt
		nodes[i].VPNError = old.VPNError
		nodes[i].VerifiedExit = old.VerifiedExit
		if old.CheckedAt.After(nodes[i].CheckedAt) {
			nodes[i].CheckedAt = old.CheckedAt
			nodes[i].Status = old.Status
			nodes[i].Latency = old.Latency
		}
	}
	a.nodes = nodes
	a.updated = time.Now()
	a.lastChecked = a.updated
	a.revision++
	saveNodeCache(nodes)
	a.mu.Unlock()
}
func classifyExit(ctx context.Context, c *http.Client, ip string) (IPInfo, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", "http://ip-api.com/json/"+ip+"?fields=status,query,country,countryCode,isp,org,mobile,hosting", nil)
	if e != nil {
		return IPInfo{}, e
	}
	res, e := c.Do(req)
	if e != nil {
		return IPInfo{}, e
	}
	defer res.Body.Close()
	var m IPInfo
	e = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&m)
	if e != nil {
		return m, e
	}
	if m.Query != ip || m.classification() != "broadband-candidate" {
		return m, errors.New("鍑哄彛鏃犳硶纭涓哄浐瀹氬甯﹀€欓€夛紝宸叉嫆缁濆惎鐢?)
	}
	return m, nil
}

