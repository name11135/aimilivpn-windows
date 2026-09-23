# AimiliVPN Windows

Native Windows VPN node manager built around OpenVPN 2.6 and Wintun. It provides node discovery, broadband candidate filtering, reachability checks, VPN exit verification, automatic failover, and a FlClash subscription endpoint.

## Features

- Merges VPNGate and mirror feeds; refreshes automatically every hour
- Country, ISP, latency, favorite, and candidate filters
- Batch TCP reachability checks and per-node VPN exit verification
- Automatic failover with previously verified nodes preferred
- Local HTTP and SOCKS5 proxy at `127.0.0.1:7928`
- FlClash subscription at `http://127.0.0.1:7929/clash`
- Management UI at `http://127.0.0.1:8686/`
- Service logs, OpenVPN logs, diagnostics, and runtime status

## Usage

1. Run `Start.cmd` as Administrator.
2. Open `http://127.0.0.1:8686/` and sign in with `config/Login.txt`.
3. Click Refresh to fetch nodes. Check only means that the TCP port is reachable; it does not prove that VPN or the exit works.
4. Use a node only after the UI reports a verified exit IP.
5. Import `http://127.0.0.1:7929/clash` into FlClash.
6. Run `Stop.cmd` to stop the service and its VPN child process.

## Data layout

- `config`: settings, favorites, and the management account
- `data`: feed data, IP metadata, and the complete node cache
- `runtime`: OpenVPN/Wintun files and temporary runtime state
- `logs`: service and OpenVPN logs

After every refresh, the merged node list is saved to `data/nodes-cache.json`. On the next start the app fetches fresh data; if the feeds are unavailable, it restores the cached list.

## Build

Install Go and run `Build.cmd` to produce `aimilivpn.exe`. The Windows runtime needs Administrator privileges to create the Wintun adapter.

## Notes

VPNGate nodes come from a public volunteer network and their availability changes over time. A reachable TCP port is not proof of a working VPN handshake or exit. Nodes without a verified exit are rejected by the proxy.

