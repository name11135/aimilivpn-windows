package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed ui.html
var uiHTML string

//go:embed ui.js
var uiJS string

//go:embed ui.css
var uiCSS string

type Node struct {
	ID             string    `json:"id"`
	Country        string    `json:"country"`
	Code           string    `json:"code"`
	IP             string    `json:"ip"`
	Port           int       `json:"port"`
	Protocol       string    `json:"protocol"`
	ISP            string    `json:"isp"`
	Classification string    `json:"classification"`
	Status         string    `json:"status"`
	Latency        int64     `json:"latency"`
	Favorite       bool      `json:"favorite"`
	VPNError       string    `json:"vpnError,omitempty"`
	CheckedAt      time.Time `json:"checkedAt"`
	VPNCheckedAt   time.Time `json:"vpnCheckedAt"`
	VPNStatus      string    `json:"vpnStatus"`
	VerifiedExit   string    `json:"verifiedExit,omitempty"`
	Config         string    `json:"-"`
}
type App struct {
	mu                              sync.RWMutex
	lifecycle                       sync.Mutex
	command                         sync.Mutex
	nodes                           []Node
	status, message, active, exitIP string
	index                           uint32
	dns                             string
	ready                           bool
	refreshing                      bool
	checking                        bool
	lastChecked                     time.Time
	checkDone, checkTotal           int
	verification                    VerifyJob
	verifyCancel                    context.CancelFunc
	updated                         time.Time
	revision                        uint64
	cancel                          context.CancelFunc
	generation                      uint64
	token                           string
	shutdown                        chan struct{}
	connections                     map[net.Conn]struct{}
	favorites                       map[string]bool
	settings                        Settings
}

type Settings struct {
	ManagementPort     int      `json:"managementPort"`
	ProxyPort          int      `json:"proxyPort"`
	SubscriptionPort   int      `json:"subscriptionPort"`
	AutoSwitch         bool     `json:"autoSwitch"`
	CheckTimeoutMS     int      `json:"checkTimeoutMs"`
	RoutingMode        string   `json:"routingMode"`
	ForceCountry       string   `json:"forceCountry"`
	FixedNodeID        string   `json:"fixedNodeId"`
	DiscoveryCountries []string `json:"discoveryCountries"`
	RefreshMinutes     int      `json:"refreshMinutes"`
	HealthSeconds      int      `json:"healthSeconds"`
	FailureThreshold   int      `json:"failureThreshold"`
}

const managementPort = 8686

var base string

func file(name string) string {
	switch name {
	case "settings.json", "favorites.json", "Login.txt":
		return filepath.Join(base, "config", name)
	case "source.csv", "ip-info.json", "nodes-cache.json":
		return filepath.Join(base, "data", name)
	case "active.ovpn", "vpngate.auth", "service.pid", "control.token":
		return filepath.Join(base, "runtime", "state", name)
	default:
		return filepath.Join(base, name)
	}
}

func prepareLayout() error {
	for _, dir := range []string{"config", "data", "logs", filepath.Join("runtime", "state")} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0700); err != nil {
			return err
		}
	}
	for _, name := range []string{"settings.json", "favorites.json", "Login.txt", "source.csv", "ip-info.json", "active.ovpn", "vpngate.auth"} {
		oldPath, newPath := filepath.Join(base, name), file(name)
		if _, err := os.Stat(newPath); err == nil {
			continue
		}
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *App) loadState() {
	if b, err := os.ReadFile(file("settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
	a.settings.ManagementPort = managementPort
	a.settings.ProxyPort = 7928
	a.settings.SubscriptionPort = 7929
	if a.settings.CheckTimeoutMS < 500 || a.settings.CheckTimeoutMS > 10000 {
		a.settings.CheckTimeoutMS = 2500
	}
	a.settings.normalize()
	if b, err := os.ReadFile(file("favorites.json")); err == nil {
		_ = json.Unmarshal(b, &a.favorites)
	}
	if a.favorites == nil {
		a.favorites = map[string]bool{}
	}
}
func writeJSON(name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(name, b)
}
func writeAtomic(name string, data []byte) error {
	target := file(name)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".aimili-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}
func (a *App) setState(state, message string) {
	a.mu.Lock()
	a.status = state
	a.message = message
	a.mu.Unlock()
	log.Printf("%s: %s", state, message)
}
func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	base = filepath.Dir(exe)
	if v := os.Getenv("AIMILI_DATA"); v != "" {
		base = v
	}
	if err = os.MkdirAll(file("logs"), 0700); err != nil {
		log.Fatal(err)
	}
	if err = prepareLayout(); err != nil {
		log.Fatal(err)
	}
	out, err := os.OpenFile(file("logs/service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, out))
	a := &App{nodes: []Node{}, status: "idle", message: "璇烽€夋嫨瀹藉甫鍊欓€夊苟杩炴帴", shutdown: make(chan struct{}), connections: map[net.Conn]struct{}{}, favorites: map[string]bool{}, settings: Settings{ManagementPort: managementPort, ProxyPort: 7928, SubscriptionPort: 7929, AutoSwitch: true, CheckTimeoutMS: 2500}}
	a.loadState()
	if err = ensureCredentials(); err != nil {
		log.Fatal(err)
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		log.Fatal(err)
	}
	a.token = hex.EncodeToString(secret)
	// Bind all ports before reporting startup; never silently run a partial service.
	listeners := []net.Listener{}
	for _, addr := range []string{"127.0.0.1:7928", fmt.Sprintf("127.0.0.1:%d", managementPort), "127.0.0.1:7929"} {
		ln, e := net.Listen("tcp", addr)
		if e != nil {
			for _, l := range listeners {
				l.Close()
			}
			log.Fatalf("绔彛 %s 琚崰鐢? %v", addr, e)
		}
		listeners = append(listeners, ln)
	}
	os.WriteFile(file("service.pid"), []byte(fmt.Sprint(os.Getpid())), 0600)
	os.WriteFile(file("control.token"), []byte(a.token), 0600)
	defer os.Remove(file("service.pid"))
	defer os.Remove(file("control.token"))
	mux := http.NewServeMux()
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		io.WriteString(w, uiJS)
	})
	mux.HandleFunc("/app.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		io.WriteString(w, uiCSS)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := strings.Replace(uiHTML, "</head>", "<link rel=\"stylesheet\" href=\"/app.css\">\n</head>", 1)
		io.WriteString(w, page)
	})
	mux.HandleFunc("/api/status", a.statusAPI)
	mux.HandleFunc("/api/logs", a.logsAPI)
	mux.HandleFunc("/api/login", a.loginAPI)
	mux.HandleFunc("/api/settings", a.mutation(a.settingsAPI))
	mux.HandleFunc("/api/account", a.mutation(a.accountAPI))
	mux.HandleFunc("/api/favorite", a.mutation(a.favoriteAPI))
	mux.HandleFunc("/api/check", a.mutation(a.checkAPI))
	mux.HandleFunc("/api/verify", a.mutation(a.verifyAPI))
	mux.HandleFunc("/api/verify/cancel", a.mutation(a.cancelVerifyAPI))
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		out := make([]Node, len(a.nodes))
		copy(out, a.nodes)
		for i := range out {
			out[i].Favorite = a.favorites[out[i].ID]
		}
		reply(w, map[string]any{"nodes": out, "revision": a.revision})
	})
	mux.HandleFunc("/api/refresh", a.mutation(func(w http.ResponseWriter, r *http.Request) { go a.refresh(); reply(w, map[string]any{"ok": true}) }))
	mux.HandleFunc("/api/connect", a.mutation(a.connectAPI))
	mux.HandleFunc("/api/disconnect", a.mutation(func(w http.ResponseWriter, r *http.Request) {
		a.command.Lock()
		defer a.command.Unlock()
		a.stop()
		reply(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc("/api/shutdown", a.mutation(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"ok": true})
		select {
		case <-a.shutdown:
		default:
			close(a.shutdown)
		}
	}))
	mux.HandleFunc("/clash", a.clash)
	restricted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			http.Error(w, "Invalid host", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/login" && r.Header.Get("X-Aimili-Token") != a.token {
			apiError(w, errors.New("璇峰厛鐧诲綍"), http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
	sub := http.NewServeMux()
	sub.HandleFunc("/clash", a.clash)
	servers := []*http.Server{{Handler: restricted, ReadHeaderTimeout: 5 * time.Second}, {Handler: sub, ReadHeaderTimeout: 5 * time.Second}}
	go a.listenProxy(listeners[0])
	for i, s := range servers {
		go s.Serve(listeners[i+1])
	}
	go a.refresh()
	go a.scheduleChecks()
	log.Printf("Windows native service ready: http://127.0.0.1:%d/", managementPort)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	select {
	case <-signals:
		select {
		case <-a.shutdown:
		default:
			close(a.shutdown)
		}
	case <-a.shutdown:
	}
	a.stop()
	a.lifecycle.Lock()
	a.lifecycle.Unlock()
	listeners[0].Close()
	for _, s := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		if err := s.Shutdown(ctx); err != nil {
			s.Close()
		}
		cancel()
	}
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, e error, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": e.Error()})
}
func (a *App) mutation(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			apiError(w, errors.New("POST required"), 405)
			return
		}
		if r.Header.Get("X-Aimili-Token") != a.token {
			apiError(w, errors.New("invalid local control token"), 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				apiError(w, errors.New("invalid origin"), 403)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		fn(w, r)
	}
}
func (a *App) statusAPI(w http.ResponseWriter, r *http.Request) {
	exe, _ := openVPNPath()
	a.mu.RLock()
	defer a.mu.RUnlock()
	candidates, reachable := 0, 0
	for _, n := range a.nodes {
		if n.Classification == "broadband-candidate" {
			candidates++
		}
		if n.Latency > 0 {
			reachable++
		}
	}
	favs := len(a.favorites)
	user, _ := legacyCredentials()
	// Status-check timestamps are independent from downloading the node feed.
	reply(w, map[string]any{"status": a.status, "message": a.message, "ready": a.ready, "active": a.active, "exitIP": a.exitIP, "interfaceIndex": a.index, "nodes": len(a.nodes), "candidates": candidates, "reachable": reachable, "favorites": favs, "refreshing": a.refreshing, "checking": a.checking, "updated": a.updated, "revision": a.revision, "openvpn": exe, "wintun": wintunPresent(), "administrator": isAdmin(), "username": user, "proxy": "127.0.0.1:7928", "management": "http://127.0.0.1:8686/", "subscription": "http://127.0.0.1:7929/clash", "settings": a.settings, "lastChecked": a.lastChecked, "checkDone": a.checkDone, "checkTotal": a.checkTotal, "statusIntervalSeconds": 60, "verification": a.verification})
}

func tailLines(path string, limit int) []string {
	if limit < 1 {
		limit = 200
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func (a *App) logsAPI(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	status := a.status
	message := a.message
	active := a.active
	ready := a.ready
	a.mu.RUnlock()
	reply(w, map[string]any{
		"status":  status,
		"message": message,
		"active":  active,
		"ready":   ready,
		"service": tailLines(file("logs/service.log"), 160),
		"openvpn": tailLines(file("logs/openvpn.log"), 160),
	})
}

func (a *App) loginAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		apiError(w, errors.New("POST required"), 405)
		return
	}
	var in struct{ Username, Password string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		apiError(w, err, 400)
		return
	}
	a.mu.RLock()
	user, pass := legacyCredentials()
	a.mu.RUnlock()
	if pass == "" || in.Username != user || in.Password != pass {
		apiError(w, errors.New("鐢ㄦ埛鍚嶆垨瀵嗙爜閿欒"), 401)
		return
	}
	reply(w, map[string]any{"ok": true, "token": a.token})
}
func legacyCredentials() (string, string) {
	b, err := os.ReadFile(file("Login.txt"))
	if err != nil {
		b, err = os.ReadFile(`E:\AimiliVPN\Login.txt`)
	}
	if err != nil {
		return "admin", ""
	}
	lines := strings.Split(string(b), "\n")
	user, pass := "admin", ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Username:") {
			user = strings.TrimSpace(strings.TrimPrefix(line, "Username:"))
		}
		if strings.HasPrefix(line, "Password:") {
			pass = strings.TrimSpace(strings.TrimPrefix(line, "Password:"))
		}
	}
	return user, pass
}
func credentialsText(user, pass string) []byte {
	return []byte("Management URL: http://127.0.0.1:8686/\nUsername: " + user + "\nPassword: " + pass + "\n")
}
func ensureCredentials() error {
	user, pass := legacyCredentials()
	if pass != "" {
		if _, err := os.Stat(file("Login.txt")); err == nil {
			return nil
		}
	}
	if pass == "" {
		b := make([]byte, 18)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		pass = hex.EncodeToString(b)
	}
	return writeAtomic("Login.txt", credentialsText(user, pass))
}
func (a *App) accountAPI(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, CurrentPassword, NewPassword string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, err, 400)
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || len(in.Username) > 64 || len(in.NewPassword) < 8 || len(in.NewPassword) > 128 || strings.ContainsAny(in.Username+in.NewPassword, "\r\n") {
		apiError(w, errors.New("鐢ㄦ埛鍚嶄笉鑳戒负绌猴紝瀵嗙爜闀垮害搴斾负 8鈥?28 浣嶏紝涓嶈兘鍖呭惈鎹㈣"), 400)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, pass := legacyCredentials()
	if pass == "" || in.CurrentPassword != pass {
		apiError(w, errors.New("褰撳墠瀵嗙爜涓嶆纭?), 403)
		return
	}
	if err := writeAtomic("Login.txt", credentialsText(in.Username, in.NewPassword)); err != nil {
		apiError(w, err, 500)
		return
	}
	reply(w, map[string]any{"ok": true})
}
func (a *App) settingsAPI(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	in := a.settings
	in.DiscoveryCountries = append([]string(nil), in.DiscoveryCountries...)
	a.mu.RUnlock()
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, err, 400)
		return
	}
	if err := in.validate(); err != nil {
		apiError(w, err, 400)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	previous := a.settings
	in.ManagementPort, in.ProxyPort, in.SubscriptionPort = managementPort, 7928, 7929
	a.settings = in
	if err := writeJSON("settings.json", a.settings); err != nil {
		a.settings = previous
		apiError(w, err, 500)
		return
	}
	reply(w, map[string]any{"ok": true, "settings": a.settings, "lastChecked": a.lastChecked, "checkDone": a.checkDone, "checkTotal": a.checkTotal, "statusIntervalSeconds": 60, "verification": a.verification})
}
func (a *App) favoriteAPI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Favorite *bool  `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == "" {
		apiError(w, errors.New("invalid node id"), 400)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for _, n := range a.nodes {
		if n.ID == in.ID {
			found = true
			break
		}
	}
	if !found {
		apiError(w, errors.New("node not found"), 404)
		return
	}
	previous := a.favorites[in.ID]
	if in.Favorite == nil {
		if previous {
			delete(a.favorites, in.ID)
		} else {
			a.favorites[in.ID] = true
		}
	} else if *in.Favorite {
		a.favorites[in.ID] = true
	} else {
		delete(a.favorites, in.ID)
	}
	value := a.favorites[in.ID]
	if err := writeJSON("favorites.json", a.favorites); err != nil {
		if previous {
			a.favorites[in.ID] = true
		} else {
			delete(a.favorites, in.ID)
		}
		apiError(w, err, 500)
		return
	}
	a.revision++
	reply(w, map[string]any{"ok": true, "id": in.ID, "favorite": value})
}
func (a *App) setNodeStatus(id, status string, latency int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.nodes {
		if a.nodes[i].ID == id {
			a.nodes[i].Status = status
			a.nodes[i].Latency = latency
			a.nodes[i].CheckedAt = time.Now()
			a.revision++
			return
		}
	}
}
func (a *App) clash(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	io.WriteString(w, "proxies:\n  - name: AimiliVPN-Live\n    type: socks5\n    server: 127.0.0.1\n    port: 7928\n    udp: false\nproxy-groups:\n  - name: AimiliVPN\n    type: select\n    proxies: [AimiliVPN-Live]\nrules:\n  - MATCH,AimiliVPN\n")
}
func (a *App) stop() {
	a.mu.Lock()
	a.generation++
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.ready = false
	a.index = 0
	a.active = ""
	a.exitIP = ""
	a.status = "idle"
	a.message = "宸叉柇寮€锛屼唬鐞嗙鍙ｄ繚鎸佺洃鍚?
	a.mu.Unlock()
	a.closeConnections()
}
func (a *App) closeConnections() {
	a.mu.Lock()
	for c := range a.connections {
		c.Close()
	}
	a.mu.Unlock()
}

