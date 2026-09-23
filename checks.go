package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type VerifyJob struct {
	Running   bool      `json:"running"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Passed    int       `json:"passed"`
	Current   string    `json:"current"`
	Cancelled bool      `json:"cancelled"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
}
type nodeSelection struct {
	ID  string   `json:"id"`
	IDs []string `json:"ids"`
}

// A nil selection means every broadband candidate, including all pages.
func (a *App) selectedNodes(ids []string) ([]Node, error) {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := []Node{}
	for _, n := range a.nodes {
		if n.Classification != "broadband-candidate" {
			continue
		}
		if len(ids) == 0 || wanted[n.ID] {
			out = append(out, n)
			delete(wanted, n.ID)
		}
	}
	if len(wanted) > 0 {
		return nil, errors.New("閮ㄥ垎鎵€閫夎妭鐐瑰凡绉婚櫎鎴栦笉鏄甯﹀€欓€夛紝璇锋洿鏂板垪琛?)
	}
	if len(out) == 0 {
		return nil, errors.New("娌℃湁鍙鏌ョ殑瀹藉甫鍊欓€?)
	}
	return out, nil
}
func probePort(n Node, timeout time.Duration) (string, int64, error) {
	if n.Protocol != "tcp" {
		return "udp-untested", 0, nil
	}
	started := time.Now()
	c, err := net.DialTimeout("tcp4", net.JoinHostPort(n.IP, fmt.Sprint(n.Port)), timeout)
	if err != nil {
		return "tcp-timeout", 0, err
	}
	c.Close()
	return "tcp-reachable", time.Since(started).Milliseconds() + 1, nil
}
func (a *App) checkAPI(w http.ResponseWriter, r *http.Request) {
	var in nodeSelection
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, err, 400)
		return
	}
	if in.ID != "" {
		a.mu.RLock()
		nodes, err := a.selectedNodes([]string{in.ID})
		timeout := a.settings.CheckTimeoutMS
		a.mu.RUnlock()
		if err != nil {
			apiError(w, err, 400)
			return
		}
		state, latency, err := probePort(nodes[0], time.Duration(timeout)*time.Millisecond)
		a.setNodeStatus(in.ID, state, latency)
		if err != nil {
			reply(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		reply(w, map[string]any{"ok": true, "latency": latency, "status": state})
		return
	}
	count, err := a.startPortChecks(in.IDs)
	if err != nil {
		apiError(w, err, 409)
		return
	}
	reply(w, map[string]any{"ok": true, "count": count})
}
func (a *App) startPortChecks(ids []string) (int, error) {
	a.mu.Lock()
	if a.checking {
		a.mu.Unlock()
		return 0, errors.New("绔彛妫€鏌ユ鍦ㄨ繘琛岋紝璇风瓑寰呭畬鎴?)
	}
	nodes, err := a.selectedNodes(ids)
	if err != nil {
		a.mu.Unlock()
		return 0, err
	}
	a.checking = true
	a.checkDone = 0
	a.checkTotal = len(nodes)
	timeout := time.Duration(a.settings.CheckTimeoutMS) * time.Millisecond
	a.mu.Unlock()
	go func() {
		jobs := make(chan Node)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := range jobs {
					state, latency, _ := probePort(n, timeout)
					a.setNodeStatus(n.ID, state, latency)
					a.mu.Lock()
					a.checkDone++
					a.mu.Unlock()
				}
			}()
		}
		for _, n := range nodes {
			jobs <- n
		}
		close(jobs)
		wg.Wait()
		a.mu.Lock()
		a.checking = false
		if len(ids) == 0 {
			a.lastChecked = time.Now()
		}
		a.mu.Unlock()
	}()
	return len(nodes), nil
}
func (a *App) checkAll() { _, _ = a.startPortChecks(nil) }
func (a *App) scheduleChecks() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastRefresh := time.Now()
	for {
		select {
		case <-a.shutdown:
			return
		case now := <-ticker.C:
			a.checkAll()
			a.mu.RLock()
			minutes := a.settings.RefreshMinutes
			a.mu.RUnlock()
			if now.Sub(lastRefresh) >= time.Duration(minutes)*time.Minute {
				lastRefresh = now
				go a.refresh()
			}
		}
	}
}

func (a *App) verifyAPI(w http.ResponseWriter, r *http.Request) {
	if !isAdmin() {
		apiError(w, errors.New("VPN 楠岃瘉闇€瑕佺鐞嗗憳鏉冮檺锛岃閫氳繃鍚姩鑴氭湰杩愯"), 503)
		return
	}
	var in nodeSelection
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, err, 400)
		return
	}
	if in.ID != "" {
		in.IDs = []string{in.ID}
	}
	if len(in.IDs) == 0 {
		apiError(w, errors.New("璇峰厛鍕鹃€夐渶瑕侀獙璇佺殑鑺傜偣"), 400)
		return
	}
	a.command.Lock()
	defer a.command.Unlock()
	a.mu.RLock()
	nodes, err := a.selectedNodes(in.IDs)
	running := a.verification.Running
	previous := a.active
	a.mu.RUnlock()
	if err != nil {
		apiError(w, err, 400)
		return
	}
	if running {
		apiError(w, errors.New("宸叉湁 VPN 楠岃瘉浠诲姟"), 409)
		return
	}
	a.stop()
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	a.verifyCancel = cancel
	gen := a.generation
	a.verification = VerifyJob{Running: true, Total: len(nodes), Started: time.Now()}
	a.status = "verifying"
	a.message = "閫愪釜楠岃瘉 VPN 鍑哄彛锛岀粨鏉熷悗鎭㈠鍏堝墠杩炴帴"
	a.mu.Unlock()
	go a.verifyNodes(ctx, gen, nodes, previous)
	reply(w, map[string]any{"ok": true, "count": len(nodes)})
}
func (a *App) cancelVerifyAPI(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.verifyCancel != nil {
		a.verifyCancel()
	}
	a.mu.Unlock()
	reply(w, map[string]any{"ok": true})
}
func (a *App) verifyNodes(ctx context.Context, gen uint64, nodes []Node, previous string) {
	a.lifecycle.Lock()
	for _, n := range nodes {
		if ctx.Err() != nil {
			break
		}
		a.mu.Lock()
		a.verification.Current = n.ID
		a.message = "姝ｅ湪楠岃瘉 " + n.Country + " " + n.IP
		a.mu.Unlock()
		err := a.runTunnel(ctx, n, true)
		if ctx.Err() != nil {
			break
		}
		a.mu.Lock()
		if err != nil {
			a.recordVPNLocked(n.ID, "failed", "", err.Error())
		}
		a.verification.Done++
		if err == nil {
			a.verification.Passed++
		}
		a.mu.Unlock()
	}
	a.lifecycle.Unlock()
	a.command.Lock()
	defer a.command.Unlock()
	a.mu.Lock()
	a.verification.Running = false
	a.verification.Current = ""
	a.verification.Finished = time.Now()
	a.verification.Cancelled = ctx.Err() != nil
	a.verifyCancel = nil
	if a.generation != gen {
		a.mu.Unlock()
		return
	}
	a.status = "idle"
	a.message = fmt.Sprintf("楠岃瘉缁撴潫锛氬凡妫€鏌?%d/%d锛屾垚鍔?%d", a.verification.Done, a.verification.Total, a.verification.Passed)
	a.cancel = nil
	pool := []Node{}
	if previous != "" {
		pool = a.connectionPool(previous)
		for i := range pool {
			if pool[i].ID == previous {
				pool[0], pool[i] = pool[i], pool[0]
				break
			}
		}
	}
	if len(pool) > 0 {
		restoreCtx, cancel := context.WithCancel(context.Background())
		a.cancel = cancel
		a.status = "connecting"
		a.message = "楠岃瘉缁撴潫锛屾鍦ㄦ仮澶嶅師杩炴帴"
		a.mu.Unlock()
		go a.runPool(restoreCtx, gen, pool)
		return
	}
	a.mu.Unlock()
}
func (a *App) recordVPNLocked(id, state, exit, message string) {
	for i := range a.nodes {
		if a.nodes[i].ID == id {
			a.nodes[i].VPNStatus = state
			a.nodes[i].VPNCheckedAt = time.Now()
			a.nodes[i].VPNError = message
			a.nodes[i].VerifiedExit = exit
			a.revision++
			return
		}
	}
}

