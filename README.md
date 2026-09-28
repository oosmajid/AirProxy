<div align="center">

![AirProxy](assets/banner.png)

# AirProxy

**A simple, beautiful macOS app that turns a V2Ray subscription or config link into a local SOCKS5 / HTTP proxy.**
*Only the chosen port is proxied — your whole system is **not** tunneled.*

[![Download](https://img.shields.io/badge/Download-.dmg-6C5CE7?style=for-the-badge&logo=apple&logoColor=white)](../../releases/latest)
[![Platform](https://img.shields.io/badge/macOS-11%2B-000000?style=for-the-badge&logo=apple&logoColor=white)](#install-macos)
[![Made with Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-2FB85F?style=for-the-badge)](LICENSE)

</div>

---

AirProxy embeds the [Xray-core](https://github.com/XTLS/Xray-core) engine, so it's a single self-contained app — no separate `xray` install needed. Built in Go with a [Fyne](https://fyne.io) UI.

## ✨ Features

| | |
|---|---|
| 🔌 **Local proxy only** | Runs SOCKS5 (and optional HTTP) on `127.0.0.1:10808`. The rest of the system stays direct. |
| 🔀 **Bypass / split-tunneling** | Route Iran (geo), or your own domains / IPs, **directly** instead of through the proxy. Iran bypass is on by default. |
| 📡 **Multiple subscriptions & links** | Add several sources, neatly grouped in the UI. |
| 🧭 **Protocols** | VMess · VLESS · Trojan · Shadowsocks · **Hysteria2** · **WireGuard** · **SOCKS** — transports **XHTTP** · WS · HTTPUpgrade · gRPC · RAW/TCP · mKCP, with TLS / **Reality**. |
| 🧩 **v2rayN-style core options** | **Fragment** (split TLS ClientHello), **ECH**, **Mux**, default uTLS fingerprint, TCP Fast Open, routing domain strategy — plus per-link `fm` (finalmask), `ech`, `pcs`, `vcn`, `spx`, `pqv`, xhttp `mode`/`extra`. |
| 🛡️ **Ignores a system VPN** | If another VPN (Happ, v2rayN TUN, …) is on, AirProxy binds to the physical interface so pings and connections reflect **your real network**, not the other VPN. |
| 🔐 **SSH tunnel** | Turn any SSH server into a proxy (dynamic port-forwarding) with password **or** private-key auth. |
| 📶 **Ping** | Per-config or per-group latency test with a live spinner. |
| ↕️ **Sort by ping** | Fastest servers first. |
| 🔁 **Auto-rotate** | If the active config drops, it automatically switches to another working one. |
| 💾 **Persistent** | Your sources, configs and settings survive restarts. |
| 🖱️ **Right-click a server** | Copy URL · Ping · Edit · Delete. |
| 🎨 **Polished UI** | Light, Happ-inspired theme · Vazirmatn font (Persian + Latin) · animated menus. |

## 📦 Install (macOS)

1. Download **`AirProxy.dmg`** from the [latest release](../../releases/latest).
2. Open it and drag **AirProxy** into **Applications**.
3. Launch it.

> The app is ad-hoc signed (no paid Apple certificate), so on first launch macOS may warn it's from an unidentified developer.
> **Right-click the app → Open**, or allow it in **System Settings → Privacy & Security**.

## 🚀 Usage

1. Tap **＋** and choose **Subscription / config link** or **SSH tunnel**.
2. Tap **Load** / it auto-loads — servers appear grouped by source.
3. Tap **🔍** on a group to ping it, then **⋯ → Sort by ping**.
4. Select a server and press the big **power button** to connect.

Then point your browser/app at the SOCKS5 proxy `127.0.0.1:10808`.

### 🔐 SSH tunnel

Tap **＋ → SSH tunnel** and fill in host / port / username, then either a **password** or a **private key** (paste it or load a key file, with an optional passphrase). AirProxy opens an SSH connection and routes the local proxy through it — the same as `ssh -D`, but built in. You can also paste a link directly:

```
ssh://user:password@host:port
```

> SSH tunnels carry **TCP** traffic only (no UDP), which covers normal web browsing.

### 🧩 Advanced (Fragment / ECH / Mux)

Open **⚙️ Settings → Advanced: Fragment / ECH / Mux…**:

- **Fragment** — splits the TLS ClientHello (`packets`: `tlshello` or `1-3`, `length`, `interval` ms) to get past DPI/SNI filtering. Applied via Xray *finalmask*, overriding any fragment carried in the link.
- **TLS / ECH** — default uTLS fingerprint for links without `fp`; a global **ECH config list** (base64, or a DNS query such as `cloudflare-ech.com+https://1.1.1.1/dns-query`) for TLS links without their own `ech`; and ECH force-query mode.
- **Mux**, **TCP Fast Open**, **routing domain strategy**.
- **Bypass system VPN** (on by default) — see below.

Settings apply to both **connect** and **ping**, so a ping measures exactly the path you'll use.

### 📶 Why pings are accurate

Each ping starts a throwaway Xray instance and requests `https://www.google.com/generate_204` **through the proxy** (HTTPS on purpose: on no-TLS configs, filtering can pass plain HTTP but kill every HTTPS site); only a genuine `204` counts (a fallback page or a filtering page does not). If another VPN is active on the Mac (default route on a `utun` interface), AirProxy binds its traffic to the physical interface (e.g. `en0`) — otherwise every config would look alive because it is silently reached **through the other VPN**.

### 🔀 Bypass (split-tunneling)

Open **⚙️ Settings → Bypass / split-tunneling…**. Toggle whole rule-sets — **Iran** (Iranian sites & IPs via bundled geo data), **Private / LAN**, **Block QUIC** — and/or add your own **domains** and **IPs / CIDRs** (one per line; `domain:`, `regexp:`, `geoip:` prefixes accepted). Matched destinations go **direct** (bypassing the proxy); everything else stays proxied. Iran bypass is enabled by default.

> Geo data (`geoip.dat` / `geosite.dat`, Iran-focused) is bundled in the app — no download needed. Maintainers can refresh it with `go run ./tools/gengeo assets/geo`.

## 🧪 Test the connection

```bash
curl --socks5-hostname 127.0.0.1:10808 https://api.ipify.org
```

If it returns the proxy server's IP, only that request went through the proxy.

## 🛠️ Build from source

Requires **Go 1.22+** and the Xcode Command Line Tools (C compiler).

```bash
git clone https://github.com/oosmajid/AirProxy.git
cd AirProxy
go build -o airproxy .
```

> Behind networks where `proxy.golang.org` is blocked, use a mirror:
> ```bash
> export GOPROXY="https://goproxy.io,https://goproxy.cn,direct"
> ```

Regenerate art assets (optional):

```bash
go run ./tools/genicon   icon.png
go run ./tools/genbanner icon.png assets/Vazirmatn-Bold.ttf assets/banner.png
```

## ⌨️ Command-line mode

The same binary also runs headless:

```bash
# single config
./airproxy --link "vless://…" --socks 10808

# from a subscription
./airproxy --sub "https://…/subscribe?token=…" --list                 # list configs
./airproxy --sub "https://…/subscribe?token=…" --index 2 --socks 10808
```

| Flag | Default | Description |
|------|---------|-------------|
| `--link` | — | a single config link |
| `--sub` | — | subscription URL |
| `--index` | `0` | which config from the subscription |
| `--listen` | `127.0.0.1` | listen IP |
| `--socks` | `10808` | SOCKS5 port |
| `--http` | `0` | HTTP port (0 = off) |

## 📄 License

[MIT](LICENSE). Bundles [Xray-core](https://github.com/XTLS/Xray-core) (MPL-2.0) and the [Vazirmatn](https://github.com/rastikerdar/vazirmatn) font (OFL).
